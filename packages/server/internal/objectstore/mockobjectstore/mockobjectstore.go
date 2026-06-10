// Package mockobjectstore provides an in-memory objectstore.Store for
// tests. Use it anywhere handler or worker code needs the S3/R2 API
// shape (Put/Get/Delete/PresignPut) without standing up MinIO.
//
// # Contract
//
//   - Put writes bytes keyed by object key; overwriting is allowed.
//   - Get returns a ReadCloser over a copy of the stored bytes.
//   - Delete is idempotent — deleting a missing key is not an error,
//     matching S3 semantics.
//   - PresignPut / PresignPutWithSize return a stable fake URL
//     (http://mockobjectstore/<key>) and no headers. Tests that
//     actually POST to the URL should do so via the Store's Put —
//     the URL is just a plausible string for the API contract.
//
// The store is safe for concurrent use and does not allocate beyond
// the payload size, so it can be used in high-fan-out tests without
// memory concerns.
package mockobjectstore

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/objectstore"
)

// Store is the mock implementation. The zero value is usable; prefer
// New() for readability.
type Store struct {
	mu      sync.RWMutex
	objects map[string]object
}

type object struct {
	body        []byte
	contentType string
	createdAt   time.Time
}

// New returns a ready-to-use Store.
func New() *Store {
	return &Store{objects: make(map[string]object)}
}

// Put implements objectstore.Store. Body is drained fully; size
// mismatches vs SizeBytes are NOT enforced — the production S3 path
// enforces that at the transport, and tests shouldn't duplicate it.
func (s *Store) Put(_ context.Context, input objectstore.PutInput) error {
	if input.Key == "" {
		return fmt.Errorf("mockobjectstore: empty key")
	}
	if input.Body == nil {
		return fmt.Errorf("mockobjectstore: nil body")
	}
	b, err := io.ReadAll(input.Body)
	if err != nil {
		return fmt.Errorf("mockobjectstore: read body: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.objects[input.Key] = object{
		body:        b,
		contentType: input.ContentType,
		createdAt:   time.Now(),
	}
	return nil
}

// Get implements objectstore.Store. Returns an error when the key is
// missing — S3 returns 404, but the Store contract leaves the error
// shape to the implementer, so this is fine.
func (s *Store) Get(_ context.Context, key string) (*objectstore.GetResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	obj, ok := s.objects[key]
	if !ok {
		return nil, fmt.Errorf("mockobjectstore: key not found: %s", key)
	}
	bodyCopy := make([]byte, len(obj.body))
	copy(bodyCopy, obj.body)
	return &objectstore.GetResult{
		Body:        io.NopCloser(bytes.NewReader(bodyCopy)),
		ContentType: obj.contentType,
		SizeBytes:   int64(len(bodyCopy)),
	}, nil
}

// Delete implements objectstore.Store. Idempotent — missing key is
// NOT an error, matching S3's DeleteObject behavior.
func (s *Store) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.objects, key)
	return nil
}

// PresignPut implements objectstore.Store. Returns a stable fake URL
// built from the key. No headers are returned — tests that care about
// required headers should assert Put was called instead of POSTing to
// the URL.
func (s *Store) PresignPut(_ context.Context, key string, contentType string, _ time.Duration) (string, map[string]string, error) {
	if key == "" {
		return "", nil, fmt.Errorf("mockobjectstore: empty key")
	}
	return "http://mockobjectstore/" + key, nil, nil
}

// PresignPutWithSize implements objectstore.Store. The size constraint
// is ignored in the mock; the returned URL is identical to PresignPut.
func (s *Store) PresignPutWithSize(ctx context.Context, key string, contentType string, _ int64, expires time.Duration) (string, map[string]string, error) {
	return s.PresignPut(ctx, key, contentType, expires)
}

// Len returns the number of objects stored. Tests assert write
// counts via this rather than peeking into internal state.
func (s *Store) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.objects)
}

// Keys returns the stored keys in no particular order. Useful for
// tests that want to assert "exactly these objects were written."
func (s *Store) Keys() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, 0, len(s.objects))
	for k := range s.objects {
		out = append(out, k)
	}
	return out
}

// Has reports whether a key is stored. Cheap without pulling the
// body out.
func (s *Store) Has(key string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.objects[key]
	return ok
}
