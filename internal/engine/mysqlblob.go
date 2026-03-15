package engine

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"time"
)

type MySQLBlobEngine struct {
	db      *sql.DB
	maxSize int64
}

func NewMySQLBlobEngine(db *sql.DB, maxSize int64) *MySQLBlobEngine {
	return &MySQLBlobEngine{db: db, maxSize: maxSize}
}

func (e *MySQLBlobEngine) Identifier() string { return "blob" }
func (e *MySQLBlobEngine) Priority() int      { return 1 }
func (e *MySQLBlobEngine) CanWrite() bool     { return e.maxSize > 0 }
func (e *MySQLBlobEngine) HasSizeLimit() bool { return true }
func (e *MySQLBlobEngine) MaxFileSize() int64 { return e.maxSize }

func (e *MySQLBlobEngine) WriteFile(ctx context.Context, data []byte, _ WriteParams) (string, error) {
	if int64(len(data)) > e.maxSize {
		return "", fmt.Errorf("file size %d exceeds MySQL blob max %d", len(data), e.maxSize)
	}

	now := time.Now().Unix()
	res, err := e.db.ExecContext(ctx,
		"INSERT INTO file_storageblob (data, dateCreated, dateModified) VALUES (?, ?, ?)",
		data, now, now)
	if err != nil {
		return "", fmt.Errorf("insert blob: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return "", fmt.Errorf("get blob id: %w", err)
	}
	return fmt.Sprintf("%d", id), nil
}

func (e *MySQLBlobEngine) ReadFile(ctx context.Context, handle string) (io.ReadCloser, error) {
	var data []byte
	err := e.db.QueryRowContext(ctx,
		"SELECT data FROM file_storageblob WHERE id = ?", handle).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("blob %s not found", handle)
	}
	if err != nil {
		return nil, fmt.Errorf("read blob: %w", err)
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (e *MySQLBlobEngine) DeleteFile(ctx context.Context, handle string) error {
	res, err := e.db.ExecContext(ctx, "DELETE FROM file_storageblob WHERE id = ?", handle)
	if err != nil {
		return fmt.Errorf("delete blob: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("blob %s not found", handle)
	}
	return nil
}
