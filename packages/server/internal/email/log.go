package email

import (
	"context"
	"log/slog"

	"github.com/google/uuid"
)

// LogSender logs emails to slog instead of sending them.
// Used in dev/test when no email provider is configured.
type LogSender struct{}

func (s *LogSender) Send(_ context.Context, msg Message) (SendResult, error) {
	id := "dev-" + uuid.NewString()[:8]
	slog.Info("email sent (dev mode)",
		"message_id", id,
		"to", msg.To,
		"subject", msg.Subject,
		"html_length", len(msg.HTML),
		"text_length", len(msg.Text),
	)
	return SendResult{MessageID: id}, nil
}
