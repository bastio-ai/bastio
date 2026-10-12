package workspace

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBGEEmbeddingBatchesAndValidates(t *testing.T) {
	var counts []int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/embed" || r.Header.Get("Authorization") != "" {
			t.Error("unexpected endpoint or credential")
		}
		var body struct {
			Inputs []string `json:"inputs"`
			Dense  bool     `json:"return_dense"`
			Sparse bool     `json:"return_sparse"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil || !body.Dense || body.Sparse {
			t.Error("unexpected request")
		}
		counts = append(counts, len(body.Inputs))
		rows := make([]struct {
			Dense []float32 `json:"dense"`
		}, len(body.Inputs))
		for i := range rows {
			rows[i].Dense = make([]float32, 1024)
			rows[i].Dense[0] = 1
		}
		_ = json.NewEncoder(w).Encode(rows)
	}))
	defer server.Close()
	client, err := NewBGEEmbeddingClient(server.URL, "bge:test-revision", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	vectors, err := client.Embed(context.Background(), make([]string, 33))
	if err != nil || len(vectors) != 33 || len(counts) != 3 || counts[0] != 16 || counts[1] != 16 || counts[2] != 1 {
		t.Fatalf("batch failure: %v %v", counts, err)
	}
	if embeddingModel(client) != "bge:test-revision" {
		t.Fatal("model identity lost")
	}
}
func TestBGERejectsBadVectorsAndDoesNotExposeServiceBody(t *testing.T) {
	for _, scenario := range []struct {
		body   string
		status int
	}{
		{`[{"dense":[1,2]}]`, 200}, {`[]`, 200}, {`[{"dense":null}]`, 200}, {`{"private":"alice@example.test"}`, 500},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(scenario.status)
			_, _ = w.Write([]byte(scenario.body))
		}))
		client, _ := NewBGEEmbeddingClient(server.URL, "bge:test", server.Client())
		_, err := client.Embed(context.Background(), []string{"synthetic"})
		server.Close()
		if err == nil || strings.Contains(err.Error(), "alice@example.test") {
			t.Fatalf("unsafe embedding error: %v", err)
		}
	}
}
