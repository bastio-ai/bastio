package workspace

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestLocalBlobStoreRoundTrip(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	store := NewLocalBlobStore(root)

	customer := uuid.New()
	source := uuid.New()
	body := "hello-from-blob"

	ref, size, err := store.Put(context.Background(), customer, source, "doc.txt",
		strings.NewReader(body))
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if size != int64(len(body)) {
		t.Fatalf("size mismatch: got %d want %d", size, len(body))
	}
	if !strings.HasPrefix(ref, "local://"+customer.String()+"/"+source.String()+"/") {
		t.Fatalf("unexpected ref shape: %q", ref)
	}

	r, err := store.Get(context.Background(), ref)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer r.Close()
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != body {
		t.Fatalf("body roundtrip: got %q want %q", got, body)
	}

	if err := store.Delete(context.Background(), ref); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := store.Get(context.Background(), ref); err == nil {
		t.Fatal("expected get to fail after delete")
	}
}

func TestLocalBlobStoreSanitizesFilename(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	store := NewLocalBlobStore(root)

	// "../../etc/passwd" must not escape the root — stripped to base.
	ref, _, err := store.Put(context.Background(), uuid.New(), uuid.New(),
		"../../etc/passwd", strings.NewReader("x"))
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if strings.Contains(ref, "..") {
		t.Fatalf("ref leaks traversal: %q", ref)
	}
}

func TestLocalBlobStoreRejectsOversize(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	store := NewLocalBlobStoreWithLimit(root, 16)
	big := bytes.Repeat([]byte("x"), 100)
	_, _, err := store.Put(context.Background(), uuid.New(), uuid.New(),
		"big.bin", bytes.NewReader(big))
	if err == nil {
		t.Fatal("expected oversize rejection")
	}
}

func TestLocalBlobStoreRejectsBadRef(t *testing.T) {
	t.Parallel()
	store := NewLocalBlobStore(t.TempDir())
	_, err := store.Get(context.Background(), "local://not-a-uuid/x/y")
	if err == nil {
		t.Fatal("expected reject for non-uuid customer in ref")
	}
	_, err = store.Get(context.Background(), "s3://forbidden/scheme")
	if err == nil {
		t.Fatal("expected reject for non-local scheme")
	}
}
