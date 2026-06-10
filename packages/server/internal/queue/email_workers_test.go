package queue

import (
	"context"
	"errors"
	"testing"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/MemaxLabs/memax/packages/server/internal/email"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// jobRow returns a non-nil rivertype.JobRow so worker code that
// reads job.Attempt etc. doesn't panic on our zero-value test jobs.
func jobRow() *rivertype.JobRow { return &rivertype.JobRow{Attempt: 1} }

// fakeSender captures the last Send invocation and returns a canned
// result or error.
type fakeSender struct {
	sendErr    error
	lastMsg    email.Message
	sendCalled int
}

func (f *fakeSender) Send(_ context.Context, msg email.Message) (email.SendResult, error) {
	f.sendCalled++
	f.lastMsg = msg
	if f.sendErr != nil {
		return email.SendResult{}, f.sendErr
	}
	return email.SendResult{MessageID: "fake-id"}, nil
}

func TestSendRenderedEmailWorker_SkipsWithoutSender(t *testing.T) {
	t.Parallel()
	w := &SendRenderedEmailWorker{Sender: nil}
	if w.Timeout(nil) == 0 {
		t.Error("Timeout should be non-zero")
	}
	err := w.Work(context.Background(), &river.Job[SendRenderedEmailArgs]{
		JobRow: jobRow(),
		Args:   SendRenderedEmailArgs{To: "a@b"},
	})
	if err != nil {
		t.Errorf("nil sender should silently skip, got %v", err)
	}
}

func TestSendRenderedEmailWorker_SendsWithConfiguredSender(t *testing.T) {
	t.Parallel()
	sender := &fakeSender{}
	w := &SendRenderedEmailWorker{Sender: sender, FromAddr: "From <from@me.test>"}
	err := w.Work(context.Background(), &river.Job[SendRenderedEmailArgs]{
		JobRow: jobRow(),
		Args: SendRenderedEmailArgs{
			To:      "recipient@x.test",
			Subject: "hi",
			HTML:    "<p>hi</p>",
			Text:    "hi",
			Tags:    map[string]string{"source": "test"},
		},
	})
	if err != nil {
		t.Fatalf("Work: %v", err)
	}
	if sender.sendCalled != 1 {
		t.Errorf("Send called %d times, want 1", sender.sendCalled)
	}
	if sender.lastMsg.From != "From <from@me.test>" {
		t.Errorf("From = %q", sender.lastMsg.From)
	}
	if len(sender.lastMsg.To) != 1 || sender.lastMsg.To[0] != "recipient@x.test" {
		t.Errorf("To = %v", sender.lastMsg.To)
	}
}

func TestSendRenderedEmailWorker_PropagatesSenderError(t *testing.T) {
	t.Parallel()
	sender := &fakeSender{sendErr: errors.New("smtp down")}
	w := &SendRenderedEmailWorker{Sender: sender, FromAddr: "f@m"}
	err := w.Work(context.Background(), &river.Job[SendRenderedEmailArgs]{
		JobRow: jobRow(),
		Args:   SendRenderedEmailArgs{To: "a@b"},
	})
	if err == nil {
		t.Fatal("expected error propagation")
	}
}

func TestSendEmailWorker_SkipsWithoutSender(t *testing.T) {
	t.Parallel()
	w := &SendEmailWorker{Sender: nil}
	if w.Timeout(nil) == 0 {
		t.Error("Timeout should be non-zero")
	}
	err := w.Work(context.Background(), &river.Job[SendEmailArgs]{
		JobRow: jobRow(),
		Args:   SendEmailArgs{Template: "welcome", To: "a@b"},
	})
	if err != nil {
		t.Errorf("nil sender should skip, got %v", err)
	}
}

func TestSendEmailWorker_UnknownTemplateIsPermanent(t *testing.T) {
	t.Parallel()
	tm, err := email.NewTemplateManager(nil, &email.StaticBrandProvider{
		Settings: &model.EmailBrandSettings{ProductName: "memax"},
	})
	if err != nil {
		t.Fatalf("NewTemplateManager: %v", err)
	}
	sender := &fakeSender{}
	w := &SendEmailWorker{Sender: sender, Templates: tm, FromAddr: "f@m"}
	err = w.Work(context.Background(), &river.Job[SendEmailArgs]{
		JobRow: jobRow(),
		Args: SendEmailArgs{
			Template: "nonexistent-template",
			To:       "a@b",
		},
	})
	// Template render errors are permanent (river.JobCancel wraps them).
	if err == nil {
		t.Fatal("unknown template should error")
	}
	if sender.sendCalled != 0 {
		t.Errorf("sender should not be called on render failure, got %d", sender.sendCalled)
	}
}

func TestSendEmailWorker_SendsKnownTemplate(t *testing.T) {
	t.Parallel()
	tm, err := email.NewTemplateManager(nil, &email.StaticBrandProvider{
		Settings: &model.EmailBrandSettings{ProductName: "memax"},
	})
	if err != nil {
		t.Fatalf("NewTemplateManager: %v", err)
	}
	// Grab a template + its sample variables via Get() so Render has
	// what it needs. Use the first bundled template.
	summaries, err := tm.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(summaries) == 0 {
		t.Skip("no bundled templates")
	}
	template := summaries[0].Name
	detail, err := tm.Get(context.Background(), template)
	if err != nil {
		t.Fatalf("Get(%q): %v", template, err)
	}

	sender := &fakeSender{}
	w := &SendEmailWorker{Sender: sender, Templates: tm, FromAddr: "f@m"}
	err = w.Work(context.Background(), &river.Job[SendEmailArgs]{
		JobRow: jobRow(),
		Args: SendEmailArgs{
			Template:  template,
			To:        "a@b",
			Variables: detail.SampleData,
		},
	})
	if err != nil {
		t.Fatalf("Work: %v", err)
	}
	if sender.sendCalled != 1 {
		t.Errorf("Send called %d times, want 1", sender.sendCalled)
	}
}
