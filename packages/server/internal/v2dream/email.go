package v2dream

import (
	"bytes"
	"context"
	"fmt"
	htmltemplate "html/template"
	"log/slog"
	"net/url"
	"strings"
	texttemplate "text/template"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/email"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/lifecycle"
)

// The morning email (the handoff's Emails board): the edition's card, what
// waits on the person, and Start review. It goes through V1's email
// framework (email.Sender: Resend in production) to the people who may
// keep in the space and have it on, once per person per edition
// (v2.dream_email_sends), with RFC 8058 one-click unsubscribe.
//
// Words: only memories' own words, read when the email is rendered (so a
// memory forgotten since the edition is left out), and only what the
// recipient can already read in the space; text from an outside source
// (quarantined) is never put in an email, which would carry it out of
// Review's ochre notice.

// MailerConfig configures the morning email.
type MailerConfig struct {
	From string
	// AppURL is the web app's origin (links), APIURL the API's (one-click
	// unsubscribe).
	AppURL, APIURL string
	Log            *slog.Logger
}

// Mailer sends editions' morning emails.
type Mailer struct {
	ledger *ledger.Ledger
	sender email.Sender
	cfg    MailerConfig
}

// NewMailer returns the mailer, or nil without a ledger or a sender.
func NewMailer(l *ledger.Ledger, s email.Sender, cfg MailerConfig) *Mailer {
	if l == nil || s == nil {
		return nil
	}
	if cfg.From == "" {
		cfg.From = DefaultFrom
	}
	cfg.AppURL = strings.TrimRight(cfg.AppURL, "/")
	if cfg.AppURL == "" {
		cfg.AppURL = "https://memax.app"
	}
	cfg.APIURL = strings.TrimRight(cfg.APIURL, "/")
	if cfg.APIURL == "" {
		cfg.APIURL = "https://api.memax.app"
	}
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}
	return &Mailer{ledger: l, sender: s, cfg: cfg}
}

// Send sends an edition's email to every recipient who hasn't had it.
func (m *Mailer) Send(ctx context.Context, spaceID, editionID uuid.UUID) error {
	sp, recipients, err := m.ledger.DreamRecipients(ctx, spaceID, editionID)
	if err != nil {
		return err
	}
	var todo []ledger.DreamRecipient
	for _, r := range recipients {
		if !r.Sent {
			todo = append(todo, r)
		}
	}
	if len(todo) == 0 {
		return nil
	}
	scope, err := m.ledger.SpaceScope(ctx, spaceID)
	if err != nil {
		return err
	}
	e, err := m.ledger.GetEdition(ctx, scope, spaceID, editionID.String())
	if err != nil {
		return err
	}
	for _, r := range todo {
		msg, err := m.Render(e, sp, r)
		if err != nil {
			return err
		}
		res, err := m.sender.Send(ctx, msg)
		if err != nil {
			return fmt.Errorf("dream: send the morning email: %w", err)
		}
		if err := m.ledger.MarkDreamEmailSent(ctx, spaceID, editionID, r.PersonID, res.MessageID); err != nil {
			return err
		}
		m.cfg.Log.InfoContext(ctx, "dream: sent the morning email", "edition", e.Ref, "space_id", spaceID.String(),
			"metric", "dream_email_sent")
	}
	return nil
}

// mailLine is one line of the card or of "Waiting on you".
type mailLine struct {
	Kind, Mark, Text string
	Plain            bool // stale: not italic, dotted underline
}

type mailData struct {
	Issue, Date, Space string
	Notes, Facts       int
	Headline           string
	FactsText          string
	Lines              []mailLine
	Waiting            []mailLine
	ReviewURL          string
	SettingsURL        string
	UnsubscribeURL     string
	NoNotes            bool
}

// Render builds one recipient's email.
func (m *Mailer) Render(e *ledger.DreamEdition, sp ledger.DreamEmailSpace, r ledger.DreamRecipient) (email.Message, error) {
	loc := location(r.TimeZone)
	d := mailData{
		Issue: fmt.Sprintf("No. %d", e.N), Date: e.Slot.In(loc).Format("Mon 2 Jan"), Space: sp.Name,
		Notes: e.NotesRead, Facts: len(e.FactRefs), NoNotes: e.NotesRead == 0,
		ReviewURL:      m.cfg.AppURL + "/" + url.PathEscape(sp.Slug) + "/review",
		SettingsURL:    m.cfg.AppURL + "/" + url.PathEscape(sp.Slug) + "/dream/" + fmt.Sprint(e.N) + "#email",
		UnsubscribeURL: m.cfg.AppURL + "/unsubscribe?token=" + url.QueryEscape(r.Token),
	}
	d.FactsText = plural(d.Facts, "fact", "facts")
	d.Headline = fmt.Sprintf("%s became", plural(d.Notes, "note", "notes"))
	words := func(mem *ledger.Memory) (string, bool) {
		if mem == nil || mem.Lifecycle == lifecycle.Forgotten || mem.Statement == "" {
			return "", false
		}
		if mem.Trust.External() {
			return "A proposal from an outside source, waiting in Review.", true
		}
		return mem.Statement, true
	}
	// The card: what Dream did, at most three lines, conflicts first.
	for _, a := range e.Actions {
		if a.Undone != nil || len(d.Lines) >= 1 {
			break
		}
		if a.Kind == ledger.DreamFold {
			if t, ok := words(a.Memory); ok {
				d.Lines = append(d.Lines, mailLine{Kind: "merged", Text: t})
			}
		}
	}
	for _, a := range e.Actions {
		if a.Kind == ledger.DreamConflict && a.Undone == nil {
			if t, ok := words(a.Memory); ok {
				d.Lines = append([]mailLine{{Kind: "conflict", Text: t}}, d.Lines...)
				break
			}
		}
	}
	if len(d.Lines) == 0 {
		for _, s := range e.Surfaced {
			if t, ok := words(s.Memory); ok {
				d.Lines = append(d.Lines, mailLine{Kind: "conflict", Text: t})
				break
			}
		}
	}
	faded := 0
	for _, a := range e.Actions {
		if a.Kind == ledger.DreamFade && a.Undone == nil {
			faded++
		}
	}
	if faded > 0 {
		d.Lines = append(d.Lines, mailLine{Kind: "faded", Text: fmt.Sprintf("%s no agent has read in 60 days.", plural(faded, "memory", "memories"))})
	}
	if len(d.Lines) > 3 {
		d.Lines = d.Lines[:3]
	}
	// Waiting on you: what still waits, as it stands now.
	seen := map[uuid.UUID]bool{}
	wait := func(mem *ledger.Memory) {
		if mem == nil || seen[mem.ID] || len(d.Waiting) >= 3 {
			return
		}
		state := mem.State
		if state != lifecycle.MarkConflict && state != lifecycle.MarkProposed && state != lifecycle.MarkStale {
			return
		}
		t, ok := words(mem)
		if !ok {
			return
		}
		seen[mem.ID] = true
		d.Waiting = append(d.Waiting, mailLine{Mark: string(state), Text: t, Plain: state == lifecycle.MarkStale})
	}
	for _, a := range e.Actions {
		if a.Kind == ledger.DreamConflict && a.Undone == nil {
			wait(a.Memory)
		}
	}
	for _, s := range e.Surfaced {
		wait(s.Memory)
	}
	for _, a := range e.Actions {
		if a.Kind == ledger.DreamPropose && a.Undone == nil {
			wait(a.Memory)
		}
	}
	for _, a := range e.Actions {
		if a.Kind == ledger.DreamStale && a.Undone == nil {
			wait(a.Memory)
		}
	}
	needs := e.NeedsYou
	subject := fmt.Sprintf("%s became %s", plural(d.Notes, "note", "notes"), d.FactsText)
	switch {
	case d.Notes == 0:
		subject = fmt.Sprintf("Dream's edition %s for %s", d.Issue, sp.Name)
	case d.Facts == 0:
		subject = fmt.Sprintf("Dream read %s", plural(d.Notes, "note", "notes"))
	}
	if needs == 1 {
		subject += " · 1 thing needs you"
	} else if needs > 1 {
		subject += fmt.Sprintf(" · %d things need you", needs)
	}
	var html, text bytes.Buffer
	if err := mailHTML.Execute(&html, d); err != nil {
		return email.Message{}, err
	}
	if err := mailText.Execute(&text, d); err != nil {
		return email.Message{}, err
	}
	oneClick := m.cfg.APIURL + "/v2/dream/email:unsubscribe?token=" + url.QueryEscape(r.Token)
	return email.Message{
		From: m.cfg.From, To: []string{r.Email}, Subject: subject, HTML: html.String(), Text: text.String(),
		Tags: map[string]string{"category": "dream_edition"},
		Headers: map[string]string{
			"List-Unsubscribe":      "<" + oneClick + ">",
			"List-Unsubscribe-Post": "List-Unsubscribe=One-Click",
		},
	}, nil
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

// The colours are the Ledger's tokens (packages/ledger-tokens, Paper), as
// literals: email clients don't read CSS custom properties.
const (
	cPaper      = "#edf1ef"
	cCard       = "#fcfdfc"
	cSheet      = "#f4f7f5"
	cNight      = "#17211b"
	cNightInk   = "#edf1ef"
	cNightInk2  = "#b0b8b3"
	cNightLine  = "#2d3932"
	cNightAcc   = "#7ed3a6"
	cNightVerm  = "#f6876e"
	cInk        = "#151b18"
	cInk2       = "#474d4a"
	cInk3       = "#5c625f"
	cLine       = "#d5dad7"
	cLineStrong = "#818683"
	cOchre      = "#895909"
	cVermilion  = "#b23521"
)

var mailHTML = htmltemplate.Must(htmltemplate.New("dream").Funcs(htmltemplate.FuncMap{
	"kindColor": func(k string) string {
		switch k {
		case "conflict":
			return cNightVerm
		case "merged":
			return cNightAcc
		}
		return cNightInk2
	},
	"markColor": func(m string) string {
		switch m {
		case "conflict":
			return cVermilion
		case "proposed":
			return cOchre
		}
		return cInk3
	},
	"markGlyph": func(m string) string {
		switch m {
		case "conflict":
			return "&#9680;"
		case "proposed":
			return "&#9675;"
		}
		return "&#8942;"
	},
	"safe": func(s string) htmltemplate.HTML { return htmltemplate.HTML(s) }, //nolint:gosec // fixed glyph entities only
}).Parse(`<!DOCTYPE html>
<html lang="en">
<head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1.0"><title>Dream</title></head>
<body style="margin:0;padding:0;background:` + cPaper + `;color:` + cInk + `;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,sans-serif;">
<table role="presentation" width="100%" cellspacing="0" cellpadding="0" style="background:` + cPaper + `;"><tr><td align="center" style="padding:32px 12px;">
<table role="presentation" width="600" cellspacing="0" cellpadding="0" style="max-width:600px;width:100%;background:` + cCard + `;border:1px solid ` + cLine + `;border-radius:8px;">
<tr><td style="padding:28px 32px 0;font-family:Georgia,'Times New Roman',serif;font-size:22px;color:` + cInk + `;">Memax</td></tr>
<tr><td style="padding:20px 32px 0;">
  <table role="presentation" width="100%" cellspacing="0" cellpadding="0" style="background:` + cNight + `;border-radius:6px;">
  <tr><td style="padding:18px 20px 0;">
    <table role="presentation" width="100%" cellspacing="0" cellpadding="0"><tr>
      <td style="font-family:Georgia,'Times New Roman',serif;font-size:20px;color:` + cNightInk + `;">Dream</td>
      <td align="right" style="font-family:'IBM Plex Mono',Menlo,Consolas,monospace;font-size:12px;color:` + cNightInk2 + `;">{{.Issue}} · {{.Date}} · {{.Space}}</td>
    </tr></table>
    <div style="border-top:1px solid ` + cNightLine + `;margin-top:12px;"></div>
  </td></tr>
  <tr><td style="padding:14px 20px 4px;font-family:Georgia,'Times New Roman',serif;font-size:28px;line-height:34px;color:` + cNightInk + `;">{{if .NoNotes}}Dream's morning edition.{{else}}{{.Headline}} <span style="color:` + cNightAcc + `;">{{.FactsText}}</span>.{{end}}</td></tr>
  {{range .Lines}}<tr><td style="padding:0 20px;"><table role="presentation" width="100%" cellspacing="0" cellpadding="0" style="border-top:1px solid ` + cNightLine + `;"><tr>
    <td width="72" valign="top" style="padding:10px 0;font-size:13px;color:{{kindColor .Kind}};">{{.Kind}}</td>
    <td valign="top" style="padding:10px 0;font-family:Georgia,'Times New Roman',serif;font-size:15px;line-height:22px;color:` + cNightInk + `;">{{.Text}}</td>
  </tr></table></td></tr>{{end}}
  <tr><td style="padding:0 0 10px;"></td></tr>
  </table>
</td></tr>
{{if .Waiting}}<tr><td style="padding:20px 32px 0;">
  <p style="margin:0 0 4px;font-size:14px;line-height:20px;font-weight:600;color:` + cInk + `;">Waiting on you</p>
  {{range .Waiting}}<table role="presentation" width="100%" cellspacing="0" cellpadding="0" style="border-bottom:1px solid ` + cLine + `;"><tr>
    <td width="22" valign="top" style="padding:12px 0 10px;color:{{markColor .Mark}};font-size:12px;">{{safe (markGlyph .Mark)}}</td>
    <td valign="top" style="padding:10px 0;font-family:Georgia,'Times New Roman',serif;font-size:16px;line-height:24px;color:` + cInk2 + `;{{if .Plain}}text-decoration:underline dotted ` + cLineStrong + `;{{else}}font-style:italic;{{end}}">{{.Text}}</td>
  </tr></table>{{end}}
</td></tr>{{end}}
<tr><td style="padding:24px 32px 32px;"><a href="{{.ReviewURL}}" style="display:inline-block;padding:12px 18px;background:` + cInk + `;color:` + cCard + `;text-decoration:none;border-radius:6px;font-size:15px;font-weight:500;">Start review</a></td></tr>
<tr><td style="padding:16px 32px 24px;border-top:1px solid ` + cLine + `;background:` + cSheet + `;font-size:12px;line-height:17px;color:` + cInk3 + `;">
  You get this because the morning edition is on. <a href="{{.SettingsURL}}" style="color:` + cInk2 + `;">Change when, or turn it off</a>. <a href="{{.UnsubscribeURL}}" style="color:` + cInk2 + `;">Unsubscribe</a>.
</td></tr>
</table>
</td></tr></table>
</body>
</html>
`))

var mailText = texttemplate.Must(texttemplate.New("dream").Parse(`Dream · {{.Issue}} · {{.Date}} · {{.Space}}

{{if .NoNotes}}Dream's morning edition.{{else}}{{.Headline}} {{.FactsText}}.{{end}}
{{range .Lines}}
{{.Kind}}: {{.Text}}{{end}}
{{if .Waiting}}
Waiting on you
{{range .Waiting}}
- {{.Text}}{{end}}
{{end}}
Start review: {{.ReviewURL}}

You get this because the morning edition is on. Change when, or turn it off: {{.SettingsURL}}
Unsubscribe: {{.UnsubscribeURL}}
`))
