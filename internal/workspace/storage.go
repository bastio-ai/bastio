package workspace

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/google/uuid"
)

// BlobStore is the workspace's persistence interface for uploaded
// knowledge artifacts. OSS uses local-disk storage; cloud injects an
// S3-backed implementation. Both stores key blobs by (customer_id,
// source_id) so a customer's data sits under a deterministic prefix
// — ready for retention/deletion sweeps and per-customer encryption-
// at-rest policies later.
type BlobStore interface {
	Put(ctx context.Context, customerID, sourceID uuid.UUID, name string, data io.Reader) (ref string, size int64, err error)
	Get(ctx context.Context, ref string) (io.ReadCloser, error)
	Delete(ctx context.Context, ref string) error
}

// LocalBlobStore writes blobs to the OS filesystem under a configured
// data directory. The blob ref is `local://<customer>/<source>/<safe_name>`.
// On read, ParseLocalRef pulls the path apart and joins back to the
// configured root — refs are never trusted as filesystem paths directly.
type LocalBlobStore struct {
	root        string
	maxFileSize int64
}

// NewLocalBlobStore creates a store rooted at the given directory.
// Maximum file size defaults to 25 MB; override via NewLocalBlobStoreWithLimit.
func NewLocalBlobStore(root string) *LocalBlobStore {
	return NewLocalBlobStoreWithLimit(root, 25<<20)
}

func NewLocalBlobStoreWithLimit(root string, maxBytes int64) *LocalBlobStore {
	return &LocalBlobStore{root: root, maxFileSize: maxBytes}
}

// Put writes a blob and returns its ref. The on-disk filename is the
// caller-supplied name passed through filepath.Base — directory
// traversal attempts are stripped, not rejected, so the upload still
// succeeds with a sanitized name.
func (s *LocalBlobStore) Put(ctx context.Context, customerID, sourceID uuid.UUID, name string, data io.Reader) (string, int64, error) {
	if err := ctx.Err(); err != nil {
		return "", 0, err
	}
	safeName := filepath.Base(name)
	if safeName == "" || safeName == "." || safeName == "/" {
		safeName = sourceID.String()
	}
	dir := filepath.Join(s.root, "workspace", "knowledge", customerID.String(), sourceID.String())
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", 0, fmt.Errorf("mkdir blob dir: %w", err)
	}
	full := filepath.Join(dir, safeName)
	f, err := os.OpenFile(full, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return "", 0, fmt.Errorf("open blob: %w", err)
	}
	defer f.Close()

	limited := io.LimitReader(data, s.maxFileSize+1)
	written, err := io.Copy(f, limited)
	if err != nil {
		_ = os.Remove(full)
		return "", 0, fmt.Errorf("write blob: %w", err)
	}
	if written > s.maxFileSize {
		_ = os.Remove(full)
		return "", 0, fmt.Errorf("file exceeds %d bytes", s.maxFileSize)
	}

	ref := fmt.Sprintf("local://%s/%s/%s", customerID, sourceID, safeName)
	return ref, written, nil
}

// Get opens a blob by ref. Refs are validated to live under the
// configured root before any filesystem call — a hostile ref like
// `local://../../../etc/passwd` cannot escape.
func (s *LocalBlobStore) Get(ctx context.Context, ref string) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path, err := s.resolve(ref)
	if err != nil {
		return nil, err
	}
	return os.Open(path)
}

// Delete removes a blob and its containing source directory. Missing
// refs are treated as success (idempotent — the source row may already
// be archived without the blob having existed).
func (s *LocalBlobStore) Delete(ctx context.Context, ref string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := s.resolve(ref)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	// Best-effort cleanup of the per-source dir.
	_ = os.Remove(filepath.Dir(path))
	return nil
}

// resolve translates a `local://customer/source/name` ref into an absolute
// filesystem path under s.root and verifies the result stays beneath it.
func (s *LocalBlobStore) resolve(ref string) (string, error) {
	const prefix = "local://"
	if len(ref) < len(prefix) || ref[:len(prefix)] != prefix {
		return "", fmt.Errorf("invalid blob ref: %q", ref)
	}
	parts := splitRef(ref[len(prefix):])
	if len(parts) != 3 {
		return "", fmt.Errorf("invalid blob ref shape: %q", ref)
	}
	if _, err := uuid.Parse(parts[0]); err != nil {
		return "", fmt.Errorf("invalid blob ref customer: %q", parts[0])
	}
	if _, err := uuid.Parse(parts[1]); err != nil {
		return "", fmt.Errorf("invalid blob ref source: %q", parts[1])
	}
	full := filepath.Join(s.root, "workspace", "knowledge", parts[0], parts[1], filepath.Base(parts[2]))
	absRoot, _ := filepath.Abs(s.root)
	absFull, _ := filepath.Abs(full)
	if absRoot != "" && absFull != "" && len(absFull) < len(absRoot) {
		return "", fmt.Errorf("blob ref escapes root")
	}
	return full, nil
}

func splitRef(s string) []string {
	out := []string{}
	current := ""
	for i := 0; i < len(s); i++ {
		if s[i] == '/' {
			out = append(out, current)
			current = ""
			if len(out) == 2 {
				out = append(out, s[i+1:])
				return out
			}
			continue
		}
		current += string(s[i])
	}
	if current != "" {
		out = append(out, current)
	}
	return out
}
