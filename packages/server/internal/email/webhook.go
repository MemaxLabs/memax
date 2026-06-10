package email

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ResendWebhookSecret is the admin-provisioned secret used to verify
// Svix-signed webhook payloads from Resend. Resend secrets start with
// `whsec_` — the prefix is stripped before base64-decoding the key.
type ResendWebhookSecret struct {
	// key is the raw HMAC key (base64-decoded).
	key []byte
}

// NewResendWebhookSecret parses a Resend webhook secret. Accepts both
// the `whsec_<b64>` form Resend surfaces in its dashboard and the raw
// base64 form. Returns an error if the secret is malformed.
func NewResendWebhookSecret(secret string) (*ResendWebhookSecret, error) {
	trimmed := strings.TrimSpace(secret)
	if trimmed == "" {
		return nil, fmt.Errorf("empty webhook secret")
	}
	trimmed = strings.TrimPrefix(trimmed, "whsec_")
	decoded, err := base64.StdEncoding.DecodeString(trimmed)
	if err != nil {
		return nil, fmt.Errorf("base64-decode webhook secret: %w", err)
	}
	if len(decoded) == 0 {
		return nil, fmt.Errorf("decoded webhook secret is empty")
	}
	return &ResendWebhookSecret{key: decoded}, nil
}

// Verify checks a Svix signature against the raw request body. Returns
// nil if any of the space-separated signatures in svixSignature header
// match. toleranceSeconds rejects timestamps outside the allowed skew
// window — set 0 to use the default of 5 minutes.
//
// Svix signing: HMAC-SHA256(id + "." + timestamp + "." + body, key),
// base64-encoded, checked against each space-separated entry in the
// svix-signature header (format: `v1,<sig> v1,<sig>`). Entries with
// unknown version prefixes are skipped.
func (s *ResendWebhookSecret) Verify(svixID, svixTimestamp, svixSignature string, body []byte, toleranceSeconds int) error {
	if s == nil || len(s.key) == 0 {
		return fmt.Errorf("webhook secret not configured")
	}
	if svixID == "" || svixTimestamp == "" || svixSignature == "" {
		return fmt.Errorf("missing svix headers")
	}

	// Reject stale timestamps to mitigate replay attacks.
	ts, err := strconv.ParseInt(svixTimestamp, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid svix timestamp: %w", err)
	}
	if toleranceSeconds <= 0 {
		toleranceSeconds = 5 * 60
	}
	delta := time.Now().Unix() - ts
	if delta < 0 {
		delta = -delta
	}
	if delta > int64(toleranceSeconds) {
		return fmt.Errorf("svix timestamp outside tolerance window (%ds)", delta)
	}

	signedPayload := svixID + "." + svixTimestamp + "." + string(body)
	mac := hmac.New(sha256.New, s.key)
	mac.Write([]byte(signedPayload))
	expected := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	for _, entry := range strings.Fields(svixSignature) {
		parts := strings.SplitN(entry, ",", 2)
		if len(parts) != 2 {
			continue
		}
		version, candidate := parts[0], parts[1]
		if version != "v1" {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(expected), []byte(candidate)) == 1 {
			return nil
		}
	}
	return fmt.Errorf("no valid svix signature match")
}
