package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bastio-ai/bastio/internal/auth"
	"github.com/bastio-ai/bastio/internal/batchstate"
	"github.com/bastio-ai/bastio/internal/providers"
	"github.com/bastio-ai/bastio/internal/security"
	"github.com/bastio-ai/bastio/internal/security/detection"
	"github.com/bastio-ai/bastio/pkg/database"
	"github.com/bastio-ai/bastio/pkg/encryption"
	"github.com/bastio-ai/bastio/pkg/gatewaypolicy"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type batchFixture struct {
	mu       sync.Mutex
	files    map[string][]byte
	jobs     map[string]json.RawMessage
	results  map[string][]byte
	received [][]byte
	serial   int
	url      string
	corrupt  bool
	freshPII bool
}

func (f *batchFixture) next(prefix string) string {
	f.serial++
	return fmt.Sprintf("%s%d", prefix, f.serial)
}
func (f *batchFixture) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if r.Header.Get("Authorization") == "Bearer sk-bastio-test" || r.Header.Get("X-Goog-Api-Key") == "sk-bastio-test" {
		http.Error(w, "Bastio credential leaked upstream", 500)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/v1beta")
	if path == "/files" && r.Method == "POST" {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		defer r.MultipartForm.RemoveAll()
		file, _, err := r.FormFile("file")
		if err != nil {
			http.Error(w, "missing file", 400)
			return
		}
		defer file.Close()
		data, _ := io.ReadAll(file)
		id := f.next("file-")
		f.files[id] = data
		f.received = append(f.received, data)
		fmt.Fprintf(w, `{"id":%q,"object":"file","purpose":"batch","bytes":%d,"filename":"batch.jsonl","created_at":1}`, id, len(data))
		return
	}
	if r.URL.Path == "/upload/v1beta/files" {
		if r.Header.Get("X-Goog-Upload-Command") == "start" {
			w.Header().Set("X-Goog-Upload-URL", f.url+"/upload/v1beta/files?upload_id=fixture")
			return
		}
		data, _ := io.ReadAll(r.Body)
		id := f.next("files/google-")
		f.files[id] = data
		f.received = append(f.received, data)
		fmt.Fprintf(w, `{"file":{"name":%q,"mimeType":"application/jsonl","sizeBytes":%q,"uri":%q,"state":"ACTIVE"}}`, id, fmt.Sprint(len(data)), f.url+"/v1beta/"+id)
		return
	}
	if r.Method == "POST" && (path == "/batches" || path == "/messages/batches" || strings.Contains(path, ":batchGenerateContent") || strings.Contains(path, ":asyncBatchEmbedContent")) {
		raw, _ := io.ReadAll(r.Body)
		f.received = append(f.received, raw)
		var obj map[string]json.RawMessage
		_ = json.Unmarshal(raw, &obj)
		if path == "/batches" {
			id := f.next("batch-")
			file := f.next("file-output-")
			var output bytes.Buffer
			_ = walkBatchJSONL(bytes.NewReader(f.files[batchString(obj, "input_file_id")]), io.Discard, func(line []byte, _ int) ([]byte, error) {
				var item map[string]json.RawMessage
				_ = json.Unmarshal(line, &item)
				text := InputText(item["body"])
				result := map[string]json.RawMessage{"custom_id": item["custom_id"], "response": json.RawMessage(fmt.Sprintf(`{"status_code":200,"body":{"model":"gpt-4o-mini","choices":[{"message":{"role":"assistant","content":%q}}],"usage":{"prompt_tokens":2,"completion_tokens":3}}}`, text))}
				next, _ := json.Marshal(result)
				output.Write(next)
				output.WriteByte('\n')
				return line, nil
			})
			f.files[file] = output.Bytes()
			job := json.RawMessage(fmt.Sprintf(`{"id":%q,"object":"batch","endpoint":"/v1/chat/completions","input_file_id":%q,"status":"completed","output_file_id":%q,"completion_window":"24h","created_at":1,"request_counts":{"total":1,"completed":1,"failed":0}}`, id, batchString(obj, "input_file_id"), file))
			f.jobs[id] = job
			w.Write(job)
			return
		}
		if path == "/messages/batches" {
			id := f.next("msgbatch-")
			var items []map[string]json.RawMessage
			_ = json.Unmarshal(obj["requests"], &items)
			var output bytes.Buffer
			for i := len(items) - 1; i >= 0; i-- {
				item := items[i]
				text := InputText(item["params"])
				fmt.Fprintf(&output, `{"custom_id":%s,"result":{"type":"succeeded","message":{"id":"msg-fixture","type":"message","role":"assistant","model":"claude-sonnet-4-5","content":[{"type":"text","text":%q}],"usage":{"input_tokens":2,"output_tokens":3},"stop_reason":"end_turn"}}}`+"\n", item["custom_id"], text)
			}
			f.results[id] = output.Bytes()
			job := json.RawMessage(fmt.Sprintf(`{"id":%q,"type":"message_batch","processing_status":"ended","request_counts":{"processing":0,"succeeded":%d,"errored":0,"canceled":0,"expired":0},"created_at":"2026-10-17T00:00:00Z","ended_at":"2026-10-17T00:00:01Z","expires_at":"2026-11-01T00:00:00Z","cancel_initiated_at":null,"results_url":%q}`, id, len(items), f.url+"/messages/batches/"+id+"/results"))
			f.jobs[id] = job
			w.Write(job)
			return
		}
		batch, _ := googleObject(obj, "batch", "batch")
		config, _ := googleObject(batch, "inputConfig", "input_config")
		var inputs []json.RawMessage
		file := batchString(config, "fileName", "file_name")
		if file != "" {
			_ = walkBatchJSONL(bytes.NewReader(f.files[file]), io.Discard, func(line []byte, _ int) ([]byte, error) {
				var item map[string]json.RawMessage
				_ = json.Unmarshal(line, &item)
				inputs = append(inputs, item["request"])
				return line, nil
			})
		} else {
			requests, _ := googleObject(config, "requests", "requests")
			var items []map[string]json.RawMessage
			_ = json.Unmarshal(requests["requests"], &items)
			for _, item := range items {
				inputs = append(inputs, item["request"])
			}
		}
		var results []json.RawMessage
		var jsonl bytes.Buffer
		for _, input := range inputs {
			text := InputText(input)
			result := json.RawMessage(fmt.Sprintf(`{"candidates":[{"content":{"role":"model","parts":[{"text":%q}]}}],"usageMetadata":{"promptTokenCount":2,"candidatesTokenCount":3},"modelVersion":"gemini-2.5-flash"}`, text))
			if strings.Contains(path, ":asyncBatchEmbedContent") {
				result = json.RawMessage(`{"embedding":{"values":[0.1,0.2]},"usageMetadata":{"promptTokenCount":2}}`)
			}
			results = append(results, json.RawMessage(`{"response":`+string(result)+`}`))
			jsonl.Write(result)
			jsonl.WriteByte('\n')
		}
		response := map[string]json.RawMessage{}
		if file != "" {
			out := f.next("files/output-")
			f.files[out] = jsonl.Bytes()
			response["responsesFile"], _ = json.Marshal(out)
		} else {
			list, _ := json.Marshal(results)
			response["inlinedResponses"] = json.RawMessage(`{"inlinedResponses":` + string(list) + `}`)
		}
		id := f.next("batches/google-")
		body, _ := json.Marshal(response)
		job := json.RawMessage(fmt.Sprintf(`{"name":%q,"done":true,"metadata":{"state":"BATCH_STATE_SUCCEEDED","model":"models/gemini-2.5-flash","output":%s},"response":%s}`, id, body, body))
		f.jobs[id] = job
		w.Write(job)
		return
	}
	if strings.HasSuffix(path, "/results") {
		id := strings.TrimSuffix(strings.TrimPrefix(path, "/messages/batches/"), "/results")
		data := f.results[id]
		if f.corrupt {
			data = append(append([]byte(nil), data...), []byte("broken\n")...)
		}
		if f.freshPII {
			data = []byte(`{"custom_id":"two","result":{"type":"succeeded","message":{"content":[{"type":"text","text":"allowed-prefix-sentinel"}]}}}` + "\n" + `{"custom_id":"one","result":{"type":"succeeded","message":{"content":[{"type":"text","text":"new@example.test"}]}}}` + "\n")
		}
		w.Header().Set("Content-Type", "application/jsonl")
		w.Write(data)
		return
	}
	if strings.HasSuffix(path, "/content") || strings.HasPrefix(path, "/download/") {
		id := strings.TrimSuffix(strings.TrimPrefix(path, "/files/"), "/content")
		if strings.HasPrefix(path, "/download/") {
			id = strings.TrimSuffix(strings.TrimPrefix(path, "/download/v1beta/"), ":download")
		}
		w.Header().Set("Content-Type", "application/jsonl")
		w.Write(f.files[id])
		return
	}
	if path == "/batches" || path == "/messages/batches" {
		var list []json.RawMessage
		for _, job := range f.jobs {
			list = append(list, job)
		}
		list = append(list, json.RawMessage(`{"id":"not-this-tenant"}`))
		_ = json.NewEncoder(w).Encode(struct {
			Data    []json.RawMessage `json:"data"`
			HasMore bool              `json:"has_more"`
		}{Data: list})
		return
	}
	id, kind := batchResourcePath(providers.ProviderOpenAI, path)
	if strings.HasPrefix(path, "/messages/") {
		id, kind = batchResourcePath(providers.ProviderAnthropic, path)
	}
	if strings.HasPrefix(path, "/batches/google-") {
		id, kind = batchResourcePath(providers.ProviderGemini, path)
	}
	if kind == "job" {
		if job, ok := f.jobs[id]; ok {
			if r.Method == http.MethodDelete {
				delete(f.jobs, id)
				w.WriteHeader(204)
				return
			}
			w.Write(job)
			return
		}
	}
	if kind == "file" {
		fmt.Fprintf(w, `{"id":%q,"deleted":true}`, id)
		return
	}
	http.Error(w, "fixture route missing: "+r.Method+" "+r.URL.Path, 404)
}

func gatewayBatchStore(t *testing.T) (*batchstate.Store, *pgxpool.Pool) {
	t.Helper()
	dsn := os.Getenv("MCP_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("disposable MCP_TEST_DATABASE_URL required")
	}
	ctx := context.Background()
	root, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(root.Close)
	name := "gateway_batches_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	ident := pgx.Identifier{name}.Sanitize()
	if _, err = root.Exec(ctx, "CREATE DATABASE "+ident); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = root.Exec(context.Background(), "DROP DATABASE "+ident+" WITH (FORCE)") })
	cfg, _ := pgxpool.ParseConfig(dsn)
	cfg.ConnConfig.Database = name
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err = (&database.DB{Pool: pool}).Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	cipher, _ := encryption.New("")
	return batchstate.New(pool, cipher, 30*24*time.Hour), pool
}

func TestProtectedNativeBatchWorkflows(t *testing.T) {
	store, pool := gatewayBatchStore(t)
	cid, other := uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{cid, other} {
		if _, err := pool.Exec(context.Background(), `INSERT INTO customers(id,name,slug) VALUES($1,'batch test',$2)`, id, id.String()); err != nil {
			t.Fatal(err)
		}
	}
	fixture := &batchFixture{files: map[string][]byte{}, jobs: map[string]json.RawMessage{}, results: map[string][]byte{}}
	upstream := httptest.NewServer(fixture)
	defer upstream.Close()
	fixture.url = upstream.URL
	registry := providers.NewRegistry()
	registry.Register(providers.NewOpenAIClientWithBaseURL(upstream.URL))
	registry.Register(providers.NewAnthropicClientWithBaseURL(upstream.URL))
	registry.Register(providers.NewGeminiClientWithBaseURL(upstream.URL + "/v1beta"))
	profile := security.DefaultProfile()
	profile.PIIAction = security.ActionTokenize
	profile.PIIRestoreResponse = true
	handler := NewHandler(registry, nil, security.NewEngine(detection.NewPIIDetector(), detection.NewSecretsDetector()), &nativeTestProfile{&profile}, nil, "fail-closed")
	handler.SetBatchStore(store)
	handler.SetNativeKeyResolver(func(r *http.Request, _ providers.Provider) string {
		if r.Header.Get("Test-Credential") != "" {
			return "different-provider-credential"
		}
		return "fixture-provider-secret"
	})
	router := chi.NewRouter()
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := cid
			if r.Header.Get("Test-Tenant") == "other" {
				id = other
			}
			ctx := auth.WithInfo(r.Context(), &auth.APIKeyInfo{CustomerID: id})
			if r.Header.Get("Test-Monitor") == "true" {
				ctx = gatewaypolicy.With(ctx, gatewaypolicy.Policy{MonitorOnly: true})
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	})
	router.Route("/v1", handler.RegisterNativeRoutes)
	call := func(method, path, body string, headers ...string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		for i := 0; i < len(headers); i += 2 {
			r.Header.Set(headers[i], headers[i+1])
		}
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, r)
		return rr
	}
	t.Run("Claude out of order, restart, policy, tenant and credential", func(t *testing.T) {
		body := `{"requests":[{"custom_id":"one","params":{"model":"claude-sonnet-4-5","max_tokens":10,"messages":[{"role":"user","content":"first@example.test"}]}},{"custom_id":"two","params":{"model":"claude-sonnet-4-5","max_tokens":10,"messages":[{"role":"user","content":"second@example.test"}]}}]}`
		rr := call("POST", "/v1/anthropic/v1/messages/batches", body)
		if rr.Code != 200 {
			t.Fatalf("create: %d %s", rr.Code, rr.Body)
		}
		var job map[string]json.RawMessage
		_ = json.Unmarshal(rr.Body.Bytes(), &job)
		id := batchString(job, "id")
		if !strings.Contains(batchString(job, "results_url"), "/v1/anthropic/v1/messages/batches/") {
			t.Fatal("SDK results URL bypasses gateway")
		}
		cipher, _ := encryption.New("")
		handler.SetBatchStore(batchstate.New(pool, cipher, 30*24*time.Hour))
		path := "/v1/anthropic/v1/messages/batches/" + id + "/results"
		rr = call("GET", path, "")
		if rr.Code != 200 || !strings.Contains(rr.Body.String(), "first@example.test") || !strings.Contains(rr.Body.String(), "second@example.test") {
			t.Fatalf("restore: %d %s", rr.Code, rr.Body)
		}
		var lines []map[string]json.RawMessage
		for _, line := range strings.Split(strings.TrimSpace(rr.Body.String()), "\n") {
			var item map[string]json.RawMessage
			_ = json.Unmarshal([]byte(line), &item)
			lines = append(lines, item)
		}
		if batchString(lines[0], "custom_id") != "two" || !strings.Contains(string(lines[0]["result"]), "second@example.test") {
			t.Fatal("out-of-order map crossed items")
		}
		for _, headers := range [][]string{{"Test-Tenant", "other"}, {"Test-Credential", "other"}, {"Range", "bytes=0-100"}} {
			rr = call("GET", path, "", headers...)
			if rr.Code < 400 {
				t.Fatal("isolation/range bypass accepted")
			}
		}
		rr = call("GET", "/v1/anthropic/messages/batches", "")
		if strings.Contains(rr.Body.String(), "not-this-tenant") {
			t.Fatal("cross-tenant list")
		}
		fixture.mu.Lock()
		fixture.corrupt = true
		fixture.mu.Unlock()
		rr = call("GET", path, "")
		if rr.Code < 400 || strings.Contains(rr.Body.String(), "first@example.test") {
			t.Fatal("partial corrupt result leaked")
		}
		fixture.mu.Lock()
		fixture.corrupt = false
		fixture.freshPII = true
		fixture.mu.Unlock()
		profile.PIIAction = security.ActionBlock
		rr = call("GET", path, "")
		if rr.Code != 403 || strings.Contains(rr.Body.String(), "new@example.test") || strings.Contains(rr.Body.String(), "allowed-prefix-sentinel") {
			t.Fatalf("fresh output PII not blocked: %d %s", rr.Code, rr.Body)
		}
		fixture.mu.Lock()
		fixture.freshPII = false
		fixture.mu.Unlock()
		profile.PIIAction = security.ActionTokenize
		rr = call("DELETE", "/v1/anthropic/messages/batches/"+id, "")
		if rr.Code != 204 || rr.Body.Len() != 0 {
			t.Fatalf("native deletion contract: %d %s", rr.Code, rr.Body)
		}
	})
	t.Run("Google inline and encrypted resumable files", func(t *testing.T) {
		rr := call("POST", "/v1/google/v1beta/models/gemini-2.5-flash:batchGenerateContent", `{"batch":{"displayName":"demo","inputConfig":{"requests":{"requests":[{"request":{"contents":[{"parts":[{"text":"inline@example.test"}]}]}}]}}}}`)
		if rr.Code != 200 || !strings.Contains(rr.Body.String(), "inline@example.test") {
			t.Fatalf("Google inline: %d %s", rr.Code, rr.Body)
		}
		input := `{"key":"one","request":{"contents":[{"parts":[{"text":"file@example.test"}]}]}}` + "\n"
		rr = call("POST", "/v1/google/upload/v1beta/files", `{"file":{"display_name":"demo"}}`, "X-Goog-Upload-Protocol", "resumable", "X-Goog-Upload-Command", "start", "X-Goog-Upload-Header-Content-Length", fmt.Sprint(len(input)), "X-Goog-Upload-Header-Content-Type", "application/jsonl")
		if rr.Code != 200 {
			t.Fatalf("start %d %s", rr.Code, rr.Body)
		}
		url := strings.TrimPrefix(rr.Header().Get("X-Goog-Upload-URL"), "https://example.com")
		rr = call("POST", url, input[:15], "X-Goog-Upload-Command", "upload", "X-Goog-Upload-Offset", "0")
		if rr.Code != 200 {
			t.Fatalf("chunk %d %s", rr.Code, rr.Body)
		}
		rr = call("POST", url, "", "X-Goog-Upload-Command", "query")
		if rr.Header().Get("X-Goog-Upload-Size-Received") != "15" {
			t.Fatal("query lost offset")
		}
		rr = call("POST", url, input[15:], "X-Goog-Upload-Command", "upload, finalize", "X-Goog-Upload-Offset", "15")
		if rr.Code != 200 {
			t.Fatalf("finalize %d %s", rr.Code, rr.Body)
		}
		var obj map[string]json.RawMessage
		_ = json.Unmarshal(rr.Body.Bytes(), &obj)
		file, _ := googleObject(obj, "file", "file")
		name := batchString(file, "name")
		rr = call("POST", "/v1/google/v1beta/models/gemini-2.5-flash:batchGenerateContent", fmt.Sprintf(`{"batch":{"displayName":"file demo","inputConfig":{"fileName":%q}}}`, name))
		if rr.Code != 200 {
			t.Fatalf("file batch %d %s", rr.Code, rr.Body)
		}
		_ = json.Unmarshal(rr.Body.Bytes(), &obj)
		response, _ := googleObject(obj, "response", "response")
		output := batchString(response, "responsesFile")
		rr = call("GET", "/v1/google/download/v1beta/"+output+":download?alt=media", "")
		if rr.Code != 200 || !strings.Contains(rr.Body.String(), "file@example.test") {
			t.Fatalf("Google file output: %d %s", rr.Code, rr.Body)
		}
	})
	t.Run("blocked inputs never reach provider and monitoring leaves originals", func(t *testing.T) {
		fixture.mu.Lock()
		before := len(fixture.received)
		fixture.mu.Unlock()
		profile.PIIAction = security.ActionBlock
		body := `{"requests":[{"custom_id":"one","params":{"model":"claude-sonnet-4-5","max_tokens":10,"messages":[{"role":"user","content":"monitor@example.test"}]}}]}`
		rr := call("POST", "/v1/anthropic/messages/batches", body)
		if rr.Code != 403 {
			t.Fatalf("blocked %d %s", rr.Code, rr.Body)
		}
		fixture.mu.Lock()
		if len(fixture.received) != before {
			t.Error("blocked batch reached upstream")
		}
		fixture.mu.Unlock()
		rr = call("POST", "/v1/anthropic/messages/batches", body, "Test-Monitor", "true")
		if rr.Code != 200 {
			t.Fatalf("monitoring blocked %d %s", rr.Code, rr.Body)
		}
		fixture.mu.Lock()
		if !strings.Contains(string(fixture.received[len(fixture.received)-1]), "monitor@example.test") {
			t.Error("monitoring rewrote original")
		}
		fixture.mu.Unlock()
		profile.PIIAction = security.ActionTokenize
	})
	t.Run("OpenAI validates whole upload before upstream and secrets have their own action", func(t *testing.T) {
		upload := func(input string) *httptest.ResponseRecorder {
			var body bytes.Buffer
			writer := multipart.NewWriter(&body)
			file, _ := writer.CreateFormFile("file", "batch.jsonl")
			_, _ = io.WriteString(file, input)
			_ = writer.WriteField("purpose", "batch")
			_ = writer.Close()
			r := httptest.NewRequest("POST", "/v1/files", &body)
			r.Header.Set("Content-Type", writer.FormDataContentType())
			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, r)
			return rr
		}
		fixture.mu.Lock()
		before := len(fixture.received)
		fixture.mu.Unlock()
		line := `{"custom_id":"one","method":"POST","url":"/v1/chat/completions","body":{"model":"gpt-4o-mini","messages":[{"role":"user","content":"safe"}]}}`
		if rr := upload(line + "\nbroken"); rr.Code != 400 {
			t.Fatalf("malformed upload: %d %s", rr.Code, rr.Body)
		}
		if rr := upload(line + "\n" + line); rr.Code != 400 {
			t.Fatalf("duplicate ID: %d %s", rr.Code, rr.Body)
		}
		profile.PIIAction = security.ActionBlock
		if rr := upload(strings.Replace(line, "safe", "blocked@example.test", 1)); rr.Code != 403 {
			t.Fatalf("blocked upload: %d %s", rr.Code, rr.Body)
		}
		fixture.mu.Lock()
		if len(fixture.received) != before {
			t.Error("invalid upload partially forwarded")
		}
		fixture.mu.Unlock()
		profile.PIIAction = security.ActionLogOnly
		rr := upload(strings.Replace(line, "safe", "keep@example.test AKIAABCDEFGHIJKLMNOP", 1))
		if rr.Code != 200 {
			t.Fatalf("secret mask: %d %s", rr.Code, rr.Body)
		}
		fixture.mu.Lock()
		received := string(fixture.received[len(fixture.received)-1])
		fixture.mu.Unlock()
		if !strings.Contains(received, "keep@example.test") || strings.Contains(received, "AKIAABCDEFGHIJKLMNOP") {
			t.Fatal("per-detector strategies were not respected")
		}
		profile.PIIAction = security.ActionTokenize
	})
	t.Run("file uploaded before policy change requires fresh inspection", func(t *testing.T) {
		fixture.mu.Lock()
		var existing string
		for id := range fixture.files {
			if strings.HasPrefix(id, "file-") {
				existing = id
				break
			}
		}
		fixture.mu.Unlock()
		// The OpenAI upload in the preceding test used log-only PII; the
		// current profile now tokenizes. Its old file cannot be submitted.
		rr := call("POST", "/v1/batches", fmt.Sprintf(`{"input_file_id":%q,"endpoint":"/v1/chat/completions","completion_window":"24h"}`, existing))
		if rr.Code != 428 {
			t.Fatalf("old upload bypassed changed policy: %d %s", rr.Code, rr.Body)
		}
	})
	t.Run("extension classifiers run per item and can stop forwarding", func(t *testing.T) {
		seen := 0
		handler.SetBatchInspection(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				raw, _ := io.ReadAll(r.Body)
				seen++
				if strings.Contains(InputText(raw), "extension-deny") {
					w.WriteHeader(403)
					return
				}
				r.Body = io.NopCloser(bytes.NewReader(raw))
				next.ServeHTTP(w, r)
			})
		}, nil)
		defer handler.SetBatchInspection(nil, nil)
		fixture.mu.Lock()
		before := len(fixture.received)
		fixture.mu.Unlock()
		body := `{"requests":[{"custom_id":"one","params":{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"allowed"}]}},{"custom_id":"two","params":{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"extension-deny"}]}}]}`
		rr := call("POST", "/v1/anthropic/messages/batches", body)
		if rr.Code != 403 || seen != 2 {
			t.Fatalf("item classification: %d %s seen=%d", rr.Code, rr.Body, seen)
		}
		fixture.mu.Lock()
		if len(fixture.received) != before {
			t.Error("extended security block forwarded job")
		}
		fixture.mu.Unlock()
	})
	t.Run("official Python SDKs", func(t *testing.T) {
		python := os.Getenv("BASTIO_BATCH_SDK_PYTHON")
		if python == "" {
			t.Skip("set BASTIO_BATCH_SDK_PYTHON to a venv with OpenAI, Anthropic and google-genai")
		}
		app := httptest.NewServer(router)
		defer app.Close()
		cmd := exec.Command(python, "testdata/batch_sdk_contract.py", app.URL)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("SDK contracts: %v\n%s", err, out)
		}
		t.Log(string(out))
	})
	t.Run("official JavaScript SDKs", func(t *testing.T) {
		directory := os.Getenv("BASTIO_BATCH_SDK_JS_DIR")
		if directory == "" {
			t.Skip("set BASTIO_BATCH_SDK_JS_DIR to a project with OpenAI, Anthropic and Google SDKs")
		}
		script, err := os.ReadFile("testdata/batch_sdk_contract.mjs")
		if err != nil {
			t.Fatal(err)
		}
		app := httptest.NewServer(router)
		defer app.Close()
		cmd := exec.Command("node", "--input-type=module", "-", app.URL)
		cmd.Dir = directory
		cmd.Stdin = bytes.NewReader(script)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("SDK contracts: %v\n%s", err, out)
		}
		t.Log(string(out))
	})
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	for _, raw := range fixture.received {
		for _, value := range []string{"first@example.test", "second@example.test", "inline@example.test", "file@example.test"} {
			if strings.Contains(string(raw), value) {
				t.Fatalf("provider received original PII %q", value)
			}
		}
	}
}

func TestBatchJSONLLongLinesMalformedAndNoTrailingNewline(t *testing.T) {
	long := `{"key":"one","request":{"contents":[{"parts":[{"text":"` + strings.Repeat("x", 128<<10) + `"}]}]}}`
	for _, input := range []string{long, long + "\n"} {
		var output bytes.Buffer
		if err := walkBatchJSONL(strings.NewReader(input), &output, func(line []byte, _ int) ([]byte, error) { return line, nil }); err != nil || !json.Valid(bytes.TrimSpace(output.Bytes())) {
			t.Fatalf("long line: %v", err)
		}
	}
	for _, input := range []string{"", long + "\nbroken", long + "\n\n"} {
		if err := walkBatchJSONL(strings.NewReader(input), io.Discard, func(line []byte, _ int) ([]byte, error) { return line, nil }); err == nil {
			t.Fatal("malformed JSONL accepted")
		}
	}
}

func TestBatchSignedThinkingOnlyBlocksChangesToSignedFields(t *testing.T) {
	for _, raw := range []string{`{"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"safe <PII_token>","signature":"signed"},{"type":"text","text":"sensitive"}]}]}`, `{"contents":[{"parts":[{"text":"safe <PII_token>","thoughtSignature":"signed"},{"text":"sensitive"}]}]}`, `{"messages":[{"content":"sensitive"}],"tools":[{"parameters":{"properties":{"signature":{"type":"string"}}}}]}`} {
		p := providers.ProviderOpenAI
		if strings.Contains(raw, "thoughtSignature") {
			p = providers.ProviderGemini
		}
		out, _ := batchMapInput([]byte(raw), p, func(s string) string { return strings.ReplaceAll(s, "sensitive", "masked") })
		if batchChangesSigned([]byte(raw), out) {
			t.Fatal("unrelated text rewrite invalidated untouched signed block")
		}
		if strings.Contains(raw, `"signed"`) {
			out, _ = batchMapInput([]byte(raw), p, func(s string) string { return strings.ReplaceAll(s, "safe", "changed") })
			if !batchChangesSigned([]byte(raw), out) {
				t.Fatal("signed thinking rewrite allowed")
			}
		}
	}
}
