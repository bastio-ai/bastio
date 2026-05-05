package workspace

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"time"
)

// EmbeddingClient turns text chunks into dense vectors used for RAG
// retrieval. The interface is intentionally narrow — one method, one
// concept — so cloud + tests can swap implementations without touching
// the workspace handler.
type EmbeddingClient interface {
	// Embed returns one vector per input. Order matches input order.
	// Embedding dimensionality is fixed by the underlying model and
	// must stay consistent for the lifetime of a customer's KB —
	// chunks embedded with a different dimension are not searchable
	// against newer queries until re-indexed.
	Embed(ctx context.Context, inputs []string) ([][]float32, error)
	Dimension() int
}

// OpenAIEmbeddingClient calls /v1/embeddings on api.openai.com (or a
// compatible endpoint). The same workspace KeyResolver / env-var
// fallback chain that powers chat resolves the API key here.
type OpenAIEmbeddingClient struct {
	BaseURL string
	Model   string
	Dim     int
	APIKey  string
	HTTP    *http.Client
}

// NewOpenAIEmbeddingClient builds a client with `text-embedding-3-small`
// defaults (1536-dim). Override Model + Dim for `-3-large` (3072) or
// `-ada-002` (legacy 1536).
func NewOpenAIEmbeddingClient(apiKey string) *OpenAIEmbeddingClient {
	return &OpenAIEmbeddingClient{
		BaseURL: "https://api.openai.com",
		Model:   "text-embedding-3-small",
		Dim:     1536,
		APIKey:  apiKey,
		HTTP:    &http.Client{Timeout: 60 * time.Second},
	}
}

func (c *OpenAIEmbeddingClient) Dimension() int { return c.Dim }

func (c *OpenAIEmbeddingClient) Embed(ctx context.Context, inputs []string) ([][]float32, error) {
	if len(inputs) == 0 {
		return nil, nil
	}
	if c.APIKey == "" {
		return nil, fmt.Errorf("openai embeddings: no API key configured")
	}

	// OpenAI accepts up to 2048 inputs per request, but per-input
	// token limits make 100 a saner ceiling for ~1KB chunks. Larger
	// KBs split into multiple round-trips.
	const batchSize = 100
	out := make([][]float32, 0, len(inputs))
	for i := 0; i < len(inputs); i += batchSize {
		end := min(i+batchSize, len(inputs))
		vs, err := c.embedBatch(ctx, inputs[i:end])
		if err != nil {
			return nil, err
		}
		out = append(out, vs...)
	}
	return out, nil
}

func (c *OpenAIEmbeddingClient) embedBatch(ctx context.Context, inputs []string) ([][]float32, error) {
	body, err := json.Marshal(map[string]any{
		"model": c.Model,
		"input": inputs,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.BaseURL+"/v1/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embeddings http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode/100 != 2 {
		buf, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("embeddings status %d: %s", resp.StatusCode, string(buf))
	}

	var parsed struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
			Index     int       `json:"index"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	if len(parsed.Data) != len(inputs) {
		return nil, fmt.Errorf("embeddings count mismatch: got %d, want %d",
			len(parsed.Data), len(inputs))
	}

	// API guarantees `index` matches request order, but sort defensively
	// in case a future provider reorders the response.
	out := make([][]float32, len(inputs))
	for _, item := range parsed.Data {
		if item.Index < 0 || item.Index >= len(out) {
			continue
		}
		out[item.Index] = item.Embedding
	}
	return out, nil
}

// CosineSimilarity returns dot(a,b) / (|a| * |b|). Defined separately
// from any embedding client so the retrieval path can score arbitrary
// vector pairs (and tests can verify the math without an HTTP fixture).
// Returns 0 when either vector is zero-length or zero-magnitude — the
// safest behavior for a "not relevant" signal.
func CosineSimilarity(a, b []float32) float32 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		af, bf := float64(a[i]), float64(b[i])
		dot += af * bf
		na += af * af
		nb += bf * bf
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return float32(dot / (math.Sqrt(na) * math.Sqrt(nb)))
}
