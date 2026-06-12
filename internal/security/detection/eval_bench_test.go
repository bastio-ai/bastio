package detection

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bastio-ai/bastio/internal/security"
)

// Eval benchmark harness — a labeled, handwritten corpus under
// testdata/eval/ and a regression gate on per-detector precision and
// recall.
//
// Every sample was written by hand for this repo (source:
// "handwritten"). Nothing is copied from public attack datasets
// (JailbreakBench etc.) — licensing review for vendoring those is
// still pending.
//
// Scoring model:
//
//   - A detector "fires" on a sample when its max weighted finding
//     (Score × Confidence) meets the detector's production default
//     threshold from DefaultInputSteps — so the gate measures what
//     the gateway would actually do, not raw pattern hits.
//   - Recall is computed over samples labeled for the detector,
//     excluding known_gap samples. known_gap marks attacks we know
//     the detector misses today (e.g. payload splitting); they're
//     reported separately so the gap list stays visible instead of
//     silently dragging the floors down. A known_gap sample that
//     starts firing shows up as "gap closed" — promote it to a
//     regular sample when that happens.
//   - Precision counts false positives ONLY against benign controls
//     (labels: []). Attack samples labeled for other detectors are
//     ignored for cross-firing — an injection prompt legitimately
//     tripping the jailbreak detector too is not a mistake.
//
// Floors are set slightly below the observed performance at the time
// the corpus was written. They are a regression gate, not a target:
// if a floor trips, a change made the detector measurably worse on
// known inputs. Raise floors when real improvements land; never
// lower them to make a test pass — mark new known gaps instead.

type evalSample struct {
	Name     string   `json:"name"`
	Text     string   `json:"text"`
	Labels   []string `json:"labels"`
	Source   string   `json:"source"`
	KnownGap bool     `json:"known_gap,omitempty"`
}

// evalFloors holds the regression floors per detector, derived from
// the observed run on 2026-06-11 (see table in test log).
// Observed on 2026-06-11 (Go 1.x, corpus v1):
//
//	injection  p=0.846 r=1.000 (3 known gaps: [INST] delimiter escape,
//	           keyword-free "repeat the text above", role-hijack phrasing
//	           below the 0.72 weighted threshold)
//	jailbreak  p=0.818 r=1.000 (2 known gaps: grandma fiction framing,
//	           payload splitting — both score below the 0.6 threshold)
//	pii        p=1.000 r=1.000
//	secrets    p=0.938 r=1.000 (1 benign FP: snake_case token value that
//	           clears the 3.5-bit entropy gate)
//
// Floors sit one regression below observed: a single new miss or new
// benign FP on the current corpus trips the corresponding gate.
var evalFloors = map[string]struct{ precision, recall float64 }{
	"injection": {precision: 0.84, recall: 0.95},
	"jailbreak": {precision: 0.80, recall: 0.95},
	"pii":       {precision: 0.95, recall: 0.95},
	"secrets":   {precision: 0.90, recall: 0.95},
}

func loadEvalCorpus(t testing.TB) []evalSample {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("testdata", "eval", "*.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("eval corpus not found: %v", err)
	}
	var corpus []evalSample
	seen := map[string]bool{}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		var samples []evalSample
		if err := json.Unmarshal(raw, &samples); err != nil {
			t.Fatalf("parse %s: %v", f, err)
		}
		for i, s := range samples {
			if s.Name == "" || s.Text == "" {
				t.Fatalf("%s: sample with empty name or text", f)
			}
			if seen[s.Name] {
				t.Fatalf("duplicate sample name %q", s.Name)
			}
			seen[s.Name] = true
			// {SPLIT} markers break realistic-looking fixture tokens in
			// the JSON so secret scanners (GitHub push protection) don't
			// flag the corpus as leaked credentials. Detectors must see
			// the assembled token, so strip the markers at load time.
			samples[i].Text = strings.ReplaceAll(s.Text, "{SPLIT}", "")
		}
		corpus = append(corpus, samples...)
	}
	return corpus
}

// evalDetectors constructs the detectors under evaluation with their
// production default thresholds (mirrors DefaultInputSteps).
func evalDetectors() []struct {
	name      string
	det       security.Detector
	threshold float64
} {
	return []struct {
		name      string
		det       security.Detector
		threshold float64
	}{
		{"injection", NewInjectionDetector(), 0.72},
		{"jailbreak", NewJailbreakDetector(), 0.6},
		{"pii", NewPIIDetector(), 0},
		{"secrets", NewSecretsDetector(), 0},
	}
}

func detectorFires(t testing.TB, det security.Detector, threshold float64, text string) bool {
	t.Helper()
	findings, err := det.Detect(context.Background(), text)
	if err != nil {
		t.Fatalf("%s detect: %v", det.Name(), err)
	}
	for _, f := range findings {
		if w := f.Score * f.Confidence; w >= threshold && w > 0 {
			return true
		}
	}
	return false
}

func hasLabel(s evalSample, label string) bool {
	for _, l := range s.Labels {
		if l == label {
			return true
		}
	}
	return false
}

func TestEvalCorpus(t *testing.T) {
	corpus := loadEvalCorpus(t)

	attacks, benign := 0, 0
	for _, s := range corpus {
		if len(s.Labels) == 0 {
			benign++
		} else {
			attacks++
		}
	}
	t.Logf("eval corpus: %d samples (%d attack, %d benign)", len(corpus), attacks, benign)

	type stats struct {
		tp, fn, fp           int
		gapsTotal, gapClosed int
	}
	results := map[string]*stats{}

	for _, d := range evalDetectors() {
		st := &stats{}
		results[d.name] = st

		for _, s := range corpus {
			fired := detectorFires(t, d.det, d.threshold, s.Text)
			switch {
			case hasLabel(s, d.name) && s.KnownGap:
				st.gapsTotal++
				if fired {
					st.gapClosed++
					t.Logf("  [%s] GAP CLOSED: %s now fires — promote to regular sample", d.name, s.Name)
				} else {
					t.Logf("  [%s] known gap (expected miss): %s", d.name, s.Name)
				}
			case hasLabel(s, d.name):
				if fired {
					st.tp++
				} else {
					st.fn++
					t.Logf("  [%s] MISS: %s: %.60q", d.name, s.Name, s.Text)
				}
			case len(s.Labels) == 0:
				if fired {
					st.fp++
					t.Logf("  [%s] FALSE POSITIVE on benign: %s: %.60q", d.name, s.Name, s.Text)
				}
			}
			// Cross-fires on attack samples labeled for other
			// detectors are intentionally not counted either way.
		}
	}

	t.Logf("%-10s %4s %4s %4s  %-9s %-9s %-12s %s",
		"detector", "tp", "fn", "fp", "precision", "recall", "floors(p/r)", "known-gaps(closed/total)")
	for _, d := range evalDetectors() {
		st := results[d.name]
		precision := 1.0
		if st.tp+st.fp > 0 {
			precision = float64(st.tp) / float64(st.tp+st.fp)
		}
		recall := 1.0
		if st.tp+st.fn > 0 {
			recall = float64(st.tp) / float64(st.tp+st.fn)
		}
		floors := evalFloors[d.name]
		t.Logf("%-10s %4d %4d %4d  %-9.3f %-9.3f %.2f/%.2f    %d/%d",
			d.name, st.tp, st.fn, st.fp, precision, recall,
			floors.precision, floors.recall, st.gapClosed, st.gapsTotal)

		if precision < floors.precision {
			t.Errorf("%s precision %.3f dropped below floor %.2f — a change regressed benign handling",
				d.name, precision, floors.precision)
		}
		if recall < floors.recall {
			t.Errorf("%s recall %.3f dropped below floor %.2f — a change regressed attack coverage",
				d.name, recall, floors.recall)
		}
	}
}

// BenchmarkEvalCorpus measures full-corpus scan time across all four
// detectors — a coarse guard on the <50ms per-request latency budget
// (one sample through all detectors must be far below that).
func BenchmarkEvalCorpus(b *testing.B) {
	corpus := loadEvalCorpus(b)
	dets := evalDetectors()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, s := range corpus {
			for _, d := range dets {
				if _, err := d.det.Detect(context.Background(), s.Text); err != nil {
					b.Fatal(err)
				}
			}
		}
	}
}
