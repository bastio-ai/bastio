package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"strings"
	"testing"
)

type remoteTransport func(*http.Request) (*http.Response, error)

func (f remoteTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func remoteResponse(status int, body, contentType string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{contentType}}, Body: io.NopCloser(strings.NewReader(body))}
}

func TestRemoteMCPRejectsPrivateDestinations(t *testing.T) {
	for _, endpoint := range []string{"http://example.com/mcp", "https://localhost/mcp", "https://127.0.0.1/mcp", "https://[::ffff:127.0.0.1]/mcp", "https://169.254.169.254/mcp", "https://user:secret@example.com/mcp", "https://example.com/mcp?token=secret"} {
		if ValidateRemoteURL(endpoint) == nil {
			t.Fatalf("accepted unsafe endpoint %s", endpoint)
		}
	}
	for _, address := range []string{"10.8.0.2", "100.64.0.1", "172.16.0.1", "192.168.1.1", "::1", "fe80::1", "fc00::1", "198.18.0.1", "203.0.113.1"} {
		if publicMCPAddress(netip.MustParseAddr(address)) {
			t.Fatalf("accepted private or reserved address %s", address)
		}
	}
	if !publicMCPAddress(netip.MustParseAddr("1.1.1.1")) {
		t.Fatal("public address denied")
	}
	client := secureMCPHTTP()
	_, err := client.Transport.(*http.Transport).DialContext(context.Background(), "tcp", "127.0.0.1:443")
	if err == nil || !strings.Contains(err.Error(), "private") {
		t.Fatalf("private resolved destination was not denied: %v", err)
	}

}

func TestRemoteMCPModernSSEAndLegacySessions(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(fmt.Sprintf("legacy=%t", legacy), func(t *testing.T) {
			initialized := false
			closed := false
			calls := 0
			clientHTTP := &http.Client{Transport: remoteTransport(func(r *http.Request) (*http.Response, error) {
				if r.Header.Get("Authorization") != "Bearer fixture-token" {
					t.Error("missing scoped credential")
				}
				if r.Method == http.MethodDelete {
					closed = true
					return remoteResponse(200, "{}", "application/json"), nil
				}
				var rpc jsonrpcMessage
				_ = json.NewDecoder(r.Body).Decode(&rpc)
				if legacy && !initialized && rpc.Method != "initialize" {
					return remoteResponse(400, `{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"initialize first"}}`, "application/json"), nil
				}
				if rpc.Method == "initialize" {
					initialized = true
					res := remoteResponse(200, `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-06-18"}}`, "application/json")
					res.Header.Set("Mcp-Session-Id", "fixture-session")
					return res, nil
				}
				if rpc.Method == "notifications/initialized" {
					return remoteResponse(202, "", "application/json"), nil
				}
				if legacy && r.Header.Get("Mcp-Session-Id") != "fixture-session" {
					t.Error("session not isolated/preserved")
				}
				if !legacy && (r.Header.Get("Mcp-Method") != rpc.Method || !strings.Contains(string(rpc.Params), "io.modelcontextprotocol/protocolVersion")) {
					t.Error("modern metadata missing")
				}
				result := `{"tools":[{"name":"search","inputSchema":{"type":"object"}}]}`
				if rpc.Method == "tools/call" {
					calls++
					if !legacy && r.Header.Get("Mcp-Name") != "search" {
						t.Error("missing MCP name")
					}
					result = `{"content":[{"type":"text","text":"safe fixture"}]}`
				}
				body := `{"jsonrpc":"2.0","id":1,"result":` + result + `}`
				if !legacy {
					return remoteResponse(200, "event: message\ndata: "+body+"\n\n", "text/event-stream"), nil
				}
				return remoteResponse(200, body, "application/json"), nil
			})}
			client, err := NewRemoteClient("https://mcp.example.com/mcp", "fixture-token", clientHTTP)
			if err != nil {
				t.Fatal(err)
			}
			tools, err := client.ListTools(context.Background())
			if err != nil || len(tools) != 1 {
				t.Fatalf("discovery: %v", err)
			}
			_, err = client.CallTool(context.Background(), "search", json.RawMessage(`{}`))
			if err != nil || calls != 1 {
				t.Fatalf("call was retried or failed: %v", err)
			}
			client.Close(context.Background())
			if legacy && !closed {
				t.Fatal("legacy session not closed")
			}
		})
	}
}
