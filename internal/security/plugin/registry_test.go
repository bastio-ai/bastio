package plugin_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/bastio-ai/bastio/internal/security/plugin"
)

// fakeDetector is a minimal plugin.Detector implementation for tests.
type fakeDetector struct {
	name     string
	findings []any
	err      error
}

func (f *fakeDetector) Name() string { return f.name }
func (f *fakeDetector) Detect(_ context.Context, _ string) ([]any, error) {
	return f.findings, f.err
}

func TestRegistryRegisterAndBuild(t *testing.T) {
	r := plugin.NewRegistry()

	err := r.Register("test.one", func(cfg json.RawMessage) (plugin.Detector, error) {
		return &fakeDetector{name: "test.one"}, nil
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if !r.Has("test.one") {
		t.Fatalf("Has(test.one) = false, want true")
	}

	d, err := r.Build("test.one", nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if d.Name() != "test.one" {
		t.Fatalf("Name = %q, want test.one", d.Name())
	}
}

func TestRegistryDoubleRegisterRejected(t *testing.T) {
	r := plugin.NewRegistry()

	factory := func(cfg json.RawMessage) (plugin.Detector, error) {
		return &fakeDetector{name: "x"}, nil
	}
	if err := r.Register("x", factory); err != nil {
		t.Fatalf("first register: %v", err)
	}
	err := r.Register("x", factory)
	if !errors.Is(err, plugin.ErrAlreadyRegistered) {
		t.Fatalf("second register error = %v, want ErrAlreadyRegistered", err)
	}
}

func TestRegistryRegisterRejectsEmptyInputs(t *testing.T) {
	r := plugin.NewRegistry()

	if err := r.Register("", func(json.RawMessage) (plugin.Detector, error) { return nil, nil }); err == nil {
		t.Fatalf("empty name accepted")
	}
	if err := r.Register("x", nil); err == nil {
		t.Fatalf("nil factory accepted")
	}
}

func TestRegistryBuildUnknownReturnsSentinel(t *testing.T) {
	r := plugin.NewRegistry()

	_, err := r.Build("nope", nil)
	if !errors.Is(err, plugin.ErrNotRegistered) {
		t.Fatalf("Build error = %v, want ErrNotRegistered", err)
	}
}

func TestRegistryBuildPropagatesFactoryError(t *testing.T) {
	r := plugin.NewRegistry()

	factoryErr := errors.New("boom")
	_ = r.Register("broken", func(cfg json.RawMessage) (plugin.Detector, error) {
		return nil, factoryErr
	})

	_, err := r.Build("broken", nil)
	if !errors.Is(err, factoryErr) {
		t.Fatalf("Build error = %v, want to wrap %v", err, factoryErr)
	}
}

func TestRegistryListIsSorted(t *testing.T) {
	r := plugin.NewRegistry()
	factory := func(cfg json.RawMessage) (plugin.Detector, error) {
		return &fakeDetector{}, nil
	}
	_ = r.Register("zebra", factory)
	_ = r.Register("alpha", factory)
	_ = r.Register("mike", factory)

	got := r.List()
	want := []string{"alpha", "mike", "zebra"}
	if len(got) != len(want) {
		t.Fatalf("List length = %d, want %d", len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("List[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestRegistryUnregister(t *testing.T) {
	r := plugin.NewRegistry()
	_ = r.Register("tmp", func(cfg json.RawMessage) (plugin.Detector, error) {
		return &fakeDetector{}, nil
	})
	if err := r.Unregister("tmp"); err != nil {
		t.Fatalf("Unregister: %v", err)
	}
	if r.Has("tmp") {
		t.Fatalf("Has(tmp) = true after Unregister")
	}
	if err := r.Unregister("tmp"); !errors.Is(err, plugin.ErrNotRegistered) {
		t.Fatalf("double Unregister error = %v, want ErrNotRegistered", err)
	}
}

func TestRegistryConcurrentAccess(t *testing.T) {
	r := plugin.NewRegistry()
	factory := func(cfg json.RawMessage) (plugin.Detector, error) {
		return &fakeDetector{}, nil
	}
	_ = r.Register("c", factory)

	var wg sync.WaitGroup
	for range 50 {
		wg.Add(2)
		go func() { defer wg.Done(); _, _ = r.Build("c", nil) }()
		go func() { defer wg.Done(); r.List() }()
	}
	wg.Wait()
}
