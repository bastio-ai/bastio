package workspace

import (
	"context"
	"testing"

	"github.com/riverqueue/river"
)

// TestArchiveWorkerKindStable locks the job-kind string used to identify
// queued archive jobs in River. Renaming would orphan in-flight jobs;
// this test forces a deliberate decision before the kind ever changes.
func TestArchiveWorkerKindStable(t *testing.T) {
	t.Parallel()
	if got := (WorkspaceArchiveArgs{}).Kind(); got != "workspace.archive_messages" {
		t.Fatalf("kind drifted to %q — orphans queued jobs across deploys", got)
	}
}

// TestArchiveWorkerInsertOptsRetryCap ensures the worker doesn't
// silently widen its retry budget. CH outages outlast 3 attempts get
// re-discovered on the next 24h tick — no point hammering the queue.
func TestArchiveWorkerInsertOptsRetryCap(t *testing.T) {
	t.Parallel()
	opts := (WorkspaceArchiveArgs{}).InsertOpts()
	if opts.MaxAttempts != 3 {
		t.Errorf("MaxAttempts changed to %d (was 3)", opts.MaxAttempts)
	}
	if opts.Queue != river.QueueDefault {
		t.Errorf("queue changed to %q (was default)", opts.Queue)
	}
}

// TestArchiveWorkerSkipsWithoutCH covers the OSS-without-CH path: the
// worker must no-op (return nil) instead of failing the job, otherwise
// every periodic tick fills the failed-jobs queue.
func TestArchiveWorkerSkipsWithoutCH(t *testing.T) {
	t.Parallel()
	w := NewArchiveWorker(nil, nil) // no pool, no CH — Work should bail early
	job := &river.Job[WorkspaceArchiveArgs]{Args: WorkspaceArchiveArgs{}}
	if err := w.Work(context.Background(), job); err != nil {
		t.Fatalf("expected nil when CH unconfigured, got %v", err)
	}
}
