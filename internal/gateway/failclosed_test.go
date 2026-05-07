package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/bastio-ai/bastio/internal/auth"
	"github.com/bastio-ai/bastio/internal/providers"
	"github.com/bastio-ai/bastio/internal/security"
)

// erroringProfileLookup simulates a DB hiccup during profile load.
// Fail-open should swallow it and proceed with DefaultProfile;
// fail-closed should block the request at the gateway boundary.
type erroringProfileLookup struct{}

func (erroringProfileLookup) GetDefault(_ context.Context, _ uuid.UUID) (*security.Profile, error) {
	return nil, errors.New("simulated db failure")
}

func newRequestWithAuth() *http.Request {
	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	return req.WithContext(auth.WithInfo(req.Context(), &auth.APIKeyInfo{
		ID:         uuid.New(),
		CustomerID: uuid.New(),
	}))
}

// TestChatCompletions_FailClosedBlocksOnProfileError pins the
// Security Center's "Fail Closed" semantics: when the security engine
// can't evaluate a request (here, a profile-load DB error), the
// gateway returns 503 with an explicit reason. In fail-open mode the
// same failure would proceed with defaults.
func TestChatCompletions_FailClosedBlocksOnProfileError(t *testing.T) {
	engine := security.NewEngine() // no detectors — result is always Pass
	h := NewHandler(providers.NewRegistry(), nil, engine, erroringProfileLookup{}, nil, "fail-closed")

	rr := httptest.NewRecorder()
	h.ChatCompletions(rr, newRequestWithAuth())

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d body=%s", rr.Code, rr.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("response body not JSON: %v", err)
	}
	if body["mode"] != "fail-closed" {
		t.Errorf("response should identify fail-closed mode, got %v", body["mode"])
	}
	if _, ok := body["trace_id"].(string); !ok {
		t.Error("response missing trace_id for correlation")
	}
	if _, ok := body["reason"].(string); !ok {
		t.Error("response missing reason")
	}
}

// TestChatCompletions_FailOpenProceedsOnProfileError is the flip
// side: a profile-load error in fail-open mode must NOT block. The
// request proceeds to provider routing (which fails for a different
// reason here, with no registered providers — either way, not 503).
func TestChatCompletions_FailOpenProceedsOnProfileError(t *testing.T) {
	engine := security.NewEngine()
	h := NewHandler(providers.NewRegistry(), nil, engine, erroringProfileLookup{}, nil, "fail-open")

	rr := httptest.NewRecorder()
	h.ChatCompletions(rr, newRequestWithAuth())

	if rr.Code == http.StatusServiceUnavailable {
		t.Fatalf("fail-open must not 503 on profile error; got %d body=%s", rr.Code, rr.Body.String())
	}
}

// TestChatCompletions_FailClosedBlocksOnNilEngine covers the startup-
// misconfig case: security engine is nil (plausible if someone wired
// the handler wrong). Fail-closed must refuse traffic rather than
// silently letting everything through.
func TestChatCompletions_FailClosedBlocksOnNilEngine(t *testing.T) {
	h := NewHandler(providers.NewRegistry(), nil, nil, nil, nil, "fail-closed")

	rr := httptest.NewRecorder()
	h.ChatCompletions(rr, newRequestWithAuth())

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d body=%s", rr.Code, rr.Body.String())
	}
}
