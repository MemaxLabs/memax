package mockobjectstore_test

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/objectstore"
	"github.com/MemaxLabs/memax/packages/server/internal/objectstore/mockobjectstore"
)

func TestPutGetRoundTrip(t *testing.T) {
	t.Parallel()
	s := mockobjectstore.New()
	ctx := context.Background()

	if err := s.Put(ctx, objectstore.PutInput{
		Key:         "a/b/c.txt",
		Body:        bytes.NewReader([]byte("hello")),
		ContentType: "text/plain",
		SizeBytes:   5,
	}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, err := s.Get(ctx, "a/b/c.txt")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer got.Body.Close()
	body, _ := io.ReadAll(got.Body)
	if string(body) != "hello" {
		t.Errorf("body = %q, want hello", string(body))
	}
	if got.ContentType != "text/plain" {
		t.Errorf("ContentType = %q", got.ContentType)
	}
	if got.SizeBytes != 5 {
		t.Errorf("SizeBytes = %d", got.SizeBytes)
	}
}

func TestPutAllowsOverwrite(t *testing.T) {
	t.Parallel()
	s := mockobjectstore.New()
	ctx := context.Background()
	_ = s.Put(ctx, objectstore.PutInput{Key: "k", Body: bytes.NewReader([]byte("v1"))})
	_ = s.Put(ctx, objectstore.PutInput{Key: "k", Body: bytes.NewReader([]byte("v2"))})
	got, _ := s.Get(ctx, "k")
	defer got.Body.Close()
	body, _ := io.ReadAll(got.Body)
	if string(body) != "v2" {
		t.Errorf("overwrite lost: %q", string(body))
	}
}

func TestGetMissingReturnsError(t *testing.T) {
	t.Parallel()
	s := mockobjectstore.New()
	_, err := s.Get(context.Background(), "nope")
	if err == nil {
		t.Error("missing key should error")
	}
}

func TestDeleteIdempotent(t *testing.T) {
	t.Parallel()
	s := mockobjectstore.New()
	ctx := context.Background()
	// Delete of missing key is not an error.
	if err := s.Delete(ctx, "nope"); err != nil {
		t.Errorf("missing-key delete should be idempotent, got %v", err)
	}
	_ = s.Put(ctx, objectstore.PutInput{Key: "x", Body: bytes.NewReader([]byte("v"))})
	if err := s.Delete(ctx, "x"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if s.Has("x") {
		t.Error("Delete didn't remove key")
	}
}

func TestPutRejectsEmptyKeyOrNilBody(t *testing.T) {
	t.Parallel()
	s := mockobjectstore.New()
	if err := s.Put(context.Background(), objectstore.PutInput{Body: bytes.NewReader([]byte(""))}); err == nil {
		t.Error("empty key should error")
	}
	if err := s.Put(context.Background(), objectstore.PutInput{Key: "k", Body: nil}); err == nil {
		t.Error("nil body should error")
	}
}

func TestPresignPutReturnsStableURL(t *testing.T) {
	t.Parallel()
	s := mockobjectstore.New()
	url, headers, err := s.PresignPut(context.Background(), "uploads/123",
		"application/octet-stream", time.Minute)
	if err != nil {
		t.Fatalf("PresignPut: %v", err)
	}
	if url != "http://mockobjectstore/uploads/123" {
		t.Errorf("URL = %q", url)
	}
	if headers != nil {
		t.Errorf("headers should be nil, got %v", headers)
	}

	// Empty key should error.
	if _, _, err := s.PresignPut(context.Background(), "", "", time.Minute); err == nil {
		t.Error("empty key should error on PresignPut")
	}
}

func TestPresignPutWithSizeIgnoresMaxBytes(t *testing.T) {
	t.Parallel()
	s := mockobjectstore.New()
	u1, _, _ := s.PresignPut(context.Background(), "k", "text/plain", time.Minute)
	u2, _, _ := s.PresignPutWithSize(context.Background(), "k", "text/plain", 999, time.Minute)
	if u1 != u2 {
		t.Error("PresignPutWithSize should yield the same URL shape as PresignPut")
	}
}

func TestLenAndKeys(t *testing.T) {
	t.Parallel()
	s := mockobjectstore.New()
	ctx := context.Background()
	for _, k := range []string{"a", "b", "c"} {
		_ = s.Put(ctx, objectstore.PutInput{Key: k, Body: bytes.NewReader([]byte(k))})
	}
	if s.Len() != 3 {
		t.Errorf("Len = %d, want 3", s.Len())
	}
	keys := map[string]bool{}
	for _, k := range s.Keys() {
		keys[k] = true
	}
	if !keys["a"] || !keys["b"] || !keys["c"] {
		t.Errorf("Keys missing entries: %v", keys)
	}
}
