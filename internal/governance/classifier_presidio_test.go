package governance

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestPresidioCollapseEntities_HighRisk(t *testing.T) {
	got := collapseEntities([]presidioEntity{
		{EntityType: "CREDIT_CARD", Score: 0.95},
		{EntityType: "EMAIL_ADDRESS", Score: 0.85},
	})
	if got.Severity != SeverityHigh {
		t.Fatalf("want high, got %s", got.Severity)
	}
	if got.Confidence < 0.9 {
		t.Fatalf("expected confidence near 0.95, got %f", got.Confidence)
	}
}

func TestPresidioCollapseEntities_MediumCluster(t *testing.T) {
	got := collapseEntities([]presidioEntity{
		{EntityType: "EMAIL_ADDRESS", Score: 0.85},
		{EntityType: "PHONE_NUMBER", Score: 0.85},
		{EntityType: "PERSON", Score: 0.9},
	})
	if got.Severity != SeverityHigh {
		t.Fatalf("3 medium entities should cluster to high, got %s", got.Severity)
	}
}

func TestPresidioCollapseEntities_SingleMedium(t *testing.T) {
	got := collapseEntities([]presidioEntity{
		{EntityType: "EMAIL_ADDRESS", Score: 0.85},
	})
	if got.Severity != SeverityMedium {
		t.Fatalf("want medium, got %s", got.Severity)
	}
}

func TestPresidioCollapseEntities_Empty(t *testing.T) {
	got := collapseEntities(nil)
	if got.Severity != SeverityLow {
		t.Fatalf("empty entity list should be low, got %s", got.Severity)
	}
}

// TestPresidioAnalyze_HappyPath stands up a fake Presidio sidecar and
// verifies the client posts the expected body, parses the response, and
// returns a high-severity ClassifyResponse.
func TestPresidioAnalyze_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/analyze" || r.Method != http.MethodPost {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body presidioRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode req: %v", err)
		}
		if body.Language != "en" {
			t.Errorf("expected language=en, got %s", body.Language)
		}
		_ = json.NewEncoder(w).Encode([]presidioEntity{
			{EntityType: "US_SSN", Score: 0.99},
		})
	}))
	defer srv.Close()

	cli := &PresidioClient{url: srv.URL, httpc: srv.Client()}
	resp, ok, err := cli.Analyze(context.Background(), "patient ssn 123-45-6789")
	if err != nil || !ok {
		t.Fatalf("expected ok=true, got ok=%v err=%v", ok, err)
	}
	if resp.Severity != SeverityHigh {
		t.Fatalf("want high, got %s", resp.Severity)
	}
}

// TestPresidioAnalyze_ServerError verifies non-2xx responses return ok=false
// (caller falls back to the rule heuristic).
func TestPresidioAnalyze_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	cli := &PresidioClient{url: srv.URL, httpc: srv.Client()}
	_, ok, err := cli.Analyze(context.Background(), "anything")
	if ok {
		t.Fatal("expected ok=false on server error")
	}
	if err == nil {
		t.Fatal("expected error on server 500")
	}
}

// TestClassifyFallbackUsedWhenPresidioMissing exercises the production code
// path: PRESIDIO_URL unset, classifyWithPresidioOrFallback should return the
// same result as classifyFallback alone.
func TestClassifyFallbackUsedWhenPresidioMissing(t *testing.T) {
	t.Setenv("PRESIDIO_URL", "")
	// Reset the lazy init for this test.
	presidioClient = nil
	presidioInitOnce = sync.Once{}

	req := ClassifyRequest{
		Layer3Hits:   []string{"secret.aws_access_key"},
		SourceDomain: "chatgpt.com",
	}
	want := classifyFallback(req)
	got := classifyWithPresidioOrFallback(context.Background(), req)
	if got.Severity != want.Severity {
		t.Fatalf("fallback mismatch: want %s got %s", want.Severity, got.Severity)
	}
}
