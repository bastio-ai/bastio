package workspace

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bastio-ai/bastio/internal/security"
	"github.com/bastio-ai/bastio/internal/security/detection"
	"github.com/google/uuid"
)

func TestWorkspaceResourceSharingIntegration(t *testing.T) {
	s := workspaceTestStore(t)
	cid, other := uuid.New(), uuid.New()
	admin := testIdentity(cid, "manager", RoleAdmin)
	for _, id := range []uuid.UUID{cid, other} {
		if _, err := s.pool.Exec(admin, `INSERT INTO customers(id,name,slug) VALUES($1,'ACL test',$2)`, id, id.String()); err != nil {
			t.Fatal(err)
		}
		if _, err := s.EnsureSettings(admin, id); err != nil {
			t.Fatal(err)
		}
	}
	for _, uid := range []string{"manager", "ann", "peter", "adam", "eve"} {
		role := "member"
		if uid == "manager" {
			role = "admin"
		}
		if _, err := s.pool.Exec(admin, `INSERT INTO workspace_members(customer_id,user_id,email,role) VALUES($1,$2,$3,$4)`, cid, uid, uid+"@example.test", role); err != nil {
			t.Fatal(err)
		}
	}
	legal, marketing := uuid.New(), uuid.New()
	for _, entry := range []struct {
		id   uuid.UUID
		name string
	}{{legal, "Legal"}, {marketing, "Marketing"}} {
		if _, err := s.pool.Exec(admin, `INSERT INTO workspace_access_groups(id,customer_id,name) VALUES($1,$2,$3)`, entry.id, cid, entry.name); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.pool.Exec(admin, `INSERT INTO workspace_access_group_members(customer_id,group_id,user_id) VALUES($1,$2,'ann'),($1,$3,'adam')`, cid, legal, marketing); err != nil {
		t.Fatal(err)
	}
	restricted := &ResourceSharing{Restricted: true, GroupIDs: []uuid.UUID{legal}, UserIDs: []string{"peter"}}
	content := "The privileged legal review protocol requires counsel approval"
	k, err := s.CreateKnowledgeSource(admin, KnowledgeSource{CustomerID: cid, Name: "Legal memo", Type: "text", InlineText: &content, Status: "ready", Sharing: restricted})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.pool.Exec(admin, `INSERT INTO workspace_knowledge_chunks(customer_id,knowledge_source_id,ordinal,content) VALUES($1,$2,0,$3)`, cid, k.ID, content); err != nil {
		t.Fatal(err)
	}
	a, err := s.CreateAssistant(admin, Assistant{CustomerID: cid, Name: "Legal assistant", DefaultProvider: "openai", DefaultModel: "gpt-6.1-sol", KnowledgeSourceIDs: []uuid.UUID{k.ID}, Sharing: restricted})
	if err != nil {
		t.Fatal(err)
	}
	for _, resource := range []struct {
		kind string
		id   uuid.UUID
	}{{"knowledge", k.ID}, {"assistant", a.ID}} {
		for _, uid := range []string{"ann", "peter"} {
			allowed, e := s.canAccess(testIdentity(cid, uid, RoleMember), cid, resource.id, resource.kind)
			if e != nil || !allowed {
				t.Fatalf("%s must be available to %s: %v", resource.kind, uid, e)
			}
		}
		for _, uid := range []string{"adam", "eve"} {
			allowed, e := s.canAccess(testIdentity(cid, uid, RoleMember), cid, resource.id, resource.kind)
			if e != nil || allowed {
				t.Fatalf("%s leaked to %s: %v", resource.kind, uid, e)
			}
		}
		allowed, e := s.canAccess(admin, cid, resource.id, resource.kind)
		if e != nil || !allowed {
			t.Fatalf("admin lost %s", resource.kind)
		}
		allowed, e = s.canAccess(testIdentity(other, "peter", RoleMember), other, resource.id, resource.kind)
		if e != nil || allowed {
			t.Fatalf("cross-tenant %s leak", resource.kind)
		}
	}
	for _, uid := range []string{"ann", "peter", "adam"} {
		hits, e := s.SearchRelevantChunks(testIdentity(cid, uid, RoleMember), cid, a.ID, "privileged legal protocol")
		if e != nil || (len(hits) > 0) != (uid != "adam") {
			t.Fatalf("search ACL for %s: %d hits, %v", uid, len(hits), e)
		}
	}
	h := NewHandler(s, nil, nil)
	profile := security.DefaultProfile()
	h.SetSecurityProfiles(&staticProfileLookup{profile: &profile})
	h.SetSecurityEngine(security.NewEngine(detection.NewPIIDetector(), detection.NewIndirectInjectionDetector()))
	forwarded := 0
	h.mcpHTTP = &http.Client{Transport: workspaceMCPTransport(func(r *http.Request) (*http.Response, error) {
		var rpc struct {
			Method string `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&rpc)
		result := `{"tools":[{"name":"search","description":"Search a handbook","inputSchema":{"type":"object"}}]}`
		if rpc.Method == "tools/call" {
			forwarded++
			result = `{"content":[{"type":"text","text":"Reviewed answer"}]}`
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"jsonrpc":"2.0","id":1,"result":` + result + `}`))}, nil
	})}
	router := h.Routes()
	request := func(method, path, body, uid string, role Role) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body)).WithContext(testIdentity(cid, uid, role))
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	if w := request("POST", "/conversations", `{"assistant_id":"`+a.ID.String()+`"}`, "adam", RoleMember); w.Code != 404 {
		t.Fatalf("restricted assistant conversation: %d %s", w.Code, w.Body.String())
	}
	granted := request("POST", "/conversations", `{"assistant_id":"`+a.ID.String()+`"}`, "peter", RoleMember)
	if granted.Code != 201 {
		t.Fatalf("allowed assistant conversation: %d %s", granted.Code, granted.Body.String())
	}
	var conv Conversation
	if json.Unmarshal(granted.Body.Bytes(), &conv) != nil {
		t.Fatal("conversation response")
	}
	// A revoked direct grant also disables an already-created conversation.
	empty := `{"type":"assistant","id":"` + a.ID.String() + `","restricted":true,"group_ids":["` + legal.String() + `"],"user_ids":[]}`
	if w := request("PUT", "/access/resources", empty, "manager", RoleAdmin); w.Code != 200 {
		t.Fatalf("ACL update: %d %s", w.Code, w.Body.String())
	}
	if _, e := h.prepareChat(httptest.NewRequest("POST", "/", nil).WithContext(testIdentity(cid, "peter", RoleMember)), &conv, SendMessageRequest{Content: "legal question"}); e == nil {
		t.Fatal("existing conversation retained revoked assistant access")
	}

	shared := `{"name":"Legal MCP","endpoint":"https://tools.example.com/mcp","scope":"workspace","sharing":{"restricted":true,"group_ids":["` + legal.String() + `"],"user_ids":["peter"]}}`
	created := request("POST", "/mcp/connections", shared, "manager", RoleAdmin)
	if created.Code != 201 {
		t.Fatalf("MCP create: %d %s", created.Code, created.Body.String())
	}
	var connection MCPConnection
	if json.Unmarshal(created.Body.Bytes(), &connection) != nil {
		t.Fatal("MCP response")
	}
	allowedCall := request("POST", "/mcp/connections/"+connection.ID.String()+"/call", `{"name":"search","arguments":{}}`, "ann", RoleMember)
	if allowedCall.Code != 200 || forwarded != 1 {
		t.Fatalf("selected group could not run tool: %d %s", allowedCall.Code, allowedCall.Body.String())
	}
	for _, entry := range []struct {
		uid     string
		visible bool
	}{{"ann", true}, {"peter", true}, {"adam", false}, {"eve", false}} {
		listed := request("GET", "/mcp", "", entry.uid, RoleMember)
		if listed.Code != 200 || strings.Contains(listed.Body.String(), "Legal MCP") != entry.visible {
			t.Fatalf("MCP discovery for %s: %d %s", entry.uid, listed.Code, listed.Body.String())
		}
		tools := request("GET", "/mcp/connections/"+connection.ID.String()+"/tools", "", entry.uid, RoleMember)
		if !entry.visible && tools.Code != 404 {
			t.Fatalf("MCP tool discovery leak for %s: %d", entry.uid, tools.Code)
		}
		call := request("POST", "/mcp/connections/"+connection.ID.String()+"/call", `{"name":"search","arguments":{}}`, entry.uid, RoleMember)
		if !entry.visible && call.Code != 404 {
			t.Fatalf("MCP call leak for %s: %d", entry.uid, call.Code)
		}
	}
	// No recipients is still restricted. Invalid recipient creation is atomic.
	locked := `{"type":"knowledge","id":"` + k.ID.String() + `","restricted":true,"group_ids":[],"user_ids":[]}`
	if w := request("PUT", "/access/resources", locked, "manager", RoleAdmin); w.Code != 200 {
		t.Fatalf("empty ACL: %d", w.Code)
	}
	if allowed, e := s.canAccess(testIdentity(cid, "ann", RoleMember), cid, k.ID, "knowledge"); e != nil || allowed {
		t.Fatal("empty restriction exposed knowledge")
	}
	policy, e := s.resourceSharing(admin, cid, k.ID, "knowledge")
	if e != nil || !policy.Restricted || len(policy.UserIDs) != 0 {
		t.Fatal("empty restriction was lost")
	}
	if _, e = s.CreateKnowledgeSource(admin, KnowledgeSource{CustomerID: cid, Name: "Invalid ACL source", Type: "text", Status: "ready", Sharing: &ResourceSharing{Restricted: true, UserIDs: []string{"outsider"}}}); e == nil {
		t.Fatal("nonmember grant accepted")
	}
	var count int
	if e = s.pool.QueryRow(admin, `SELECT count(*) FROM workspace_knowledge_sources WHERE customer_id=$1 AND name='Invalid ACL source'`, cid).Scan(&count); e != nil || count != 0 {
		t.Fatal("failed sharing left a public source")
	}
	// Group grants follow membership changes; direct grants are removed by the
	// member-deletion trigger and do not return when that identity rejoins.
	if e = s.RemoveMember(admin, cid, "peter"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(admin, `INSERT INTO workspace_members(customer_id,user_id,email,role) VALUES($1,'peter','peter@example.test','member')`, cid); e != nil {
		t.Fatal(e)
	}
	if allowed, e := s.canAccess(testIdentity(cid, "peter", RoleMember), cid, connection.ID, "mcp"); e != nil || allowed {
		t.Fatal("removed member retained direct MCP grant")
	}
	if _, e = s.pool.Exec(admin, `DELETE FROM workspace_access_group_members WHERE customer_id=$1 AND group_id=$2 AND user_id='ann'`, cid, legal); e != nil {
		t.Fatal(e)
	}
	if allowed, e := s.canAccess(testIdentity(cid, "ann", RoleMember), cid, connection.ID, "mcp"); e != nil || allowed {
		t.Fatal("removed legal-group member retained MCP access")
	}
	forwardedBefore := forwarded
	deniedCall := request("POST", "/mcp/connections/"+connection.ID.String()+"/call", `{"name":"search","arguments":{}}`, "ann", RoleMember)
	if deniedCall.Code != 404 || forwarded != forwardedBefore {
		t.Fatal("revoked group member reached remote MCP tool")
	}
}
