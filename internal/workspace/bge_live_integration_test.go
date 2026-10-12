package workspace

import (
	"context"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/bastio-ai/bastio/internal/security"
	"github.com/bastio-ai/bastio/internal/security/detection"
	"github.com/google/uuid"
)

// Explicitly opt into the owned GPU using synthetic data and a disposable DB.
func TestBGELiveKnowledgeIntegration(t *testing.T) {
	endpoint, address := os.Getenv("WORKSPACE_TEST_BGE_URL"), os.Getenv("WORKSPACE_TEST_BGE_PRIVATE_ADDR")
	if endpoint == "" || address == "" {
		t.Skip("owned GPU endpoint and private address are required")
	}
	s := workspaceTestStore(t)
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, network, address)
	}
	defer transport.CloseIdleConnections()
	client, err := NewBGEEmbeddingClient(endpoint, "bge:live-integration", &http.Client{Transport: transport, Timeout: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	cid := uuid.New()
	ctx := testIdentity(cid, "gpu-verification", RoleOwner)
	_, _ = s.EnsureSettings(ctx, cid)
	text := "Employees request annual leave from their supervisor. Contact alice@example.test for approval."
	source, err := s.CreateKnowledgeSource(ctx, KnowledgeSource{CustomerID: cid, Name: "Synthetic employee handbook", Type: "text", Status: "ready", InlineText: &text})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.chunkAndEmbedWithScan(ctx, source, nil, false, nil); err != nil {
		t.Fatal(err)
	}
	profile := security.DefaultProfile()
	h := NewHandler(s, nil, nil)
	h.SetEmbeddingClient(client)
	h.SetSecurityEngine(security.NewEngine(detection.NewPIIDetector()))
	h.SetSecurityProfiles(&staticProfileLookup{profile: &profile})
	if err = h.reindexKnowledge(ctx, cid, source.ID); err != nil {
		t.Fatal(err)
	}
	var dimension int
	var content, model string
	err = s.pool.QueryRow(ctx, `SELECT vector_dims(embedding_vec),content,embedding_model FROM workspace_knowledge_chunks WHERE customer_id=$1 AND knowledge_source_id=$2 LIMIT 1`, cid, source.ID).Scan(&dimension, &content, &model)
	if err != nil || dimension != 1024 || model != client.ModelID() || strings.Contains(content, "alice@example.test") {
		t.Fatalf("real GPU indexing failed: dimension=%d model=%s error=%v", dimension, model, err)
	}
	query := "How can staff arrange time off?"
	keywords, err := s.SearchRelevantChunks(ctx, cid, uuid.Nil, query)
	if err != nil || len(keywords) != 0 {
		t.Fatal("synthetic semantic query unexpectedly has keyword matches")
	}
	hits := h.findRelevantChunks(ctx, cid, uuid.Nil, query)
	if len(hits) != 1 || hits[0].SourceID != source.ID || hits[0].Score <= 0 {
		t.Fatalf("real GPU semantic retrieval failed: %v", hits)
	}
	t.Logf("owned GPU → protected text → pgvector(1024) → semantic retrieval: passed (score=%d)", hits[0].Score)
}
