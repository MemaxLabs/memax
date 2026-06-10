package email

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ResendSender sends emails via Resend's HTTP API (https://resend.com/docs/api-reference).
// Production default — requires RESEND_API_KEY env var.
type ResendSender struct {
	apiKey     string
	httpClient *http.Client
}

// NewResendSender creates a Resend email sender.
func NewResendSender(apiKey string) *ResendSender {
	return &ResendSender{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (s *ResendSender) Send(ctx context.Context, msg Message) (SendResult, error) {
	body := map[string]any{
		"from":    msg.From,
		"to":      msg.To,
		"subject": msg.Subject,
	}
	if msg.HTML != "" {
		body["html"] = msg.HTML
	}
	if msg.Text != "" {
		body["text"] = msg.Text
	}
	if msg.ReplyTo != "" {
		body["reply_to"] = msg.ReplyTo
	}
	if len(msg.Tags) > 0 {
		tags := make([]map[string]string, 0, len(msg.Tags))
		for k, v := range msg.Tags {
			tags = append(tags, map[string]string{"name": k, "value": v})
		}
		body["tags"] = tags
	}
	if len(msg.Headers) > 0 {
		// Resend's API accepts a headers object keyed by header name.
		// Forwarded as-is to the outbound SMTP envelope.
		body["headers"] = msg.Headers
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return SendResult{}, fmt.Errorf("resend: marshal body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.resend.com/emails", bytes.NewReader(jsonBody))
	if err != nil {
		return SendResult{}, fmt.Errorf("resend: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return SendResult{}, fmt.Errorf("resend: request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return SendResult{}, fmt.Errorf("resend: read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return SendResult{}, fmt.Errorf("resend: HTTP %d: %s", resp.StatusCode, respBody)
	}

	var result struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return SendResult{}, fmt.Errorf("resend: parse response: %w", err)
	}

	return SendResult{MessageID: result.ID}, nil
}
