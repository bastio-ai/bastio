package overlay_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bastio-ai/bastio/internal/security/overlay"
)

func TestShadowEventDeduper_FirstCallRecords(t *testing.T) {
	d := overlay.NewShadowEventDeduper(time.Minute)
	if !d.ShouldRecord(uuid.New(), "would_block") {
		t.Fatal("first call must return true")
	}
}

func TestShadowEventDeduper_SameKeySuppressedWithinWindow(t *testing.T) {
	d := overlay.NewShadowEventDeduper(time.Minute)
	v := uuid.New()
	if !d.ShouldRecord(v, "would_block") {
		t.Fatal("first call must return true")
	}
	if d.ShouldRecord(v, "would_block") {
		t.Fatal("second call within window must return false")
	}
}

func TestShadowEventDeduper_DifferentDivergencesIndependent(t *testing.T) {
	d := overlay.NewShadowEventDeduper(time.Minute)
	v := uuid.New()
	if !d.ShouldRecord(v, "would_block") {
		t.Fatal("block event must record")
	}
	if !d.ShouldRecord(v, "would_allow") {
		t.Fatal("allow event for same version must record (different divergence)")
	}
}

func TestShadowEventDeduper_DifferentVersionsIndependent(t *testing.T) {
	d := overlay.NewShadowEventDeduper(time.Minute)
	v1 := uuid.New()
	v2 := uuid.New()
	if !d.ShouldRecord(v1, "would_block") {
		t.Fatal("v1 must record")
	}
	if !d.ShouldRecord(v2, "would_block") {
		t.Fatal("v2 must record (different version)")
	}
}

func TestShadowEventDeduper_ZeroWindowDisablesDedup(t *testing.T) {
	d := overlay.NewShadowEventDeduper(0)
	v := uuid.New()
	for range 5 {
		if !d.ShouldRecord(v, "would_block") {
			t.Fatal("zero-window deduper must always return true")
		}
	}
}

func TestShadowEventDeduper_NilIsSafe(t *testing.T) {
	var d *overlay.ShadowEventDeduper
	if !d.ShouldRecord(uuid.New(), "would_block") {
		t.Fatal("nil deduper must return true (no-op)")
	}
}

func TestShadowEventDeduper_RecordsAfterWindowElapses(t *testing.T) {
	d := overlay.NewShadowEventDeduper(5 * time.Millisecond)
	v := uuid.New()
	if !d.ShouldRecord(v, "would_block") {
		t.Fatal("first must record")
	}
	time.Sleep(10 * time.Millisecond)
	if !d.ShouldRecord(v, "would_block") {
		t.Fatal("after window, same key must record again")
	}
}
