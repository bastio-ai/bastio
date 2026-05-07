// Command eval runs the security detection regression harness.
//
// Usage:
//
//	go run ./cmd/eval                   # all fixtures under internal/evaluation/fixtures/
//	go run ./cmd/eval -path ./custom    # specific directory
//	go run ./cmd/eval -json             # machine-readable output
//
// Exit code is 1 when any dataset has failures, so this plugs into CI
// as a regression gate without extra scripting.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/bastio-ai/bastio/internal/evaluation"
	"github.com/bastio-ai/bastio/internal/security"
	"github.com/bastio-ai/bastio/internal/security/detection"
)

func main() {
	path := flag.String("path", "internal/evaluation/fixtures", "directory containing *.json fixture files")
	asJSON := flag.Bool("json", false, "emit the report as JSON instead of text")
	flag.Parse()

	engine := security.NewEngine(
		detection.NewInjectionDetector(),
		detection.NewPIIDetector(),
		detection.NewJailbreakDetector(),
		detection.NewSecretsDetector(),
		detection.NewIndirectInjectionDetector(),
		detection.NewExfilDetector(),
		// topic_policy with nil DB is a no-op — registering it here
		// keeps DefaultInputSteps happy without noisy "unknown
		// detector" warnings. Real eval with customer rules belongs
		// in the cloud benchmark harness (see roadmap doc).
		detection.NewTopicPolicyDetector(nil, nil, 0),
	)
	runner := evaluation.Runner{Engine: engine}

	datasets, err := evaluation.LoadDatasetFS(os.DirFS("."), *path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "load fixtures:", err)
		os.Exit(2)
	}
	if len(datasets) == 0 {
		fmt.Fprintln(os.Stderr, "no fixtures found at", *path)
		os.Exit(2)
	}

	ctx := context.Background()
	var reports []evaluation.Report
	var failed bool

	for _, ds := range datasets {
		report := runner.Run(ctx, ds)
		reports = append(reports, report)
		if len(report.Failures) > 0 {
			failed = true
		}
		if !*asJSON {
			evaluation.WriteText(os.Stdout, report)
			fmt.Println()
		}
	}

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(reports)
	}

	if failed {
		os.Exit(1)
	}
}
