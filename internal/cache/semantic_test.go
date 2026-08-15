package cache

import (
	"context"
	"fmt"
	"math"
	"sync"
	"testing"
	"time"
)

func TestCosineSimilarity(t *testing.T) {
	tests := []struct {
		name     string
		a        []float32
		b        []float32
		expected float32
		delta    float32
	}{
		{
			name:     "identical vectors",
			a:        []float32{1.0, 2.0, 3.0},
			b:        []float32{1.0, 2.0, 3.0},
			expected: 1.0,
			delta:    0.0001,
		},
		{
			name:     "orthogonal vectors",
			a:        []float32{1.0, 0.0},
			b:        []float32{0.0, 1.0},
			expected: 0.0,
			delta:    0.0001,
		},
		{
			name:     "opposite vectors",
			a:        []float32{1.0, 0.0},
			b:        []float32{-1.0, 0.0},
			expected: -1.0,
			delta:    0.0001,
		},
		{
			name:     "high similarity",
			a:        []float32{0.9, 0.1, 0.0},
			b:        []float32{0.88, 0.12, 0.01},
			expected: 0.999,
			delta:    0.01,
		},
		{
			name:     "different lengths",
			a:        []float32{1.0, 2.0},
			b:        []float32{1.0, 2.0, 3.0},
			expected: 0.0,
			delta:    0.0001,
		},
		{
			name:     "zero norm vector",
			a:        []float32{0.0, 0.0, 0.0},
			b:        []float32{1.0, 2.0, 3.0},
			expected: 0.0,
			delta:    0.0001,
		},
		{
			name:     "empty vectors",
			a:        []float32{},
			b:        []float32{},
			expected: 0.0,
			delta:    0.0001,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sim := CosineSimilarity(tt.a, tt.b)
			if math.Abs(float64(sim-tt.expected)) > float64(tt.delta) {
				t.Errorf("CosineSimilarity() = %v, expected %v (delta %v)", sim, tt.expected, tt.delta)
			}
		})
	}
}

func TestSemanticCache_ThresholdMatching(t *testing.T) {
	ctx := context.Background()
	sc := NewSemanticCache(100)

	custID := "cust_123"
	model := "gpt-4o"
	prompt := "What is the capital of France?"
	emb := []float32{0.1, 0.2, 0.3, 0.4, 0.5}
	resp := []byte(`{"response":"Paris"}`)

	sc.Store(ctx, custID, model, prompt, emb, resp, 1*time.Hour)

	// Query with identical embedding (similarity 1.0 >= 0.95)
	entry, sim, hit := sc.Query(ctx, custID, model, emb, 0.95)
	if !hit || entry == nil {
		t.Fatalf("expected semantic cache hit, got miss")
	}
	if sim < 0.99 {
		t.Errorf("expected similarity >= 0.99, got %f", sim)
	}
	if string(entry.Response) != string(resp) {
		t.Errorf("expected response %s, got %s", resp, entry.Response)
	}

	// Query with slightly varied embedding (similarity ~0.98 >= 0.95)
	similarEmb := []float32{0.11, 0.20, 0.29, 0.41, 0.49}
	entry, sim, hit = sc.Query(ctx, custID, model, similarEmb, 0.95)
	if !hit || entry == nil {
		t.Fatalf("expected semantic cache hit for similar embedding, got miss")
	}
	if sim < 0.95 {
		t.Errorf("expected similarity >= 0.95, got %f", sim)
	}

	// Query with dissimilar embedding (similarity < 0.95)
	dissimilarEmb := []float32{0.9, -0.2, 0.1, -0.4, 0.0}
	_, _, hit = sc.Query(ctx, custID, model, dissimilarEmb, 0.95)
	if hit {
		t.Fatalf("expected semantic cache miss for dissimilar embedding, got hit")
	}
}

func TestSemanticCache_ModelAndCustomerIsolation(t *testing.T) {
	ctx := context.Background()
	sc := NewSemanticCache(100)

	cust1 := "customer_A"
	cust2 := "customer_B"
	model1 := "gpt-4o"
	model2 := "claude-3-5-sonnet"
	prompt := "Explain quantum computing"
	emb := []float32{0.5, 0.5, 0.5, 0.5}
	resp := []byte(`{"response":"Quantum computing uses qubits."}`)

	sc.Store(ctx, cust1, model1, prompt, emb, resp, 1*time.Hour)

	// Same customer, different model -> miss
	_, _, hit := sc.Query(ctx, cust1, model2, emb, 0.90)
	if hit {
		t.Errorf("expected miss for different model")
	}

	// Different customer, same model -> miss
	_, _, hit = sc.Query(ctx, cust2, model1, emb, 0.90)
	if hit {
		t.Errorf("expected miss for different customer")
	}

	// Same customer, same model -> hit
	entry, sim, hit := sc.Query(ctx, cust1, model1, emb, 0.90)
	if !hit || entry == nil || sim < 0.99 {
		t.Errorf("expected hit for exact customer and model")
	}
}

func TestSemanticCache_HighestSimilaritySelection(t *testing.T) {
	ctx := context.Background()
	sc := NewSemanticCache(100)

	cust := "cust_1"
	model := "gpt-4o"

	// Store entry 1
	emb1 := []float32{0.1, 0.2, 0.3}
	resp1 := []byte(`{"resp": 1}`)
	sc.Store(ctx, cust, model, "prompt 1", emb1, resp1, 1*time.Hour)

	// Store entry 2 (closer to target)
	emb2 := []float32{0.11, 0.21, 0.31}
	resp2 := []byte(`{"resp": 2}`)
	sc.Store(ctx, cust, model, "prompt 2", emb2, resp2, 1*time.Hour)

	// Store entry 3 (further)
	emb3 := []float32{0.5, 0.5, 0.5}
	resp3 := []byte(`{"resp": 3}`)
	sc.Store(ctx, cust, model, "prompt 3", emb3, resp3, 1*time.Hour)

	// Query target is exact match to emb2
	entry, sim, hit := sc.Query(ctx, cust, model, emb2, 0.90)
	if !hit {
		t.Fatalf("expected hit")
	}
	if string(entry.Response) != string(resp2) {
		t.Errorf("expected closest entry (resp2), got %s", entry.Response)
	}
	if sim < 0.999 {
		t.Errorf("expected ~1.0 similarity for exact match, got %f", sim)
	}
}

func TestSemanticCache_Eviction(t *testing.T) {
	ctx := context.Background()
	// Capacity = 3 entries
	sc := NewSemanticCache(3)

	cust := "cust_evict"
	model := "gpt-4o"

	sc.Store(ctx, cust, model, "p1", []float32{0.1, 0.0}, []byte("resp1"), 1*time.Hour)
	sc.Store(ctx, cust, model, "p2", []float32{0.2, 0.0}, []byte("resp2"), 1*time.Hour)
	sc.Store(ctx, cust, model, "p3", []float32{0.3, 0.0}, []byte("resp3"), 1*time.Hour)

	if sc.Len() != 3 {
		t.Fatalf("expected 3 entries, got %d", sc.Len())
	}

	// Store 4th entry -> oldest (p1) should be evicted
	sc.Store(ctx, cust, model, "p4", []float32{0.4, 0.0}, []byte("resp4"), 1*time.Hour)

	if sc.Len() != 3 {
		t.Fatalf("expected cache size to stay at capacity 3, got %d", sc.Len())
	}

	// Verify p1 was evicted (no longer matches by ID or prompt)
	sc.mu.RLock()
	foundP1 := false
	for _, e := range sc.entries {
		if e.Prompt == "p1" {
			foundP1 = true
		}
	}
	sc.mu.RUnlock()

	if foundP1 {
		t.Errorf("expected p1 to be evicted")
	}
}

func TestSemanticCache_TTLExpiration(t *testing.T) {
	ctx := context.Background()
	sc := NewSemanticCache(10)

	cust := "cust_ttl"
	model := "gpt-4o"
	emb := []float32{1.0, 2.0}

	// Store with very short TTL (1 millisecond)
	sc.Store(ctx, cust, model, "short-lived", emb, []byte("quick"), 1*time.Millisecond)

	time.Sleep(10 * time.Millisecond)

	_, _, hit := sc.Query(ctx, cust, model, emb, 0.90)
	if hit {
		t.Errorf("expected expired entry to miss")
	}
}

func TestSemanticCache_Concurrency(t *testing.T) {
	ctx := context.Background()
	sc := NewSemanticCache(100)
	var wg sync.WaitGroup

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			cust := fmt.Sprintf("cust_%d", idx%3)
			model := "gpt-4o"
			emb := []float32{float32(idx) * 0.1, 0.5, 0.2}
			resp := []byte(fmt.Sprintf("resp_%d", idx))

			sc.Store(ctx, cust, model, fmt.Sprintf("prompt_%d", idx), emb, resp, 1*time.Hour)
			_, _, _ = sc.Query(ctx, cust, model, emb, 0.90)
		}(i)
	}

	wg.Wait()
	if sc.Len() == 0 {
		t.Errorf("expected entries in cache after concurrent operations")
	}
}
