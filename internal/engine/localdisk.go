package engine

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
)

var localHandlePattern = regexp.MustCompile(`^[a-f0-9]{2}/[a-f0-9]{2}/[a-f0-9]{28}$`)

type LocalDiskEngine struct {
	root string
}

func NewLocalDiskEngine(root string) (*LocalDiskEngine, error) {
	if root == "" || root == "/" || root[0] != '/' {
		return nil, fmt.Errorf("local disk root must be an absolute path, got %q", root)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("create storage root: %w", err)
	}
	return &LocalDiskEngine{root: root}, nil
}

func (e *LocalDiskEngine) Identifier() string { return "local-disk" }
func (e *LocalDiskEngine) Priority() int      { return 5 }
func (e *LocalDiskEngine) CanWrite() bool     { return true }
func (e *LocalDiskEngine) HasSizeLimit() bool { return false }
func (e *LocalDiskEngine) MaxFileSize() int64 { return 0 }

func (e *LocalDiskEngine) WriteFile(_ context.Context, data []byte, _ WriteParams) (string, error) {
	handle, err := generateLocalHandle()
	if err != nil {
		return "", err
	}

	fullPath := filepath.Join(e.root, handle)
	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir %s: %w", dir, err)
	}

	if err := os.WriteFile(fullPath, data, 0o644); err != nil {
		return "", fmt.Errorf("write file: %w", err)
	}
	return handle, nil
}

func (e *LocalDiskEngine) ReadFile(_ context.Context, handle string) (io.ReadCloser, error) {
	if !localHandlePattern.MatchString(handle) {
		return nil, fmt.Errorf("malformed local disk handle: %q", handle)
	}
	fullPath := filepath.Join(e.root, handle)
	f, err := os.Open(fullPath)
	if err != nil {
		return nil, fmt.Errorf("open file: %w", err)
	}
	return f, nil
}

func (e *LocalDiskEngine) DeleteFile(_ context.Context, handle string) error {
	if !localHandlePattern.MatchString(handle) {
		return fmt.Errorf("malformed local disk handle: %q", handle)
	}
	fullPath := filepath.Join(e.root, handle)
	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		return nil
	}
	return os.Remove(fullPath)
}

func generateLocalHandle() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate random handle: %w", err)
	}
	h := hex.EncodeToString(b)
	return fmt.Sprintf("%s/%s/%s", h[:2], h[2:4], h[4:]), nil
}
