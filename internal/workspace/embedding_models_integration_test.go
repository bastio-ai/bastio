package workspace

import (
	"context"
	"strings"
	"testing"

	"github.com/bastio-ai/bastio/internal/security"
	"github.com/bastio-ai/bastio/internal/security/detection"
	"github.com/google/uuid"
)

type modelEmbeddingFixture struct {
	model  string
	dim    int
	inputs []string
}

func (e *modelEmbeddingFixture) ModelID() string { return e.model }
func (e *modelEmbeddingFixture) Dimension() int  { return e.dim }
func (e *modelEmbeddingFixture) Embed(_ context.Context, inputs []string) ([][]float32, error) {
	e.inputs = append(e.inputs, inputs...)
	out := make([][]float32, len(inputs))
	for i := range out {
		out[i] = make([]float32, e.dim)
		out[i][0] = 1
	}
	return out, nil
}

func TestWorkspaceEmbeddingModelsAndReindexIntegration(t *testing.T) {
	s := workspaceTestStore(t)
	cid, other := uuid.New(), uuid.New()
	ctx := testIdentity(cid, "employee", RoleMember)
	_, _ = s.EnsureSettings(ctx, cid)
	newClient := &modelEmbeddingFixture{model: "bge:test-revision", dim: 1024}
	legacyClient := &modelEmbeddingFixture{model: "openai:text-embedding-3-small", dim: 1536}
	wrongModel := &modelEmbeddingFixture{model: "bge:other-revision", dim: 1024}
	text := "Employees request annual leave from their manager."
	var desired uuid.UUID
	for i, entry := range []struct {
		customer uuid.UUID
		client   EmbeddingClient
	}{{cid, newClient}, {cid, legacyClient}, {cid, wrongModel}, {other, newClient}} {
		src, err := s.CreateKnowledgeSource(ctx, KnowledgeSource{CustomerID: entry.customer, Name: "Handbook", Type: "text", Status: "ready", InlineText: &text})
		if err != nil {
			t.Fatal(err)
		}
		if err = s.chunkAndEmbedWithScan(ctx, src, entry.client, false, nil); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			desired = src.ID
		}
	}
	v := make([]float32, 1024)
	v[0] = 1
	hits, err := s.VectorSearchPGModel(ctx, cid, uuid.Nil, v, 6, newClient.ModelID())
	if err != nil || len(hits) != 1 || hits[0].SourceID != desired {
		t.Fatalf("vector spaces or tenants mixed: %v %v", hits, err)
	}
	oldText := "Contact alice@example.test about annual leave."
	src, err := s.CreateKnowledgeSource(ctx, KnowledgeSource{CustomerID: cid, Name: "Old handbook", Type: "text", Status: "ready", InlineText: &oldText})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.chunkAndEmbedWithScan(ctx, src, nil, false, nil); err != nil {
		t.Fatal(err)
	}
	profile := security.DefaultProfile()
	h := NewHandler(s, nil, nil)
	h.SetEmbeddingClient(newClient)
	h.SetSecurityEngine(security.NewEngine(detection.NewPIIDetector()))
	h.SetSecurityProfiles(&staticProfileLookup{profile: &profile})
	newClient.inputs = nil
	if err = h.reindexKnowledge(ctx, cid, src.ID); err != nil {
		t.Fatal(err)
	}
	if len(newClient.inputs) == 0 || strings.Contains(strings.Join(newClient.inputs, "\n"), "alice@example.test") {
		t.Fatal("raw PII reached re-index embedder")
	}
	stored, err := s.GetKnowledgeSource(ctx, cid, src.ID)
	if err != nil || stored.InlineText == nil || strings.Contains(*stored.InlineText, "alice@example.test") {
		t.Fatal("security-processed text was not retained")
	}
	newClient.inputs = nil
	_ = h.findRelevantChunks(ctx, cid, uuid.Nil, "Ask alice@example.test about holiday requests")
	if len(newClient.inputs) == 0 || strings.Contains(strings.Join(newClient.inputs, "\n"), "alice@example.test") {
		t.Fatal("raw search-query PII reached embedder")
	}
}

func TestReindexQuarantinesBlockedSourceIntegration(t *testing.T) {
	s := workspaceTestStore(t)
	cid := uuid.New()
	ctx := testIdentity(cid, "owner", RoleOwner)
	_, _ = s.EnsureSettings(ctx, cid)
	text := "Contact alice@example.test."
	src, err := s.CreateKnowledgeSource(ctx, KnowledgeSource{CustomerID: cid, Name: "Sensitive", Type: "text", Status: "ready", InlineText: &text})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.chunkAndEmbedWithScan(ctx, src, nil, false, nil); err != nil {
		t.Fatal(err)
	}
	profile := security.DefaultProfile()
	profile.PIIAction = security.ActionBlock
	spy := &modelEmbeddingFixture{model: "bge:test", dim: 1024}
	h := NewHandler(s, nil, nil)
	h.SetEmbeddingClient(spy)
	h.SetSecurityEngine(security.NewEngine(detection.NewPIIDetector()))
	h.SetSecurityProfiles(&staticProfileLookup{profile: &profile})
	if err = h.reindexKnowledge(ctx, cid, src.ID); err != nil {
		t.Fatal(err)
	}
	stored, _ := s.GetKnowledgeSource(ctx, cid, src.ID)
	if stored.Status != "quarantined" || len(spy.inputs) != 0 {
		t.Fatal("blocked source was embedded")
	}
	hits, err := s.SearchRelevantChunks(ctx, cid, uuid.Nil, "Contact")
	if err != nil || len(hits) != 0 {
		t.Fatal("quarantined chunks remained retrievable")
	}
}
