package workspace

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bastio-ai/bastio/internal/observability"
	"github.com/bastio-ai/bastio/pkg/encryption"
	"github.com/google/uuid"
)

func TestMCPHistoryPoliciesCaptureAndExpiry(t *testing.T) {
	s := workspaceToolsTestStore(t)
	cid, foreign, connection, other := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	owner := testIdentity(cid, "owner", RoleOwner)
	for _, id := range []uuid.UUID{cid, foreign} {
		if _, err := s.pool.Exec(owner, `INSERT INTO customers(id,name,slug) VALUES($1,'History test',$2)`, id, id.String()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.EnsureSettings(owner, cid); err != nil {
		t.Fatal(err)
	}
	_, err := s.pool.Exec(owner, `CREATE TABLE IF NOT EXISTS workspace_members(customer_id UUID NOT NULL,user_id TEXT NOT NULL,role TEXT DEFAULT 'member',PRIMARY KEY(customer_id,user_id))`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.pool.Exec(owner, `CREATE TRIGGER workspace_member_direct_share_cleanup AFTER DELETE ON workspace_members FOR EACH ROW EXECUTE FUNCTION clear_workspace_direct_shares()`)
	if err != nil {
		t.Fatal(err)
	}
	for _, uid := range []string{"employee", "reviewer", "peer"} {
		if _, err = s.pool.Exec(owner, `INSERT INTO workspace_members(customer_id,user_id) VALUES($1,$2)`, cid, uid); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []uuid.UUID{connection, other} {
		if _, err = s.pool.Exec(owner, `INSERT INTO workspace_mcp_connections(id,customer_id,owner_user_id,name,endpoint,scope) VALUES($1,$2,'owner','Fixture','https://example.test/mcp','workspace')`, id, cid); err != nil {
			t.Fatal(err)
		}
	}
	h := NewHandler(s, nil, nil)
	h.mcpCipher, _ = encryption.New("")
	h.obsRecorder = observability.NewRecorder(nil)
	routes := h.Routes()
	request := func(ctx context.Context, method, path string, body any) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(body)
		r := httptest.NewRequest(method, path, strings.NewReader(string(raw))).WithContext(ctx)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		routes.ServeHTTP(w, r)
		return w
	}
	getPolicy := func() MCPHistoryPolicy {
		w := request(owner, "GET", "/mcp/history/policy", nil)
		if w.Code != 200 {
			t.Fatalf("get policy %d %s", w.Code, w.Body.String())
		}
		var out mcpHistoryPolicyResponse
		if json.Unmarshal(w.Body.Bytes(), &out) != nil {
			t.Fatal("policy JSON")
		}
		return out.Policy
	}
	p := getPolicy()
	if p.Mode != "metadata_only" {
		t.Fatal("history enabled by default")
	}
	if w := request(testIdentity(cid, "employee", RoleMember), "PUT", "/mcp/history/policy", p); w.Code != 403 {
		t.Fatal("employee changed history settings")
	}
	p.Mode = "sanitized"
	p.ConnectionIDs = []uuid.UUID{connection}
	p.ViewerUserIDs = []string{"reviewer"}
	if w := request(owner, "PUT", "/mcp/history/policy", p); w.Code != 200 {
		t.Fatalf("enable %d %s", w.Code, w.Body.String())
	}
	stale := p
	p = getPolicy()
	if w := request(owner, "PUT", "/mcp/history/policy", stale); w.Code != 409 {
		t.Fatal("stale policy overwrote current policy")
	}
	begin := func(ctx context.Context, id uuid.UUID, conv uuid.UUID) *mcpToolTrace {
		r := httptest.NewRequest("POST", "/call", nil).WithContext(ctx)
		_, trace := h.beginMCPTrace(httptest.NewRecorder(), r, MCPConnection{ID: id}, "search", "manual", conv)
		return trace
	}
	capture := func(ctx context.Context, id uuid.UUID) *mcpToolTrace {
		trace := begin(ctx, id, uuid.Nil)
		trace.captureHistoryPayload(ctx, "input", json.RawMessage(`{"query":"email bob@example.test and use SSN 123-45-6789","password":"tiny"}`))
		trace.captureHistoryPayload(ctx, "output", mustJSONString("yellow hats; token: gh"+"p_"+strings.Repeat("a", 36)))
		trace.outcome("ok", "completed", 200, "")
		trace.complete()
		return trace
	}
	read := func(ctx context.Context, id uuid.UUID) MCPHistoryResponse {
		w := request(ctx, "GET", "/mcp/traces/"+id.String()+"/content", nil)
		if w.Code != 200 {
			t.Fatalf("read %d %s", w.Code, w.Body.String())
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("history response cacheable")
		}
		var out MCPHistoryResponse
		if json.Unmarshal(w.Body.Bytes(), &out) != nil {
			t.Fatal("content JSON")
		}
		return out
	}
	trace := capture(testIdentity(cid, "employee", RoleMember), connection)
	if trace.trace.Tags["mcp.history.state"] != "retained" {
		t.Fatalf("capture failed: %+v", trace.trace.Tags)
	}
	got := read(owner, trace.trace.ID)
	if got.State != "sanitized" || got.Content == nil {
		t.Fatalf("missing history %+v", got)
	}
	raw, _ := json.Marshal(got)
	for _, secret := range []string{"bob@example.test", "123-45-6789", "tiny", "ghp_aaaaaaaa"} {
		if strings.Contains(string(raw), secret) {
			t.Fatal("sensitive fixture retained")
		}
	}
	var sealed string
	if err = s.pool.QueryRow(owner, `SELECT encrypted_content::text FROM workspace_mcp_history WHERE customer_id=$1 AND trace_id=$2`, cid, trace.trace.ID).Scan(&sealed); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sealed, "yellow hats") || strings.Contains(sealed, "bob@example.test") {
		t.Fatal("plaintext persisted")
	}
	if read(testIdentity(cid, "reviewer", RoleViewer), trace.trace.ID).State != "sanitized" {
		t.Fatal("selected viewer denied")
	}
	if read(testIdentity(cid, "peer", RoleMember), trace.trace.ID).State != "restricted" {
		t.Fatal("peer saw captured payload")
	}
	if read(testIdentity(foreign, "owner", RoleOwner), trace.trace.ID).Content != nil {
		t.Fatal("cross-tenant history read")
	}
	if capture(owner, other).trace.Tags["mcp.history.state"] == "retained" {
		t.Fatal("unselected integration captured")
	}
	if capture(WithTemporaryChat(owner, true), connection).trace.Tags["mcp.history.state"] == "retained" {
		t.Fatal("temporary chat captured")
	}
	// Current integration ACL applies in addition to the viewer grant.
	if _, err = s.pool.Exec(owner, `INSERT INTO workspace_resource_sharing(customer_id,resource_type,resource_id,restricted) VALUES($1,'mcp',$2,TRUE)`, cid, connection); err != nil {
		t.Fatal(err)
	}
	if read(testIdentity(cid, "reviewer", RoleViewer), trace.trace.ID).State != "restricted" {
		t.Fatal("history bypassed integration sharing")
	}
	if _, err = s.pool.Exec(owner, `DELETE FROM workspace_resource_sharing WHERE customer_id=$1`, cid); err != nil {
		t.Fatal(err)
	}
	if _, err = s.pool.Exec(owner, `DELETE FROM workspace_members WHERE customer_id=$1 AND user_id='reviewer'`, cid); err != nil {
		t.Fatal(err)
	}
	if _, err = s.pool.Exec(owner, `INSERT INTO workspace_members(customer_id,user_id) VALUES($1,'reviewer')`, cid); err != nil {
		t.Fatal(err)
	}
	if read(testIdentity(cid, "reviewer", RoleViewer), trace.trace.ID).State != "restricted" {
		t.Fatal("reinvitation revived a revoked grant")
	}
	p = getPolicy()
	for _, invalid := range []MCPHistoryPolicy{
		{Mode: "raw", RetentionDays: 7, Version: p.Version},
		{Mode: "sanitized", RetentionDays: 91, CaptureAll: true, Version: p.Version},
		{Mode: "sanitized", RetentionDays: 7, ConnectionIDs: []uuid.UUID{uuid.New()}, Version: p.Version},
		{Mode: "sanitized", RetentionDays: 7, CaptureAll: true, ViewerUserIDs: []string{"not-a-member"}, Version: p.Version},
		{Mode: "sanitized", RetentionDays: 7, CaptureAll: true, ViewerGroupIDs: []uuid.UUID{uuid.New()}, Version: p.Version},
	} {
		if w := request(owner, "PUT", "/mcp/history/policy", invalid); w.Code != 400 {
			t.Fatalf("invalid policy accepted: %d", w.Code)
		}
	}
	// Group reviewers lose access immediately when group membership is removed.
	group := uuid.New()
	if _, err = s.pool.Exec(owner, `INSERT INTO workspace_access_groups(id,customer_id,name) VALUES($1,$2,'Reviewers')`, group, cid); err != nil {
		t.Fatal(err)
	}
	if _, err = s.pool.Exec(owner, `INSERT INTO workspace_access_group_members(customer_id,group_id,user_id) VALUES($1,$2,'peer')`, cid, group); err != nil {
		t.Fatal(err)
	}
	p.ViewerGroupIDs = []uuid.UUID{group}
	if w := request(owner, "PUT", "/mcp/history/policy", p); w.Code != 200 {
		t.Fatalf("grant group %s", w.Body.String())
	}
	if read(testIdentity(cid, "peer", RoleMember), trace.trace.ID).State != "sanitized" {
		t.Fatal("group reviewer denied")
	}
	if _, err = s.pool.Exec(owner, `DELETE FROM workspace_access_group_members WHERE customer_id=$1 AND group_id=$2`, cid, group); err != nil {
		t.Fatal(err)
	}
	if read(testIdentity(cid, "peer", RoleMember), trace.trace.ID).State != "restricted" {
		t.Fatal("removed group reviewer retained access")
	}
	if _, err = s.pool.Exec(owner, `DELETE FROM workspace_access_groups WHERE customer_id=$1 AND id=$2`, cid, group); err != nil {
		t.Fatal(err)
	}
	if len(getPolicy().ViewerGroupIDs) != 0 {
		t.Fatal("deleted group retained policy grant")
	}
	// Tightening retention clamps existing snapshots; loosening never extends them.
	p = getPolicy()
	p.RetentionDays = 1
	if w := request(owner, "PUT", "/mcp/history/policy", p); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	shortExpiry := read(owner, trace.trace.ID).ExpiresAt
	if shortExpiry == nil || shortExpiry.After(time.Now().Add(25*time.Hour)) {
		t.Fatal("retention decrease did not clamp existing history")
	}
	p = getPolicy()
	p.RetentionDays = 14
	if w := request(owner, "PUT", "/mcp/history/policy", p); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if later := read(owner, trace.trace.ID).ExpiresAt; later == nil || !later.Equal(*shortExpiry) {
		t.Fatal("retention increase extended old history")
	}
	// Captured conversations are purged on privacy changes and deletion.
	conv, err := s.CreateConversation(owner, Conversation{CustomerID: cid, UserID: "owner", Title: "History privacy"})
	if err != nil {
		t.Fatal(err)
	}
	for _, operation := range []string{"private", "delete"} {
		convoTrace := begin(owner, connection, conv.ID)
		convoTrace.captureHistoryPayload(owner, "input", json.RawMessage(`{"query":"a jacket"}`))
		convoTrace.complete()
		if read(owner, convoTrace.trace.ID).Content == nil {
			t.Fatal("stored conversation not captured")
		}
		if operation == "private" {
			_, err = s.pool.Exec(owner, `UPDATE workspace_conversations SET content_stored=FALSE WHERE id=$1`, conv.ID)
		} else {
			_, err = s.pool.Exec(owner, `DELETE FROM workspace_conversations WHERE id=$1`, conv.ID)
		}
		if err != nil {
			t.Fatal(err)
		}
		if read(owner, convoTrace.trace.ID).Content != nil {
			t.Fatal("private/deleted conversation history remained readable")
		}
		if operation == "private" {
			_, err = s.pool.Exec(owner, `UPDATE workspace_conversations SET content_stored=TRUE WHERE id=$1`, conv.ID)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	// In-flight captures cannot recreate cleared history.
	pending := begin(owner, connection, uuid.Nil)
	pending.captureHistoryPayload(owner, "input", json.RawMessage(`{"query":"a jacket"}`))
	if w := request(owner, "DELETE", "/mcp/history", nil); w.Code != 204 {
		t.Fatal("delete history failed")
	}
	pending.complete()
	if read(owner, pending.trace.ID).Content != nil {
		t.Fatal("in-flight capture recreated deleted history")
	}
	// No-store beats capture and invalidates requests across off/on toggles.
	pending = begin(owner, connection, uuid.Nil)
	pending.captureHistoryPayload(owner, "input", json.RawMessage(`{"query":"a jacket"}`))
	off, on := false, true
	if _, err = s.UpdateSettings(owner, cid, SettingsPatch{StoreConversationContent: &off}); err != nil {
		t.Fatal(err)
	}
	if capture(owner, connection).trace.Tags["mcp.history.reason"] != "workspace_no_store" {
		t.Fatal("no-storage workspace captured history")
	}
	p = getPolicy()
	if w := request(owner, "PUT", "/mcp/history/policy", p); w.Code != 400 {
		t.Fatal("capture enabled under no-storage policy")
	}
	if _, err = s.UpdateSettings(owner, cid, SettingsPatch{StoreConversationContent: &on}); err != nil {
		t.Fatal(err)
	}
	pending.complete()
	if read(owner, pending.trace.ID).Content != nil {
		t.Fatal("privacy toggle did not invalidate capture")
	}
	trace = capture(owner, connection)
	if _, err = s.pool.Exec(owner, `UPDATE workspace_mcp_history SET expires_at=now()-interval '1 second' WHERE customer_id=$1`, cid); err != nil {
		t.Fatal(err)
	}
	if read(owner, trace.trace.ID).State != "expired" {
		t.Fatal("expired payload readable")
	}
	cleanup, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { h.RunMCPHistoryMaintenance(cleanup); close(done) }()
	deadline := time.Now().Add(3 * time.Second)
	var count int
	for time.Now().Before(deadline) {
		s.pool.QueryRow(owner, `SELECT count(*) FROM workspace_mcp_history WHERE customer_id=$1`, cid).Scan(&count)
		if count == 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done
	if count != 0 {
		t.Fatal("expiry did not physically delete ciphertext")
	}
	// Ciphertexts cannot be moved to a different trace, even within one tenant.
	first, second := capture(owner, connection), capture(owner, connection)
	if _, err = s.pool.Exec(owner, `UPDATE workspace_mcp_history SET encrypted_content=(SELECT encrypted_content FROM workspace_mcp_history WHERE customer_id=$1 AND trace_id=$2) WHERE customer_id=$1 AND trace_id=$3`, cid, first.trace.ID, second.trace.ID); err != nil {
		t.Fatal(err)
	}
	if w := request(owner, "GET", "/mcp/traces/"+second.trace.ID.String()+"/content", nil); w.Code != 503 || strings.Contains(w.Body.String(), "yellow hats") {
		t.Fatal("ciphertext was not trace-bound")
	}
	// Revoking an integration physically purges its captured content.
	p = getPolicy()
	p.CaptureAll = true
	if w := request(owner, "PUT", "/mcp/history/policy", p); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	revoked := capture(owner, other)
	if read(owner, revoked.trace.ID).Content == nil {
		t.Fatal("all-integrations capture omitted an active integration")
	}
	if _, err = s.pool.Exec(owner, `UPDATE workspace_mcp_connections SET revoked_at=now() WHERE customer_id=$1 AND id=$2`, cid, other); err != nil {
		t.Fatal(err)
	}
	if err = s.pool.QueryRow(owner, `SELECT count(*) FROM workspace_mcp_history WHERE customer_id=$1 AND connection_id=$2`, cid, other).Scan(&count); err != nil || count != 0 {
		t.Fatal("revoked integration retained ciphertext")
	}
	// Disabling capture purges stored content.
	trace = capture(owner, connection)
	p = getPolicy()
	p.Mode = "metadata_only"
	if w := request(owner, "PUT", "/mcp/history/policy", p); w.Code != 200 {
		t.Fatalf("disable %s", w.Body.String())
	}
	if read(owner, trace.trace.ID).Content != nil {
		t.Fatal("disabled history still readable")
	}
}
