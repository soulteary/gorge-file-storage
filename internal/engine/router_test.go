package engine

import (
	"context"
	"io"
	"testing"
)

type mockEngine struct {
	id        string
	priority  int
	canWrite  bool
	sizeLimit int64
}

func (m *mockEngine) Identifier() string { return m.id }
func (m *mockEngine) Priority() int      { return m.priority }
func (m *mockEngine) CanWrite() bool     { return m.canWrite }
func (m *mockEngine) HasSizeLimit() bool { return m.sizeLimit > 0 }
func (m *mockEngine) MaxFileSize() int64 { return m.sizeLimit }

func (m *mockEngine) WriteFile(_ context.Context, _ []byte, _ WriteParams) (string, error) {
	return "mock-handle", nil
}
func (m *mockEngine) ReadFile(_ context.Context, _ string) (io.ReadCloser, error) {
	return nil, nil
}
func (m *mockEngine) DeleteFile(_ context.Context, _ string) error {
	return nil
}

func TestRouterSelectForWrite(t *testing.T) {
	blob := &mockEngine{id: "blob", priority: 1, canWrite: true, sizeLimit: 1000000}
	disk := &mockEngine{id: "local-disk", priority: 5, canWrite: true, sizeLimit: 0}
	s3 := &mockEngine{id: "amazon-s3", priority: 100, canWrite: true, sizeLimit: 0}

	router := NewRouter([]StorageEngine{s3, blob, disk})

	eng, err := router.SelectForWrite(500)
	if err != nil {
		t.Fatal(err)
	}
	if eng.Identifier() != "blob" {
		t.Errorf("small file: got %q, want blob", eng.Identifier())
	}

	eng, err = router.SelectForWrite(2000000)
	if err != nil {
		t.Fatal(err)
	}
	if eng.Identifier() != "local-disk" {
		t.Errorf("large file: got %q, want local-disk", eng.Identifier())
	}
}

func TestRouterNoWritable(t *testing.T) {
	readonly := &mockEngine{id: "test", priority: 1, canWrite: false}
	router := NewRouter([]StorageEngine{readonly})

	_, err := router.SelectForWrite(100)
	if err == nil {
		t.Error("should fail when no writable engine")
	}
}

func TestRouterGetEngine(t *testing.T) {
	blob := &mockEngine{id: "blob", priority: 1, canWrite: true, sizeLimit: 1000000}
	router := NewRouter([]StorageEngine{blob})

	eng, err := router.GetEngine("blob")
	if err != nil {
		t.Fatal(err)
	}
	if eng.Identifier() != "blob" {
		t.Errorf("got %q, want blob", eng.Identifier())
	}

	_, err = router.GetEngine("nonexistent")
	if err == nil {
		t.Error("should fail for unknown engine")
	}
}
