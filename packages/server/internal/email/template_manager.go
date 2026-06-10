package email

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"maps"
	"sort"
	"strings"
	texttemplate "text/template"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

type OverrideStore interface {
	ListEmailTemplateOverrides(ctx context.Context) ([]model.EmailTemplateOverride, error)
	GetEmailTemplateOverride(ctx context.Context, name string) (*model.EmailTemplateOverride, error)
	UpsertEmailTemplateOverride(ctx context.Context, override *model.EmailTemplateOverride) error
	PublishEmailTemplateOverride(ctx context.Context, name string, publishedBy *string) (*model.EmailTemplateOverride, error)
	DeleteEmailTemplateOverride(ctx context.Context, name string, deletedBy *string) error
	ListEmailTemplateRevisions(ctx context.Context, name string, limit int) ([]model.EmailTemplateRevision, error)
}

type templateDefinition struct {
	Name        string
	Category    string
	Description string
	Variables   []model.EmailTemplateVariable
	SampleData  map[string]string
	Subject     string
	HTML        string
	Text        string
	// ReasonHTML / ReasonText render a "You're receiving this because…"
	// line in the structured footer, per CAN-SPAM transparency norm. Go
	// template syntax is evaluated against the same data map as the body.
	// Empty means the reason line is omitted (ad-hoc campaign bodies are
	// expected to carry their own context in the body copy).
	ReasonHTML string
	ReasonText string
}

type draftTemplate struct {
	Subject    string
	HTML       string
	Text       string
	EditorKind string
}

// TemplateManager serves admin reads/writes and runtime rendering for
// outbound email. Body-only templates are wrapped in a shared base
// layout at render time, merging the current brand settings (logo,
// footer, colors) into the output.
type TemplateManager struct {
	store       OverrideStore
	brand       BrandProvider
	definitions map[string]templateDefinition
	ordered     []string
	baseHTML    *template.Template
	baseText    *texttemplate.Template
	// appBaseURL is the public origin used to resolve relative logo
	// URLs (e.g. "/images/memax-icon.svg") into absolute URLs that
	// render in email clients. Optional — when empty, relative URLs
	// are left untouched (they'll break in most clients but the
	// admin already saw the preview and chose to keep them). Set via
	// SetAppBaseURL at server startup from APP_BASE_URL env var.
	appBaseURL string
	// docsBaseURL is the public origin of the developer hub (defaults
	// to production). Auto-injected into every template's data map as
	// `{{ .DocsURL }}` so any template can link to docs without the
	// caller threading it through. Set via SetDocsBaseURL at startup
	// from DOCS_BASE_URL env var. Falls back to the hardcoded prod
	// URL (see defaultDocsBaseURL) when unconfigured — never empty,
	// so templates using `{{ .DocsURL }}` always resolve.
	docsBaseURL string
}

// defaultDocsBaseURL is the prod fallback used when DOCS_BASE_URL is
// unset at startup. Hardcoded here (not an env var with no default) so
// that a forgotten env in staging/prod still produces valid email
// links instead of rendering `https://` or an empty string.
const defaultDocsBaseURL = "https://docs.memax.app"

// defaultAppBaseURL mirrors defaultDocsBaseURL for the web app origin
// — same reasoning applies. Used by auto-injected `{{ .AppURL }}`.
const defaultAppBaseURL = "https://memax.app"

// SetAppBaseURL configures the origin that `resolveLogoURL` prepends
// to relative logo paths. Also drives the `{{ .AppURL }}` auto-injected
// template variable. Safe to call with an empty string or whitespace —
// the variable falls back to `defaultAppBaseURL` so templates never
// see empty. TrimSpace BEFORE TrimRight because TrimRight("  ", "/")
// keeps the spaces, which would bypass the empty-check fallback and
// ship a literal "   " URL into email links.
func (m *TemplateManager) SetAppBaseURL(baseURL string) {
	m.appBaseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
}

// SetDocsBaseURL configures the origin auto-injected into render data
// as `{{ .DocsURL }}`. Same whitespace-handling rationale as
// SetAppBaseURL — trim space first, then trailing slashes, so that
// DOCS_BASE_URL=" " still falls back to `defaultDocsBaseURL`.
func (m *TemplateManager) SetDocsBaseURL(baseURL string) {
	m.docsBaseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
}

// injectCommonVars returns a copy of the caller's data map with the
// auto-injected URL variables (`AppURL`, `DocsURL`) layered in
// underneath. Caller-provided keys win, so a template explicitly
// passing `DocsURL` can still override the default. Returns a new map
// so the caller's input is never mutated.
func (m *TemplateManager) injectCommonVars(data map[string]string) map[string]string {
	out := make(map[string]string, len(data)+2)
	out["AppURL"] = m.effectiveAppBaseURL()
	out["DocsURL"] = m.effectiveDocsBaseURL()
	maps.Copy(out, data)
	return out
}

func (m *TemplateManager) effectiveAppBaseURL() string {
	if m.appBaseURL != "" {
		return m.appBaseURL
	}
	return defaultAppBaseURL
}

func (m *TemplateManager) effectiveDocsBaseURL() string {
	if m.docsBaseURL != "" {
		return m.docsBaseURL
	}
	return defaultDocsBaseURL
}

// resolveLogoURL turns a relative path like "/images/memax-icon.svg"
// into an absolute URL using the configured app origin. Absolute URLs
// (http://, https://, data:) pass through unchanged. Empty URLs also
// pass through — the base layout falls back to the product-name text
// rendering when LogoURL is empty.
//
// Only clean root-relative paths are rewritten. We intentionally
// reject:
//   - protocol-relative URLs (`//example.com/foo.svg`), which the
//     admin probably didn't mean and which would produce
//     `APP_BASE_URL//example.com/foo.svg`
//   - path-traversal segments (`../`, `./`), which would produce
//     `APP_BASE_URL/../foo.svg` and either redirect off-origin or
//     generate confusing 404s
//
// When the input is ambiguous, we pass it through unchanged — same
// stance as when `appBaseURL` is empty — rather than invent a
// rewrite that could point somewhere unsafe.
func resolveLogoURL(raw, appBaseURL string) string {
	if raw == "" {
		return ""
	}
	lower := strings.ToLower(raw)
	if strings.HasPrefix(lower, "http://") ||
		strings.HasPrefix(lower, "https://") ||
		strings.HasPrefix(lower, "data:") {
		return raw
	}
	// Protocol-relative URLs collide with root-relative and should
	// never be rewritten against APP_BASE_URL. Either the admin meant
	// a full URL (use https://...) or typed it by mistake; either way,
	// don't invent a rewrite.
	if strings.HasPrefix(raw, "//") {
		return raw
	}
	// Reject traversal segments anywhere in the path. Preset paths
	// from the admin UI are always clean `/images/<name>.svg`; a `../`
	// means something weird happened and we shouldn't help.
	if strings.Contains(raw, "/..") ||
		strings.Contains(raw, "../") ||
		strings.HasPrefix(raw, "./") ||
		strings.Contains(raw, "/./") {
		return raw
	}
	if appBaseURL == "" {
		// Leave it alone — some clients will still resolve relative
		// paths against their own domain and render nothing, but
		// rewriting to a broken base is no better.
		return raw
	}
	if !strings.HasPrefix(raw, "/") {
		raw = "/" + raw
	}
	return appBaseURL + raw
}

func NewTemplateManager(store OverrideStore, brand BrandProvider) (*TemplateManager, error) {
	defs, err := loadTemplateDefinitions()
	if err != nil {
		return nil, err
	}
	ordered := make([]string, 0, len(defs))
	for name := range defs {
		ordered = append(ordered, name)
	}
	sort.Strings(ordered)

	baseHTMLSrc, err := templateFS.ReadFile("templates/base.html")
	if err != nil {
		return nil, fmt.Errorf("load base html layout: %w", err)
	}
	baseHTML, err := template.New("base.html").Option("missingkey=error").Parse(string(baseHTMLSrc))
	if err != nil {
		return nil, fmt.Errorf("parse base html layout: %w", err)
	}

	baseTextSrc, err := templateFS.ReadFile("templates/base.txt")
	if err != nil {
		return nil, fmt.Errorf("load base text layout: %w", err)
	}
	baseText, err := texttemplate.New("base.txt").Option("missingkey=error").Parse(string(baseTextSrc))
	if err != nil {
		return nil, fmt.Errorf("parse base text layout: %w", err)
	}

	return &TemplateManager{
		store:       store,
		brand:       brand,
		definitions: defs,
		ordered:     ordered,
		baseHTML:    baseHTML,
		baseText:    baseText,
	}, nil
}

func loadTemplateDefinitions() (map[string]templateDefinition, error) {
	meta := map[string]templateDefinition{
		"hub_invite": {
			Name:        "hub_invite",
			Category:    "hubs",
			Description: "Initial hub invite email sent to a team member.",
			Variables: []model.EmailTemplateVariable{
				{Key: "InviterName", Description: "Display name of the inviter.", Example: "Avery"},
				{Key: "HubName", Description: "Name of the hub being joined.", Example: "Design ops"},
				{Key: "Role", Description: "Granted role inside the hub.", Example: "contributor"},
				{Key: "InviteURL", Description: "Join link for the invite.", Example: "https://memax.app/invites/abc123"},
				{Key: "ExpiresAt", Description: "Human-readable expiry date.", Example: "May 1, 2026"},
			},
			SampleData: map[string]string{
				"InviterName": "Avery",
				"HubName":     "Design ops",
				"Role":        "contributor",
				"InviteURL":   "https://memax.app/invites/invite_abc123",
				"ExpiresAt":   "May 1, 2026",
			},
			ReasonHTML: `You're receiving this because {{.InviterName}} invited you to join <strong>{{.HubName}}</strong> on memax.`,
			ReasonText: `You're receiving this because {{.InviterName}} invited you to join {{.HubName}} on memax.`,
		},
		"hub_invite_reminder": {
			Name:        "hub_invite_reminder",
			Category:    "hubs",
			Description: "Reminder email for an unaccepted hub invite that is close to expiring.",
			Variables: []model.EmailTemplateVariable{
				{Key: "InviterName", Description: "Display name of the inviter.", Example: "Avery"},
				{Key: "HubName", Description: "Name of the hub being joined.", Example: "Design ops"},
				{Key: "Role", Description: "Granted role inside the hub.", Example: "viewer"},
				{Key: "InviteURL", Description: "Join link for the invite.", Example: "https://memax.app/invites/invite_abc123"},
				{Key: "ExpiresAt", Description: "Human-readable expiry date.", Example: "May 1, 2026"},
			},
			SampleData: map[string]string{
				"InviterName": "Avery",
				"HubName":     "Design ops",
				"Role":        "viewer",
				"InviteURL":   "https://memax.app/invites/invite_abc123",
				"ExpiresAt":   "May 1, 2026",
			},
			ReasonHTML: `You're receiving this reminder because your invite to <strong>{{.HubName}}</strong> hasn't been accepted yet.`,
			ReasonText: `You're receiving this reminder because your invite to {{.HubName}} hasn't been accepted yet.`,
		},
		"waitlist_confirmation": {
			Name:        "waitlist_confirmation",
			Category:    "waitlist",
			Description: "Confirmation email after someone joins the waitlist.",
			// DocsURL is auto-injected by injectCommonVars; listing it
			// here documents it for the admin editor and powers the
			// preview sample data.
			Variables: []model.EmailTemplateVariable{
				{Key: "DocsURL", Description: "Developer docs origin.", Example: defaultDocsBaseURL},
			},
			SampleData: map[string]string{
				"DocsURL": defaultDocsBaseURL,
			},
			ReasonHTML: `You're receiving this because you joined the memax waitlist.`,
			ReasonText: `You're receiving this because you joined the memax waitlist.`,
		},
		"waitlist_invite": {
			Name:        "waitlist_invite",
			Category:    "waitlist",
			Description: "Invite email sent when a waitlist user is approved.",
			Variables: []model.EmailTemplateVariable{
				{Key: "RegisterURL", Description: "Registration link for the invite.", Example: "https://memax.app/register?invite=wl_123"},
				{Key: "ExpiresAt", Description: "Human-readable expiry date.", Example: "May 1, 2026"},
			},
			SampleData: map[string]string{
				"RegisterURL": "https://memax.app/register?invite=wl_123",
				"ExpiresAt":   "May 1, 2026",
			},
			ReasonHTML: `You're receiving this because you signed up for early access to memax.`,
			ReasonText: `You're receiving this because you signed up for early access to memax.`,
		},
		"waitlist_invite_reminder": {
			Name:        "waitlist_invite_reminder",
			Category:    "waitlist",
			Description: "Reminder email for an approved waitlist invite that is close to expiring.",
			Variables: []model.EmailTemplateVariable{
				{Key: "RegisterURL", Description: "Registration link for the invite.", Example: "https://memax.app/register?invite=wl_123"},
				{Key: "ExpiresAt", Description: "Human-readable expiry date.", Example: "May 1, 2026"},
			},
			SampleData: map[string]string{
				"RegisterURL": "https://memax.app/register?invite=wl_123",
				"ExpiresAt":   "May 1, 2026",
			},
			ReasonHTML: `You're receiving this reminder because your early-access invite hasn't been claimed yet.`,
			ReasonText: `You're receiving this reminder because your early-access invite hasn't been claimed yet.`,
		},
		"email_otp": {
			Name:        "email_otp",
			Category:    "auth",
			Description: "Sign-in code sent when a user requests email OTP login.",
			Variables: []model.EmailTemplateVariable{
				{Key: "Code", Description: "The 6-digit sign-in code.", Example: "428193"},
				{Key: "ExpiresInMinutes", Description: "How long the code is valid, in minutes.", Example: "10"},
				{Key: "RequestContext", Description: "Where the request came from (IP or location hint).", Example: "an unknown device"},
			},
			SampleData: map[string]string{
				"Code":             "428193",
				"ExpiresInMinutes": "10",
				"RequestContext":   "an unknown device",
			},
			ReasonHTML: `You're receiving this because someone requested a sign-in code for this email on memax.`,
			ReasonText: `You're receiving this because someone requested a sign-in code for this email on memax.`,
		},
	}

	for name, def := range meta {
		htmlBytes, err := templateFS.ReadFile("templates/" + name + ".html")
		if err != nil {
			return nil, fmt.Errorf("load %s html: %w", name, err)
		}
		textBytes, err := templateFS.ReadFile("templates/" + name + ".txt")
		if err != nil {
			return nil, fmt.Errorf("load %s text: %w", name, err)
		}
		subject, textBody := splitSubject(string(textBytes))
		def.HTML = string(htmlBytes)
		def.Subject = subject
		def.Text = textBody
		meta[name] = def
	}

	return meta, nil
}

func splitSubject(text string) (subject string, body string) {
	lines := strings.SplitN(text, "\n", 2)
	if len(lines) >= 1 && strings.HasPrefix(lines[0], "Subject: ") {
		subject = strings.TrimPrefix(lines[0], "Subject: ")
		if len(lines) >= 2 {
			body = strings.TrimLeft(lines[1], "\n")
		}
		return subject, body
	}
	return "", text
}

func (m *TemplateManager) List(ctx context.Context) ([]model.AdminEmailTemplateSummary, error) {
	overridesByName := map[string]model.EmailTemplateOverride{}
	if m.store != nil {
		overrides, err := m.store.ListEmailTemplateOverrides(ctx)
		if err != nil {
			return nil, err
		}
		for _, override := range overrides {
			overridesByName[override.Name] = override
		}
	}

	summaries := make([]model.AdminEmailTemplateSummary, 0, len(m.ordered))
	for _, name := range m.ordered {
		def := m.definitions[name]
		summary := model.AdminEmailTemplateSummary{
			Name:          def.Name,
			Category:      def.Category,
			Description:   def.Description,
			Subject:       def.Subject,
			VariableCount: len(def.Variables),
		}
		if override, ok := overridesByName[name]; ok {
			summary.HasOverride = true
			summary.HasUnpublishedDraft = override.HasUnpublishedDraft()
			// List shows the PUBLISHED subject — it's what currently
			// goes out on real sends, not the in-progress draft. The
			// badge above tells the admin when there's unpublished work.
			summary.Subject = pickString(override.Subject, def.Subject)
			summary.UpdatedAt = &override.UpdatedAt
			summary.UpdatedBy = override.UpdatedBy
		}
		summaries = append(summaries, summary)
	}

	return summaries, nil
}

func (m *TemplateManager) Get(ctx context.Context, name string) (*model.AdminEmailTemplateDetail, error) {
	def, ok := m.definitions[name]
	if !ok {
		return nil, fmt.Errorf("template not found")
	}
	override, err := m.getOverride(ctx, name)
	if err != nil {
		return nil, err
	}
	effective := m.mergeDraft(def, override)
	published := m.mergePublished(def, override)
	brand, err := m.resolveBrand(ctx)
	if err != nil {
		return nil, err
	}
	preview, err := m.renderWrappedWithReason(effective, def.ReasonHTML, def.ReasonText, m.injectCommonVars(def.SampleData), brand)
	if err != nil {
		return nil, err
	}
	revisions := []model.EmailTemplateRevision{}
	if m.store != nil {
		revisions, err = m.store.ListEmailTemplateRevisions(ctx, name, 12)
		if err != nil {
			return nil, err
		}
	}
	// PublishedAvailable is true when either a real publish has
	// happened (override.PublishedAt != nil) OR the file default is
	// always usable as a scaffold (the "Default" state). That lets the
	// scaffold picker show published or default content without ever
	// accidentally copying an unpublished draft.
	publishedAvailable := override == nil || override.PublishedAt != nil

	return &model.AdminEmailTemplateDetail{
		Name:                def.Name,
		Category:            def.Category,
		Description:         def.Description,
		Variables:           def.Variables,
		SampleData:          cloneMap(def.SampleData),
		DefaultSubject:      def.Subject,
		DefaultHTML:         def.HTML,
		DefaultText:         def.Text,
		EffectiveSubject:    effective.Subject,
		EffectiveHTML:       effective.HTML,
		EffectiveText:       effective.Text,
		EffectiveEditorKind: effective.EditorKind,
		PublishedSubject:    published.Subject,
		PublishedHTML:       published.HTML,
		PublishedText:       published.Text,
		PublishedAvailable:  publishedAvailable,
		Override:            override,
		Revisions:           revisions,
		Preview: model.AdminEmailTemplatePreview{
			Subject: preview.Subject,
			HTML:    preview.HTML,
			Text:    preview.Text,
			Data:    cloneMap(def.SampleData),
		},
	}, nil
}

func (m *TemplateManager) Preview(ctx context.Context, name string, subject, html, text string, sampleData map[string]string) (model.AdminEmailTemplatePreview, error) {
	def, ok := m.definitions[name]
	if !ok {
		return model.AdminEmailTemplatePreview{}, fmt.Errorf("template not found")
	}
	override, err := m.getOverride(ctx, name)
	if err != nil {
		return model.AdminEmailTemplatePreview{}, err
	}
	effective := m.mergeDraft(def, override)
	if subject != "" {
		effective.Subject = subject
	}
	if html != "" {
		effective.HTML = html
	}
	if text != "" {
		effective.Text = text
	}
	data := cloneMap(def.SampleData)
	for key, value := range sampleData {
		if strings.TrimSpace(key) == "" {
			continue
		}
		data[key] = value
	}
	brand, err := m.resolveBrand(ctx)
	if err != nil {
		return model.AdminEmailTemplatePreview{}, err
	}
	rendered, err := m.renderWrappedWithReason(effective, def.ReasonHTML, def.ReasonText, m.injectCommonVars(data), brand)
	if err != nil {
		return model.AdminEmailTemplatePreview{}, err
	}
	return model.AdminEmailTemplatePreview{
		Subject: rendered.Subject,
		HTML:    rendered.HTML,
		Text:    rendered.Text,
		Data:    data,
	}, nil
}

// ErrOverrideLooksLikeFullDocument is returned when an admin tries to
// save a template override containing full-document chrome (<!DOCTYPE>,
// <html>, <head>, or <body>). Templates are now body-only fragments and
// get wrapped by the shared base layout at render time — a full document
// would produce nested <html> and broken email markup.
var ErrOverrideLooksLikeFullDocument = fmt.Errorf("template override must be a body-only fragment — remove <!DOCTYPE>, <html>, <head>, and <body>")

// UpsertOverride writes the admin's edits as the template's DRAFT.
// Published columns are untouched — the admin must call Publish to
// promote draft → live. The store's INSERT path mirrors draft →
// published for first saves only; subsequent saves are draft-only.
func (m *TemplateManager) UpsertOverride(ctx context.Context, name, subject, html, text, notes, editorKind string, editorState map[string]any, updatedBy *string) error {
	if _, ok := m.definitions[name]; !ok {
		return fmt.Errorf("template not found")
	}
	if m.store == nil {
		return fmt.Errorf("template overrides unavailable")
	}
	if looksLikeFullDocument(html) {
		return ErrOverrideLooksLikeFullDocument
	}
	override := &model.EmailTemplateOverride{
		Name:         name,
		DraftSubject: subject,
		DraftHTML:    html,
		DraftText:    text,
		Notes:        notes,
		EditorKind:   pickString(editorKind, "html"),
		EditorState:  editorState,
		UpdatedBy:    updatedBy,
	}
	return m.store.UpsertEmailTemplateOverride(ctx, override)
}

// PublishOverride promotes the current draft to published. Returns
// ErrOverrideNotFound if no override row exists (admin never saved a
// draft) — callers should render this as 404 "save a draft first".
func (m *TemplateManager) PublishOverride(ctx context.Context, name string, publishedBy *string) (*model.EmailTemplateOverride, error) {
	if _, ok := m.definitions[name]; !ok {
		return nil, fmt.Errorf("template not found")
	}
	if m.store == nil {
		return nil, fmt.Errorf("template overrides unavailable")
	}
	published, err := m.store.PublishEmailTemplateOverride(ctx, name, publishedBy)
	if err != nil {
		return nil, err
	}
	return published, nil
}

func (m *TemplateManager) DeleteOverride(ctx context.Context, name string, deletedBy *string) error {
	if _, ok := m.definitions[name]; !ok {
		return fmt.Errorf("template not found")
	}
	if m.store == nil {
		return fmt.Errorf("template overrides unavailable")
	}
	return m.store.DeleteEmailTemplateOverride(ctx, name, deletedBy)
}

// RenderAdHoc wraps admin-authored subject/html/text in the current
// brand layout, without consulting the named templates map. Used by
// campaign email sends where the admin writes the body in the
// composer rather than referencing a saved template.
//
// Body HTML is rejected if it looks like a full document — same
// guard as UpsertOverride — to prevent double-chrome renders.
func (m *TemplateManager) RenderAdHoc(ctx context.Context, subject, html, text string) (RenderResult, error) {
	if looksLikeFullDocument(html) {
		return RenderResult{}, ErrOverrideLooksLikeFullDocument
	}
	brand, err := m.resolveBrand(ctx)
	if err != nil {
		return RenderResult{}, err
	}
	body, err := renderBody(draftTemplate{
		Subject: subject,
		HTML:    html,
		Text:    text,
	}, m.injectCommonVars(nil))
	if err != nil {
		return RenderResult{}, err
	}
	return m.wrapInLayout(body, brand)
}

// Render resolves the template + PUBLISHED override + brand settings
// into the final wrapped HTML/text. Called from the email send worker.
// Never reads draft fields — unpublished admin edits stay out of real
// sends until an explicit Publish promotes them.
func (m *TemplateManager) Render(ctx context.Context, name string, data map[string]string) (RenderResult, error) {
	def, ok := m.definitions[name]
	if !ok {
		return RenderResult{}, fmt.Errorf("template %q not found", name)
	}
	override, err := m.getOverride(ctx, name)
	if err != nil {
		return RenderResult{}, err
	}
	brand, err := m.resolveBrand(ctx)
	if err != nil {
		return RenderResult{}, err
	}
	return m.renderWrappedWithReason(m.mergePublished(def, override), def.ReasonHTML, def.ReasonText, m.injectCommonVars(data), brand)
}

func (m *TemplateManager) getOverride(ctx context.Context, name string) (*model.EmailTemplateOverride, error) {
	if m.store == nil {
		return nil, nil
	}
	override, err := m.store.GetEmailTemplateOverride(ctx, name)
	if err != nil {
		return nil, err
	}
	return override, nil
}

func (m *TemplateManager) resolveBrand(ctx context.Context) (*model.EmailBrandSettings, error) {
	if m.brand == nil {
		return defaultBrand(), nil
	}
	settings, err := m.brand.GetEmailBrandSettings(ctx)
	if err != nil {
		return nil, fmt.Errorf("load email brand settings: %w", err)
	}
	if settings == nil {
		return defaultBrand(), nil
	}
	return settings, nil
}

// mergeDraft returns the effective template content for EDITOR paths
// (the admin UI's editor/preview/test-send). It prefers the admin's
// latest unpublished draft; empty draft fields fall back to the
// published snapshot, and published fallbacks further fall back to the
// file-based default. This is what an admin sees and previews —
// "what would this look like if I published right now?"
func (m *TemplateManager) mergeDraft(def templateDefinition, override *model.EmailTemplateOverride) draftTemplate {
	draft := draftTemplate{
		Subject:    def.Subject,
		HTML:       def.HTML,
		Text:       def.Text,
		EditorKind: "html",
	}
	if override == nil {
		return draft
	}
	// Draft first, then published, then file default.
	draft.Subject = pickString(override.DraftSubject, pickString(override.Subject, draft.Subject))
	draft.HTML = pickString(override.DraftHTML, pickString(override.HTML, draft.HTML))
	draft.Text = pickString(override.DraftText, pickString(override.Text, draft.Text))
	draft.EditorKind = pickString(override.EditorKind, draft.EditorKind)
	return draft
}

// mergePublished returns the effective content for REAL-SEND paths
// (the worker actually mailing a user). Never reads draft fields — if
// the admin hasn't published, real sends fall back to the file
// default. This is the critical test-vs-prod separation: draft edits
// cannot leak to production without an explicit Publish.
func (m *TemplateManager) mergePublished(def templateDefinition, override *model.EmailTemplateOverride) draftTemplate {
	draft := draftTemplate{
		Subject:    def.Subject,
		HTML:       def.HTML,
		Text:       def.Text,
		EditorKind: "html",
	}
	if override == nil || override.PublishedAt == nil {
		return draft
	}
	draft.Subject = pickString(override.Subject, draft.Subject)
	draft.HTML = pickString(override.HTML, draft.HTML)
	draft.Text = pickString(override.Text, draft.Text)
	draft.EditorKind = pickString(override.EditorKind, draft.EditorKind)
	return draft
}

// looksLikeFullDocument reports whether html contains chrome that would
// produce a nested-document render when wrapped in the base layout.
// Matches are case-insensitive; any whitespace between `<` and the tag
// name is rejected. We deliberately over-reject here — admins editing
// body fragments have no reason to emit these tags.
func looksLikeFullDocument(html string) bool {
	lower := strings.ToLower(html)
	for _, marker := range []string{"<!doctype", "<html", "<head", "<body"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func pickString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func cloneMap(input map[string]string) map[string]string {
	if input == nil {
		return map[string]string{}
	}
	out := make(map[string]string, len(input))
	maps.Copy(out, input)
	return out
}

// renderWrappedWithReason is the full render path. The reason templates
// (HTML + text) follow the same Go template syntax as the body and
// render against the same data map, so transactional footer lines can
// interpolate variables like {{.HubName}} alongside the body copy.
func (m *TemplateManager) renderWrappedWithReason(draft draftTemplate, reasonHTML, reasonText string, data map[string]string, brand *model.EmailBrandSettings) (RenderResult, error) {
	body, err := renderBody(draft, data)
	if err != nil {
		return RenderResult{}, err
	}
	rendered, err := renderReason(reasonHTML, reasonText, data)
	if err != nil {
		return RenderResult{}, err
	}
	return m.wrapInLayoutWithOptions(body, brand, layoutOptions{
		IncludeUnsubscribe: false,
		ReasonHTML:         template.HTML(rendered.HTML),
		ReasonText:         rendered.Text,
	})
}

// reasonResult is the pre-rendered "why you're receiving this" block
// passed into the base layout. Empty strings mean no reason line is
// surfaced.
type reasonResult struct {
	HTML string
	Text string
}

func renderReason(reasonHTML, reasonText string, data map[string]string) (reasonResult, error) {
	if reasonHTML == "" && reasonText == "" {
		return reasonResult{}, nil
	}
	var out reasonResult
	if reasonHTML != "" {
		tmpl, err := template.New("reason_html").Option("missingkey=error").Parse(reasonHTML)
		if err != nil {
			return reasonResult{}, fmt.Errorf("parse reason html: %w", err)
		}
		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, data); err != nil {
			return reasonResult{}, fmt.Errorf("render reason html: %w", err)
		}
		out.HTML = buf.String()
	}
	if reasonText != "" {
		tmpl, err := texttemplate.New("reason_text").Option("missingkey=error").Parse(reasonText)
		if err != nil {
			return reasonResult{}, fmt.Errorf("parse reason text: %w", err)
		}
		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, data); err != nil {
			return reasonResult{}, fmt.Errorf("render reason text: %w", err)
		}
		out.Text = buf.String()
	}
	return out, nil
}

func renderBody(draft draftTemplate, data map[string]string) (RenderResult, error) {
	subjectTmpl, err := texttemplate.New("subject").Option("missingkey=error").Parse(draft.Subject)
	if err != nil {
		return RenderResult{}, fmt.Errorf("parse subject template: %w", err)
	}
	textTmpl, err := texttemplate.New("text").Option("missingkey=error").Parse(draft.Text)
	if err != nil {
		return RenderResult{}, fmt.Errorf("parse text template: %w", err)
	}
	htmlTmpl, err := template.New("html").Option("missingkey=error").Parse(draft.HTML)
	if err != nil {
		return RenderResult{}, fmt.Errorf("parse html template: %w", err)
	}

	var subjectBuf bytes.Buffer
	if err := subjectTmpl.Execute(&subjectBuf, data); err != nil {
		return RenderResult{}, fmt.Errorf("render subject template: %w", err)
	}
	var textBuf bytes.Buffer
	if err := textTmpl.Execute(&textBuf, data); err != nil {
		return RenderResult{}, fmt.Errorf("render text template: %w", err)
	}
	var htmlBuf bytes.Buffer
	if err := htmlTmpl.Execute(&htmlBuf, data); err != nil {
		return RenderResult{}, fmt.Errorf("render html template: %w", err)
	}

	return RenderResult{
		Subject: subjectBuf.String(),
		HTML:    htmlBuf.String(),
		Text:    textBuf.String(),
	}, nil
}

// layoutData is passed to the base HTML layout template.
//
// Trust model (INTENTIONAL, do not narrow without replacing the admin
// auth surface): Body, Brand.FooterHTML, and Brand.ReasonHTML are
// emitted verbatim via template.HTML. Admin-only surfaces write these
// values; no end-user input reaches them. The admin preview iframe
// renders the same HTML through srcDoc, so any HTML sanitization added
// here must be kept in parity with the preview path.
type layoutData struct {
	Subject string
	Body    template.HTML
	Brand   layoutBrand
}

type layoutBrand struct {
	LogoURL         string
	LogoAlt         string
	ProductName     string
	FooterHTML      template.HTML
	PrimaryColor    string
	BackgroundColor string
	SurfaceColor    string
	BorderColor     string
	MutedColor      string
	BodyColor       string
	// Structured compliance footer. Rendered in a fixed order by the
	// base layout: reason → admin notes (FooterHTML) → support → company
	// identity + address → legal links → unsubscribe (marketing only).
	SupportEmail   string
	CompanyName    string
	CompanyAddress string
	PrivacyURL     string
	TermsURL       string
	// ReasonHTML / ReasonText carry the "you're receiving this because…"
	// line, pre-rendered against the template's data map. Empty means no
	// reason line is surfaced.
	ReasonHTML template.HTML
	ReasonText string
	// IncludeUnsubscribe + UnsubscribePlaceholder drive the footer
	// unsubscribe link. Production uses the placeholder as a marker
	// that the campaign worker string-replaces per-recipient with the
	// real token URL. Transactional sends leave IncludeUnsubscribe
	// false — hub invites and password reset aren't opt-outable.
	IncludeUnsubscribe     bool
	UnsubscribePlaceholder string
}

// UnsubscribePlaceholder is the token the campaign worker swaps out
// per-recipient. Chosen to be distinct from any realistic URL so the
// strings.ReplaceAll at send time can't false-match inside a template
// body or footer HTML block.
const UnsubscribePlaceholder = "__MEMAX_UNSUBSCRIBE_URL__"

// layoutOptions bundles the per-render knobs that don't come from the
// brand singleton. Zero value = transactional render with no reason
// line and no unsubscribe footer.
type layoutOptions struct {
	IncludeUnsubscribe bool
	ReasonHTML         template.HTML
	ReasonText         string
}

func (m *TemplateManager) wrapInLayout(body RenderResult, brand *model.EmailBrandSettings) (RenderResult, error) {
	return m.wrapInLayoutWithOptions(body, brand, layoutOptions{})
}

// WrapAdHocWithUnsubscribe wraps a pre-rendered body in the base layout
// with the unsubscribe footer enabled. The worker calls this once per
// campaign and then does per-recipient strings.ReplaceAll for the
// placeholder. Exposed for the campaign send worker.
func (m *TemplateManager) WrapAdHocWithUnsubscribe(ctx context.Context, subject, html, text string) (RenderResult, error) {
	if looksLikeFullDocument(html) {
		return RenderResult{}, ErrOverrideLooksLikeFullDocument
	}
	brand, err := m.resolveBrand(ctx)
	if err != nil {
		return RenderResult{}, err
	}
	body, err := renderBody(draftTemplate{
		Subject: subject,
		HTML:    html,
		Text:    text,
	}, m.injectCommonVars(nil))
	if err != nil {
		return RenderResult{}, err
	}
	return m.wrapInLayoutWithOptions(body, brand, layoutOptions{IncludeUnsubscribe: true})
}

func (m *TemplateManager) wrapInLayoutWithOptions(body RenderResult, brand *model.EmailBrandSettings, opts layoutOptions) (RenderResult, error) {
	htmlData := layoutData{
		Subject: body.Subject,
		Body:    template.HTML(body.HTML),
		Brand: layoutBrand{
			LogoURL:                resolveLogoURL(brand.LogoURL, m.appBaseURL),
			LogoAlt:                pickString(brand.LogoAlt, "memax"),
			ProductName:            pickString(brand.ProductName, "memax"),
			FooterHTML:             template.HTML(brand.FooterHTML),
			PrimaryColor:           pickString(brand.PrimaryColor, "#111111"),
			BackgroundColor:        pickString(brand.BackgroundColor, "#fafafa"),
			SurfaceColor:           pickString(brand.SurfaceColor, "#ffffff"),
			BorderColor:            pickString(brand.BorderColor, "#e5e5e5"),
			MutedColor:             pickString(brand.MutedColor, "#888888"),
			BodyColor:              pickString(brand.BodyColor, "#444444"),
			SupportEmail:           brand.SupportEmail,
			CompanyName:            brand.CompanyName,
			CompanyAddress:         brand.CompanyAddress,
			PrivacyURL:             brand.PrivacyURL,
			TermsURL:               brand.TermsURL,
			ReasonHTML:             opts.ReasonHTML,
			ReasonText:             opts.ReasonText,
			IncludeUnsubscribe:     opts.IncludeUnsubscribe,
			UnsubscribePlaceholder: UnsubscribePlaceholder,
		},
	}

	var htmlBuf bytes.Buffer
	if err := m.baseHTML.Execute(&htmlBuf, htmlData); err != nil {
		return RenderResult{}, fmt.Errorf("wrap html in base layout: %w", err)
	}

	textData := struct {
		Body  string
		Brand struct {
			FooterText             string
			SupportEmail           string
			CompanyName            string
			CompanyAddress         string
			PrivacyURL             string
			TermsURL               string
			ReasonText             string
			IncludeUnsubscribe     bool
			UnsubscribePlaceholder string
		}
	}{
		Body: body.Text,
	}
	textData.Brand.FooterText = brand.FooterText
	textData.Brand.SupportEmail = brand.SupportEmail
	textData.Brand.CompanyName = brand.CompanyName
	textData.Brand.CompanyAddress = brand.CompanyAddress
	textData.Brand.PrivacyURL = brand.PrivacyURL
	textData.Brand.TermsURL = brand.TermsURL
	textData.Brand.ReasonText = opts.ReasonText
	textData.Brand.IncludeUnsubscribe = opts.IncludeUnsubscribe
	textData.Brand.UnsubscribePlaceholder = UnsubscribePlaceholder

	var textBuf bytes.Buffer
	if err := m.baseText.Execute(&textBuf, textData); err != nil {
		return RenderResult{}, fmt.Errorf("wrap text in base layout: %w", err)
	}

	return RenderResult{
		Subject: body.Subject,
		HTML:    htmlBuf.String(),
		Text:    textBuf.String(),
	}, nil
}
