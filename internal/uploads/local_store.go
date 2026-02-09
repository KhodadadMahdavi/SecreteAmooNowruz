package uploads

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"secreteamoonowruz/internal/config"
)

type LocalStore struct {
	root string
}

func NewLocalStore(cfg config.StorageConfig) (*LocalStore, error) {
	root := filepath.Join("data", "uploads", cfg.Bucket)
	if strings.HasPrefix(cfg.Endpoint, "file://") {
		customRoot := strings.TrimPrefix(cfg.Endpoint, "file://")
		if strings.TrimSpace(customRoot) != "" {
			root = filepath.Join(customRoot, cfg.Bucket)
		}
	}

	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("create uploads root: %w", err)
	}

	return &LocalStore{root: root}, nil
}

func (s *LocalStore) Upload(_ context.Context, key, contentType string, body io.Reader, _ int64) error {
	objectPath, err := s.pathForKey(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(objectPath), 0o755); err != nil {
		return fmt.Errorf("create object directory: %w", err)
	}

	file, err := os.Create(objectPath)
	if err != nil {
		return fmt.Errorf("create object file: %w", err)
	}
	if _, err := io.Copy(file, body); err != nil {
		_ = file.Close()
		return fmt.Errorf("write object file: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close object file: %w", err)
	}

	if err := os.WriteFile(objectPath+".content_type", []byte(contentType), 0o644); err != nil {
		return fmt.Errorf("write object metadata: %w", err)
	}
	return nil
}

func (s *LocalStore) Download(_ context.Context, key string) (io.ReadCloser, string, error) {
	objectPath, err := s.pathForKey(key)
	if err != nil {
		return nil, "", err
	}

	file, err := os.Open(objectPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, "", ErrObjectNotFound
		}
		return nil, "", fmt.Errorf("open object file: %w", err)
	}

	contentTypeBytes, err := os.ReadFile(objectPath + ".content_type")
	contentType := strings.TrimSpace(string(contentTypeBytes))
	if err != nil || contentType == "" {
		probe := make([]byte, 512)
		n, _ := file.Read(probe)
		_, _ = file.Seek(0, io.SeekStart)
		contentType = http.DetectContentType(probe[:n])
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	return file, contentType, nil
}

func (s *LocalStore) pathForKey(key string) (string, error) {
	clean := filepath.Clean(strings.TrimSpace(key))
	if clean == "." || clean == "" {
		return "", fmt.Errorf("invalid object key")
	}
	if strings.Contains(clean, "..") {
		return "", fmt.Errorf("invalid object key")
	}
	return filepath.Join(s.root, clean), nil
}
