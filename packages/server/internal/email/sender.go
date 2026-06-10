// Package email provides a provider-agnostic email delivery interface.
//
// Architecture:
//   - Sender interface: one method, Send(ctx, Message) → (SendResult, error)
//   - Implementations: ResendSender (HTTP API, production), LogSender (dev/test)
//   - Template rendering is separate — callers render HTML/text before calling Send
//   - Provider selection via env vars at startup (dependency injection)
//
// Adding a new provider is: one file implementing Sender + one env var check.
package email

import "context"

// Message is a single outbound email.
type Message struct {
	From    string   // "memax <noreply@memax.app>"
	To      []string // recipient(s)
	Subject string
	HTML    string            // rendered HTML body
	Text    string            // plaintext fallback
	ReplyTo string            // optional
	Tags    map[string]string // provider metadata (e.g., Resend tags)
	// Headers are RFC 5322 mail headers passed through to the provider.
	// Used for List-Unsubscribe (RFC 2369) and List-Unsubscribe-Post
	// (RFC 8058) on campaign emails so Gmail/Apple Mail render an
	// unsubscribe affordance in the UI chrome.
	Headers map[string]string
}

// SendResult is the provider's response after successfully sending.
type SendResult struct {
	MessageID string // provider-assigned ID for tracking/debugging
}

// Sender sends a single email. Implementations are provider-specific.
// The interface is intentionally minimal — one method, no batching,
// no template rendering. Templates are resolved BEFORE calling Send.
type Sender interface {
	Send(ctx context.Context, msg Message) (SendResult, error)
}
