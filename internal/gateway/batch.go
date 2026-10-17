package gateway

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/bastio-ai/bastio/internal/auth"
	"github.com/bastio-ai/bastio/internal/batchstate"
	"github.com/bastio-ai/bastio/internal/llmpipeline"
	"github.com/bastio-ai/bastio/internal/providers"
	"github.com/bastio-ai/bastio/internal/security"
	"github.com/bastio-ai/bastio/internal/security/overlay"
	"github.com/bastio-ai/bastio/pkg/gatewaypolicy"
	"github.com/google/uuid"
)

// Batch APIs keep their native wire formats. Only prompt/answer text is
// transformed; correlation IDs, schemas and job control fields stay intact.
const batchLineLimit = 64 << 20
const batchJSONLimit = 256 << 20
const batchFileLimit int64 = 2 << 30
const batchItemLimit = 100000

type batchManifest struct {
	ID       string   `json:"id"`
	TraceID  string   `json:"trace_id"`
	Keys     []string `json:"keys,omitempty"` // hashes in input order, never raw custom IDs
	Model    string   `json:"model,omitempty"`
	Endpoint string   `json:"endpoint,omitempty"`
	Policy   string   `json:"policy"`
	keySet   map[string]bool
}
type batchResource struct {
	Manifest batchManifest `json:"manifest"`
	Role     string        `json:"role,omitempty"`
	Job      string        `json:"job,omitempty"`
}
type batchCall struct {
	h       *Handler
	r       *http.Request
	info    *auth.APIKeyInfo
	route   *nativeRoute
	client  providers.NativeClient
	profile *security.Profile
	scope   string
	trace   uuid.UUID
	start   time.Time
	scan    *security.ScanResult
	tags    map[string]string
	status  uint16
}

func isBatchEndpoint(provider providers.Provider, path string) bool {
	path = normalizeBatchPath(path)
	if provider == "" {
		return isBatchEndpoint(providers.ProviderOpenAI, path) || isBatchEndpoint(providers.ProviderAnthropic, path) || isBatchEndpoint(providers.ProviderGemini, path)
	}
	if provider == providers.ProviderAnthropic {
		return path == "/messages/batches" || strings.HasPrefix(path, "/messages/batches/")
	}
	if provider == providers.ProviderGemini {
		return strings.HasPrefix(path, "/batches") || strings.HasSuffix(path, ":batchGenerateContent") || strings.HasSuffix(path, ":asyncBatchEmbedContent") || strings.HasPrefix(path, "/files") || strings.HasPrefix(path, "/upload/") || strings.HasPrefix(path, "/download/") || strings.HasPrefix(path, "/batch-uploads/")
	}
	return provider == providers.ProviderOpenAI && (path == "/batches" || strings.HasPrefix(path, "/batches/") || path == "/files" || strings.HasPrefix(path, "/files/"))
}
func normalizeBatchPath(path string) string {
	for _, version := range []string{"/v1/", "/v1beta/"} {
		if strings.HasPrefix(path, version) {
			return "/" + strings.TrimPrefix(path, version)
		}
	}
	return path
}

func (h *Handler) handleBatchAPI(w http.ResponseWriter, r *http.Request, provider providers.Provider, path string) bool {
	if !isBatchEndpoint(provider, path) {
		return false
	}
	path = normalizeBatchPath(path)
	info, ok := auth.FromContext(r.Context())
	if !ok {
		writeRouteError(w, &routeError{401, "unauthorized"})
		return true
	}
	route, err := h.nativeRoute(r, info, provider, "")
	if err != nil {
		writeRouteError(w, err)
		return true
	}
	if !isBatchEndpoint(route.provider, path) {
		writeRouteError(w, &routeError{400, "batch API requires an explicit OpenAI, Anthropic or Google provider"})
		return true
	}
	if h.batchStore == nil {
		writeRouteError(w, &routeError{503, "durable encrypted batch storage is not configured"})
		return true
	}
	select {
	case h.batchGate <- struct{}{}:
		defer func() { <-h.batchGate }()
	default:
		writeRouteError(w, &routeError{429, "batch inspection capacity is busy; retry later"})
		return true
	}
	if route.key == "" {
		writeRouteError(w, &routeError{502, "no provider credential configured"})
		return true
	}
	client, err := h.providers.Native(route.provider)
	if err != nil {
		writeRouteError(w, err)
		return true
	}
	trace := uuid.New()
	w.Header().Set("X-Bastio-Trace-Id", trace.String())
	w.Header().Set("X-Bastio-Request-Id", trace.String())
	profile, snap, ident, err := h.resolveProfileWithOverlay(r.Context(), info.CustomerID, route.proxyID)
	if (err != nil || h.security == nil) && h.failClosed() && !gatewaypolicy.From(r.Context()).MonitorOnly {
		h.writeFailClosedBlock(w, trace, "security scan unavailable")
		return true
	}
	ctx := security.WithProfile(r.Context(), profile)
	if snap != nil {
		ctx = overlay.WithActive(ctx, snap, ident)
	}
	// Provider project/organization overrides participate in vault isolation too.
	scope := batchHash(string(route.provider) + "\x00" + route.proxyID.String() + "\x00" + route.key + "\x00" + r.Header.Get("OpenAI-Project") + "\x00" + r.Header.Get("OpenAI-Organization"))
	b := &batchCall{h: h, r: r.WithContext(ctx), info: info, route: route, client: client, profile: profile, scope: scope, trace: trace, start: time.Now(), status: 200, tags: map[string]string{"batch.operation": "control", "batch.provider": string(route.provider)}}
	defer func() {
		status := "ok"
		if b.status >= 400 {
			status = "error"
		}
		if b.status == 403 {
			status = "blocked"
		}
		zero := 0.0
		h.recordTrace(b.r.Context(), recordInput{TraceID: trace, ProxyID: route.proxyID, Method: r.Method, HTTPStatus: b.status, Info: info, Provider: string(route.provider), Path: r.URL.Path, Start: b.start, Status: status, Scan: b.scan, Headers: r.Header, IPAddress: r.RemoteAddr, UserAgent: r.UserAgent(), CostCents: &zero, Tags: b.tags, TraceName: "batch." + strings.ToLower(r.Method)})
	}()
	err = b.serve(w, path)
	if err != nil {
		b.status = 502
		var e *routeError
		if errors.As(err, &e) {
			b.status = uint16(e.status)
		}
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			err = &routeError{413, "batch request exceeds upload limit"}
			b.status = 413
		}
		writeRouteError(w, err)
	}
	return true
}
func batchHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
func (b *batchCall) key(kind, id string) batchstate.Key {
	return batchstate.Key{Customer: b.info.CustomerID, Scope: b.scope, Kind: kind, ID: id}
}
func (b *batchCall) put(kind, id string, resource batchResource) error {
	raw, err := json.Marshal(resource)
	if err != nil {
		return err
	}
	return b.h.batchStore.Put(b.r.Context(), b.key(kind, id), raw, 0)
}
func (b *batchCall) get(kind, id string) (batchResource, error) {
	var resource batchResource
	rec, err := b.h.batchStore.Get(b.r.Context(), b.key(kind, id))
	if errors.Is(err, batchstate.ErrMissing) {
		return resource, &routeError{404, "batch resource not found in this tenant, gateway and provider credential scope; upload and create batches through this gateway"}
	}
	if err != nil {
		return resource, err
	}
	err = json.Unmarshal(rec.Payload, &resource)
	resource.Manifest.keySet = make(map[string]bool, len(resource.Manifest.Keys))
	for _, key := range resource.Manifest.Keys {
		resource.Manifest.keySet[key] = true
	}
	return resource, err
}
func (b *batchCall) forward(method, path string, body []byte, reader io.Reader, length int64, headers http.Header) (*http.Response, error) {
	q := b.r.URL.Query()
	q.Del("proxy_id")
	q.Del("key")
	headers = headers.Clone()
	for _, key := range []string{"Range", "If-None-Match", "If-Modified-Since", "If-Match"} {
		headers.Del(key)
	}
	return b.client.Forward(b.r.Context(), &providers.NativeRequest{Method: method, Path: path, Query: q, Headers: headers, Body: body, BodyReader: reader, ContentLength: length}, b.route.key)
}
func readBatchJSON(reader io.Reader, limit int64) ([]byte, error) {
	raw, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > limit {
		return nil, &routeError{413, "batch JSON exceeds supported size"}
	}
	if !json.Valid(raw) {
		if len(bytes.TrimSpace(raw)) == 0 {
			return nil, nil
		}
		return nil, &routeError{400, "invalid batch JSON"}
	}
	return raw, nil
}
func batchSpool() (*os.File, error) {
	f, err := os.CreateTemp("", "bastio-batch-*")
	if err != nil {
		return nil, err
	}
	// Unlink immediately on our Unix server: a crash cannot leave plaintext
	// quarantine or result files behind. The open 0600 descriptor still works.
	if err = os.Remove(f.Name()); err != nil {
		_ = f.Close()
		return nil, err
	}
	return f, nil
}
func removeBatchSpool(f *os.File) {
	if f != nil {
		name := f.Name()
		_ = f.Close()
		_ = os.Remove(name)
	}
}
func rewindBatch(f *os.File) (int64, error) {
	size, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		return 0, err
	}
	_, err = f.Seek(0, io.SeekStart)
	return size, err
}

// serve owns file and asynchronous job lifecycles. Large inputs/results never
// enter the ordinary native io.ReadAll path.
func (b *batchCall) serve(w http.ResponseWriter, path string) error {
	if b.route.provider == providers.ProviderGemini && (strings.HasPrefix(path, "/upload/") || strings.HasPrefix(path, "/batch-uploads/")) {
		return b.googleUpload(w, path)
	}
	if b.route.provider == providers.ProviderOpenAI && path == "/files" && b.r.Method == http.MethodPost {
		return b.openAIUpload(w)
	}
	if b.isResults(path) {
		return b.downloadResults(w, path)
	}
	var raw []byte
	var err error
	if b.r.Body != nil && (b.r.Method == http.MethodPost || b.r.Method == http.MethodPatch) {
		raw, err = readBatchJSON(b.r.Body, batchJSONLimit)
		if err != nil {
			return err
		}
	}
	resource := batchResource{}
	create := b.r.Method == http.MethodPost && (path == "/batches" || path == "/messages/batches" || strings.HasSuffix(path, ":batchGenerateContent") || strings.HasSuffix(path, ":asyncBatchEmbedContent"))
	if create {
		b.tags["batch.operation"] = "create"
		raw, resource, err = b.prepareCreate(path, raw)
		if err != nil {
			return err
		}
		if err = b.put("manifest", resource.Manifest.ID, resource); err != nil {
			return err
		}
	} else if id, kind := batchResourcePath(b.route.provider, path); id != "" {
		resource, err = b.get(kind, id)
		if err != nil {
			return err
		}
		b.tags["batch.job_id"] = resource.Job
	}
	resp, err := b.forward(b.r.Method, path, raw, nil, 0, b.r.Header)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return b.copyError(w, resp)
	}
	out, err := readBatchJSON(resp.Body, batchJSONLimit)
	if err != nil {
		return &routeError{502, "invalid or oversized provider batch response"}
	}
	if len(out) == 0 && (resp.StatusCode == http.StatusNoContent || b.r.Method == http.MethodDelete) {
		if id, kind := batchResourcePath(b.route.provider, path); id != "" {
			if err = b.h.batchStore.Delete(b.r.Context(), b.key(kind, id)); err != nil {
				return err
			}
		}
		b.status = uint16(resp.StatusCode)
		copyProviderResponseHeaders(w.Header(), resp.Header)
		w.WriteHeader(resp.StatusCode)
		return nil
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(out, &obj) != nil || obj == nil {
		return &routeError{502, "invalid provider batch object"}
	}
	if create {
		id := batchString(obj, "id", "name")
		if id == "" {
			return &routeError{502, "provider returned no batch identifier"}
		}
		resource.Job = id
		resource.Role = "job"
		if err = b.put("job", id, resource); err != nil {
			w.Header().Set("X-Should-Retry", "false")
			return &routeError{503, "provider accepted batch " + id + " but gateway registration failed; do not resubmit; contact your administrator"}
		}
		b.tags["batch.job_id"] = id
	}
	if resource.Job != "" {
		if err = b.bindOutputFiles(obj, resource); err != nil {
			return err
		}
		if b.route.provider == providers.ProviderGemini {
			out, err = b.googleInlineResults(out, resource)
			if err != nil {
				return err
			}
			_ = json.Unmarshal(out, &obj)
		}
	}
	// Lists are filtered against our tenant-scoped vault, even when a deployment
	// uses shared upstream credentials. Pagination cursors remain provider-owned.
	listKey := "data"
	if b.route.provider == providers.ProviderGemini {
		listKey = "operations"
		if _, ok := obj["files"]; ok {
			listKey = "files"
		}
	}
	var list []json.RawMessage
	if json.Unmarshal(obj[listKey], &list) == nil && list != nil {
		filtered := make([]json.RawMessage, 0, len(list))
		kind := "job"
		if strings.Contains(path, "files") {
			kind = "file"
		}
		for _, entry := range list {
			var item map[string]json.RawMessage
			_ = json.Unmarshal(entry, &item)
			id := batchString(item, "id", "name")
			known, e := b.get(kind, id)
			if e == nil {
				if kind == "job" {
					if e = b.bindOutputFiles(item, known); e != nil {
						return e
					}
					if b.route.provider == providers.ProviderGemini {
						entry, e = b.googleInlineResults(entry, known)
						if e != nil {
							return e
						}
					}
				}
				filtered = append(filtered, entry)
			} else {
				var routeErr *routeError
				if !errors.As(e, &routeErr) {
					return e
				}
			}
		}
		obj[listKey], _ = json.Marshal(filtered)
	}
	b.rewriteBatchLinks(obj)
	out, _ = json.Marshal(obj)
	if b.r.Method == http.MethodDelete {
		if id, kind := batchResourcePath(b.route.provider, path); id != "" {
			if err = b.h.batchStore.Delete(b.r.Context(), b.key(kind, id)); err != nil {
				return err
			}
		}
	}
	b.status = uint16(resp.StatusCode)
	copyProviderResponseHeaders(w.Header(), resp.Header)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	_, err = w.Write(out)
	return err
}
func (b *batchCall) copyError(w http.ResponseWriter, resp *http.Response) error {
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	b.status = uint16(resp.StatusCode)
	copyProviderResponseHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	_, err = w.Write(raw)
	return err
}
func batchString(obj map[string]json.RawMessage, keys ...string) string {
	for _, key := range keys {
		var v string
		if json.Unmarshal(obj[key], &v) == nil && v != "" {
			return v
		}
	}
	return ""
}
func batchResourcePath(provider providers.Provider, path string) (string, string) {
	if provider == providers.ProviderAnthropic {
		if strings.HasPrefix(path, "/messages/batches/") {
			return strings.Split(strings.TrimPrefix(path, "/messages/batches/"), "/")[0], "job"
		}
		return "", ""
	}
	if strings.HasPrefix(path, "/batches/") {
		id := strings.Split(strings.TrimPrefix(path, "/batches/"), "/")[0]
		id = strings.Split(id, ":")[0]
		if provider == providers.ProviderGemini {
			id = "batches/" + id
		}
		return id, "job"
	}
	if strings.HasPrefix(path, "/files/") {
		id := strings.Split(strings.TrimPrefix(path, "/files/"), "/")[0]
		if provider == providers.ProviderGemini {
			id = "files/" + strings.Split(id, ":")[0]
		}
		return id, "file"
	}
	return "", ""
}
func (b *batchCall) newManifest() batchManifest {
	return batchManifest{ID: uuid.NewString(), TraceID: b.trace.String(), Policy: b.inputPolicyFingerprint()}
}
func (b *batchCall) inputPolicyFingerprint() string {
	policy := gatewaypolicy.From(b.r.Context())
	active, _ := overlay.FromContext(b.r.Context())
	raw, _ := json.Marshal(struct {
		Profile     *security.Profile
		Overlay     *overlay.Active
		MonitorOnly bool
		Revision    string
	}{b.profile, active, policy.MonitorOnly, policy.InputRevision})
	return batchHash(string(raw))
}
func (b *batchCall) prepareCreate(path string, raw []byte) ([]byte, batchResource, error) {
	resource := batchResource{Manifest: b.newManifest()}
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil || obj == nil {
		return nil, resource, &routeError{400, "invalid batch object"}
	}
	if b.route.provider == providers.ProviderOpenAI {
		file := batchString(obj, "input_file_id")
		known, err := b.get("file", file)
		if err != nil {
			return nil, resource, err
		}
		if known.Role != "input" {
			return nil, resource, &routeError{400, "batch input must be a JSONL file uploaded with purpose=batch"}
		}
		if known.Manifest.Policy != b.inputPolicyFingerprint() {
			return nil, resource, &routeError{428, "batch input was inspected under a different security policy; upload the input file again before submitting this batch"}
		}
		resource.Manifest = known.Manifest
		resource.Manifest.TraceID = b.trace.String()
		endpoint := batchString(obj, "endpoint")
		if resource.Manifest.Endpoint != endpoint {
			return nil, resource, &routeError{400, "batch endpoint does not match uploaded JSONL requests"}
		}
		return raw, resource, nil
	}
	if b.route.provider == providers.ProviderGemini {
		return b.prepareGoogleCreate(path, obj, resource)
	}
	var items []map[string]json.RawMessage
	if json.Unmarshal(obj["requests"], &items) != nil || len(items) == 0 || len(items) > batchItemLimit {
		return nil, resource, &routeError{400, "batch requests must contain 1 to 100000 items"}
	}
	seen := map[string]bool{}
	for i, item := range items {
		id := batchString(item, "custom_id")
		if id == "" || len(id) > 256 || seen[id] {
			return nil, resource, &routeError{400, "batch custom_id values must be unique, nonempty and at most 256 bytes"}
		}
		seen[id] = true
		params := item["params"]
		next, err := b.inputItem(params, batchHash(id), &resource.Manifest)
		if err != nil {
			return nil, resource, fmt.Errorf("batch item %d: %w", i+1, err)
		}
		item["params"] = next
	}
	obj["requests"], _ = json.Marshal(items)
	out, _ := json.Marshal(obj)
	return out, resource, nil
}
func (b *batchCall) inputItem(raw []byte, key string, manifest *batchManifest) ([]byte, error) {
	var inspectErr error
	raw, inspectErr = b.inspect(raw, false)
	if inspectErr != nil {
		return nil, inspectErr
	}
	if !json.Valid(raw) || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, &routeError{400, "invalid batch request item"}
	}
	if rejected, allowed := validateImageContent(raw, b.h.imageURLAllowlist); !allowed {
		return nil, &routeError{403, "image URL is outside trusted hosts: " + rejected}
	}
	if len(manifest.Keys) >= batchItemLimit {
		return nil, &routeError{413, "batch exceeds item limit"}
	}
	manifest.Keys = append(manifest.Keys, key)
	var model struct {
		Model string `json:"model"`
	}
	_ = json.Unmarshal(raw, &model)
	if manifest.Model == "" {
		manifest.Model = model.Model
	}
	text := batchInputText(raw, b.route.provider)
	scan := b.h.runSecurityScan(b.r, b.info, text, b.trace, b.profile)
	b.addScan(scan)
	if scan != nil && scan.ShouldBlock && !gatewaypolicy.From(b.r.Context()).MonitorOnly {
		return nil, &routeError{403, "batch input withheld by security policy; item=" + key[:12]}
	}
	if scan == nil || gatewaypolicy.From(b.r.Context()).MonitorOnly || (scan.Action != security.ActionTokenize && !scan.Action.IsMaskLike()) {
		return raw, nil
	}
	tm := security.NewRequestTokenMap(b.profile.PIITokenStyle)
	steps, _ := security.StepsFromLegacyProfile(*b.profile)
	available := true
	transform := func(s string) string {
		next, ok := b.h.security.SanitizeDecisions(s, steps, scan.DetectorActions, tm, false)
		available = available && ok
		return next
	}
	out, _ := batchMapInput(raw, b.route.provider, transform)
	if !available {
		return nil, &routeError{403, "batch input requires a security rewrite that is unavailable"}
	}
	if batchChangesSigned(raw, out) {
		return nil, &routeError{403, "security policy cannot rewrite signed thinking content in batch input"}
	}
	if tm.Size() > 0 {
		if err := b.h.batchStore.SaveTokens(b.r.Context(), b.key("tokens", manifest.ID+"/"+key), tm); err != nil {
			return nil, err
		}
	}
	return out, nil
}
func batchMapInput(raw []byte, p providers.Provider, transform textTransform) ([]byte, bool) {
	if p == providers.ProviderGemini {
		return mapGoogleContent(raw, transform)
	}
	signed, ch := batchMapThinking(raw, transform)
	out, changed := mapInput(signed, transform)
	return out, ch || changed
}
func batchInputText(raw []byte, p providers.Provider) string {
	var text []string
	_, _ = batchMapInput(raw, p, func(s string) string { text = append(text, s); return s })
	return strings.Join(text, "\n")
}

func (b *batchCall) outputItem(raw []byte, key string, resource batchResource) ([]byte, error) {
	var inspectErr error
	raw, inspectErr = b.inspect(raw, true)
	if inspectErr != nil {
		return nil, inspectErr
	}
	var text []string
	_, _ = mapBatchOutput(raw, b.route.provider, func(s string) string { text = append(text, s); return s })
	scan := b.h.runSecurityScanFrom(b.r, b.info, strings.Join(text, "\n"), b.trace, b.profile, "response")
	b.addScan(scan)
	monitor := gatewaypolicy.From(b.r.Context()).MonitorOnly
	if scan != nil && scan.ShouldBlock && !monitor {
		return nil, &routeError{403, "batch result withheld by current security policy; job=" + resource.Job + " item=" + key[:12]}
	}
	out := raw
	if !monitor {
		var tm *security.TokenMap
		var err error
		// Restore only items whose ID belongs to the original manifest.
		if resource.Manifest.keySet == nil {
			resource.Manifest.keySet = make(map[string]bool, len(resource.Manifest.Keys))
			for _, id := range resource.Manifest.Keys {
				resource.Manifest.keySet[id] = true
			}
		}
		known := resource.Manifest.keySet[key]
		if known && b.profile.PIIRestoreResponse {
			tm, err = b.h.batchStore.LoadTokens(b.r.Context(), b.key("tokens", resource.Manifest.ID+"/"+key))
			if err != nil {
				return nil, err
			}
		}
		out, _ = mapBatchOutput(raw, b.route.provider, func(s string) string {
			if scan != nil && (scan.Action.IsMaskLike() || scan.Action == security.ActionTokenize) && b.h.security != nil {
				_, steps := security.StepsFromLegacyProfile(*b.profile)
				var ok bool
				s, ok = b.h.security.SanitizeDecisions(s, steps, scan.DetectorActions, nil, true)
				if !ok {
					err = &routeError{403, "batch result requires a security rewrite that is unavailable"}
				}
			}
			return s
		})
		if err != nil {
			return nil, err
		}
		if batchChangesSigned(raw, out) {
			return nil, &routeError{403, "security policy cannot rewrite signed thinking content in batch result"}
		}
		if tm != nil {
			out, _ = mapBatchOutputMode(out, b.route.provider, func(s string) string { next, _ := tm.Restore(s); return next }, true)
		}
	}
	// At-most-once best-effort analytics per result, independent of repeated SDK
	// downloads/polls. Job control calls have zero usage; each item owns its usage.
	fresh, err := b.h.batchStore.Once(b.r.Context(), b.key("recorded", resource.Job+"/"+key))
	if err != nil {
		return nil, err
	}
	if fresh {
		inTokens, outTokens := batchUsage(raw)
		model := batchModel(raw)
		if model == "" {
			model = resource.Manifest.Model
		}
		cost := llmpipeline.EstimateCostCents(model, inTokens, outTokens) * 0.5
		tags := map[string]string{"batch.operation": "result_item", "batch.job_id": resource.Job, "batch.create_trace_id": resource.Manifest.TraceID, "batch.item_hash": key, "batch.cost_source": "estimated_50_percent_batch_rate"}
		b.h.recordTrace(b.r.Context(), recordInput{TraceID: uuid.New(), ProxyID: b.route.proxyID, Method: "BATCH", HTTPStatus: 200, Info: b.info, Model: model, Provider: string(b.route.provider), Path: b.r.URL.Path, Start: b.start, Status: "ok", Scan: scan, RespBody: out, InputTokens: inTokens, OutputTokens: outTokens, Headers: b.r.Header, CostCents: &cost, Tags: tags, TraceName: "batch.result"})
	}
	return out, nil
}

func (b *batchCall) storedInputItem(line []byte) ([]byte, error) {
	var obj map[string]json.RawMessage
	if json.Unmarshal(line, &obj) != nil || obj == nil {
		return nil, &routeError{502, "invalid provider input file"}
	}
	field := "body"
	if b.route.provider == providers.ProviderGemini {
		field = "request"
	}
	raw := obj[field]
	next, err := b.storedInput(raw)
	if err != nil {
		return nil, err
	}
	obj[field] = next
	return json.Marshal(obj)
}
func (b *batchCall) storedInput(raw []byte) ([]byte, error) {
	text := batchInputText(raw, b.route.provider)
	scan := b.h.runSecurityScanFrom(b.r, b.info, text, b.trace, b.profile, "response")
	b.addScan(scan)
	if gatewaypolicy.From(b.r.Context()).MonitorOnly || scan == nil {
		return raw, nil
	}
	if scan.ShouldBlock {
		return nil, &routeError{403, "stored batch input withheld by current output policy"}
	}
	_, steps := security.StepsFromLegacyProfile(*b.profile)
	available := true
	next, _ := batchMapInput(raw, b.route.provider, func(text string) string {
		s, ok := b.h.security.SanitizeDecisions(text, steps, scan.DetectorActions, nil, true)
		available = available && ok
		return s
	})
	if !available || batchChangesSigned(raw, next) {
		return nil, &routeError{403, "stored batch input cannot be safely rewritten"}
	}
	return next, nil
}

func mapBatchOutput(raw []byte, p providers.Provider, transform textTransform) ([]byte, bool) {
	return mapBatchOutputMode(raw, p, transform, false)
}
func mapBatchOutputMode(raw []byte, p providers.Provider, transform textTransform, skipSigned bool) ([]byte, bool) {
	if p == providers.ProviderGemini {
		return mapGoogleContentMode(raw, transform, skipSigned)
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil || obj == nil {
		return raw, false
	}
	for _, key := range []string{"body", "result", "response", "message"} {
		if value, ok := obj[key]; ok {
			var text string
			if json.Unmarshal(value, &text) == nil {
				next := transform(text)
				if next != text {
					obj[key], _ = json.Marshal(next)
					out, _ := json.Marshal(obj)
					return out, true
				}
				return raw, false
			}
			next, changed := mapBatchOutputMode(value, p, transform, skipSigned)
			if changed {
				obj[key] = next
				out, _ := json.Marshal(obj)
				return out, true
			}
			return raw, false
		}
	}
	if value, ok := obj["error"]; ok {
		var details map[string]json.RawMessage
		if json.Unmarshal(value, &details) == nil {
			changed := false
			for _, key := range []string{"message", "details"} {
				if raw, ok := details[key]; ok {
					next, ch := mapDataStrings(raw, transform)
					details[key] = next
					changed = changed || ch
				}
			}
			if changed {
				obj["error"], _ = json.Marshal(details)
				out, _ := json.Marshal(obj)
				return out, true
			}
		}
	}
	thinking := transform
	if skipSigned {
		thinking = func(s string) string { return s }
	}
	return mapNativeOutputPolicy(raw, transform, thinking)
}

func batchMapThinking(raw []byte, transform textTransform) ([]byte, bool) {
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) == nil && obj != nil {
		changed := false
		if batchString(obj, "type") == "thinking" {
			if value, exists := obj["thinking"]; exists {
				next, ch := mapContent(value, transform)
				obj["thinking"] = next
				changed = ch
			}
		}
		for key, value := range obj {
			if key == "thinking" {
				continue
			}
			next, ch := batchMapThinking(value, transform)
			obj[key] = next
			changed = changed || ch
		}
		if !changed {
			return raw, false
		}
		out, _ := json.Marshal(obj)
		return out, true
	}
	var items []json.RawMessage
	if json.Unmarshal(raw, &items) == nil && items != nil {
		changed := false
		for i, value := range items {
			next, ch := batchMapThinking(value, transform)
			items[i] = next
			changed = changed || ch
		}
		if changed {
			out, _ := json.Marshal(items)
			return out, true
		}
	}
	return raw, false
}
func batchUsage(raw []byte) (int, int) {
	var obj map[string]json.RawMessage
	_ = json.Unmarshal(raw, &obj)
	for _, k := range []string{"body", "result", "response", "message"} {
		if next, ok := obj[k]; ok {
			return batchUsage(next)
		}
	}
	var usage struct {
		Prompt   int `json:"promptTokenCount"`
		Output   int `json:"candidatesTokenCount"`
		Thoughts int `json:"thoughtsTokenCount"`
	}
	if json.Unmarshal(obj["usageMetadata"], &usage) == nil {
		return usage.Prompt, usage.Output + usage.Thoughts
	}
	return nativeUsage(raw)
}
func batchModel(raw []byte) string {
	var obj map[string]json.RawMessage
	_ = json.Unmarshal(raw, &obj)
	if model := batchString(obj, "model", "modelVersion"); model != "" {
		return model
	}
	for _, key := range []string{"body", "result", "response", "message"} {
		if next, ok := obj[key]; ok {
			return batchModel(next)
		}
	}
	return ""
}

func batchChangesSigned(before, after []byte) bool {
	if bytes.Equal(before, after) || !(bytes.Contains(before, []byte(`"signature"`)) || bytes.Contains(before, []byte(`"thoughtSignature"`)) || bytes.Contains(before, []byte(`"thought_signature"`))) {
		return false
	}
	var left, right map[string]json.RawMessage
	if json.Unmarshal(before, &left) == nil && left != nil {
		if json.Unmarshal(after, &right) != nil {
			return true
		}
		if batchString(left, "type") == "thinking" && batchString(left, "signature") != "" && !batchJSONEqual(left["thinking"], right["thinking"]) {
			return true
		}
		if batchString(left, "thoughtSignature", "thought_signature") != "" {
			for _, key := range []string{"text", "functionCall", "function_call"} {
				if !batchJSONEqual(left[key], right[key]) {
					return true
				}
			}
		}
		for key, value := range left {
			if batchChangesSigned(value, right[key]) {
				return true
			}
		}
		return false
	}
	var a, b []json.RawMessage
	if json.Unmarshal(before, &a) == nil {
		if json.Unmarshal(after, &b) != nil || len(a) != len(b) {
			return true
		}
		for i, value := range a {
			if batchChangesSigned(value, b[i]) {
				return true
			}
		}
	}
	return false
}
func batchJSONEqual(a, b []byte) bool {
	var s, t string
	if json.Unmarshal(a, &s) == nil && json.Unmarshal(b, &t) == nil {
		return s == t
	}
	var left, right map[string]json.RawMessage
	if json.Unmarshal(a, &left) == nil && left != nil {
		if json.Unmarshal(b, &right) != nil || len(left) != len(right) {
			return false
		}
		for key, value := range left {
			if !batchJSONEqual(value, right[key]) {
				return false
			}
		}
		return true
	}
	var x, y []json.RawMessage
	if json.Unmarshal(a, &x) == nil && x != nil {
		if json.Unmarshal(b, &y) != nil || len(x) != len(y) {
			return false
		}
		for i, value := range x {
			if !batchJSONEqual(value, y[i]) {
				return false
			}
		}
		return true
	}
	return bytes.Equal(bytes.TrimSpace(a), bytes.TrimSpace(b))
}

// A job can contain 100k items. The operation trace carries a bounded summary;
// item traces carry their own findings. Never mutate an item's live decision.
func (b *batchCall) addScan(scan *security.ScanResult) {
	if scan == nil {
		return
	}
	if b.scan == nil {
		b.scan = &security.ScanResult{}
	}
	if scan.ThreatScore > b.scan.ThreatScore {
		b.scan.ThreatScore = scan.ThreatScore
	}
	if scan.ShouldBlock {
		b.scan.ShouldBlock = true
		b.scan.Action = security.ActionBlock
	}
	if batchActionRank(scan.Action) > batchActionRank(b.scan.Action) {
		b.scan.Action = scan.Action
	}
	if len(b.scan.Findings) < 100 {
		limit := 100 - len(b.scan.Findings)
		if limit > len(scan.Findings) {
			limit = len(scan.Findings)
		}
		b.scan.Findings = append(b.scan.Findings, scan.Findings[:limit]...)
	}
	if len(b.scan.ThreatTypes) < 100 {
		b.scan.ThreatTypes = append(b.scan.ThreatTypes, scan.ThreatTypes...)
	}
}
func batchActionRank(action security.Action) int {
	switch action {
	case security.ActionBlock:
		return 6
	case security.ActionTokenize:
		return 5
	case security.ActionMask, security.ActionRedact:
		return 4
	case security.ActionWarn:
		return 3
	case security.ActionLogOnly, security.ActionLog:
		return 2
	case security.ActionPass:
		return 1
	}
	return 0
}
