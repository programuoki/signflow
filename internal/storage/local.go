package storage

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
)

// LocalStore writes blobs as files under a base directory.
type LocalStore struct {
	dir string
}

// keyPattern matches the keys we generate (32 hex chars). Open/Delete validate
// against it so a stored key can never escape the base dir via path traversal.
var keyPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)

// NewLocalStore ensures dir exists and returns a store rooted there.
func NewLocalStore(dir string) (*LocalStore, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create upload dir: %w", err)
	}
	return &LocalStore{dir: dir}, nil
}

func (s *LocalStore) Save(_ context.Context, r io.Reader) (string, int64, string, error) {
	key, err := newKey()
	if err != nil {
		return "", 0, "", err
	}
	path := filepath.Join(s.dir, key)

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return "", 0, "", err
	}

	h := sha256.New()
	size, copyErr := io.Copy(io.MultiWriter(f, h), r) // hash while writing
	closeErr := f.Close()
	if copyErr != nil || closeErr != nil {
		_ = os.Remove(path) // don't leave a half-written file behind
		return "", 0, "", errors.Join(copyErr, closeErr)
	}
	return key, size, hex.EncodeToString(h.Sum(nil)), nil
}

func (s *LocalStore) Open(_ context.Context, key string) (io.ReadCloser, error) {
	path, err := s.safePath(key)
	if err != nil {
		return nil, err
	}
	return os.Open(path)
}

func (s *LocalStore) Delete(_ context.Context, key string) error {
	path, err := s.safePath(key)
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (s *LocalStore) safePath(key string) (string, error) {
	if !keyPattern.MatchString(key) {
		return "", fmt.Errorf("invalid storage key")
	}
	return filepath.Join(s.dir, key), nil
}

func newKey() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
