// Package attachments carries the signing + inline-eligibility helpers
// used by the attachment view path.
//
// The view endpoint is intentionally unauthenticated at the HTTP
// middleware layer — the HMAC signature IS the authorization. Callers
// obtain a short-lived URL by hitting the authenticated
// /view-url endpoint, which boundary-checks ownership, then renders
// <img src> against the signed URL directly.
//
// Reuse within the TTL window is expected — the same thumbnail will
// render in the inbox modal, the memory detail page, and the lightbox
// in a single session. Single-use was considered and rejected; it
// flaps on re-renders, retries, and cache revalidation.
package attachments

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Signer produces and verifies signed view URLs for attachments.
//
// The signed payload binds only to attachment ID and expiry: the
// server-side handler re-derives filename, content-type, storage key
// and inline eligibility from the DB row, so a leaked signing key
// cannot be used to coax inline disposition or flip a row's
// eligibility — only to access rows the attacker already knows the
// ID of within the TTL window.
type Signer struct {
	key []byte
	// DefaultTTL is the issued URL lifetime. Kept short so a leaked URL
	// window is bounded, but long enough that the same image reused
	// across inbox modal / detail / lightbox / browser retry stays on
	// a single signed URL.
	DefaultTTL time.Duration
}

// NewSigner returns a Signer, or nil if key is empty. Callers must
// check for nil — a nil signer means the view endpoint is disabled
// (no ATTACHMENT_VIEW_SIGNING_KEY set in env).
func NewSigner(key string) *Signer {
	trimmed := strings.TrimSpace(key)
	if trimmed == "" {
		return nil
	}
	return &Signer{
		key:        []byte(trimmed),
		DefaultTTL: 10 * time.Minute,
	}
}

// Sign returns an absolute URL for the view endpoint with a signature
// and expiry bound to attachmentID. baseURL should include the scheme
// and host but no query string (e.g. "https://api.memax.app").
//
// The query params are placed in a fixed order so the signing input
// is canonical. Callers must not mutate the returned URL — the
// signature only covers `id` and `exp`.
func (s *Signer) Sign(baseURL string, attachmentID string, ttl time.Duration) (viewURL string, expiresAt time.Time, err error) {
	if s == nil {
		return "", time.Time{}, errors.New("attachments: signer not configured")
	}
	if strings.TrimSpace(attachmentID) == "" {
		return "", time.Time{}, errors.New("attachments: empty attachment id")
	}
	if ttl <= 0 {
		ttl = s.DefaultTTL
	}
	exp := time.Now().Add(ttl).Unix()
	sig := s.sign(attachmentID, exp)

	u, err := url.Parse(strings.TrimRight(baseURL, "/") + "/v1/attachments/view")
	if err != nil {
		return "", time.Time{}, fmt.Errorf("attachments: invalid base url: %w", err)
	}
	q := u.Query()
	q.Set("id", attachmentID)
	q.Set("exp", strconv.FormatInt(exp, 10))
	q.Set("sig", sig)
	u.RawQuery = q.Encode()
	return u.String(), time.Unix(exp, 0).UTC(), nil
}

// Verify returns nil if sig is a valid HMAC for (attachmentID, exp)
// and exp has not passed. It returns ErrBadSignature for any mismatch
// including expiry — the view handler maps both to HTTP 403 so
// callers cannot distinguish "wrong key" from "expired" from the
// failure response.
func (s *Signer) Verify(attachmentID string, exp int64, sig string) error {
	if s == nil {
		return errors.New("attachments: signer not configured")
	}
	if strings.TrimSpace(attachmentID) == "" || sig == "" {
		return ErrBadSignature
	}
	if time.Now().Unix() > exp {
		return ErrBadSignature
	}
	expected := s.sign(attachmentID, exp)
	if !hmac.Equal([]byte(expected), []byte(sig)) {
		return ErrBadSignature
	}
	return nil
}

// ErrBadSignature covers both tamper and expiry. The view handler
// deliberately maps both to 403 with no detail to avoid leaking
// which fix the attacker needs (fresh sig vs new URL).
var ErrBadSignature = errors.New("attachments: bad or expired signature")

func (s *Signer) sign(attachmentID string, exp int64) string {
	mac := hmac.New(sha256.New, s.key)
	// Canonical input: "id|exp". Pipe is safe because UUIDs and
	// numeric unix timestamps cannot contain it.
	mac.Write([]byte(attachmentID + "|" + strconv.FormatInt(exp, 10)))
	return hex.EncodeToString(mac.Sum(nil))
}
