package governance

import (
	"fmt"
	"os"
	"path/filepath"
)

func writeTempHTML(content []byte) (string, error) {
	dir := filepath.Join(os.TempDir(), "bastio-governance")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("mkdir temp: %w", err)
	}
	f, err := os.CreateTemp(dir, "report-*.html")
	if err != nil {
		return "", fmt.Errorf("create temp: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(content); err != nil {
		return "", fmt.Errorf("write temp: %w", err)
	}
	return f.Name(), nil
}

func cleanupTemp(path string) {
	_ = os.Remove(path)
}

func readFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}
