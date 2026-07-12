// Package storage abstracts where uploaded document bytes live.
//
// Dev uses the local filesystem (LocalStore). The interface is the seam for a
// production object store (S3, Cloudflare R2, a Railway Volume) — the same
// pattern as the email.Sender interface. See the README for why this matters on
// Railway, whose container filesystem is ephemeral.
package storage

import (
	"context"
	"io"
)

// Store persists opaque blobs keyed by a string the store itself generates.
type Store interface {
	// Save streams r to storage, returning a freshly generated key, the number
	// of bytes written, and the lowercase hex SHA-256 of the content. The hash
	// is computed in the same pass as the write — the file is never fully
	// buffered in memory.
	Save(ctx context.Context, r io.Reader) (key string, size int64, sha256hex string, err error)
	// Open returns a reader for the blob at key. Caller closes it.
	Open(ctx context.Context, key string) (io.ReadCloser, error)
	// Delete removes the blob. Deleting a missing key is not an error.
	Delete(ctx context.Context, key string) error
}
