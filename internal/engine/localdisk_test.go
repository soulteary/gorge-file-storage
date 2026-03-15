package engine

import (
	"context"
	"io"
	"os"
	"testing"
)

func TestLocalDiskRoundTrip(t *testing.T) {
	dir, err := os.MkdirTemp("", "localdisk-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	eng, err := NewLocalDiskEngine(dir)
	if err != nil {
		t.Fatal(err)
	}

	if eng.Identifier() != "local-disk" {
		t.Errorf("Identifier = %q, want local-disk", eng.Identifier())
	}

	ctx := context.Background()
	data := []byte("hello phorge file storage")

	handle, err := eng.WriteFile(ctx, data, WriteParams{})
	if err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if !localHandlePattern.MatchString(handle) {
		t.Fatalf("handle %q does not match pattern", handle)
	}

	rc, err := eng.ReadFile(ctx, handle)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	got, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(got) != string(data) {
		t.Errorf("ReadFile = %q, want %q", got, data)
	}

	if err := eng.DeleteFile(ctx, handle); err != nil {
		t.Fatalf("DeleteFile: %v", err)
	}

	_, err = eng.ReadFile(ctx, handle)
	if err == nil {
		t.Error("ReadFile after delete should fail")
	}
}

func TestLocalDiskRejectsBadHandle(t *testing.T) {
	dir, err := os.MkdirTemp("", "localdisk-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	eng, err := NewLocalDiskEngine(dir)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()

	_, err = eng.ReadFile(ctx, "../../../etc/passwd")
	if err == nil {
		t.Error("ReadFile should reject path traversal")
	}

	err = eng.DeleteFile(ctx, "../../bad")
	if err == nil {
		t.Error("DeleteFile should reject path traversal")
	}
}

func TestLocalDiskBadRoot(t *testing.T) {
	_, err := NewLocalDiskEngine("")
	if err == nil {
		t.Error("should reject empty root")
	}
	_, err = NewLocalDiskEngine("relative/path")
	if err == nil {
		t.Error("should reject relative path")
	}
}
