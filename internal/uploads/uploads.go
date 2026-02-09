package uploads

import (
	"context"
	"errors"
	"io"
)

var ErrObjectNotFound = errors.New("object not found")

type Store interface {
	Upload(ctx context.Context, key, contentType string, body io.Reader, size int64) error
	Download(ctx context.Context, key string) (io.ReadCloser, string, error)
}
