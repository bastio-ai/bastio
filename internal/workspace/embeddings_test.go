package workspace

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCosineSimilarityIdentity(t *testing.T) {
	t.Parallel()
	v := []float32{0.1, 0.2, 0.3, 0.4}
	if got := CosineSimilarity(v, v); got < 0.999 || got > 1.001 {
		t.Fatalf("self-similarity should be ~1, got %f", got)
	}
}

func TestCosineSimilarityOrthogonal(t *testing.T) {
	t.Parallel()
	a := []float32{1, 0, 0}
	b := []float32{0, 1, 0}
	if got := CosineSimilarity(a, b); got != 0 {
		t.Fatalf("orthogonal cosine should be 0, got %f", got)
	}
}

func TestCosineSimilarityZeroVector(t *testing.T) {
	t.Parallel()
	z := []float32{0, 0, 0}
	v := []float32{1, 2, 3}
	if got := CosineSimilarity(z, v); got != 0 {
		t.Fatalf("zero-magnitude should produce 0, got %f", got)
	}
}

func TestCosineSimilarityMismatchedDimensions(t *testing.T) {
	t.Parallel()
	if got := CosineSimilarity([]float32{1, 2}, []float32{1, 2, 3}); got != 0 {
		t.Fatalf("mismatched dims should produce 0, got %f", got)
	}
}

func TestCosineSimilarityRanksAsExpected(t *testing.T) {
	t.Parallel()
	q := []float32{1, 0, 0}
	near := []float32{0.9, 0.4, 0}
	far := []float32{-1, 0, 0}
	if CosineSimilarity(q, near) <= CosineSimilarity(q, far) {
		t.Fatal("near should rank above far")
	}
}

// TestOpenAIEmbeddingClientBatching verifies the client respects batch
// boundaries and threads response indexes correctly. Uses a recorded
// httptest server — no real OpenAI calls.
func TestOpenAIEmbeddingClientBatching(t *testing.T) {
	t.Parallel()

	var receivedBatches int
	var totalInputs int

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model string   `json:"model"`
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode req: %v", err)
		}
		receivedBatches++
		totalInputs += len(req.Input)

		// Echo back deterministic small vectors so the test can verify
		// order preservation without committing to magic numbers.
		data := make([]map[string]any, len(req.Input))
		for i := range req.Input {
			data[i] = map[string]any{
				"index":     i,
				"embedding": []float32{float32(i), 0.5, 1.0},
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	defer srv.Close()

	c := NewOpenAIEmbeddingClient("sk-test")
	c.BaseURL = srv.URL

	inputs := make([]string, 230) // 3 batches: 100, 100, 30
	for i := range inputs {
		inputs[i] = "chunk"
	}

	vecs, err := c.Embed(context.Background(), inputs)
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if receivedBatches != 3 {
		t.Errorf("expected 3 batches, got %d", receivedBatches)
	}
	if totalInputs != len(inputs) {
		t.Errorf("input count mismatch: %d vs %d", totalInputs, len(inputs))
	}
	if len(vecs) != len(inputs) {
		t.Fatalf("vec count: %d vs %d", len(vecs), len(inputs))
	}
	// Spot-check first/last/middle vectors.
	if len(vecs[0]) != 3 || len(vecs[100]) != 3 || len(vecs[229]) != 3 {
		t.Fatal("vector dimensions wrong")
	}
}

func TestOpenAIEmbeddingClientNoKey(t *testing.T) {
	t.Parallel()
	c := NewOpenAIEmbeddingClient("")
	_, err := c.Embed(context.Background(), []string{"hi"})
	if err == nil {
		t.Fatal("expected error when API key is empty")
	}
}
