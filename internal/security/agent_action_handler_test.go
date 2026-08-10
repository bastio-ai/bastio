package security

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAgentActionHandler_DangerousCommand(t *testing.T) {
	handler := NewAgentActionHandler()

	reqBody := AgentActionRequest{
		ToolName: "run_bash",
		Arguments: map[string]interface{}{
			"command": "rm -rf /",
		},
	}

	bodyBytes, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/agent-action", bytes.NewReader(bodyBytes))
	w := httptest.NewRecorder()

	handler.InspectAction(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden for dangerous command, got %d", w.Code)
	}

	var res AgentActionResponse
	if err := json.NewDecoder(w.Body).Decode(&res); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if res.Allowed {
		t.Errorf("expected allowed=false for dangerous command")
	}

	if res.Action != "block" {
		t.Errorf("expected action=block, got %s", res.Action)
	}
}
