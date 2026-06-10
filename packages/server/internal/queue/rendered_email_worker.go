package queue

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/riverqueue/river"

	"github.com/MemaxLabs/memax/packages/server/internal/email"
)

// SendRenderedEmailWorker sends already-rendered email bodies.
type SendRenderedEmailWorker struct {
	river.WorkerDefaults[SendRenderedEmailArgs]
	Sender   email.Sender
	FromAddr string
}

func (w *SendRenderedEmailWorker) Timeout(_ *river.Job[SendRenderedEmailArgs]) time.Duration {
	return 30 * time.Second
}

func (w *SendRenderedEmailWorker) Work(ctx context.Context, job *river.Job[SendRenderedEmailArgs]) error {
	args := job.Args

	if w.Sender == nil {
		slog.WarnContext(ctx, "rendered email sender not configured, skipping", "to", args.To)
		return nil
	}

	result, err := w.Sender.Send(ctx, email.Message{
		From:    w.FromAddr,
		To:      []string{args.To},
		Subject: args.Subject,
		HTML:    args.HTML,
		Text:    args.Text,
		Tags:    args.Tags,
		Headers: args.Headers,
	})
	if err != nil {
		slog.ErrorContext(ctx, "rendered email send failed",
			"to", args.To,
			"attempt", job.Attempt,
			"error", err)
		return fmt.Errorf("send rendered email: %w", err)
	}

	slog.InfoContext(ctx, "rendered email sent", "to", args.To, "message_id", result.MessageID)
	return nil
}
