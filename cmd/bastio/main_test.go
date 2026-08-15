package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
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

