package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/bastio-ai/bastio/internal/security"
)

// jsonrpcMessage represents a generic JSON-RPC 2.0 message (request, response, or notification).
type jsonrpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonrpcError   `json:"error,omitempty"`
}

type jsonrpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

type toolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

type toolsListResult struct {
	Tools []toolDefinition `json:"tools"`
}

type toolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema,omitempty"`
}

type toolCallResult struct {
	Content []toolContentBlock `json:"content"`
	IsError bool               `json:"isError,omitempty"`
}

type toolContentBlock struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Data     string `json:"data,omitempty"`
	MIMEType string `json:"mimeType,omitempty"`
}

// StdioProxy implements an inline Model Context Protocol (MCP) tool security firewall.
// It wraps an MCP server child process over standard I/O streams and scans line-by-line
// JSON-RPC requests and responses for prompt injection, destructive tool commands, PII,
// and secret leaks.
type StdioProxy struct {
	engine      *security.Engine
	profileName string
	command     string
	args        []string
	in          io.Reader
	out         io.Writer
	errOut      io.Writer

	pendingMu sync.Mutex
	pending   map[string]pendingCall
	runtime   *RuntimeClient
	tokenEnv  string

	outMu sync.Mutex // guards writes to out
}

// NewStdioProxy creates a new MCP StdioProxy.
func NewStdioProxy(
	engine *security.Engine,
	profileName string,
	command string,
	args []string,
	in io.Reader,
	out io.Writer,
	errOut io.Writer,
) *StdioProxy {
	return &StdioProxy{
		engine:      engine,
		profileName: profileName,
		command:     command,
		args:        args,
		in:          in,
		out:         out,
		errOut:      errOut,
		pending:     make(map[string]pendingCall),
	}
}

// Run spawns the configured child process and proxies stdin/stdout/stderr with security scanning.
// It blocks until the child process exits or the context is cancelled.
func (p *StdioProxy) Run(ctx context.Context) error {
	if p.command == "" {
		return errors.New("mcp: command cannot be empty")
	}

	cmd := exec.CommandContext(ctx, p.command, p.args...)
	// A third-party tool server must never inherit its enrollment credential.
	secretEnv := p.tokenEnv
	if secretEnv == "" {
		secretEnv = "BASTIO_MCP_TOKEN"
	}
	cmd.Env = make([]string, 0, len(os.Environ()))
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, secretEnv+"=") && !strings.HasPrefix(entry, "BASTIO_MCP_TOKEN=") {
			cmd.Env = append(cmd.Env, entry)
		}
	}

	childIn, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("mcp: open stdin pipe: %w", err)
	}
	defer childIn.Close()

	childOut, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("mcp: open stdout pipe: %w", err)
	}
	defer childOut.Close()

	childErr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("mcp: open stderr pipe: %w", err)
	}
	defer childErr.Close()

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("mcp: start child process %q: %w", p.command, err)
	}

	slog.Info("mcp proxy started child process", "command", p.command)

	serveErr := p.Serve(ctx, p.in, p.out, childIn, childOut, childErr, p.errOut)

	// Wait for child process to exit
	waitErr := cmd.Wait()
	if serveErr != nil && !errors.Is(serveErr, io.EOF) {
		return serveErr
	}
	if waitErr != nil && ctx.Err() == nil {
		return fmt.Errorf("mcp: child process exited with error: %w", waitErr)
	}
	return nil
}

// Serve coordinates streams between the MCP client and child server.
func (p *StdioProxy) Serve(
	ctx context.Context,
	clientIn io.Reader,
	clientOut io.Writer,
	childIn io.Writer,
	childOut io.Reader,
	childErr io.Reader,
	errOut io.Writer,
) error {
	streamCtx, cancelStreams := context.WithCancel(ctx)
	defer cancelStreams()
	interruptsDone := make(chan struct{})
	defer close(interruptsDone)
	// Context checks alone cannot interrupt a blocking stdin read. Close pipe
	// readers on shutdown so a terminated child or client cannot strand Run.
	go func() {
		select {
		case <-streamCtx.Done():
			for _, stream := range []io.Reader{clientIn, childOut, childErr} {
				if closer, ok := stream.(io.Closer); ok {
					_ = closer.Close()
				}
			}
			if closer, ok := childIn.(io.Closer); ok {
				_ = closer.Close()
			}
		case <-interruptsDone:
		}
	}()
	var wg sync.WaitGroup
	errCh := make(chan error, 3)

	// Forward stderr directly to errOut
	if childErr != nil && errOut != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = io.Copy(errOut, childErr)
		}()
	}

	// Client -> Child processing
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := p.processClientStream(streamCtx, clientIn, clientOut, childIn); err != nil && !errors.Is(err, io.EOF) && streamCtx.Err() == nil {
			errCh <- err
		}
		// Close child stdin if closer
		if closer, ok := childIn.(io.Closer); ok {
			_ = closer.Close()
		}
	}()

	// Child -> Client processing
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := p.processServerStream(streamCtx, childOut, clientOut); err != nil && !errors.Is(err, io.EOF) && streamCtx.Err() == nil {
			errCh <- err
		}
		// Only real streams need EOF cancellation. Buffer fixtures are already
		// finite and can finish their independent reads without interruption.
		if _, ok := childOut.(io.Closer); ok {
			cancelStreams()
		}
	}()

	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			return err
		}
	}
	return nil
}

func (p *StdioProxy) processClientStream(ctx context.Context, clientIn io.Reader, clientOut io.Writer, childIn io.Writer) error {
	reader := bufio.NewReaderSize(clientIn, 64*1024)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line, err := readBoundedLine(reader)
		if len(line) > 0 {
			trimmed := bytes.TrimRight(line, "\r\n")
			if len(trimmed) > 0 {
				p.handleClientMessage(ctx, trimmed, clientOut, childIn)
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
}

func (p *StdioProxy) handleClientMessage(ctx context.Context, raw []byte, clientOut io.Writer, childIn io.Writer) {
	var msg jsonrpcMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		// Not valid JSON-RPC; forward unchanged
		p.writeToChild(childIn, raw)
		return
	}

	reqIDStr := string(msg.ID)
	if msg.Method != "" && len(msg.ID) > 0 {
		if !p.trackRequest(reqIDStr, msg.Method) {
			p.reject(clientOut, msg.ID, "Too many pending MCP requests")
			return
		}
	}

	if msg.Method == "tools/call" {
		var params toolCallParams
		err := json.Unmarshal(msg.Params, &params)
		if len(params.Arguments) == 0 {
			params.Arguments = json.RawMessage(`{}`)
		}
		params.Arguments = bytes.TrimSpace(params.Arguments)
		if err != nil || !toolNameRE.MatchString(params.Name) || !json.Valid(params.Arguments) || params.Arguments[0] != '{' {
			p.reject(clientOut, msg.ID, "Invalid MCP tool call")
			p.popRequest(reqIDStr)
			return
		}
		policy := Policy{}
		if p.runtime != nil {
			var fresh bool
			policy, fresh = p.runtime.Policy()
			if !fresh {
				p.record(params.Name, "request", "blocked", []string{"control_unavailable"}, 0)
				p.reject(clientOut, msg.ID, "MCP enrollment unavailable, stale or revoked")
				p.popRequest(reqIDStr)
				return
			}
		}
		decision := Inspect(ctx, p.engine, InspectRequest{ToolName: params.Name, Arguments: params.Arguments, Policy: policy})
		p.record(params.Name, "request", decision.Action, decision.Categories, decision.DurationMS)
		if decision.Action == "blocked" {
			p.reject(clientOut, msg.ID, "Blocked by Bastio Security: tool policy or unsafe action")
			p.popRequest(reqIDStr)
			return
		}
		if len(msg.ID) > 0 {
			p.pendingMu.Lock()
			call := p.pending[reqIDStr]
			call.tool = params.Name
			p.pending[reqIDStr] = call
			p.pendingMu.Unlock()
		}
		if decision.Action == "sanitized" {
			params.Arguments = decision.Arguments
			body, err := replaceJSONField(msg.Params, "arguments", decision.Arguments)
			if err == nil {
				msg.Params = body
				rewritten, err := replaceJSONField(raw, "params", body)
				if err == nil {
					p.writeToChild(childIn, rewritten)
					return
				}
			}
			p.reject(clientOut, msg.ID, "Unable to sanitize MCP arguments safely")
			p.popRequest(reqIDStr)
			return
		}
	}

	// Forward unchanged
	p.writeToChild(childIn, raw)
}

func (p *StdioProxy) processServerStream(ctx context.Context, childOut io.Reader, clientOut io.Writer) error {
	reader := bufio.NewReaderSize(childOut, 64*1024)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line, err := readBoundedLine(reader)
		if len(line) > 0 {
			trimmed := bytes.TrimRight(line, "\r\n")
			if len(trimmed) > 0 {
				p.handleServerMessage(ctx, trimmed, clientOut)
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
}

func (p *StdioProxy) handleServerMessage(ctx context.Context, raw []byte, clientOut io.Writer) {
	var msg jsonrpcMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		p.writeToClient(clientOut, raw)
		return
	}

	var call pendingCall
	if len(msg.ID) > 0 {
		call = p.popRequest(string(msg.ID))
	}
	method := call.method

	// 1. Inspect tools/list responses for prompt injection in descriptions
	if (method == "tools/list" || bytes.Contains(msg.Result, []byte(`"tools"`))) && len(msg.Result) > 0 {
		var listRes toolsListResult
		if err := json.Unmarshal(msg.Result, &listRes); err == nil && len(listRes.Tools) > 0 {
			modified := false
			for i := range listRes.Tools {
				desc := listRes.Tools[i].Description
				if desc == "" || p.engine == nil {
					continue
				}

				scanRes := p.engine.Scan(ctx, &security.ScanRequest{Content: desc})
				if scanRes != nil && (scanRes.ShouldBlock || len(scanRes.Findings) > 0) {
					slog.Warn("mcp: malicious tool description detected in tools/list response",
						"tool", listRes.Tools[i].Name,
						"threat_types", scanRes.ThreatTypes,
					)
					p.record(listRes.Tools[i].Name, "tool_description", "sanitized", categoriesFromScan(scanRes), float64(scanRes.ScanDuration.Microseconds())/1000)
					listRes.Tools[i].Description = "[BLOCKED BY BASTIO: Malicious tool description detected]"
					modified = true
				}
			}

			if modified {
				if newResultBytes, err := rewriteToolDescriptions(msg.Result, listRes.Tools); err == nil {
					msg.Result = newResultBytes
					if rewritten, err := replaceJSONField(raw, "result", newResultBytes); err == nil {
						p.writeToClient(clientOut, rewritten)
						return
					}
				}
			}
		}
	}

	// 2. Inspect tools/call responses for leaked secrets or PII
	if (method == "tools/call" || bytes.Contains(msg.Result, []byte(`"content"`))) && len(msg.Result) > 0 {
		var callRes toolCallResult
		if err := json.Unmarshal(msg.Result, &callRes); err == nil {
			modified := false
			action := "allowed"
			categories := []string{}
			for i := range callRes.Content {
				if callRes.Content[i].Type == "text" && callRes.Content[i].Text != "" && p.engine != nil {
					text := callRes.Content[i].Text
					scanRes := p.engine.Scan(ctx, &security.ScanRequest{
						Content:   text,
						PIIAction: security.ActionMask,
					})

					if scanRes != nil {
						categories = append(categories, categoriesFromScan(scanRes)...)
						if scanRes.ShouldBlock {
							callRes.Content[i].Text = "[BLOCKED BY BASTIO: Unsafe tool result]"
							modified = true
							action = "blocked"
							continue
						}
						if scanRes.SanitizedContent != "" && scanRes.SanitizedContent != text {
							callRes.Content[i].Text = scanRes.SanitizedContent
							if action != "blocked" {
								action = "sanitized"
							}
							modified = true
						}
					}
				}
			}

			var fields map[string]json.RawMessage
			if json.Unmarshal(msg.Result, &fields) == nil && len(fields["structuredContent"]) > 0 && p.engine != nil {
				structured := fields["structuredContent"]
				scan := p.engine.Scan(ctx, &security.ScanRequest{Content: string(structured), PIIAction: security.ActionMask})
				if scan != nil {
					categories = append(categories, categoriesFromScan(scan)...)
					if scan.ShouldBlock {
						delete(fields, "structuredContent")
						modified = true
						action = "blocked"
					} else if scan.SanitizedContent != "" && scan.SanitizedContent != string(structured) {
						if json.Valid([]byte(scan.SanitizedContent)) {
							fields["structuredContent"] = json.RawMessage(scan.SanitizedContent)
						} else {
							delete(fields, "structuredContent")
						}
						modified = true
						if action != "blocked" {
							action = "sanitized"
						}
					}
				}
				// Keep the validated structure for the protocol-preserving rewrite.
				msg.Result, _ = json.Marshal(fields)
			}
			if call.tool != "" {
				p.record(call.tool, "response", action, categories, float64(time.Since(call.started).Microseconds())/1000)
			}
			if modified {
				if newResultBytes, err := rewriteToolContent(msg.Result, callRes.Content); err == nil {
					msg.Result = newResultBytes
					if rewritten, err := replaceJSONField(raw, "result", newResultBytes); err == nil {
						p.writeToClient(clientOut, rewritten)
						return
					}
				}
			}
		}
	}

	p.writeToClient(clientOut, raw)
}

func (p *StdioProxy) trackRequest(reqID, method string) bool {
	p.pendingMu.Lock()
	defer p.pendingMu.Unlock()
	for id, call := range p.pending {
		if time.Since(call.started) > 10*time.Minute {
			delete(p.pending, id)
		}
	}
	if len(p.pending) >= 1024 {
		return false
	}
	if _, exists := p.pending[reqID]; exists {
		return false
	}
	p.pending[reqID] = pendingCall{method: method, started: time.Now()}
	return true
}

func (p *StdioProxy) popRequest(reqID string) pendingCall {
	p.pendingMu.Lock()
	defer p.pendingMu.Unlock()
	method := p.pending[reqID]
	delete(p.pending, reqID)
	return method
}

func (p *StdioProxy) writeToChild(childIn io.Writer, b []byte) {
	if childIn == nil {
		return
	}
	_, _ = childIn.Write(append(bytes.TrimRight(b, "\r\n"), '\n'))
}

func (p *StdioProxy) writeToClient(clientOut io.Writer, b []byte) {
	if clientOut == nil {
		return
	}
	p.outMu.Lock()
	defer p.outMu.Unlock()
	_, _ = clientOut.Write(append(bytes.TrimRight(b, "\r\n"), '\n'))
}

type pendingCall struct {
	method  string
	tool    string
	started time.Time
}

func (p *StdioProxy) SetRuntime(c *RuntimeClient) { p.runtime = c }
func (p *StdioProxy) record(tool, direction, action string, categories []string, ms float64) {
	if p.runtime == nil {
		return
	}
	if !toolNameRE.MatchString(tool) {
		tool = "unknown_tool"
	}
	id, err := uuid.NewV7()
	if err != nil {
		return
	}
	unique := []string{}
	for _, cat := range categories {
		if toolNameRE.MatchString(cat) && !slices.Contains(unique, cat) && len(unique) < 16 {
			unique = append(unique, cat)
		}
	}
	categories = unique
	ms = min(max(ms, 0), 600000)
	p.runtime.Record(Event{ID: id, ToolName: tool, Direction: direction, Action: action, Categories: categories, DurationMS: ms, OccurredAt: time.Now().UTC()})
}
func categoriesFromScan(result *security.ScanResult) []string {
	out := []string{}
	for _, v := range result.ThreatTypes {
		out = append(out, string(v))
	}
	return out
}
func (p *StdioProxy) reject(out io.Writer, id json.RawMessage, message string) {
	if len(id) == 0 {
		return
	}
	body, _ := json.Marshal(jsonrpcMessage{JSONRPC: "2.0", ID: id, Error: &jsonrpcError{Code: -32600, Message: message}})
	p.writeToClient(out, body)
}

func (p *StdioProxy) SetTokenEnv(name string) { p.tokenEnv = name }

// Bound wire messages without truncating or forwarding a partial JSON value.
func readBoundedLine(r *bufio.Reader) ([]byte, error) {
	var line []byte
	for {
		part, err := r.ReadSlice('\n')
		if len(line)+len(part) > 4<<20 {
			return nil, errors.New("mcp: message exceeds 4 MiB limit")
		}
		line = append(line, part...)
		if !errors.Is(err, bufio.ErrBufferFull) {
			return line, err
		}
	}
}

// Preserve JSON-RPC extensions and tool annotations when rewriting inspected fields.
func replaceJSONField(raw []byte, key string, value json.RawMessage) ([]byte, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	fields[key] = value
	return json.Marshal(fields)
}
func rewriteToolDescriptions(raw json.RawMessage, tools []toolDefinition) ([]byte, error) {
	var result struct {
		Tools []json.RawMessage `json:"tools"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	for i, tool := range tools {
		encoded, _ := json.Marshal(tool.Description)
		var err error
		result.Tools[i], err = replaceJSONField(result.Tools[i], "description", encoded)
		if err != nil {
			return nil, err
		}
	}
	encoded, err := json.Marshal(result.Tools)
	if err != nil {
		return nil, err
	}
	return replaceJSONField(raw, "tools", encoded)
}
func rewriteToolContent(raw json.RawMessage, content []toolContentBlock) ([]byte, error) {
	var result struct {
		Content []json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	for i, block := range content {
		if block.Type != "text" {
			continue
		}
		encoded, _ := json.Marshal(block.Text)
		var err error
		result.Content[i], err = replaceJSONField(result.Content[i], "text", encoded)
		if err != nil {
			return nil, err
		}
	}
	encoded, err := json.Marshal(result.Content)
	if err != nil {
		return nil, err
	}
	// Preserve separately inspected structured content and protocol extensions.
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	fields["content"] = encoded
	return json.Marshal(fields)
}
