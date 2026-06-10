package email

import (
	"context"
	"strings"
	"testing"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// ─── resolveLogoURL ─────────────────────────────────────────────

func TestResolveLogoURL(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		raw     string
		baseURL string
		want    string
	}{
		{"absolute https passes through", "https://example.com/logo.png", "https://app", "https://example.com/logo.png"},
		{"absolute http passes through", "http://example.com/logo.png", "https://app", "http://example.com/logo.png"},
		{"data URL passes through", "data:image/png;base64,iVBOR...", "https://app", "data:image/png;base64,iVBOR..."},
		{"empty returns empty", "", "https://app", ""},
		{"protocol-relative passes through (don't invent)", "//cdn.example.com/l.svg", "https://app", "//cdn.example.com/l.svg"},
		{"traversal segments pass through", "/images/../secret/l.svg", "https://app", "/images/../secret/l.svg"},
		{"./ prefix passes through", "./l.svg", "https://app", "./l.svg"},
		{"root-relative rewritten", "/images/memax.svg", "https://app.memax.app", "https://app.memax.app/images/memax.svg"},
		{"relative gets / prepended", "images/memax.svg", "https://app.memax.app", "https://app.memax.app/images/memax.svg"},
		{"no base URL leaves relative alone", "/images/memax.svg", "", "/images/memax.svg"},
		{"trailing slash stripped from base", "/l.svg", "https://app.memax.app/", "https://app.memax.app/" + "/l.svg"[1:]},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// SetAppBaseURL strips trailing slash, so mirror that here.
			base := strings.TrimRight(tc.baseURL, "/")
			got := resolveLogoURL(tc.raw, base)
			if got != tc.want {
				t.Errorf("resolveLogoURL(%q, %q) = %q, want %q",
					tc.raw, base, got, tc.want)
			}
		})
	}
}

// ─── SetAppBaseURL ──────────────────────────────────────────────

func TestSetAppBaseURLStripsTrailingSlash(t *testing.T) {
	t.Parallel()
	m := &TemplateManager{}
	m.SetAppBaseURL("https://app.memax.app/")
	if m.appBaseURL != "https://app.memax.app" {
		t.Errorf("appBaseURL = %q", m.appBaseURL)
	}
	m.SetAppBaseURL("")
	if m.appBaseURL != "" {
		t.Errorf("clear failed: %q", m.appBaseURL)
	}
}

// ─── NewTemplateManager + List + Get + Render ──────────────────
//
// These exercise the full real template bundle (embedded at build
// time). No store/brand overrides — both nil — so we hit the
// file-default paths only.

func TestNewTemplateManagerWithStaticBrand(t *testing.T) {
	t.Parallel()
	brand := &StaticBrandProvider{Settings: &model.EmailBrandSettings{
		ProductName:  "memax",
		PrimaryColor: "#111",
	}}
	mgr, err := NewTemplateManager(nil, brand)
	if err != nil {
		t.Fatalf("NewTemplateManager: %v", err)
	}
	if mgr == nil {
		t.Fatal("expected non-nil manager")
	}
	if len(mgr.ordered) == 0 {
		t.Error("no templates loaded")
	}
}

func TestListReturnsEveryBundledTemplate(t *testing.T) {
	t.Parallel()
	mgr, err := NewTemplateManager(nil, &StaticBrandProvider{})
	if err != nil {
		t.Fatalf("NewTemplateManager: %v", err)
	}
	summaries, err := mgr.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(summaries) != len(mgr.ordered) {
		t.Errorf("List returned %d, manager has %d",
			len(summaries), len(mgr.ordered))
	}
	// No override store → every template is default-only.
	for _, s := range summaries {
		if s.HasOverride {
			t.Errorf("%q should not show HasOverride with nil store", s.Name)
		}
	}
}

func TestGetForBundledTemplate(t *testing.T) {
	t.Parallel()
	mgr, err := NewTemplateManager(nil, &StaticBrandProvider{})
	if err != nil {
		t.Fatalf("NewTemplateManager: %v", err)
	}
	if len(mgr.ordered) == 0 {
		t.Skip("no templates bundled")
	}
	name := mgr.ordered[0]
	detail, err := mgr.Get(context.Background(), name)
	if err != nil {
		t.Fatalf("Get(%q): %v", name, err)
	}
	if detail.Name != name {
		t.Errorf("Name = %q", detail.Name)
	}
	// Without an override, PublishedAvailable is true — the default
	// template always counts as publishable scaffold.
	if !detail.PublishedAvailable {
		t.Errorf("PublishedAvailable should be true for default-only, got false")
	}
}

func TestGetUnknownTemplate(t *testing.T) {
	t.Parallel()
	mgr, err := NewTemplateManager(nil, &StaticBrandProvider{})
	if err != nil {
		t.Fatalf("NewTemplateManager: %v", err)
	}
	if _, err := mgr.Get(context.Background(), "not-a-template"); err == nil {
		t.Error("unknown template should error")
	}
}

func TestRenderBundledTemplateFillsSubjectAndBody(t *testing.T) {
	t.Parallel()
	mgr, err := NewTemplateManager(nil, &StaticBrandProvider{
		Settings: &model.EmailBrandSettings{ProductName: "memax"},
	})
	if err != nil {
		t.Fatalf("NewTemplateManager: %v", err)
	}
	if len(mgr.ordered) == 0 {
		t.Skip("no templates bundled")
	}
	name := mgr.ordered[0]
	def := mgr.definitions[name]

	out, err := mgr.Render(context.Background(), name, def.SampleData)
	if err != nil {
		t.Fatalf("Render(%q): %v", name, err)
	}
	if out.Subject == "" {
		t.Error("Subject empty after Render")
	}
	if out.HTML == "" {
		t.Error("HTML empty after Render")
	}
	if out.Text == "" {
		t.Error("Text empty after Render")
	}
}

func TestRenderAdHocWrapsInLayout(t *testing.T) {
	t.Parallel()
	mgr, err := NewTemplateManager(nil, &StaticBrandProvider{
		Settings: &model.EmailBrandSettings{ProductName: "memax"},
	})
	if err != nil {
		t.Fatalf("NewTemplateManager: %v", err)
	}
	out, err := mgr.RenderAdHoc(context.Background(),
		"Hello from memax",
		"<p>Hi there!</p>",
		"Hi there!")
	if err != nil {
		t.Fatalf("RenderAdHoc: %v", err)
	}
	if !strings.Contains(out.HTML, "Hi") {
		t.Errorf("HTML missing inline body: %q", out.HTML[:min(200, len(out.HTML))])
	}
	if out.Subject != "Hello from memax" {
		t.Errorf("Subject = %q", out.Subject)
	}
}

// ─── StaticBrandProvider fallback ───────────────────────────────

func TestStaticBrandProviderNilFallsBackToDefault(t *testing.T) {
	t.Parallel()
	var p *StaticBrandProvider
	got, err := p.GetEmailBrandSettings(context.Background())
	if err != nil {
		t.Fatalf("GetEmailBrandSettings: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil default settings")
	}
	if got.ProductName == "" {
		t.Error("default brand should have a product name")
	}
}

func TestStaticBrandProviderReturnsDefensiveCopy(t *testing.T) {
	t.Parallel()
	p := &StaticBrandProvider{Settings: &model.EmailBrandSettings{
		ProductName: "original",
	}}
	got, _ := p.GetEmailBrandSettings(context.Background())
	got.ProductName = "mutated"
	// The provider's internal state must NOT see our mutation — it
	// handed us a copy, not a pointer-alias.
	got2, _ := p.GetEmailBrandSettings(context.Background())
	if got2.ProductName != "original" {
		t.Errorf("provider returned aliased pointer: got %q after external mutation",
			got2.ProductName)
	}
}

// ─── Structured footer rendering ──────────────────────────────────
//
// These lock down the footer composition contract so a future accidental
// change to base.html (or layoutBrand plumbing) trips a test instead of
// silently shipping emails with missing CAN-SPAM identity or an
// unexpected unsubscribe row on transactional mail.

func brandWithStructuredFooter() *model.EmailBrandSettings {
	return &model.EmailBrandSettings{
		ProductName:    "memax",
		SupportEmail:   "hello@memax.app",
		CompanyName:    "Memax Labs, Inc.",
		CompanyAddress: "548 Market St PMB 12345, San Francisco, CA 94104",
		PrivacyURL:     "https://memax.app/privacy",
		TermsURL:       "https://memax.app/terms",
	}
}

func TestTransactionalRenderIncludesStructuredFooterExceptUnsubscribe(t *testing.T) {
	t.Parallel()
	brand := &StaticBrandProvider{Settings: brandWithStructuredFooter()}
	mgr, err := NewTemplateManager(nil, brand)
	if err != nil {
		t.Fatalf("NewTemplateManager: %v", err)
	}
	out, err := mgr.Render(context.Background(), "hub_invite",
		mgr.definitions["hub_invite"].SampleData)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	// Identity + legal + support must surface on transactional sends.
	for _, want := range []string{
		"mailto:hello@memax.app",
		"Memax Labs, Inc.",
		"548 Market St PMB 12345",
		"https://memax.app/privacy",
		"https://memax.app/terms",
	} {
		if !strings.Contains(out.HTML, want) {
			t.Errorf("transactional HTML missing %q", want)
		}
	}
	// Plain-text footer uses bare email + full URLs (no mailto/HTML wrap).
	for _, want := range []string{
		"hello@memax.app",
		"Memax Labs, Inc.",
		"548 Market St PMB 12345",
		"https://memax.app/privacy",
		"https://memax.app/terms",
	} {
		if !strings.Contains(out.Text, want) {
			t.Errorf("transactional text missing %q", want)
		}
	}

	// Unsubscribe MUST NOT appear on transactional sends.
	if strings.Contains(out.HTML, UnsubscribePlaceholder) {
		t.Error("transactional HTML leaked unsubscribe placeholder")
	}
	if strings.Contains(strings.ToLower(out.HTML), "unsubscribe") {
		t.Error("transactional HTML contains 'unsubscribe' — should be marketing-only")
	}
	if strings.Contains(strings.ToLower(out.Text), "unsubscribe") {
		t.Error("transactional text contains 'unsubscribe' — should be marketing-only")
	}
}

func TestMarketingRenderIncludesUnsubscribeWithPlaceholder(t *testing.T) {
	t.Parallel()
	brand := &StaticBrandProvider{Settings: brandWithStructuredFooter()}
	mgr, err := NewTemplateManager(nil, brand)
	if err != nil {
		t.Fatalf("NewTemplateManager: %v", err)
	}
	out, err := mgr.WrapAdHocWithUnsubscribe(context.Background(),
		"Product news", "<p>Hello from memax.</p>", "Hello from memax.")
	if err != nil {
		t.Fatalf("WrapAdHocWithUnsubscribe: %v", err)
	}
	if !strings.Contains(out.HTML, UnsubscribePlaceholder) {
		t.Error("marketing HTML missing unsubscribe placeholder — campaign worker cannot substitute")
	}
	if !strings.Contains(out.Text, UnsubscribePlaceholder) {
		t.Error("marketing text missing unsubscribe placeholder")
	}
	// Compliance identity should also render on marketing.
	if !strings.Contains(out.HTML, "Memax Labs, Inc.") {
		t.Error("marketing HTML missing company identity")
	}
}

func TestReasonLineRendersWithTemplateVariables(t *testing.T) {
	t.Parallel()
	brand := &StaticBrandProvider{Settings: brandWithStructuredFooter()}
	mgr, err := NewTemplateManager(nil, brand)
	if err != nil {
		t.Fatalf("NewTemplateManager: %v", err)
	}
	out, err := mgr.Render(context.Background(), "hub_invite",
		map[string]string{
			"InviterName": "Avery",
			"HubName":     "Design ops",
			"Role":        "contributor",
			"InviteURL":   "https://memax.app/invites/abc",
			"ExpiresAt":   "May 1, 2026",
		})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	// Reason line should interpolate {{.InviterName}} and {{.HubName}}
	// against the same data map as the body.
	if !strings.Contains(out.HTML, "Avery invited you to join") {
		t.Error("reason HTML did not interpolate InviterName")
	}
	if !strings.Contains(out.HTML, "<strong>Design ops</strong>") {
		t.Error("reason HTML did not interpolate HubName as strong text")
	}
	if !strings.Contains(out.Text, "Avery invited you to join Design ops on memax.") {
		t.Error("reason text did not interpolate against body data map")
	}
}

func TestStructuredFooterOmitsSectionsWhenEmpty(t *testing.T) {
	t.Parallel()
	// Only SupportEmail set — company identity and legal links should
	// NOT render (rendering empty placeholders would be noisy/ugly).
	brand := &StaticBrandProvider{Settings: &model.EmailBrandSettings{
		ProductName:  "memax",
		SupportEmail: "hello@memax.app",
	}}
	mgr, err := NewTemplateManager(nil, brand)
	if err != nil {
		t.Fatalf("NewTemplateManager: %v", err)
	}
	out, err := mgr.Render(context.Background(), "waitlist_confirmation",
		map[string]string{})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(out.HTML, "hello@memax.app") {
		t.Error("expected support email to render when set")
	}
	// No Privacy / Terms → those anchor tags must be absent.
	if strings.Contains(out.HTML, ">Privacy<") {
		t.Error("Privacy link rendered despite empty PrivacyURL")
	}
	if strings.Contains(out.HTML, ">Terms<") {
		t.Error("Terms link rendered despite empty TermsURL")
	}
}

func TestPreviewIsParityWithSendForStructuredFields(t *testing.T) {
	t.Parallel()
	// The admin preview path (Get) and the real-send path (Render) must
	// produce identical HTML/text for the same brand + data.
	// Divergence here is the exact class of bug that shipped Support +
	// Unsubscribe URL template as "visible in preview" when they weren't
	// actually rendered at send time.
	brand := &StaticBrandProvider{Settings: brandWithStructuredFooter()}
	mgr, err := NewTemplateManager(nil, brand)
	if err != nil {
		t.Fatalf("NewTemplateManager: %v", err)
	}
	const name = "hub_invite"
	def := mgr.definitions[name]

	detail, err := mgr.Get(context.Background(), name)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	sendRender, err := mgr.Render(context.Background(), name, def.SampleData)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if detail.Preview.HTML != sendRender.HTML {
		t.Error("preview HTML differs from send HTML — admin would see a render the recipients never get")
	}
	if detail.Preview.Text != sendRender.Text {
		t.Error("preview text differs from send text — admin would see a render the recipients never get")
	}
}
