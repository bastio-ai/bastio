package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"time"
)

type RemoteTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// RemoteClient supports current per-request MCP and initialization-based
// Streamable HTTP. Each Workspace operation gets its own isolated session.
type RemoteClient struct {
	endpoint, bearer, version, session string
	http                               *http.Client
	ownHTTP                            bool
}

func ValidToolName(name string) bool { return toolNameRE.MatchString(name) }

func ValidateRemoteURL(endpoint string) error {
	u, err := url.Parse(endpoint)
	if err != nil || len(endpoint) > 2048 || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("provide an HTTPS MCP endpoint without credentials or query parameters")
	}
	if strings.EqualFold(u.Hostname(), "localhost") || strings.HasSuffix(strings.ToLower(u.Hostname()), ".localhost") {
		return errors.New("private MCP endpoints are not supported")
	}
	if ip, err := netip.ParseAddr(u.Hostname()); err == nil && !publicMCPAddress(ip) {
		return errors.New("private MCP endpoints are not supported")
	}
	return nil
}
func publicMCPAddress(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	for _, cidr := range []string{"100.64.0.0/10", "198.18.0.0/15", "192.0.0.0/24", "192.0.2.0/24", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "fec0::/10", "2001:db8::/32"} {
		if netip.MustParsePrefix(cidr).Contains(ip) {
			return false
		}
	}
	return true
}

// Resolve and pin the destination on each new connection, preventing DNS
// rebinding into private networks. Redirects and environment proxies are off.
func secureMCPHTTP() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, errors.New("invalid MCP destination")
		}
		addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil || len(addresses) == 0 {
			return nil, errors.New("mcp destination unavailable")
		}
		for _, ip := range addresses {
			if !publicMCPAddress(ip) {
				return nil, errors.New("private MCP destination denied")
			}
		}
		dialer := net.Dialer{Timeout: 10 * time.Second}
		var conn net.Conn
		for _, ip := range addresses {
			conn, err = dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if err == nil {
				return conn, nil
			}
		}
		return nil, errors.New("mcp connection unavailable")
	}
	return &http.Client{Transport: transport, Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

// An injected client is for tests/operator-owned transports, never user input.
func NewRemoteClient(endpoint, bearer string, client *http.Client) (*RemoteClient, error) {
	if err := ValidateRemoteURL(endpoint); err != nil {
		return nil, err
	}
	if len(bearer) > 8192 || strings.ContainsAny(bearer, "\r\n") {
		return nil, errors.New("invalid MCP credential")
	}
	owned := client == nil
	if client == nil {
		client = secureMCPHTTP()
	}
	return &RemoteClient{endpoint: endpoint, bearer: bearer, version: "2026-07-28", http: client, ownHTTP: owned}, nil
}

type remoteRPCError struct{ code, status int }

func (e *remoteRPCError) Error() string {
	return fmt.Sprintf("mcp request failed (HTTP %d, RPC %d)", e.status, e.code)
}

func (c *RemoteClient) ListTools(ctx context.Context) ([]RemoteTool, error) {
	var tools []RemoteTool
	cursor := ""
	for page := 0; page < 5; page++ {
		params := struct {
			Cursor string `json:"cursor,omitempty"`
		}{cursor}
		raw, err := remoteRPC(c, ctx, "tools/list", params)
		if err != nil && page == 0 {
			var rpcErr *remoteRPCError
			if errors.As(err, &rpcErr) && (rpcErr.status == 400 || rpcErr.code == -32600 || rpcErr.code == -32601 || rpcErr.code == -32000) {
				if err = c.initializeLegacy(ctx); err == nil {
					raw, err = remoteRPC(c, ctx, "tools/list", params)
				}
			}
		}
		if err != nil {
			return nil, err
		}
		var result struct {
			Tools  []RemoteTool `json:"tools"`
			Cursor string       `json:"nextCursor"`
		}
		if json.Unmarshal(raw, &result) != nil {
			return nil, errors.New("invalid MCP tool list")
		}
		for _, tool := range result.Tools {
			if !toolNameRE.MatchString(tool.Name) || len(tool.Description) > 8192 || len(tool.InputSchema) > 32<<10 || !json.Valid(tool.InputSchema) {
				return nil, errors.New("unsupported MCP tool definition")
			}
			tools = append(tools, tool)
			if len(tools) > 200 {
				return nil, errors.New("mcp tool list exceeds limit")
			}
		}
		if result.Cursor == "" {
			return tools, nil
		}
		if len(result.Cursor) > 1024 || result.Cursor == cursor {
			return nil, errors.New("invalid MCP pagination")
		}
		cursor = result.Cursor
	}
	return nil, errors.New("mcp tool list exceeds page limit")
}
func (c *RemoteClient) CallTool(ctx context.Context, name string, arguments json.RawMessage) (json.RawMessage, error) {
	return remoteRPC(c, ctx, "tools/call", toolCallParams{Name: name, Arguments: arguments})
}
func (c *RemoteClient) initializeLegacy(ctx context.Context) error {
	c.version = "2025-11-25"
	params := struct {
		Version      string   `json:"protocolVersion"`
		Capabilities struct{} `json:"capabilities"`
		Client       struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"clientInfo"`
	}{Version: c.version}
	params.Client.Name = "Bastio Workspace"
	params.Client.Version = "1.0"
	raw, err := remoteRPC(c, ctx, "initialize", params)
	if err != nil {
		return err
	}
	var result struct {
		Version string `json:"protocolVersion"`
	}
	if json.Unmarshal(raw, &result) != nil || !slices.Contains([]string{"2025-03-26", "2025-06-18", "2025-11-25"}, result.Version) {
		return errors.New("unsupported MCP protocol version")
	}
	c.version = result.Version
	_, err = c.send(ctx, jsonrpcMessage{JSONRPC: "2.0", Method: "notifications/initialized"})
	return err
}
func (c *RemoteClient) Close(ctx context.Context) {
	if c.ownHTTP {
		defer c.http.CloseIdleConnections()
	}
	if c.session == "" {
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.endpoint, nil)
	if err != nil {
		return
	}
	c.headers(req, "", "")
	res, err := c.http.Do(req)
	if err == nil {
		res.Body.Close()
	}
}
func remoteRPC[T any](c *RemoteClient, ctx context.Context, method string, params T) (json.RawMessage, error) {
	// Encode without arbitrary header forwarding. Server tool schemas cannot
	// delegate platform credentials, sampling, roots or elicitation actions.
	encoded, err := json.Marshal(params)
	if err != nil {
		return nil, errors.New("invalid MCP parameters")
	}
	if c.version == "2026-07-28" {
		var fields map[string]json.RawMessage
		if json.Unmarshal(encoded, &fields) != nil {
			return nil, errors.New("invalid MCP parameters")
		}
		fields["_meta"] = json.RawMessage(`{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientInfo":{"name":"Bastio Workspace","version":"1.0"},"io.modelcontextprotocol/clientCapabilities":{}}`)
		encoded, err = json.Marshal(fields)
		if err != nil {
			return nil, err
		}
	}
	return c.send(ctx, jsonrpcMessage{JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: method, Params: encoded})
}
func (c *RemoteClient) headers(req *http.Request, method, name string) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Protocol-Version", c.version)
	if method != "" {
		req.Header.Set("Mcp-Method", method)
	}
	if name != "" {
		req.Header.Set("Mcp-Name", name)
	}
	if c.session != "" {
		req.Header.Set("Mcp-Session-Id", c.session)
	}
	if c.bearer != "" {
		req.Header.Set("Authorization", "Bearer "+c.bearer)
	}
}
func (c *RemoteClient) send(ctx context.Context, message jsonrpcMessage) (json.RawMessage, error) {
	body, err := json.Marshal(message)
	if err != nil {
		return nil, errors.New("invalid MCP message")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, errors.New("invalid MCP endpoint")
	}
	var call toolCallParams
	_ = json.Unmarshal(message.Params, &call)
	c.headers(req, message.Method, call.Name)
	res, err := c.http.Do(req)
	if err != nil {
		return nil, errors.New("mcp service unavailable")
	}
	defer res.Body.Close()
	if session := res.Header.Get("Mcp-Session-Id"); session != "" {
		if len(session) > 128 || strings.ContainsAny(session, "\r\n") {
			return nil, errors.New("invalid MCP session")
		}
		c.session = session
	}
	if len(message.ID) == 0 && (res.StatusCode == 202 || res.StatusCode == 204 || res.StatusCode == 200) {
		return nil, nil
	}
	reader := io.LimitReader(res.Body, 2<<20)
	var reply jsonrpcMessage
	if strings.HasPrefix(res.Header.Get("Content-Type"), "text/event-stream") {
		scanner := bufio.NewScanner(reader)
		scanner.Buffer(make([]byte, 4096), 1<<20)
		var data strings.Builder
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "data:") {
				if data.Len() > 0 {
					data.WriteByte('\n')
				}
				data.WriteString(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
			}
			if line == "" && data.Len() > 0 {
				var event jsonrpcMessage
				if json.Unmarshal([]byte(data.String()), &event) != nil {
					return nil, errors.New("invalid MCP stream")
				}
				data.Reset()
				if bytes.Equal(event.ID, message.ID) && event.Method == "" {
					reply = event
					break
				}
			}
		}
	} else {
		_ = json.NewDecoder(reader).Decode(&reply)
	}
	if reply.Error != nil {
		return nil, &remoteRPCError{reply.Error.Code, res.StatusCode}
	}
	if res.StatusCode != 200 {
		return nil, &remoteRPCError{status: res.StatusCode}
	}
	if reply.JSONRPC != "2.0" || !bytes.Equal(reply.ID, message.ID) || !json.Valid(reply.Result) {
		return nil, errors.New("invalid MCP response")
	}
	return reply.Result, nil
}
