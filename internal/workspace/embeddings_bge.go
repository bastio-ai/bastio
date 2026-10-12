package workspace

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// BGEEmbeddingClient uses an operator-configured, self-hosted BGE-M3 service.
// ModelID includes the deployment revision so vectors from different models
// cannot be compared. The same client is used for documents and search queries.
type BGEEmbeddingClient struct {
	endpoint string
	modelID  string
	http     *http.Client
}

func NewBGEEmbeddingClient(base, modelID string, client *http.Client) (*BGEEmbeddingClient, error) {
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, fmt.Errorf("invalid bge embedding endpoint")
	}
	if modelID == "" || len(modelID) > 200 {
		return nil, fmt.Errorf("bge model identity is required")
	}
	if client == nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.Proxy = nil
		client = &http.Client{Transport: transport, Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	return &BGEEmbeddingClient{endpoint: strings.TrimRight(base, "/") + "/embed", modelID: modelID, http: client}, nil
}
func (c *BGEEmbeddingClient) Dimension() int  { return 1024 }
func (c *BGEEmbeddingClient) ModelID() string { return c.modelID }
func (c *BGEEmbeddingClient) Embed(ctx context.Context, inputs []string) ([][]float32, error) {
	out := make([][]float32, 0, len(inputs))
	// Bound GPU contention with the other services sharing this deployment.
	for start := 0; start < len(inputs); start += 16 {
		end := min(start+16, len(inputs))
		body, err := json.Marshal(struct {
			Inputs []string `json:"inputs"`
			Dense  bool     `json:"return_dense"`
			Sparse bool     `json:"return_sparse"`
		}{inputs[start:end], true, false})
		if err != nil {
			return nil, fmt.Errorf("encode BGE request: %w", err)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("create BGE request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		res, err := c.http.Do(req)
		if err != nil {
			return nil, fmt.Errorf("bge embedding service unavailable")
		}
		var rows []struct {
			Dense []float32 `json:"dense"`
		}
		if res.StatusCode != http.StatusOK {
			res.Body.Close()
			return nil, fmt.Errorf("bge embedding service returned HTTP %d", res.StatusCode)
		}
		err = json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(&rows)
		res.Body.Close()
		if err != nil || len(rows) != end-start {
			return nil, fmt.Errorf("invalid bge embedding response")
		}
		for _, row := range rows {
			if !validEmbedding(row.Dense, c.Dimension()) {
				return nil, fmt.Errorf("invalid bge embedding vector")
			}
			out = append(out, row.Dense)
		}
	}
	return out, nil
}

func validEmbedding(v []float32, dimension int) bool {
	if len(v) != dimension {
		return false
	}
	var norm float64
	for _, x := range v {
		if math.IsNaN(float64(x)) || math.IsInf(float64(x), 0) {
			return false
		}
		norm += float64(x) * float64(x)
	}
	return norm > 0
}

func allValidEmbeddings(vectors [][]float32, dimension int) bool {
	for _, v := range vectors {
		if !validEmbedding(v, dimension) {
			return false
		}
	}
	return true
}
func embeddingModel(client EmbeddingClient) string {
	if client == nil {
		return ""
	}
	return client.ModelID()
}
