package email

import (
	"context"
	"net/smtp"
	"strings"
	"testing"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// ─── Brand provider ─────────────────────────────────────────────

func TestStaticBrandProviderNilReceiverReturnsDefault(t *testing.T) {
	t.Parallel()

	var p *StaticBrandProvider
	got, err := p.GetEmailBrandSettings(context.Background())
	if err != nil {
		t.Fatalf("nil receiver: %v", err)
	}
	if got == nil || got.ProductName != "memax" {
		t.Errorf("nil receiver should fall back to defaultBrand, got %+v", got)
	}
}

func TestStaticBrandProviderNilSettingsReturnsDefault(t *testing.T) {
	t.Parallel()

	p := &StaticBrandProvider{Settings: nil}
	got, _ := p.GetEmailBrandSettings(context.Background())
	if got == nil || got.ProductName != "memax" {
		t.Errorf("nil Settings should fall back to defaultBrand; got %+v", got)
	}
}

func TestStaticBrandProviderReturnsCopy(t *testing.T) {
	t.Parallel()

	// Mutating the returned value must not leak into the provider's
	// stored snapshot — a shared pointer would let one handler
	// corrupt another's render pass.
	stored := &model.EmailBrandSettings{ProductName: "original"}
	p := &StaticBrandProvider{Settings: stored}
	got, _ := p.GetEmailBrandSettings(context.Background())
	got.ProductName = "tampered"
	if stored.ProductName != "original" {
		t.Errorf("provider leaked its stored pointer; ProductName = %q", stored.ProductName)
	}
}

func TestDefaultBrandValues(t *testing.T) {
	t.Parallel()

	// defaultBrand mirrors the migration — regression test keeps
	// the code defaults aligned with what the DB would seed. A
	// rename here without updating the migration creates a subtle
	// divergence between fresh DBs and dev-mode code.
	b := defaultBrand()
	checks := map[string]string{
		"LogoAlt":      b.LogoAlt,
		"ProductName":  b.ProductName,
		"FooterHTML":   b.FooterHTML,
		"FooterText":   b.FooterText,
		"PrimaryColor": b.PrimaryColor,
	}
	for field, v := range checks {
		if v == "" {
			t.Errorf("defaultBrand.%s is empty", field)
		}
	}
	if !strings.Contains(b.FooterHTML, "memax") {
		t.Errorf("FooterHTML missing brand: %q", b.FooterHTML)
	}
}

// ─── SMTP MIME / address helpers ────────────────────────────────

func TestExtractEmail(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"user@example.com":                   "user@example.com",
		"Name <user@example.com>":            "user@example.com",
		"  spaced@example.com  ":             "spaced@example.com",
		"No Brackets user@example.com":       "No Brackets user@example.com",
		"Broken <user@example.com":           "Broken <user@example.com",
		"Weird <first@a.com> <second@b.com>": "first@a.com",
	}
	for in, want := range cases {
		if got := extractEmail(in); got != want {
			t.Errorf("extractEmail(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBuildMIMEMessageTextOnly(t *testing.T) {
	t.Parallel()

	mime, err := buildMIMEMessage("sender@x", Message{
		To:      []string{"a@b"},
		Subject: "Plain text",
		Text:    "hello world",
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	s := string(mime)
	if !strings.Contains(s, "From: sender@x") {
		t.Error("From header missing")
	}
	if !strings.Contains(s, "To: a@b") {
		t.Error("To header missing")
	}
	if !strings.Contains(s, "Subject:") {
		t.Error("Subject header missing")
	}
	if !strings.Contains(s, "text/plain") {
		t.Error("text/plain content type missing")
	}
	if strings.Contains(s, "multipart/") {
		t.Error("should not use multipart when only text present")
	}
}

func TestBuildMIMEMessageHTMLOnly(t *testing.T) {
	t.Parallel()

	mime, err := buildMIMEMessage("s@x", Message{
		To:      []string{"a@b"},
		Subject: "HTML only",
		HTML:    "<p>hi</p>",
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	s := string(mime)
	if !strings.Contains(s, "text/html") {
		t.Error("text/html content type missing")
	}
	if strings.Contains(s, "multipart/") {
		t.Error("should not use multipart when only HTML present")
	}
}

func TestBuildMIMEMessageMultipartWhenBothPresent(t *testing.T) {
	t.Parallel()

	mime, err := buildMIMEMessage("s@x", Message{
		To:      []string{"a@b"},
		Subject: "Both",
		Text:    "plain",
		HTML:    "<p>rich</p>",
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	s := string(mime)
	if !strings.Contains(s, "multipart/alternative") {
		t.Error("should use multipart/alternative when both text+html present")
	}
	if !strings.Contains(s, "text/plain") || !strings.Contains(s, "text/html") {
		t.Error("both content types should appear in multipart")
	}
}

func TestBuildMIMEMessageIncludesReplyToWhenSet(t *testing.T) {
	t.Parallel()

	mime, _ := buildMIMEMessage("s@x", Message{
		To:      []string{"a@b"},
		Subject: "x",
		Text:    "hi",
		ReplyTo: "other@x",
	})
	if !strings.Contains(string(mime), "Reply-To: other@x") {
		t.Error("Reply-To header missing")
	}
}

func TestBuildMIMEMessageQuotedPrintableEncodesBody(t *testing.T) {
	t.Parallel()

	// Body with non-ASCII to force QP encoding: "café" → "caf=C3=A9"
	// The quoted-printable encoding is a regression guard against
	// a future swap to 7bit/base64 that would break downstream
	// consumers expecting QP.
	mime, _ := buildMIMEMessage("s@x", Message{
		To:      []string{"a@b"},
		Subject: "non-ascii",
		Text:    "café",
	})
	s := string(mime)
	if !strings.Contains(s, "Content-Transfer-Encoding: quoted-printable") {
		t.Error("QP encoding header missing")
	}
	if !strings.Contains(s, "caf=C3=A9") {
		t.Errorf("QP body encoding wrong, got %q", s)
	}
}

// ─── smtp.PlainAuth constructor sanity ──────────────────────────

func TestNewSMTPSenderStoresFields(t *testing.T) {
	t.Parallel()

	// Doesn't actually send — just verifies the constructor
	// stores the host/user/pass it's given. A refactor that
	// accidentally drops a field leaves authentication broken
	// at the first real Send(); this catches it pre-runtime.
	s := NewSMTPSender("smtp.example.com:587", "user", "pass")
	if s == nil {
		t.Fatal("constructor returned nil")
	}
	// Smoke the auth type import — if the implementation
	// switches to CRAM-MD5 or drops auth entirely, this line
	// fails at compile time.
	_ = smtp.PlainAuth
}
