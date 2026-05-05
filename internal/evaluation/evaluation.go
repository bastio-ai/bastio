// Package evaluation is the regression harness for Bastio's detection
// engine. It loads labeled fixture datasets (attack prompts + expected
// decisions), runs them through an Engine, and reports precision /
// recall / accuracy per detector so pattern edits can be graded
// objectively instead of by vibe.
//
// This is the OSS baseline — deterministic patterns vs. a small
// curated fixture set. Managed deployments extend this harness with
// benchmark datasets (HarmBench, JailbreakBench, PromptBench) and
// ongoing precision / recall tracking across releases.
package evaluation

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"sort"
	"strings"

	"github.com/bastio-ai/bastio/internal/security"
)

// Example is a single labeled fixture: the input the engine scans and
// the expected decision. Direction defaults to "input" when empty.
// Detector is optional — set when the fixture targets a specific
// detector's precision; leave empty to grade the aggregate decision.
type Example struct {
	ID            string            `json:"id"`
	Content       string            `json:"content"`
	Role          string            `json:"role,omitempty"`
	Direction     security.Direction `json:"direction,omitempty"`
	ExpectBlock   bool              `json:"expect_block"`
	ExpectAction  security.Action   `json:"expect_action,omitempty"`
	Detector      string            `json:"detector,omitempty"`
	Category      string            `json:"category,omitempty"` // e.g. "injection.override", "pii.credit_card"
	Notes         string            `json:"notes,omitempty"`
}

// Dataset is the input to a Run. A dataset groups examples by a
// descriptive name (e.g. "pii-core", "injection-multilingual") so
// reports can be filtered by scope.
type Dataset struct {
	Name     string    `json:"name"`
	Examples []Example `json:"examples"`
}

// LoadDataset reads a JSON array of Examples from r and wraps them
// in a Dataset with the given name.
func LoadDataset(name string, r io.Reader) (Dataset, error) {
	var examples []Example
	if err := json.NewDecoder(r).Decode(&examples); err != nil {
		return Dataset{}, fmt.Errorf("decode dataset %q: %w", name, err)
	}
	return Dataset{Name: name, Examples: examples}, nil
}

// LoadDatasetFS walks fsys and loads every *.json file as a Dataset.
// File name (without .json) becomes the dataset name. Useful for
// pointing the harness at a fixtures directory.
func LoadDatasetFS(fsys fs.FS, root string) ([]Dataset, error) {
	var out []Dataset
	err := fs.WalkDir(fsys, root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".json") {
			return nil
		}
		f, err := fsys.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		name := strings.TrimSuffix(d.Name(), ".json")
		ds, err := LoadDataset(name, f)
		if err != nil {
			return err
		}
		out = append(out, ds)
		return nil
	})
	return out, err
}

// Result captures one example's outcome. Correct is true when the
// engine's decision matched the expectation.
type Result struct {
	Example Example         `json:"example"`
	GotAction   security.Action `json:"got_action"`
	GotBlock    bool            `json:"got_block"`
	Correct     bool            `json:"correct"`
	FiredSteps  []string        `json:"fired_steps,omitempty"`
}

// Report aggregates per-detector precision / recall plus overall
// accuracy. Used by `cmd/eval` and CI regression gates.
type Report struct {
	Dataset  string            `json:"dataset"`
	Total    int               `json:"total"`
	Correct  int               `json:"correct"`
	Accuracy float64           `json:"accuracy"`
	Detector map[string]DetectorStats `json:"detector"`
	Failures []Result          `json:"failures,omitempty"`
}

// DetectorStats holds confusion-matrix counts for one detector. We
// compute precision = tp / (tp + fp) and recall = tp / (tp + fn) at
// summary time rather than storing the ratios, because aggregation
// across multiple datasets requires the raw counts.
type DetectorStats struct {
	TruePos  int     `json:"true_pos"`
	FalsePos int     `json:"false_pos"`
	FalseNeg int     `json:"false_neg"`
	Precision float64 `json:"precision"`
	Recall    float64 `json:"recall"`
	F1        float64 `json:"f1"`
}

// Runner executes datasets against an engine. Kept separate from
// Report so tests can stub it out.
type Runner struct {
	Engine *security.Engine
}

// Run evaluates every example in ds and produces a Report. It uses
// the engine's default input/output pipelines — the harness does not
// accept custom step lists to avoid grading against bespoke configs
// that won't match production.
func (r *Runner) Run(ctx context.Context, ds Dataset) Report {
	report := Report{
		Dataset:  ds.Name,
		Total:    len(ds.Examples),
		Detector: map[string]DetectorStats{},
	}

	for _, ex := range ds.Examples {
		steps := security.DefaultInputSteps()
		if ex.Direction == security.DirectionOutput {
			steps = security.DefaultOutputSteps()
		}
		opts := &security.RunOptions{Canonicalize: true, Role: ex.Role}
		res := r.Engine.RunSteps(ctx, ex.Content, steps, opts)

		got := Result{
			Example:   ex,
			GotAction: res.Action,
			GotBlock:  res.ShouldBlock,
		}
		for _, s := range res.Steps {
			if s.Fired && !s.Skipped {
				got.FiredSteps = append(got.FiredSteps, s.Detector)
			}
		}

		expect := ex.ExpectBlock || ex.ExpectAction == security.ActionBlock
		actualBlocked := res.ShouldBlock

		got.Correct = expect == actualBlocked
		if ex.ExpectAction != "" {
			got.Correct = got.Correct && res.Action == ex.ExpectAction
		}

		// Per-detector stats. A fixture that names a detector is a
		// positive example: we expect that detector to fire. TP when
		// it does, FN when it doesn't. FP tracking requires
		// negatively-labeled fixtures per detector, which the current
		// schema doesn't carry — aggregate accuracy captures
		// false-block issues in the meantime.
		if ex.Detector != "" {
			stats := report.Detector[ex.Detector]
			if contains(got.FiredSteps, ex.Detector) {
				stats.TruePos++
			} else {
				stats.FalseNeg++
			}
			report.Detector[ex.Detector] = stats
		}

		if got.Correct {
			report.Correct++
		} else {
			report.Failures = append(report.Failures, got)
		}
	}

	if report.Total > 0 {
		report.Accuracy = float64(report.Correct) / float64(report.Total)
	}
	for name, stats := range report.Detector {
		if d := stats.TruePos + stats.FalsePos; d > 0 {
			stats.Precision = float64(stats.TruePos) / float64(d)
		}
		if d := stats.TruePos + stats.FalseNeg; d > 0 {
			stats.Recall = float64(stats.TruePos) / float64(d)
		}
		if stats.Precision+stats.Recall > 0 {
			stats.F1 = 2 * stats.Precision * stats.Recall / (stats.Precision + stats.Recall)
		}
		report.Detector[name] = stats
	}
	return report
}

// WriteText dumps a human-readable summary to w. Minimal, designed
// for CI logs and local iteration — JSON consumers should marshal
// the Report directly.
func WriteText(w io.Writer, r Report) {
	fmt.Fprintf(w, "Dataset: %s\n", r.Dataset)
	fmt.Fprintf(w, "  %d / %d correct (%.1f%%)\n", r.Correct, r.Total, r.Accuracy*100)

	if len(r.Detector) > 0 {
		fmt.Fprintln(w, "  Per-detector precision / recall / F1:")
		names := make([]string, 0, len(r.Detector))
		for k := range r.Detector {
			names = append(names, k)
		}
		sort.Strings(names)
		for _, n := range names {
			s := r.Detector[n]
			fmt.Fprintf(w, "    %-24s P=%.2f R=%.2f F1=%.2f  (tp=%d fp=%d fn=%d)\n",
				n, s.Precision, s.Recall, s.F1, s.TruePos, s.FalsePos, s.FalseNeg)
		}
	}

	if len(r.Failures) > 0 {
		fmt.Fprintf(w, "  %d failures:\n", len(r.Failures))
		for _, f := range r.Failures {
			fmt.Fprintf(w, "    - [%s] %q  expect_block=%v got_block=%v got_action=%s\n",
				f.Example.ID, truncate(f.Example.Content, 60),
				f.Example.ExpectBlock, f.GotBlock, f.GotAction)
		}
	}
}

func contains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
