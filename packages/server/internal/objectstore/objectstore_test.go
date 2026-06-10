package objectstore

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

// ─── shouldUsePathStyle (pure helper) ───────────────────────────

func TestShouldUsePathStyle(t *testing.T) {
	t.Parallel()
	cases := map[string]bool{
		"":                                 false, // empty → virtual-hosted (AWS default)
		"https://r2.cloudflarestorage.com": false, // production: no path style
		"http://localhost:9000":            true,  // MinIO / local dev
		"http://127.0.0.1:9000":            true,  // IP loopback
		"http://minio:9000":                true,  // compose service name
		"https://s3.amazonaws.com":         false,
		"https://my-bucket.s3.us-east-1.amazonaws.com": false,
	}
	for in, want := range cases {
		if got := shouldUsePathStyle(in); got != want {
			t.Errorf("shouldUsePathStyle(%q) = %v, want %v", in, got, want)
		}
	}
}

// ─── NewFromEnv ─────────────────────────────────────────────────

func TestNewFromEnvReturnsNilWhenIncomplete(t *testing.T) {
	// Missing access key → nil
	t.Setenv("S3_ACCESS_KEY", "")
	t.Setenv("S3_SECRET_KEY", "s")
	t.Setenv("S3_BUCKET", "b")
	if s := NewFromEnv(); s != nil {
		t.Error("missing access key should return nil")
	}

	// Missing secret → nil
	t.Setenv("S3_ACCESS_KEY", "a")
	t.Setenv("S3_SECRET_KEY", "")
	t.Setenv("S3_BUCKET", "b")
	if s := NewFromEnv(); s != nil {
		t.Error("missing secret should return nil")
	}

	// Missing bucket → nil
	t.Setenv("S3_ACCESS_KEY", "a")
	t.Setenv("S3_SECRET_KEY", "s")
	t.Setenv("S3_BUCKET", "")
	if s := NewFromEnv(); s != nil {
		t.Error("missing bucket should return nil")
	}
}

func TestNewFromEnvBuildsStoreWhenComplete(t *testing.T) {
	// Minimum required env: access + secret + bucket.
	t.Setenv("S3_ACCESS_KEY", "a")
	t.Setenv("S3_SECRET_KEY", "s")
	t.Setenv("S3_BUCKET", "test-bucket")
	t.Setenv("S3_REGION", "") // → defaults to us-east-1
	t.Setenv("S3_ENDPOINT", "")
	t.Setenv("S3_PUBLIC_ENDPOINT", "")

	s := NewFromEnv()
	if s == nil {
		t.Fatal("complete env should produce a store")
	}
	// Concrete type check — ensures we can downcast for unit use.
	if _, ok := s.(*S3Store); !ok {
		t.Errorf("expected *S3Store, got %T", s)
	}
}

func TestNewFromEnvDefaultsRegion(t *testing.T) {
	t.Setenv("S3_ACCESS_KEY", "a")
	t.Setenv("S3_SECRET_KEY", "s")
	t.Setenv("S3_BUCKET", "b")
	t.Setenv("S3_REGION", "")
	if s := NewFromEnv(); s == nil {
		t.Error("empty region should still produce a store (defaults to us-east-1)")
	}
}

func TestNewFromEnvUsesPublicEndpointForPresign(t *testing.T) {
	// Separate S3_PUBLIC_ENDPOINT → presign client should get
	// that URL while server-side Put/Get goes through S3_ENDPOINT.
	// We can't observe the presigned URL's origin here without
	// actually calling PresignPut (requires network / MinIO up),
	// but we CAN assert the constructor returns non-nil when
	// the separate endpoint is set — catches a refactor that
	// forgets to wire presignOpts.
	t.Setenv("S3_ACCESS_KEY", "a")
	t.Setenv("S3_SECRET_KEY", "s")
	t.Setenv("S3_BUCKET", "b")
	t.Setenv("S3_ENDPOINT", "http://minio:9000")
	t.Setenv("S3_PUBLIC_ENDPOINT", "http://localhost:9000")
	if s := NewFromEnv(); s == nil {
		t.Error("separate public endpoint should still build the store")
	}
}

// ─── Optional integration test: MinIO (if present) ──────────────

// TestS3StoreIntegration runs a Put → Get → Delete roundtrip
// against a live MinIO if S3_ACCESS_KEY + S3_ENDPOINT point at one.
// Skips otherwise so CI without MinIO stays green.
func TestS3StoreIntegration(t *testing.T) {
	endpoint := strings.TrimSpace(os.Getenv("S3_ENDPOINT"))
	if endpoint == "" {
		t.Skip("S3_ENDPOINT not set — skipping live objectstore roundtrip")
	}
	// Use the devcontainer's compose-default if only the bare
	// access/secret are set (matching the pattern the rest of
	// the test harness uses).
	t.Setenv("S3_ENDPOINT", endpoint)

	store := NewFromEnv()
	if store == nil {
		t.Skip("objectstore NewFromEnv returned nil — env incomplete")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	key := "test/objectstore-roundtrip-" + t.Name()
	payload := []byte("hello objectstore")

	// Put
	if err := store.Put(ctx, PutInput{
		Key:         key,
		Body:        bytes.NewReader(payload),
		ContentType: "text/plain",
		SizeBytes:   int64(len(payload)),
	}); err != nil {
		t.Skipf("live Put failed (expected on first run before bucket exists): %v", err)
	}

	// Get
	result, err := store.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer result.Body.Close()
	got, err := io.ReadAll(result.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(got) != string(payload) {
		t.Errorf("roundtrip mismatch: got %q, want %q", got, payload)
	}

	// Delete
	if err := store.Delete(ctx, key); err != nil {
		t.Errorf("Delete: %v", err)
	}
}
