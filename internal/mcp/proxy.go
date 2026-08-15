package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"sync"

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
	pending   map[string]string // reqID string -> method

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
		pending:     make(map[string]string),
	}
}

// Run spawns the configured child process and proxies stdin/stdout/stderr with security scanning.
// It blocks until the child process exits or the context is cancelled.
func (p *StdioProxy) Run(ctx context.Context) error {
	if p.command == "" {
		return errors.New("mcp: command cannot be empty")
	}

	cmd := exec.CommandContext(ctx, p.command, p.args...)

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

	slog.Info("mcp proxy started child process", "command", p.command, "args", p.args)

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
		if err := p.processClientStream(ctx, clientIn, clientOut, childIn); err != nil && !errors.Is(err, io.EOF) {
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
		if err := p.processServerStream(ctx, childOut, clientOut); err != nil && !errors.Is(err, io.EOF) {
			errCh <- err
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

		line, err := reader.ReadBytes('\n')
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
		p.trackRequest(reqIDStr, msg.Method)
	}

	if msg.Method == "tools/call" {
		var params toolCallParams
		if err := json.Unmarshal(msg.Params, &params); err == nil {
			argContent := string(params.Arguments)
			scanReq := &security.ScanRequest{
				Content: argContent,
			}

			var scanRes *security.ScanResult
			if p.engine != nil && argContent != "" {
				scanRes = p.engine.Scan(ctx, scanReq)
			}

			if scanRes != nil && scanRes.ShouldBlock {
				slog.Warn("mcp: blocked tool call due to security policy violation",
					"tool", params.Name,
					"threat_types", scanRes.ThreatTypes,
					"threat_score", scanRes.ThreatScore,
				)

				idVal := msg.ID
				if len(idVal) == 0 {
					idVal = json.RawMessage("null")
				}

				errResp := fmt.Sprintf(
					`{"jsonrpc":"2.0","id":%s,"error":{"code":-32600,"message":"Blocked by Bastio Security: destructive action or policy violation detected"}}`+"\n",
					string(idVal),
				)
				p.writeToClient(clientOut, []byte(errResp))
				return
			}

			if scanRes != nil && (scanRes.Action == security.ActionMask || scanRes.Action == security.ActionTokenize) {
				sanitized := scanRes.SanitizedContent
				if sanitized != "" && sanitized != argContent {
					var newArgs json.RawMessage
					if json.Valid([]byte(sanitized)) {
						newArgs = json.RawMessage(sanitized)
					} else {
						quoted, _ := json.Marshal(sanitized)
						newArgs = json.RawMessage(quoted)
					}
					params.Arguments = newArgs
					newParamsBytes, err := json.Marshal(params)
					if err == nil {
						msg.Params = newParamsBytes
						rewritten, err := json.Marshal(msg)
						if err == nil {
							p.writeToChild(childIn, rewritten)
							return
						}
					}
				}
			}
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

		line, err := reader.ReadBytes('\n')
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

	var method string
	if len(msg.ID) > 0 {
		method = p.popRequest(string(msg.ID))
	}

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
					listRes.Tools[i].Description = "[BLOCKED BY BASTIO: Malicious tool description detected]"
					modified = true
				}
			}

			if modified {
				if newResultBytes, err := json.Marshal(listRes); err == nil {
					msg.Result = newResultBytes
					if rewritten, err := json.Marshal(msg); err == nil {
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
		if err := json.Unmarshal(msg.Result, &callRes); err == nil && len(callRes.Content) > 0 {
			modified := false
			for i := range callRes.Content {
				if callRes.Content[i].Type == "text" && callRes.Content[i].Text != "" && p.engine != nil {
					text := callRes.Content[i].Text
					scanRes := p.engine.Scan(ctx, &security.ScanRequest{
						Content:   text,
						PIIAction: security.ActionMask,
					})

					if scanRes != nil {
						if scanRes.SanitizedContent != "" && scanRes.SanitizedContent != text {
							callRes.Content[i].Text = scanRes.SanitizedContent
							modified = true
						} else if scanRes.ShouldBlock {
							callRes.Content[i].Text = "[BLOCKED BY BASTIO: Sensitive data or security policy violation in tool result]"
							modified = true
						}
					}
				}
			}

			if modified {
				if newResultBytes, err := json.Marshal(callRes); err == nil {
					msg.Result = newResultBytes
					if rewritten, err := json.Marshal(msg); err == nil {
						p.writeToClient(clientOut, rewritten)
						return
					}
				}
			}
		}
	}

	p.writeToClient(clientOut, raw)
}

func (p *StdioProxy) trackRequest(reqID, method string) {
	p.pendingMu.Lock()
	defer p.pendingMu.Unlock()
	p.pending[reqID] = method
}

func (p *StdioProxy) popRequest(reqID string) string {
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
