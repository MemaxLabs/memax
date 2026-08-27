// Package secrets is the single home for sensitive-content pattern
// matching. Two consumers with two DIFFERENT postures share it:
//
//   - Push gating (DetectCredentials): memories are chunked, embedded
//     and recalled VERBATIM across every agent the user connects — a
//     credential that enters a memory becomes a replay device. Pushes
//     carrying one are rejected outright with an actionable error.
//     The reject set is CREDENTIALS ONLY: vendor-prefixed keys, PEM
//     private keys, JWTs, and key=value assignments. It deliberately
//     excludes PII and network-shape patterns (internal hostnames,
//     RFC1918 IPs, long hex, digit runs) — a developer's memory
//     product records git SHAs, localhost URLs and intranet topology
//     as a matter of course, and rejecting those would make push
//     unusable. Custody is refused entirely (no "encrypted secret
//     store"): the memory pipeline requires the server to READ
//     content, which is architecturally incompatible with the
//     zero-knowledge model credential storage demands. The future
//     shape is secret REFERENCES resolved against the user's own
//     vault — integration, not custody (plan 03 extension, open item).
//
//   - Grounding redaction (Redact): admin AI-assist replaces matches
//     from the WIDER list (credentials + PII + network shapes) with
//     "[redacted]" before note text reaches a prompt — silent
//     redaction is correct there because the reader is a marketing
//     email model, not the note's owner.
package secrets

import "regexp"

// credentialPattern pairs a human-readable name (surfaced in the
// rejection error so the user knows WHAT tripped) with its matcher.
type credentialPattern struct {
	Name string
	rx   *regexp.Regexp
}

// credentialPatterns is the push-REJECT set — high-confidence
// credential shapes only. Every entry answers yes to: "if this string
// is recalled verbatim into another agent's context, is that a
// credential leak?"
var credentialPatterns = []credentialPattern{
	// OpenAI / Anthropic / GitHub / Slack-style prefixed keys.
	{"vendor API key", regexp.MustCompile(`(?i)\b(?:sk|pk|xoxb|xoxa|xoxp|xoxs|xapp|ghp|gho|ghu|ghs|ghr|github_pat)[_-][A-Za-z0-9_-]{16,}\b`)},
	// Stripe live-mode keys.
	{"Stripe live key", regexp.MustCompile(`(?i)\b(?:sk|pk|rk)_live_[A-Za-z0-9]{16,}\b`)},
	// AWS access key IDs.
	{"AWS access key", regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`)},
	// Google Cloud API keys.
	{"Google API key", regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{35}\b`)},
	// PEM private keys. Certificates are NOT here — certs are public.
	{"private key block", regexp.MustCompile(`(?s)-----BEGIN[^-]*?PRIVATE KEY-----`)},
	// JWTs — short-lived but replayable while alive.
	{"JWT", regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\b`)},
	// key=value assignment leaks — the classic pasted .env / curl line.
	{"credential assignment", regexp.MustCompile(`(?i)\b(?:client_secret|api[_-]?key|secret|token|password|passwd|authorization)\s*[:=]\s*["']?[A-Za-z0-9._\-+/]{16,}["']?`)},
	// Bearer tokens.
	{"bearer token", regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._-]{20,}\b`)},
}

// DetectCredentials returns the names of every credential pattern the
// text matches, deduplicated in pattern order. Empty result = clean.
func DetectCredentials(text string) []string {
	if text == "" {
		return nil
	}
	var found []string
	for _, p := range credentialPatterns {
		if p.rx.MatchString(text) {
			found = append(found, p.Name)
		}
	}
	return found
}

// redactOnlyPatterns extends the credential set for the redaction
// consumer: PII and network shapes that should not reach an external
// prompt but are legitimate memory content.
var redactOnlyPatterns = []*regexp.Regexp{
	// Full PEM envelopes (private keys AND certificates) — redact the
	// whole block so snippets don't dangle a BEGIN header.
	regexp.MustCompile(`(?s)-----BEGIN[^-]*?PRIVATE KEY-----.*?-----END[^-]*?PRIVATE KEY-----`),
	regexp.MustCompile(`(?s)-----BEGIN[^-]*?CERTIFICATE-----.*?-----END[^-]*?CERTIFICATE-----`),
	// Long hex secret (32+ chars continuous).
	regexp.MustCompile(`\b[a-f0-9]{40,}\b`),
	// US SSN.
	regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`),
	// Credit-card-ish digit runs.
	regexp.MustCompile(`\b(?:\d[ -]?){13,19}\b`),
	// Internal hostnames / localhost.
	regexp.MustCompile(`\b(?:https?://)?(?:localhost|(?:\w+\.)?(?:intranet|internal|corp|lan)\.\w+)\b`),
	// RFC1918 / loopback IPs.
	regexp.MustCompile(`\b(?:10|127|192\.168|172\.(?:1[6-9]|2\d|3[0-1]))\.\d{1,3}\.\d{1,3}(?:\.\d{1,3})?\b`),
}

// Redact replaces every credential and redact-only match with
// "[redacted]". Behavior-compatible with the admin AI-assist list it
// replaces.
func Redact(s string) string {
	if s == "" {
		return ""
	}
	// Envelope patterns first: the credential set's BEGIN-header
	// matcher would otherwise consume the header and leave the body.
	for _, rx := range redactOnlyPatterns {
		s = rx.ReplaceAllString(s, "[redacted]")
	}
	for _, p := range credentialPatterns {
		s = p.rx.ReplaceAllString(s, "[redacted]")
	}
	return s
}
