package queue

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/riverqueue/river"

	"github.com/MemaxLabs/memax/packages/server/internal/email"
)

// SendEmailWorker renders a template and sends an email via the configured Sender.
type SendEmailWorker struct {
	river.WorkerDefaults[SendEmailArgs]
	Sender    email.Sender
	Templates *email.TemplateManager
	FromAddr  string // default From address, e.g., "memax <noreply@memax.app>"
}

func (w *SendEmailWorker) Timeout(_ *river.Job[SendEmailArgs]) time.Duration {
	return 30 * time.Second
}

func (w *SendEmailWorker) Work(ctx context.Context, job *river.Job[SendEmailArgs]) error {
	args := job.Args

	if w.Sender == nil {
		slog.WarnContext(ctx, "email sender not configured, skipping", "template", args.Template, "to", args.To)
		return nil
	}

	// Render template
	rendered, err := w.Templates.Render(ctx, args.Template, args.Variables)
	if err != nil {
		// Template errors are not retryable — fail permanently
		slog.ErrorContext(ctx, "email template render failed",
			"template", args.Template, "error", err)
		return river.JobCancel(fmt.Errorf("template render: %w", err))
	}

	// Send
	result, err := w.Sender.Send(ctx, email.Message{
		From:    w.FromAddr,
		To:      []string{args.To},
		Subject: rendered.Subject,
		HTML:    rendered.HTML,
		Text:    rendered.Text,
		Tags:    map[string]string{"template": args.Template},
	})
	if err != nil {
		slog.ErrorContext(ctx, "email send failed",
			"template", args.Template,
			"to", args.To,
			"attempt", job.Attempt,
			"error", err)
		return fmt.Errorf("send email: %w", err) // retryable
	}

	slog.InfoContext(ctx, "email sent",
		"template", args.Template,
		"to", args.To,
		"message_id", result.MessageID)

	return nil
}
