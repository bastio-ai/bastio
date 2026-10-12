package workspace

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/bastio-ai/bastio/internal/observability"
	"github.com/bastio-ai/bastio/internal/security"
	"github.com/bastio-ai/bastio/pkg/clickhouse"

	"github.com/bastio-ai/bastio/internal/providers"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Run against a disposable database with OSS and Cloud migrations applied.
func workspaceTestStore(t *testing.T) *Store {
	t.Helper()
	url := os.Getenv("WORKSPACE_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("WORKSPACE_TEST_DATABASE_URL requires an isolated migrated database")
	}
	p, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	return NewStore(p)
}
func testIdentity(cid uuid.UUID, uid string, role Role) context.Context {
	ctx := context.WithValue(context.Background(), ctxCustomerID, cid)
	ctx = context.WithValue(ctx, ctxUserID, uid)
	return WithRole(ctx, role)
}
func TestWorkspaceContentStorageIntegration(t *testing.T) {
	s := workspaceTestStore(t)
	cid := uuid.New()
	ctx := testIdentity(cid, "employee", RoleMember)
	if _, err := s.EnsureSettings(ctx, cid); err != nil {
		t.Fatal(err)
	}
	off := false
	if _, err := s.UpdateSettings(ctx, cid, SettingsPatch{StoreConversationContent: &off}); err != nil {
		t.Fatal(err)
	}
	c, err := s.CreateConversation(ctx, Conversation{CustomerID: cid, UserID: "employee", Title: "confidential acquisition"})
	if err != nil {
		t.Fatal(err)
	}
	if c.Title != "Temporary chat" || c.ContentStored {
		t.Fatal("private title persisted")
	}
	sensitive := "private employee document with attachments"
	errText := sensitive
	returned, err := s.AppendMessage(ctx, Message{CustomerID: cid, ConversationID: c.ID, Role: "assistant", Content: sensitive, Metadata: json.RawMessage(`{"citations":[{"source_name":"secret filename"}]}`), Error: &errText, PromptTokens: 10, CompletionTokens: 20, CostCentsExact: 2.75})
	if err != nil {
		t.Fatal(err)
	}
	if returned.Content != sensitive {
		t.Fatal("active response lost")
	}
	saved, err := s.ListMessages(ctx, cid, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved) != 1 || saved[0].Content != "" || string(saved[0].Metadata) != "{}" || saved[0].Error != nil || saved[0].PromptTokens != 10 {
		t.Fatalf("content leaked or usage lost: %+v", saved)
	}
	if err := s.RenameConversation(ctx, cid, c.ID, "secret title"); err != nil {
		t.Fatal(err)
	}
	read, err := s.GetConversation(ctx, cid, c.ID)
	if err != nil || read.Title != "Temporary chat" {
		t.Fatal("rename bypassed privacy")
	}
	on := true
	if _, err = s.UpdateSettings(ctx, cid, SettingsPatch{StoreConversationContent: &on}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.AppendMessage(ctx, Message{CustomerID: cid, ConversationID: c.ID, Role: "user", Content: sensitive}); err != nil {
		t.Fatal(err)
	}
	saved, err = s.ListMessages(ctx, cid, c.ID)
	if err != nil || saved[1].Content != "" {
		t.Fatal("temporary thread became durable")
	}
}
func TestWorkspaceConcurrentSpendingIntegration(t *testing.T) {
	s := workspaceTestStore(t)
	cid := uuid.New()
	ctx := testIdentity(cid, "employee", RoleMember)
	s.EnsureSettings(ctx, cid)
	cap := 1
	if _, err := s.UpdateSettings(ctx, cid, SettingsPatch{SpendCapCents: &cap}); err != nil {
		t.Fatal(err)
	}
	h := NewHandler(s, nil, nil)
	var wg sync.WaitGroup
	var lock sync.Mutex
	ids := []uuid.UUID{}
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id, err := h.reserveSpend(ctx, cid, "gpt-4o-mini", []providers.Message{{Role: "user", Content: "hello"}})
			if err == nil {
				lock.Lock()
				ids = append(ids, id)
				lock.Unlock()
			}
		}()
	}
	wg.Wait()
	st, err := s.SpendStatus(ctx, cid)
	if err != nil || st.UsedCents > 1 || len(ids) == 0 || len(ids) == 40 {
		t.Fatalf("limit failed: %+v ids=%d err=%v", st, len(ids), err)
	}
	for _, id := range ids {
		if err := h.settleSpend(cid, id, 0); err != nil {
			t.Fatal(err)
		}
	}
	st, err = s.SpendStatus(ctx, cid)
	if err != nil || st.UsedCents > 0.000001 {
		t.Fatal("settlement did not release unused budget")
	}
	other := uuid.New()
	s.EnsureSettings(ctx, other)
	st, err = s.SpendStatus(ctx, other)
	if err != nil || st.UsedCents != 0 {
		t.Fatal("tenant spend leaked")
	}
}
func TestWorkspaceKnowledgeAccessIntegration(t *testing.T) {
	s := workspaceTestStore(t)
	cid := uuid.New()
	other := uuid.New()
	admin := testIdentity(cid, "admin", RoleAdmin)
	member := testIdentity(cid, "employee", RoleMember)
	s.EnsureSettings(admin, cid)
	k, err := s.CreateKnowledgeSource(admin, KnowledgeSource{CustomerID: cid, Name: "Finance", Type: "text", SourceRef: "", Status: "ready", Metadata: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	gid := uuid.New()
	if _, err = s.pool.Exec(admin, `INSERT INTO workspace_access_groups(id,customer_id,name) VALUES($1,$2,'Finance');`, gid, cid); err != nil {
		t.Fatal(err)
	}
	if _, err = s.pool.Exec(admin, `INSERT INTO workspace_resource_access(customer_id,resource_type,resource_id,group_id) VALUES($1,'knowledge',$2,$3)`, cid, k.ID, gid); err != nil {
		t.Fatal(err)
	}
	if _, err = s.pool.Exec(admin, `INSERT INTO workspace_knowledge_chunks(customer_id,knowledge_source_id,ordinal,content) VALUES($1,$2,0,'salary forecasts confidential')`, cid, k.ID); err != nil {
		t.Fatal(err)
	}
	hits, err := s.SearchRelevantChunks(member, cid, uuid.Nil, "salary")
	if err != nil || len(hits) != 0 {
		t.Fatalf("unauthorized retrieval: %v %v", hits, err)
	}
	if _, err = s.pool.Exec(admin, `INSERT INTO workspace_access_group_members(customer_id,group_id,user_id) VALUES($1,$2,'employee')`, cid, gid); err != nil {
		t.Fatal(err)
	}
	hits, err = s.SearchRelevantChunks(member, cid, uuid.Nil, "salary")
	if err != nil || len(hits) != 1 {
		t.Fatalf("authorized retrieval: %v %v", hits, err)
	}
	hits, err = s.SearchRelevantChunks(testIdentity(other, "employee", RoleAdmin), other, uuid.Nil, "salary")
	if err != nil || len(hits) != 0 {
		t.Fatal("cross-tenant retrieval")
	}
}

func TestWorkspaceSearchAndTemporaryHistoryIntegration(t *testing.T) {
	s := workspaceTestStore(t)
	cid := uuid.New()
	ctx := testIdentity(cid, "employee", RoleMember)
	if _, err := s.EnsureSettings(ctx, cid); err != nil {
		t.Fatal(err)
	}
	c, err := s.CreateConversation(ctx, Conversation{CustomerID: cid, UserID: "employee", Title: "Ordinary title"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.AppendMessage(ctx, Message{CustomerID: cid, ConversationID: c.ID, Role: "user", Content: "confidential acquisition forecast"}); err != nil {
		t.Fatal(err)
	}
	found, err := s.ListConversations(ctx, cid, "employee", 50, "acquisition")
	if err != nil || len(found) != 1 {
		t.Fatalf("message search failed: %v %v", found, err)
	}
	found, err = s.ListConversations(ctx, cid, "another-employee", 50, "acquisition")
	if err != nil || len(found) != 0 {
		t.Fatal("search leaked another employee's messages")
	}
	off := false
	if _, err = s.UpdateSettings(ctx, cid, SettingsPatch{StoreConversationContent: &off}); err != nil {
		t.Fatal(err)
	}
	h := NewHandler(s, nil, nil)
	r := httptest.NewRequest("POST", "/", nil).WithContext(ctx)
	b := SendMessageRequest{Content: "What was the first topic?", History: []HistoryMessage{{Role: "user", Content: "Quarterly planning"}, {Role: "assistant", Content: "Let's plan the next quarter."}}}
	r, err = h.prepareChat(r, c, b)
	if err != nil {
		t.Fatal(err)
	}
	history, err := h.chatHistory(r.Context(), cid, c.ID, b)
	if err != nil || len(history) != 3 || history[0].Content != "Quarterly planning" || history[2].Content != b.Content {
		t.Fatalf("browser context not preserved: %v %v", history, err)
	}
	b.History[0].Role = "system"
	if _, err = h.prepareChat(r, c, b); err == nil {
		t.Fatal("browser could supply a system instruction")
	}
}

func TestWorkspaceTracePrivacyIntegration(t *testing.T) {
	s := workspaceTestStore(t)
	url := os.Getenv("WORKSPACE_TEST_CLICKHOUSE_URL")
	if url == "" {
		t.Skip("WORKSPACE_TEST_CLICKHOUSE_URL requires isolated migrated ClickHouse")
	}
	ctx := context.Background()
	ch, err := clickhouse.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer ch.Close()
	cid := uuid.New()
	if _, err = s.EnsureSettings(ctx, cid); err != nil {
		t.Fatal(err)
	}
	off := false
	if _, err = s.UpdateSettings(ctx, cid, SettingsPatch{StoreConversationContent: &off}); err != nil {
		t.Fatal(err)
	}
	recorder := observability.NewRecorder(ch)
	recorder.Start(ctx)
	h := NewHandler(s, nil, nil)
	h.obsRecorder = recorder
	secret := "private employee acquisition details"
	scan := &security.ScanResult{Action: security.ActionBlock, ShouldBlock: true, ThreatScore: 0.9, SanitizedContent: secret, Findings: []security.Finding{{ThreatType: security.ThreatInjection, Severity: security.SeverityHigh, Action: security.ActionBlock, Score: 0.9, Confidence: 1, DetectorName: "injection", MatchedContent: secret, MatchedPattern: secret, Message: secret, Source: secret}}}
	tid := uuid.New()
	h.recordChatTrace(ctx, chatTraceInput{customerID: cid, conversationID: uuid.New(), traceID: tid, startedAt: time.Now().UTC(), completedAt: time.Now().UTC(), provider: "openai", model: "gpt-4o-mini", requestBody: secret, responseBody: secret, scanResult: scan})
	if err = recorder.Close(ctx); err != nil {
		t.Fatal(err)
	}
	var request, response string
	if err = ch.Conn.QueryRow(ctx, `SELECT request_body,response_body FROM bastio.traces WHERE customer_id=? AND id=?`, cid, tid).Scan(&request, &response); err != nil {
		t.Fatal(err)
	}
	if request != "" || response != "" {
		t.Fatal("private trace retained message content")
	}
	var matched, pattern, source string
	if err = ch.Conn.QueryRow(ctx, `SELECT matched_content,matched_pattern,source FROM bastio.security_threat_logs WHERE customer_id=? AND trace_id=?`, cid, tid).Scan(&matched, &pattern, &source); err != nil {
		t.Fatal(err)
	}
	if matched != "" || pattern != "" || source != "" {
		t.Fatal("private security finding retained a snippet")
	}
	if scan.Findings[0].MatchedContent != secret {
		t.Fatal("scrubbing mutated the active security decision")
	}
}
