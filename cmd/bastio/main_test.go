package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestCLI_Usage(t *testing.T) {
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	printUsage()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()

	if !strings.Contains(out, "bastio <command>") {
		t.Errorf("expected usage output, got %s", out)
	}
}

func TestCLI_ScanSafe(t *testing.T) {
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	runScan([]string{"What is the weather today?"})

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()

	if !strings.Contains(out, "PASSED") {
		t.Errorf("expected PASSED verdict, got %s", out)
	}
}

func TestCLI_Eval(t *testing.T) {
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	runEval([]string{"-path", "../../internal/evaluation/fixtures", "-json"})

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()

	if !strings.Contains(out, "injection") || !strings.Contains(out, "accuracy") {
		t.Errorf("expected json eval output, got %s", out)
	}
}

func TestCLI_Usage_ContainsMCPProxy(t *testing.T) {
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	printUsage()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()

	if !strings.Contains(out, "mcp-proxy") {
		t.Errorf("expected usage output to mention mcp-proxy, got %s", out)
	}
}

// Exercise the actual CLI entry point in a subprocess: stdout must remain
// machine-readable JSON-RPC, including startup/shutdown diagnostics.
func TestCLI_MCPProtocolStdout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestCLI_MCPLauncher$")
	cmd.Env = append(os.Environ(), "BASTIO_CLI_MCP_TEST=1")
	cmd.Stdin = strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"clock"}}` + "\n")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("CLI failed: %v %s", err, stderr.String())
	}
	var msg struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      int             `json:"id"`
		Result  json.RawMessage `json:"result"`
	}
	dec := json.NewDecoder(bytes.NewReader(out))
	if err = dec.Decode(&msg); err != nil || msg.JSONRPC != "2.0" || msg.ID != 1 {
		t.Fatalf("stdout is not protocol JSON: %s", out)
	}
	var extra json.RawMessage
	if err = dec.Decode(&extra); err != io.EOF {
		t.Fatalf("unexpected stdout after reply: %s", out)
	}
	if !strings.Contains(stderr.String(), "mcp proxy started child process") {
		t.Fatal("diagnostics did not use stderr")
	}
}
func TestCLI_MCPLauncher(t *testing.T) {
	if os.Getenv("BASTIO_CLI_MCP_TEST") != "1" {
		return
	}
	runMcpProxy([]string{"--", os.Args[0], "-test.run=^TestCLI_MCPChild$"})
	os.Exit(0)
}
func TestCLI_MCPChild(t *testing.T) {
	if os.Getenv("BASTIO_CLI_MCP_TEST") != "1" {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var msg struct {
			ID json.RawMessage `json:"id"`
		}
		_ = json.Unmarshal(scanner.Bytes(), &msg)
		_ = json.NewEncoder(os.Stdout).Encode(struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      json.RawMessage `json:"id"`
			Result  struct {
				Content []string `json:"content"`
			} `json:"result"`
		}{JSONRPC: "2.0", ID: msg.ID})
	}
	os.Exit(0)
}
