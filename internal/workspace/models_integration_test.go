package workspace

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/bastio-ai/bastio/internal/providers"
	"github.com/google/uuid"
)

type readinessKeys struct{ key string }

func (k *readinessKeys) ResolveKey(_ context.Context, _ uuid.UUID, provider string) (string, error) {
	if provider == "openai" {
		return k.key, nil
	}
	return "", nil
}
func TestWorkspaceModelsCredentialRotationPolicyAndRealTestPipeline(t *testing.T) {
	store := workspaceTestStore(t)
	cid := uuid.New()
	admin := testIdentity(cid, "manager", RoleAdmin)
	if _, err := store.pool.Exec(admin, `INSERT INTO customers(id,name,slug) VALUES($1,'Model setup test',$2)`, cid, cid.String()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.EnsureSettings(admin, cid); err != nil {
		t.Fatal(err)
	}
	var inference, discovery atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/models":
			discovery.Add(1)
			if r.Header.Get("Authorization") == "Bearer rotated" {
				io.WriteString(w, `{"data":[{"id":"gpt-4o-mini"}]}`)
			} else {
				io.WriteString(w, `{"data":[{"id":"gpt-4.1-nano"},{"id":"gpt-4o-mini"},{"id":"gpt-5-pro"},{"id":"gpt-live-1"},{"id":"text-embedding-3-small"}]}`)
			}
		case "/chat/completions":
			inference.Add(1)
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			if body["max_completion_tokens"] != float64(512) {
				t.Fatal("test output is not bounded")
			}
			io.WriteString(w, `{"id":"reply","model":"gpt-4.1-nano","choices":[{"message":{"role":"assistant","content":"Hello robot"},"finish_reason":"stop"}],"usage":{"prompt_tokens":20,"completion_tokens":5}}`)
		default:
			t.Fatalf("unexpected provider route %s", r.URL.Path)
		}
	}))
	defer upstream.Close()
	registry := providers.NewRegistry()
	registry.Register(providers.NewOpenAIClientWithBaseURL(upstream.URL))
	keys := &readinessKeys{key: "original"}
	h := NewHandler(store, registry, keys)
	routes := h.Routes()
	call := func(method, path, body string, ctx context.Context) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(ctx)
		out := httptest.NewRecorder()
		routes.ServeHTTP(out, req)
		return out
	}
	listed := call("GET", "/me/models?manage=true", "", admin)
	if listed.Code != 200 {
		t.Fatalf("discovery %d %s", listed.Code, listed.Body.String())
	}
	if strings.Contains(listed.Body.String(), "gpt-5-pro") || strings.Contains(listed.Body.String(), "text-embedding") || strings.Contains(listed.Body.String(), "original") {
		t.Fatal("non-chat model or credential leaked")
	}
	call("GET", "/me/models?manage=true", "", admin)
	if discovery.Load() != 1 {
		t.Fatalf("provider model inventory not cached, calls=%d, listing=%s", discovery.Load(), listed.Body.String())
	}
	tested := call("POST", "/models/test", `{"provider":"openai","model":"gpt-4.1-nano"}`, admin)
	if tested.Code != 200 || !strings.Contains(tested.Body.String(), `"success":true`) || inference.Load() != 1 {
		t.Fatalf("test %d %s", tested.Code, tested.Body.String())
	}
	var storedBodies, visible int
	store.pool.QueryRow(admin, `SELECT count(*) FROM workspace_messages WHERE customer_id=$1 AND (content<>'' OR metadata<>'{}'::jsonb)`, cid).Scan(&storedBodies)
	if storedBodies != 0 {
		t.Fatal("model test content persisted")
	}
	chats, err := store.ListConversations(admin, cid, "manager", 50)
	if err != nil || len(chats) != 0 {
		t.Fatal("test polluted conversation history")
	}
	tokens, messages, err := store.MemberUsage(admin, cid, "manager")
	if err != nil || tokens != 25 || messages != 1 {
		t.Fatal("test bypassed usage accounting")
	}
	allowed := []AllowedModel{{Provider: "openai", Model: "gpt-4.1-nano"}}
	if _, err = store.UpdateSettings(admin, cid, SettingsPatch{AllowedModels: &allowed}); err != nil {
		t.Fatal(err)
	}
	employee := testIdentity(cid, "employee", RoleMember)
	restricted := call("GET", "/me/models", "", employee)
	if restricted.Code != 200 || strings.Contains(restricted.Body.String(), `"id":"gpt-4o-mini"`) || !strings.Contains(restricted.Body.String(), `"check_status":"passed"`) {
		t.Fatalf("member policy/evidence wrong: %s", restricted.Body.String())
	}
	if got := call("POST", "/models/test", `{"provider":"openai","model":"gpt-4o-mini"}`, employee); got.Code != 403 {
		t.Fatal("employee can run admin model tests")
	}
	keys.key = "rotated"
	rotated := call("GET", "/me/models?manage=true", "", admin)
	if strings.Contains(rotated.Body.String(), `"check_status":"passed"`) || discovery.Load() != 2 {
		t.Fatal("rotation retained stale test evidence")
	}
	other := testIdentity(uuid.New(), "manager", RoleAdmin)
	otherID := customerIDFromCtx(other)
	store.pool.Exec(other, `INSERT INTO customers(id,name,slug) VALUES($1,'Other setup',$2)`, otherID, otherID.String())
	store.EnsureSettings(other, otherID)
	otherList := call("GET", "/me/models?manage=true", "", other)
	if otherList.Code != 200 || strings.Contains(otherList.Body.String(), `"check_status":"passed"`) {
		t.Fatal("another workspace saw test evidence")
	}
	store.pool.QueryRow(admin, `SELECT count(*) FROM workspace_conversations WHERE customer_id=$1 AND is_setup_test`, cid).Scan(&visible)
	if visible != 1 {
		t.Fatal("operational test record missing")
	}
}
func TestWorkspaceSetupPrivacyAndRecipientPreview(t *testing.T) {
	store := workspaceTestStore(t)
	cid := uuid.New()
	admin := testIdentity(cid, "manager", RoleAdmin)
	if _, err := store.pool.Exec(admin, `INSERT INTO customers(id,name,slug) VALUES($1,'Model setup test',$2)`, cid, cid.String()); err != nil {
		t.Fatal(err)
	}
	settings, err := store.EnsureSettings(admin, cid)
	if err != nil {
		t.Fatal(err)
	}
	for _, uid := range []string{"ann", "adam"} {
		if _, err = store.pool.Exec(admin, `INSERT INTO workspace_members(customer_id,user_id,email,role) VALUES($1,$2,$3,'member')`, cid, uid, uid+"@example.test"); err != nil {
			t.Fatal(err)
		}
	}
	legal := uuid.New()
	store.pool.Exec(admin, `INSERT INTO workspace_access_groups(id,customer_id,name) VALUES($1,$2,'Legal')`, legal, cid)
	store.pool.Exec(admin, `INSERT INTO workspace_access_group_members VALUES($1,$2,'ann')`, cid, legal)
	sharing := &ResourceSharing{Restricted: true, GroupIDs: []uuid.UUID{legal}, UserIDs: []string{}}
	text := "Legal policy approved by counsel"
	source, err := store.CreateKnowledgeSource(admin, KnowledgeSource{CustomerID: cid, Name: "Legal policy", Type: "text", InlineText: &text, Status: "ready", Sharing: sharing})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.CreateAssistant(admin, Assistant{CustomerID: cid, Name: "Legal helper", DefaultProvider: "openai", DefaultModel: "gpt-4.1-nano", KnowledgeSourceIDs: []uuid.UUID{source.ID}, Sharing: sharing})
	if err != nil {
		t.Fatal(err)
	}
	h := NewHandler(store, providers.NewRegistry(), nil)
	call := func(method, path, body string, ctx context.Context) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(ctx)
		out := httptest.NewRecorder()
		h.Routes().ServeHTTP(out, req)
		return out
	}
	ann := call("GET", "/setup/preview?user_id=ann", "", admin)
	adam := call("GET", "/setup/preview?user_id=adam", "", admin)
	if ann.Code != 200 || !strings.Contains(ann.Body.String(), "Legal policy") || !strings.Contains(ann.Body.String(), "Legal helper") || strings.Contains(adam.Body.String(), "Legal policy") || strings.Contains(adam.Body.String(), "Legal helper") {
		t.Fatalf("recipient preview leaked scope: %s / %s", ann.Body.String(), adam.Body.String())
	}
	if out := call("GET", "/setup/preview?user_id=ann", "", testIdentity(uuid.New(), "manager", RoleAdmin)); out.Code != 404 {
		t.Fatal("preview crossed tenant boundary")
	}
	if out := call("GET", "/setup/preview?user_id=adam", "", testIdentity(cid, "ann", RoleMember)); out.Code != 403 {
		t.Fatal("employee can preview other members")
	}
	raw, _ := json.Marshal(privacyReview(settings))
	if out := call("POST", "/setup/review-privacy", string(raw), admin); out.Code != 200 {
		t.Fatal("privacy review failed")
	}
	status := call("GET", "/setup", "", admin)
	if status.Code != 200 || !strings.Contains(status.Body.String(), `"id":"privacy","complete":true`) || strings.Contains(status.Body.String(), `"ready":true`) {
		t.Fatalf("incorrect setup status %s", status.Body.String())
	}
	off := false
	store.UpdateSettings(admin, cid, SettingsPatch{StoreConversationContent: &off})
	status = call("GET", "/setup", "", admin)
	if !strings.Contains(status.Body.String(), `"id":"privacy","complete":false`) {
		t.Fatal("changing policy retained old review")
	}
	if out := call("POST", "/setup/review-privacy", string(raw), admin); out.Code != 409 {
		t.Fatal("stale privacy review accepted")
	}
}
