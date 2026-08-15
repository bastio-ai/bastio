package cache

import (
	"context"
	"math"
	"sync"
	"time"

	"github.com/google/uuid"
)

// VectorEntry represents a single cached item indexed by its vector embedding.
type VectorEntry struct {
	ID         string    `json:"id"`
	Prompt     string    `json:"prompt"`
	Embedding  []float32 `json:"embedding"`
	Response   []byte    `json:"response"`
	Model      string    `json:"model"`
	CustomerID string    `json:"customer_id"`
	CreatedAt  time.Time `json:"created_at"`
	ExpiresAt  time.Time `json:"expires_at,omitempty"`
}

// SemanticCache provides an in-memory vector cache with thread-safe operations,
// cosine similarity nearest-neighbor lookup, and bounded capacity with eviction.
type SemanticCache struct {
	mu         sync.RWMutex
	maxEntries int
	entries    []*VectorEntry
}

// NewSemanticCache creates a new SemanticCache with the specified maximum entry capacity.
// If maxEntries <= 0, a default of 5,000 entries is used.
func NewSemanticCache(maxEntries int) *SemanticCache {
	if maxEntries <= 0 {
		maxEntries = 5000
	}
	return &SemanticCache{
		maxEntries: maxEntries,
		entries:    make([]*VectorEntry, 0, maxEntries),
	}
}

// CosineSimilarity computes the cosine similarity between two float32 vectors.
// Returns a value between -1.0 and 1.0 (clamped). Returns 0 if vectors differ in length or have zero norm.
func CosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}

	var dotProduct, normA, normB float64
	for i := 0; i < len(a); i++ {
		valA := float64(a[i])
		valB := float64(b[i])
		dotProduct += valA * valB
		normA += valA * valA
		normB += valB * valB
	}

	if normA <= 0 || normB <= 0 {
		return 0
	}

	sim := float32(dotProduct / (math.Sqrt(normA) * math.Sqrt(normB)))
	if sim > 1.0 {
		return 1.0
	}
	if sim < -1.0 {
		return -1.0
	}
	return sim
}

// Query looks up the closest vector entry matching customerID and model whose
// cosine similarity with the query embedding is greater than or equal to threshold.
// If multiple matching entries exceed threshold, the one with highest similarity is returned.
func (sc *SemanticCache) Query(_ context.Context, customerID, model string, embedding []float32, threshold float32) (*VectorEntry, float32, bool) {
	if len(embedding) == 0 || customerID == "" || model == "" {
		return nil, 0, false
	}

	sc.mu.RLock()
	defer sc.mu.RUnlock()

	now := time.Now()
	var bestEntry *VectorEntry
	var maxSim float32 = -1.0

	for _, e := range sc.entries {
		if e.CustomerID != customerID || e.Model != model {
			continue
		}
		if !e.ExpiresAt.IsZero() && now.After(e.ExpiresAt) {
			continue
		}

		sim := CosineSimilarity(e.Embedding, embedding)
		if sim >= threshold && sim > maxSim {
			maxSim = sim
			bestEntry = e
		}
	}

	if bestEntry != nil {
		return bestEntry, maxSim, true
	}
	return nil, 0, false
}

// Store adds a new entry into the semantic vector cache with optional TTL.
// When max capacity is reached, expired entries are purged or the oldest entry is evicted (FIFO).
func (sc *SemanticCache) Store(_ context.Context, customerID, model, prompt string, embedding []float32, response []byte, ttl time.Duration) {
	if len(embedding) == 0 || len(response) == 0 || customerID == "" || model == "" {
		return
	}

	sc.mu.Lock()
	defer sc.mu.Unlock()

	now := time.Now()

	// If capacity is reached, first purge expired entries
	if len(sc.entries) >= sc.maxEntries {
		active := make([]*VectorEntry, 0, len(sc.entries))
		for _, e := range sc.entries {
			if e.ExpiresAt.IsZero() || now.Before(e.ExpiresAt) {
				active = append(active, e)
			}
		}
		sc.entries = active

		// If still at or above capacity, evict the oldest entry (FIFO)
		for len(sc.entries) >= sc.maxEntries {
			sc.entries = sc.entries[1:]
		}
	}

	var expiresAt time.Time
	if ttl > 0 {
		expiresAt = now.Add(ttl)
	}

	entry := &VectorEntry{
		ID:         uuid.New().String(),
		Prompt:     prompt,
		Embedding:  embedding,
		Response:   response,
		Model:      model,
		CustomerID: customerID,
		CreatedAt:  now,
		ExpiresAt:  expiresAt,
	}

	sc.entries = append(sc.entries, entry)
}

// Len returns the current number of stored entries.
func (sc *SemanticCache) Len() int {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	return len(sc.entries)
}

// Clear empties all entries from the cache.
func (sc *SemanticCache) Clear() {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.entries = sc.entries[:0]
}
