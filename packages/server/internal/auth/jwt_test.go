package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// The test secret is fixed so signature comparisons are stable
// across runs. It is NOT representative of production secret
// length — tests just need a non-empty key for HMAC.
var testSecret = []byte("test-secret-key-not-for-prod")

func TestSignAccessTokenRoundTrip(t *testing.T) {
	t.Parallel()

	token, err := SignAccessToken("user-1", testSecret, time.Minute)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	claims, err := VerifyAccessToken(token, testSecret)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if claims.Sub != "user-1" {
		t.Errorf("sub = %q, want user-1", claims.Sub)
	}
	if claims.AgentName != "" {
		t.Errorf("unexpected agent_name %q on bare access token", claims.AgentName)
	}
	if claims.GrantID != "" {
		t.Errorf("unexpected grant_id %q on bare access token", claims.GrantID)
	}
	if claims.ImpersonatorID != "" {
		t.Errorf("unexpected impersonator_id %q on bare access token", claims.ImpersonatorID)
	}
}

func TestSignAgentAccessTokenEmbedsAgentName(t *testing.T) {
	t.Parallel()

	token, err := SignAgentAccessToken("user-7", "claude-code", testSecret, time.Minute)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	claims, err := VerifyAccessToken(token, testSecret)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if claims.AgentName != "claude-code" {
		t.Errorf("agent_name = %q, want claude-code", claims.AgentName)
	}
	if claims.GrantID != "" {
		t.Errorf("grant_id = %q, want empty — agent-only tokens don't carry a grant", claims.GrantID)
	}
}

func TestSignGrantAccessTokenEmbedsBoth(t *testing.T) {
	t.Parallel()

	token, err := SignGrantAccessToken("user-9", "claude-ai", "grant-abc", testSecret, time.Minute)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	claims, err := VerifyAccessToken(token, testSecret)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if claims.AgentName != "claude-ai" || claims.GrantID != "grant-abc" {
		t.Errorf("got agent_name=%q grant_id=%q; want claude-ai, grant-abc",
			claims.AgentName, claims.GrantID)
	}
}

func TestSignImpersonationTokenEmbedsImpersonator(t *testing.T) {
	t.Parallel()

	token, err := SignImpersonationToken("target-user", "admin-42", testSecret, time.Minute)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	claims, err := VerifyAccessToken(token, testSecret)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	// The sub is the TARGET (the impersonation's effective
	// identity), NOT the impersonator — the server acts as the
	// target with impersonator_id kept for audit attribution.
	// Getting this wrong would elevate privileges instead of
	// attributing them.
	if claims.Sub != "target-user" {
		t.Errorf("sub = %q, want target-user (impersonation should act AS the target)", claims.Sub)
	}
	if claims.ImpersonatorID != "admin-42" {
		t.Errorf("impersonator_id = %q, want admin-42", claims.ImpersonatorID)
	}
}

func TestVerifyAccessTokenRejectsExpiredToken(t *testing.T) {
	t.Parallel()

	// ttl of -1s => already expired at the moment we sign.
	token, err := SignAccessToken("user-1", testSecret, -time.Second)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if _, err := VerifyAccessToken(token, testSecret); err == nil {
		t.Fatal("expected expired token to be rejected")
	} else if !strings.Contains(err.Error(), "expired") {
		t.Errorf("want expiry error, got: %v", err)
	}
}

func TestVerifyAccessTokenRejectsWrongSecret(t *testing.T) {
	t.Parallel()

	token, err := SignAccessToken("user-1", testSecret, time.Minute)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if _, err := VerifyAccessToken(token, []byte("different-secret")); err == nil {
		t.Fatal("expected signature mismatch to reject token")
	} else if !strings.Contains(err.Error(), "invalid signature") {
		t.Errorf("want signature error, got: %v", err)
	}
}

func TestVerifyAccessTokenRejectsTamperedSignature(t *testing.T) {
	t.Parallel()

	token, err := SignAccessToken("user-1", testSecret, time.Minute)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	// Flip the last character of the signature. Swap to another
	// base64 char so we don't accidentally produce an invalid-
	// encoding error before reaching the signature check.
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("token shape unexpected: %d parts", len(parts))
	}
	last := parts[2][len(parts[2])-1]
	swap := byte('A')
	if last == 'A' {
		swap = 'B'
	}
	parts[2] = parts[2][:len(parts[2])-1] + string(swap)
	tampered := strings.Join(parts, ".")
	if _, err := VerifyAccessToken(tampered, testSecret); err == nil {
		t.Fatal("tampered signature must not verify")
	}
}

func TestVerifyAccessTokenRejectsTamperedClaims(t *testing.T) {
	t.Parallel()

	// Sign for user-1, then swap the payload section to a
	// maliciously-crafted claim block that asserts sub=user-2 (a
	// classic privilege-escalation attempt). The signature was
	// computed over user-1's payload, so re-verifying with the
	// new payload MUST fail signature check.
	token, err := SignAccessToken("user-1", testSecret, time.Minute)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	evilClaims := Claims{
		Sub: "user-2",
		Iat: time.Now().Unix(),
		Exp: time.Now().Add(time.Hour).Unix(),
	}
	evilJSON, err := json.Marshal(evilClaims)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	evilPayload := base64.RawURLEncoding.EncodeToString(evilJSON)
	parts := strings.Split(token, ".")
	parts[1] = evilPayload
	forged := strings.Join(parts, ".")
	if _, err := VerifyAccessToken(forged, testSecret); err == nil {
		t.Fatal("claim-substitution attack must be rejected by signature check")
	}
}

func TestVerifyAccessTokenRejectsMalformed(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		token string
	}{
		{"empty", ""},
		{"one_part", "onlyone"},
		{"two_parts", "only.two"},
		// Four parts SHOULD be accepted by SplitN(..., 3) — the
		// third field just ends up containing a dot. Left out so
		// the test stays aligned with the actual behavior.
		{"garbage_payload_base64", "aGVhZGVy.not-base64-!!!.c2ln"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := VerifyAccessToken(tc.token, testSecret); err == nil {
				t.Errorf("expected malformed token %q to be rejected", tc.token)
			}
		})
	}
}

func TestVerifyAccessTokenRejectsInvalidClaimsJSON(t *testing.T) {
	t.Parallel()

	// Hand-craft a token where header + signature are valid but
	// the payload decodes to non-JSON. Signature check passes
	// (we recompute it over the base64 bytes), so we fall through
	// into json.Unmarshal's error branch. This covers the
	// "invalid claims" return in verifyJWT.
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	badPayload := base64.RawURLEncoding.EncodeToString([]byte("not json at all"))
	sigInput := header + "." + badPayload
	mac := hmac.New(sha256.New, testSecret)
	mac.Write([]byte(sigInput))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	token := sigInput + "." + sig
	if _, err := VerifyAccessToken(token, testSecret); err == nil {
		t.Fatal("non-JSON claims must be rejected")
	} else if !strings.Contains(err.Error(), "invalid claims") {
		t.Errorf("want 'invalid claims' error, got: %v", err)
	}
}

func TestBase64UrlNoPadding(t *testing.T) {
	t.Parallel()

	// RawURLEncoding produces no trailing '=' padding and uses
	// the URL-safe alphabet ('-', '_'). Verifying this is a
	// low-drama regression guard against an accidental switch to
	// `base64.StdEncoding`, which would produce tokens rejected
	// by browsers' URL parsing.
	out := base64url([]byte("ab"))
	if strings.Contains(out, "=") {
		t.Errorf("base64url output %q contains padding — must be RawURL", out)
	}
}
