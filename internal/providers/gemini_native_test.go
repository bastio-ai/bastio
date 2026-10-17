package providers

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestGeminiNativeUploadsAuthenticateToConfiguredOrigin(t *testing.T) {
	called := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called++
		if r.Header.Get("X-Goog-Api-Key") != "provider-key" || r.Header.Get("Authorization") != "" || r.URL.Query().Get("key") != "" {
			t.Error("provider credential boundary broken")
		}
		if r.URL.Path != "/upload/v1beta/files" || r.Header.Get("X-Goog-Upload-Command") != "upload, finalize" {
			t.Errorf("upload contract %s", r.URL)
		}
		raw, _ := io.ReadAll(r.Body)
		if string(raw) != "protected file" {
			t.Error("streamed body lost")
		}
	}))
	defer server.Close()
	client := NewGeminiClientWithBaseURL(server.URL + "/v1beta")
	req := &NativeRequest{Method: "POST", Path: "/upload/v1beta/files", Query: url.Values{"key": {"bastio-secret"}}, Headers: http.Header{"X-Goog-Api-Key": {"bastio-secret"}, "Authorization": {"Bearer bastio-secret"}, "X-Goog-Upload-Command": {"upload, finalize"}}, BodyReader: strings.NewReader("protected file"), ContentLength: 14, UploadURL: server.URL + "/upload/v1beta/files?upload_id=fixture&key=bastio-secret"}
	resp, err := client.Forward(context.Background(), req, "provider-key")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if called != 1 {
		t.Fatal("request missing")
	}
	for _, bad := range []string{"https://evil.example/upload/v1beta/files", server.URL + "/v1beta/models", server.URL + "/upload/../models", strings.Replace(server.URL, "http:", "https:", 1) + "/upload/v1beta/files"} {
		copy := *req
		copy.UploadURL = bad
		if _, err = client.Forward(context.Background(), &copy, "provider-key"); err == nil {
			t.Fatal("untrusted upload destination accepted")
		}
	}
	if called != 1 {
		t.Fatal("untrusted upload made a network call")
	}
}
