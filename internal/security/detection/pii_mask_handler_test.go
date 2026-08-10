package detection

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPIIMaskHandler_MaskAndUnmask(t *testing.T) {
	handler := NewPIIMaskHandler()

	reqBody := PIIMaskRequest{
		Text: "Please email me at test@example.com for details.",
		Mode: "tokenize",
	}

	bodyBytes, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/mask", bytes.NewReader(bodyBytes))
	w := httptest.NewRecorder()

	handler.Mask(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", w.Code)
	}

	var res PIIMaskResponse
	if err := json.NewDecoder(w.Body).Decode(&res); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(res.Tokens) == 0 {
		t.Errorf("expected tokenized output, got none")
	}

	// Test unmasking
	unmaskReq := PIIUnmaskRequest{
		Text:   res.ProcessedText,
		Tokens: res.Tokens,
	}

	unmaskBytes, _ := json.Marshal(unmaskReq)
	uReq := httptest.NewRequest(http.MethodPost, "/unmask", bytes.NewReader(unmaskBytes))
	uW := httptest.NewRecorder()

	handler.Unmask(uW, uReq)

	if uW.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for unmask, got %d", uW.Code)
	}

	var uRes PIIUnmaskResponse
	_ = json.NewDecoder(uW.Body).Decode(&uRes)

	if uRes.UnmaskedText != reqBody.Text {
		t.Errorf("expected unmasked text '%s', got '%s'", reqBody.Text, uRes.UnmaskedText)
	}
}
