package engine

import (
	"context"
	"io"
)

type WriteParams struct {
	Name     string
	MimeType string
}

type FileInfo struct {
	Handle   string `json:"handle"`
	Engine   string `json:"engine"`
	Size     int64  `json:"size"`
	MimeType string `json:"mimeType,omitempty"`
}

type StorageEngine interface {
	Identifier() string
	Priority() int
	CanWrite() bool
	HasSizeLimit() bool
	MaxFileSize() int64

	WriteFile(ctx context.Context, data []byte, params WriteParams) (handle string, err error)
	ReadFile(ctx context.Context, handle string) (io.ReadCloser, error)
	DeleteFile(ctx context.Context, handle string) error
}
