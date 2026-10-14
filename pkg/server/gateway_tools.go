package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"time"
	"unicode"

	"github.com/bastio-ai/bastio/internal/auth"
	"github.com/bastio-ai/bastio/internal/gateway"
	"github.com/bastio-ai/bastio/internal/providers"
	"github.com/bastio-ai/bastio/internal/proxy"
	"github.com/bastio-ai/bastio/pkg/tenant"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const gatewayHelloPrompt = "Say hello in one short, cheerful sentence. Include a tiny robot celebrating a successful connection."

type gatewayToolProxyStore interface {
	GetByID(context.Context, uuid.UUID, uuid.UUID) (*proxy.Proxy, error)
}
type gatewayToolKeyIssuer interface {
	WithTemporaryGatewayKey(context.Context, uuid.UUID, uuid.UUID, func(string)) error
}
type gatewayTools struct {
	proxies  gatewayToolProxyStore
	keys     gatewayToolKeyIssuer
	pipeline http.Handler
	native   func(providers.Provider) bool
	limiter  *gateway.RateLimiter
	origins  []string
}

func (g *gatewayTools) ownedProxy(w http.ResponseWriter, r *http.Request) (*proxy.Proxy, uuid.UUID, bool) {
	cid, err := tenant.FromContext(r.Context())
	if err != nil || cid == uuid.Nil {
		http.Error(w, `{"error":"workspace identity missing"}`, http.StatusUnauthorized)
		return nil, cid, false
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, `{"error":"invalid gateway ID"}`, 400)
		return nil, cid, false
	}
	p, err := g.proxies.GetByID(r.Context(), cid, id)
	if err != nil || p == nil || p.CustomerID != cid || !p.IsActive {
		http.Error(w, `{"error":"gateway not found or disabled"}`, 404)
		return nil, cid, false
	}
	return p, cid, true
}

func (g *gatewayTools) sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}
	if strings.EqualFold(parsed.Host, r.Host) {
		return true
	}
	for _, trusted := range g.origins {
		if trusted != "*" && strings.TrimRight(trusted, "/") == origin {
			return true
		}
	}
	return false
}

// A stable per-user/gateway bucket limits background key issuance too, rather
// than allowing every newly issued key to start a fresh rate-limit bucket.
func (g *gatewayTools) limited(w http.ResponseWriter, r *http.Request, p *proxy.Proxy, cid uuid.UUID, run func()) {
	if g.limiter == nil {
		run()
		return
	}
	actor, _ := r.Context().Value(UserIDKey).(string)
	rpm := 10
	info := &auth.APIKeyInfo{ID: uuid.NewSHA1(uuid.NameSpaceOID, []byte("dashboard-test:"+cid.String()+":"+p.ID.String()+":"+actor)), CustomerID: cid, RateLimitRPM: &rpm}
	g.limiter.Middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { run() })).ServeHTTP(w, r.WithContext(auth.WithInfo(r.Context(), info)))
}

func (g *gatewayTools) forward(w http.ResponseWriter, r *http.Request, p *proxy.Proxy, cid uuid.UUID, method, path string, body []byte) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	err := g.keys.WithTemporaryGatewayKey(ctx, cid, p.ID, func(secret string) {
		// A fresh chi context prevents management-route parameters being reused by
		// the native gateway router. Only server-owned headers enter the pipeline.
		ctx = context.WithValue(ctx, chi.RouteCtxKey, chi.NewRouteContext())
		request := r.Clone(ctx)
		request.Method = method
		request.URL = &url.URL{Path: path}
		request.RequestURI = path
		request.Body = io.NopCloser(bytes.NewReader(body))
		request.ContentLength = int64(len(body))
		request.Header = make(http.Header)
		request.Header.Set("Authorization", "Bearer "+secret)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Cache-Control", "no-store")
		request.Header.Set("X-Bastio-Proxy-Id", p.ID.String())
		request.Header.Set("X-Bastio-Provider", string(p.TargetProvider))
		request.Header.Set("User-Agent", "Bastio dashboard connection test")
		g.pipeline.ServeHTTP(w, request)
	})
	if err != nil {
		http.Error(w, `{"error":"could not prepare gateway test credentials"}`, 503)
	}
}

func (g *gatewayTools) Test(w http.ResponseWriter, r *http.Request) {
	if !g.sameOrigin(r) {
		http.Error(w, `{"error":"gateway tests require a trusted dashboard origin"}`, 403)
		return
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		http.Error(w, `{"error":"JSON body required"}`, 400)
		return
	}
	var input struct {
		Model string `json:"model"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil {
		http.Error(w, `{"error":"invalid gateway test request"}`, 400)
		return
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		http.Error(w, `{"error":"invalid gateway test request"}`, 400)
		return
	}
	input.Model = strings.TrimSpace(input.Model)
	if input.Model == "" || len(input.Model) > 256 || strings.IndexFunc(input.Model, unicode.IsControl) >= 0 {
		http.Error(w, `{"error":"choose a valid model"}`, 400)
		return
	}
	p, cid, ok := g.ownedProxy(w, r)
	if !ok {
		return
	}
	body := map[string]any{"model": input.Model, "stream": false, "messages": []map[string]string{{"role": "user", "content": gatewayHelloPrompt}}}
	path := "/v1/chat/completions"
	if p.TargetProvider == providers.ProviderOpenAI {
		body["max_completion_tokens"] = 512
		if effort := gatewayHelloReasoningEffort(input.Model); effort != "" {
			body["reasoning_effort"] = effort
		}
	} else {
		body["max_tokens"] = 512
	}
	switch p.TargetProvider {
	case providers.ProviderOpenAI, providers.ProviderOpenRouter, providers.ProviderXAI:
		path = "/v1/guard/" + p.ID.String() + "/v1/chat/completions"
	case providers.ProviderAnthropic:
		path = "/v1/guard/" + p.ID.String() + "/v1/messages"
	}
	encoded, _ := json.Marshal(body)
	g.limited(w, r, p, cid, func() { g.forward(w, r, p, cid, "POST", path, encoded) })
}

type gatewayListedModel = providers.ListedModel
type gatewayModelListing struct {
	Provider string               `json:"provider"`
	Source   string               `json:"source"`
	Models   []gatewayListedModel `json:"models"`
}

func (g *gatewayTools) Models(w http.ResponseWriter, r *http.Request) {
	p, cid, ok := g.ownedProxy(w, r)
	if !ok {
		return
	}
	if !g.native(p.TargetProvider) {
		writeJSON(w, 200, gatewayModelListing{string(p.TargetProvider), "catalog", []gatewayListedModel{}})
		return
	}
	g.limited(w, r, p, cid, func() {
		response := httptest.NewRecorder()
		g.forward(response, r, p, cid, "GET", "/v1/guard/"+p.ID.String()+"/v1/models", nil)
		w.Header().Set("Cache-Control", "no-store")
		if response.Code != 200 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(response.Code)
			_, _ = w.Write(response.Body.Bytes())
			return
		}
		models, err := parseGatewayModels(p.TargetProvider, response.Body.Bytes())
		if err != nil {
			http.Error(w, `{"error":"provider returned an invalid model list"}`, 502)
			return
		}
		writeJSON(w, 200, gatewayModelListing{string(p.TargetProvider), "provider", models})
	})
}

func parseGatewayModels(provider providers.Provider, raw []byte) ([]gatewayListedModel, error) {
	return providers.ParseChatModels(provider, raw)
}
func gatewayHelloReasoningEffort(model string) string { return providers.HelloReasoningEffort(model) }
