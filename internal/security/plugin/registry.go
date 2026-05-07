// Package plugin provides a registry for third-party security
// detectors. Plugins are compiled into a Bastio binary via
// blank-import in cmd/server/main.go; they register a factory
// function at init() time. Overlays then reference a plugin by name
// in their `plugin_detectors[]` list, and the runtime builds detector
// instances from the registered factory on demand.
//
// Plugins never run automatically — an overlay must explicitly list
// them. A plugin factory that returns an error is logged and skipped;
// a failing plugin cannot fail the primary detection pipeline.
//
// This package deliberately avoids any dependency on internal/security
// to keep the import graph acyclic. The detector contract below
// matches the shape of security.Detector — callers adapt between the
// two at the seam.
package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
)

// Detector is the minimal contract a plugin must satisfy. It mirrors
// the shape of security.Detector without importing that package (which
// would create an import cycle via server.go wiring).
//
// Findings is intentionally []any here — the security-package adapter
// converts them to security.Finding at the call site. This keeps the
// plugin API loosely coupled: a plugin binary compiled against an
// older / newer Bastio version still links cleanly.
type Detector interface {
	// Name returns the stable identifier used to reference the plugin
	// in an overlay's plugin_detectors[] list. Must match the name
	// passed to Register.
	Name() string
	// Detect scans content and returns whatever findings the plugin
	// produces. The security-package adapter converts []any into
	// []security.Finding at the integration point. Error implies a
	// plugin-internal failure; the caller logs and drops the finding.
	Detect(ctx context.Context, content string) ([]any, error)
}

// Factory builds a Detector instance from an opaque configuration blob
// taken from an overlay's plugin_detectors[].config field. The
// factory should validate its input and return a clear error for
// malformed configs.
type Factory func(config json.RawMessage) (Detector, error)

// Registry is a thread-safe name → factory map. Registrations are
// expected to happen at init() time; Build calls happen per-request
// or per-engine-construction.
type Registry struct {
	mu        sync.RWMutex
	factories map[string]Factory
}

// NewRegistry returns an empty Registry. Most callers want the
// process-wide Default registry — NewRegistry exists for tests that
// need isolation.
func NewRegistry() *Registry {
	return &Registry{factories: map[string]Factory{}}
}

// ErrAlreadyRegistered is returned when Register is called twice for
// the same name. Register is meant to be called from init(); a double
// registration indicates a programming mistake (two plugins claiming
// the same name, or a double blank-import).
var ErrAlreadyRegistered = errors.New("plugin: name already registered")

// ErrNotRegistered is returned when Build or Unregister is called with
// a name that has no factory.
var ErrNotRegistered = errors.New("plugin: name not registered")

// Register adds a factory for the given name. Returns
// ErrAlreadyRegistered if the name is already claimed. Names are
// case-sensitive.
func (r *Registry) Register(name string, factory Factory) error {
	if name == "" {
		return fmt.Errorf("plugin: name is required")
	}
	if factory == nil {
		return fmt.Errorf("plugin: factory is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.factories[name]; ok {
		return fmt.Errorf("%w: %s", ErrAlreadyRegistered, name)
	}
	r.factories[name] = factory
	return nil
}

// MustRegister panics if Register fails. Convenience for init() blocks
// where an error is programmer-fatal anyway.
func (r *Registry) MustRegister(name string, factory Factory) {
	if err := r.Register(name, factory); err != nil {
		panic(err)
	}
}

// Unregister removes a factory. Primarily for tests; production code
// should let init-time registrations stand for the life of the process.
func (r *Registry) Unregister(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.factories[name]; !ok {
		return fmt.Errorf("%w: %s", ErrNotRegistered, name)
	}
	delete(r.factories, name)
	return nil
}

// Build invokes the registered factory for name and returns a fresh
// Detector. Returns ErrNotRegistered when no factory is registered.
// Configuration errors bubble up from the factory unchanged.
func (r *Registry) Build(name string, config json.RawMessage) (Detector, error) {
	r.mu.RLock()
	f, ok := r.factories[name]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrNotRegistered, name)
	}
	return f(config)
}

// List returns the names of all registered factories, sorted. Used by
// the dashboard "available plugins" view.
func (r *Registry) List() []string {
	r.mu.RLock()
	names := make([]string, 0, len(r.factories))
	for n := range r.factories {
		names = append(names, n)
	}
	r.mu.RUnlock()
	sort.Strings(names)
	return names
}

// Has reports whether a factory is registered under name.
func (r *Registry) Has(name string) bool {
	r.mu.RLock()
	_, ok := r.factories[name]
	r.mu.RUnlock()
	return ok
}

// Default is the process-wide registry used by blank-imports. Plugins
// package themselves as:
//
//	package myplugin
//	import sp "github.com/bastio-ai/bastio/internal/security/plugin"
//	func init() {
//	    sp.Register("mycorp.foo", func(cfg json.RawMessage) (sp.Detector, error) { ... })
//	}
//
// and the operator enables them with:
//
//	import _ "github.com/mycorp/bastio-plugin-foo"
//
// in their fork of cmd/server/main.go.
var Default = NewRegistry()

// Register is a shortcut for Default.Register — the form used from
// init() blocks in plugin packages.
func Register(name string, factory Factory) error {
	return Default.Register(name, factory)
}

// MustRegister is the panicking shortcut for Default.MustRegister.
func MustRegister(name string, factory Factory) {
	Default.MustRegister(name, factory)
}
