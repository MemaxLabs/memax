package email

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strconv"
	"testing"
	"time"
)

// signPayload mirrors the Svix signing algorithm so tests can produce
// valid signatures without depending on the svix SDK.
func signPayload(t *testing.T, key []byte, svixID, ts string, body []byte) string {
	t.Helper()
	signed := svixID + "." + ts + "." + string(body)
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(signed))
	return "v1," + base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func TestResendWebhookSecretAcceptsValidSignature(t *testing.T) {
	rawKey := []byte("test-secret-key-32-bytes-long-ok")
	encoded := "whsec_" + base64.StdEncoding.EncodeToString(rawKey)
	secret, err := NewResendWebhookSecret(encoded)
	if err != nil {
		t.Fatalf("parse secret: %v", err)
	}

	body := []byte(`{"type":"email.delivered","data":{}}`)
	svixID := "msg_abc123"
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := signPayload(t, rawKey, svixID, ts, body)

	if err := secret.Verify(svixID, ts, sig, body, 0); err != nil {
		t.Fatalf("expected valid signature, got: %v", err)
	}
}

func TestResendWebhookSecretRejectsTamperedBody(t *testing.T) {
	rawKey := []byte("test-secret-key-32-bytes-long-ok")
	secret, err := NewResendWebhookSecret(base64.StdEncoding.EncodeToString(rawKey))
	if err != nil {
		t.Fatalf("parse secret: %v", err)
	}

	body := []byte(`{"type":"email.delivered","data":{}}`)
	svixID := "msg_abc123"
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := signPayload(t, rawKey, svixID, ts, body)

	// Tamper after signing — simulates body-in-transit modification.
	tampered := []byte(`{"type":"email.bounced","data":{}}`)
	if err := secret.Verify(svixID, ts, sig, tampered, 0); err == nil {
		t.Fatal("expected tampered body to be rejected")
	}
}

func TestResendWebhookSecretRejectsStaleTimestamp(t *testing.T) {
	rawKey := []byte("test-secret-key-32-bytes-long-ok")
	secret, err := NewResendWebhookSecret(base64.StdEncoding.EncodeToString(rawKey))
	if err != nil {
		t.Fatalf("parse secret: %v", err)
	}

	body := []byte(`{}`)
	svixID := "msg_stale"
	// 1 hour in the past — outside the default 5-minute tolerance.
	staleTS := strconv.FormatInt(time.Now().Add(-1*time.Hour).Unix(), 10)
	sig := signPayload(t, rawKey, svixID, staleTS, body)

	if err := secret.Verify(svixID, staleTS, sig, body, 0); err == nil {
		t.Fatal("expected stale timestamp to be rejected")
	}
}

func TestResendWebhookSecretAcceptsMultipleSignaturesPicksValidOne(t *testing.T) {
	rawKey := []byte("test-secret-key-32-bytes-long-ok")
	secret, err := NewResendWebhookSecret(base64.StdEncoding.EncodeToString(rawKey))
	if err != nil {
		t.Fatalf("parse secret: %v", err)
	}

	body := []byte(`{"x":1}`)
	svixID := "msg_multi"
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	validSig := signPayload(t, rawKey, svixID, ts, body)

	// Resend rotates keys by publishing multiple signatures — the
	// verifier must accept if ANY entry matches the active secret.
	header := fmt.Sprintf("v1,%s %s", "forged-nonsense", validSig)
	if err := secret.Verify(svixID, ts, header, body, 0); err != nil {
		t.Fatalf("expected any-match to succeed, got: %v", err)
	}
}
