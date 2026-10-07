package v2api_test

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MemaxLabs/memax/packages/server/internal/handler/v2api"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
	"github.com/MemaxLabs/memax/packages/server/internal/trust"
	"github.com/MemaxLabs/memax/packages/server/internal/websurface"
)

type notificationChoice struct {
	Event     string `json:"event"`
	InApp     bool   `json:"in_app"`
	Email     bool   `json:"email"`
	EmailSent bool   `json:"email_sent"`
}

type notificationSettings struct {
	Version        int                  `json:"version"`
	TimeZone       string               `json:"time_zone"`
	TimeZoneSource string               `json:"time_zone_source"`
	Events         []notificationChoice `json:"events"`
	QuietHours     struct {
		On           bool   `json:"on"`
		From         string `json:"from"`
		Until        string `json:"until"`
		GatesThrough bool   `json:"gates_through"`
	} `json:"quiet_hours"`
	ReviewAfterDays int `json:"review_after_days"`
}

func (s notificationSettings) event(name string) notificationChoice {
	for _, e := range s.Events {
		if e.Event == name {
			return e
		}
	}
	return notificationChoice{}
}

// The settings are a person's: defaults first, edits with If-Match that
// replay on their key and clash on a stale version, each person's own (RLS
// on the person), and no agent reads or changes them.
func TestNotificationSettings(t *testing.T) {
	t.Parallel()
	e := newEnv(t, v2api.WithNotifications(v2api.NotificationDelivery{MorningEmail: true}))
	zz, jy := e.user("zz"), e.user("jy")
	e.space(zz, policy.SpaceProject, "memax-v2")
	token := e.session(zz)

	var s notificationSettings
	r := e.do(call{method: "GET", path: "/v2/me/notifications", token: token}).ok(http.StatusOK, &s)
	if r.header.Get("ETag") != `"1"` || s.Version != 1 {
		t.Fatalf("a new person's version: ETag %s, %d", r.header.Get("ETag"), s.Version)
	}
	order := []string{"decision_gate", "morning_edition", "review_waiting", "drift", "stale", "write_held",
		"weekly_summary", "forget_done", "agent_changed"}
	if len(s.Events) != len(order) {
		t.Fatalf("events %v", s.Events)
	}
	for i, name := range order {
		if s.Events[i].Event != name {
			t.Errorf("event %d is %s, want %s (the page's order)", i, s.Events[i].Event, name)
		}
	}
	if !s.event("decision_gate").Email || s.event("drift").Email || s.event("weekly_summary").InApp ||
		!s.event("morning_edition").EmailSent || s.event("decision_gate").EmailSent {
		t.Errorf("defaults: %+v", s.Events)
	}
	if !s.QuietHours.On || s.QuietHours.From != "20:00" || s.QuietHours.Until != "08:00" || !s.QuietHours.GatesThrough ||
		s.ReviewAfterDays != 1 || s.TimeZone != "UTC" || s.TimeZoneSource != "default" {
		t.Errorf("defaults: %+v", s)
	}

	// An edit needs If-Match.
	change := map[string]any{
		"events":            map[string]any{"drift": map[string]bool{"email": true}},
		"quiet_hours":       map[string]any{"from": "21:30", "until": "07:00"},
		"review_after_days": 2,
	}
	e.do(call{method: "PATCH", path: "/v2/me/notifications", token: token, body: change, invalid: true}).
		fails(http.StatusPreconditionRequired, "precondition_required")

	key := uuid.NewString()
	r = e.do(call{method: "PATCH", path: "/v2/me/notifications", token: token, body: change,
		header: map[string]string{"If-Match": `"1"`, "Idempotency-Key": key}}).ok(http.StatusOK, &s)
	if !s.event("drift").Email || s.QuietHours.From != "21:30" || s.QuietHours.Until != "07:00" || s.ReviewAfterDays != 2 ||
		!s.QuietHours.On || s.Version != 2 || r.header.Get("ETag") != `"2"` {
		t.Fatalf("after the edit: %+v, ETag %s", s, r.header.Get("ETag"))
	}
	// Its retry replays instead of clashing with the version it wrote.
	r = e.do(call{method: "PATCH", path: "/v2/me/notifications", token: token, body: change,
		header: map[string]string{"If-Match": `"1"`, "Idempotency-Key": key}}).ok(http.StatusOK, &s)
	if r.header.Get("Idempotent-Replayed") != "true" || s.Version != 2 {
		t.Errorf("retry: replayed %q, version %d", r.header.Get("Idempotent-Replayed"), s.Version)
	}
	// The same key for another change is refused.
	e.do(call{method: "PATCH", path: "/v2/me/notifications", token: token, body: map[string]any{"review_after_days": 5},
		header: map[string]string{"If-Match": `"2"`, "Idempotency-Key": key}}).
		fails(http.StatusUnprocessableEntity, "idempotency_key_reused")
	// Another tab, a version behind, clashes.
	clash := e.do(call{method: "PATCH", path: "/v2/me/notifications", token: token,
		body:   map[string]any{"events": map[string]any{"stale": map[string]bool{"email": true}}},
		header: map[string]string{"If-Match": `"1"`}}).fails(http.StatusPreconditionFailed, "edit_clash")
	if clash.Details.ExpectedVersion != 1 || clash.Details.CurrentVersion != 2 {
		t.Errorf("clash details: %+v", clash.Details)
	}
	// Quiet hours turn off and keep their times; equal times are refused.
	e.do(call{method: "PATCH", path: "/v2/me/notifications", token: token,
		body:   map[string]any{"quiet_hours": map[string]any{"on": false, "gates_through": false}},
		header: map[string]string{"If-Match": `"2"`}}).ok(http.StatusOK, &s)
	if s.QuietHours.On || s.QuietHours.GatesThrough || s.QuietHours.From != "21:30" || s.Version != 3 {
		t.Errorf("quiet hours off: %+v", s)
	}
	e.do(call{method: "PATCH", path: "/v2/me/notifications", token: token,
		body:   map[string]any{"quiet_hours": map[string]any{"from": "07:00"}},
		header: map[string]string{"If-Match": `"3"`}}).fails(http.StatusBadRequest, "invalid_request")
	for name, body := range map[string]any{
		"an unknown event":    map[string]any{"events": map[string]any{"party": map[string]bool{"email": true}}},
		"in-app is no choice": map[string]any{"events": map[string]any{"drift": map[string]bool{"email": true, "in_app": false}}},
		"a time that isn't":   map[string]any{"quiet_hours": map[string]any{"from": "8am"}},
		"too many days":       map[string]any{"review_after_days": 30},
		"nothing":             map[string]any{},
	} {
		got := e.do(call{method: "PATCH", path: "/v2/me/notifications", token: token, body: body, invalid: true,
			header: map[string]string{"If-Match": `"3"`}})
		if got.status != http.StatusBadRequest {
			t.Errorf("%s: %d %s", name, got.status, got.body)
		}
	}

	// RLS on the person: jy reads the defaults, never zz's, and changes
	// only their own.
	var theirs notificationSettings
	e.do(call{method: "GET", path: "/v2/me/notifications", token: e.session(jy)}).ok(http.StatusOK, &theirs)
	if theirs.Version != 1 || theirs.event("drift").Email || theirs.ReviewAfterDays != 1 {
		t.Errorf("jy sees %+v, want the defaults", theirs)
	}
	e.do(call{method: "PATCH", path: "/v2/me/notifications", token: e.session(jy), body: map[string]any{"review_after_days": 7},
		header: map[string]string{"If-Match": `"1"`}}).ok(http.StatusOK, &theirs)
	e.do(call{method: "GET", path: "/v2/me/notifications", token: token}).ok(http.StatusOK, &s)
	if s.ReviewAfterDays != 2 || s.Version != 3 {
		t.Errorf("jy's edit reached zz: %+v", s)
	}
	ctx := context.Background()
	tx, err := e.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var visible int
	if err := func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT set_config('role', 'memax_v2', true), set_config('app.person_id', $1, true)`, jy.String()); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `SELECT count(*) FROM v2.notification_settings WHERE person_id = $1`, zz).Scan(&visible)
	}(tx); err != nil {
		t.Fatal(err)
	}
	if visible != 0 {
		t.Errorf("as jy, memax_v2 sees zz's notification settings")
	}

	// An agent's key reads and changes nothing here, even its person's.
	agent, _ := e.apiKey(zz, keyOpts{agent: "codex"})
	e.do(call{method: "GET", path: "/v2/me/notifications", token: agent}).fails(http.StatusForbidden, "permission_denied")
	e.do(call{method: "PATCH", path: "/v2/me/notifications", token: agent, body: map[string]any{"review_after_days": 3},
		header: map[string]string{"If-Match": `"3"`}}).fails(http.StatusForbidden, "permission_denied")
}

// The morning edition's email is one setting with Dream's: the
// notifications page, Settings › Account and the email's one-click
// unsubscribe read and write the same column, and an edit made before an
// unsubscribe can't turn the email back on.
func TestMorningEmailIsDreamsSetting(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	zz := e.user("zz")
	e.space(zz, policy.SpaceProject, "memax-v2")
	token := e.session(zz)
	type dreamSettings struct {
		MorningEmail bool `json:"morning_email"`
	}

	var s notificationSettings
	e.do(call{method: "GET", path: "/v2/me/notifications", token: token}).ok(http.StatusOK, &s)
	if !s.event("morning_edition").Email || s.event("morning_edition").EmailSent {
		t.Fatalf("morning edition: %+v (this deployment sends no email)", s.event("morning_edition"))
	}
	// Off here is off in Dream's settings.
	e.do(call{method: "PATCH", path: "/v2/me/notifications", token: token,
		body:   map[string]any{"events": map[string]any{"morning_edition": map[string]bool{"email": false}}},
		header: map[string]string{"If-Match": `"1"`}}).ok(http.StatusOK, &s)
	var d dreamSettings
	e.do(call{method: "GET", path: "/v2/dream/settings", token: token}).ok(http.StatusOK, &d)
	if d.MorningEmail || s.event("morning_edition").Email {
		t.Fatalf("after turning it off here: Dream's %v, here %v", d.MorningEmail, s.event("morning_edition").Email)
	}
	if n := e.count(`SELECT count(*) FROM v2.notification_settings WHERE person_id = $1 AND email ? 'morning_edition'`, zz); n != 0 {
		t.Errorf("the morning email's choice was copied into notification_settings")
	}
	// On in Dream's settings is on here, at a new version.
	before := s.Version
	e.do(call{method: "PATCH", path: "/v2/dream/settings", token: token, body: map[string]any{"morning_email": true}}).
		ok(http.StatusOK, &d)
	e.do(call{method: "GET", path: "/v2/me/notifications", token: token}).ok(http.StatusOK, &s)
	if !s.event("morning_edition").Email || s.Version <= before {
		t.Fatalf("after Dream's settings: %+v, version %d (was %d)", s.event("morning_edition"), s.Version, before)
	}
	// The email's unsubscribe link turns it off here too, and an edit made
	// from the page as it was before is refused rather than undoing it.
	stale := s.Version
	var tok string
	if err := e.pool.QueryRow(context.Background(), `SELECT unsubscribe_token FROM v2.dream_settings WHERE person_id = $1`, zz).Scan(&tok); err != nil {
		t.Fatal(err)
	}
	e.do(call{method: "POST", path: "/v2/dream/email:unsubscribe?token=" + tok}).ok(http.StatusOK, nil)
	e.do(call{method: "GET", path: "/v2/me/notifications", token: token}).ok(http.StatusOK, &s)
	if s.event("morning_edition").Email || s.Version <= stale {
		t.Fatalf("after the unsubscribe link: %+v, version %d (was %d)", s.event("morning_edition"), s.Version, stale)
	}
	e.do(call{method: "PATCH", path: "/v2/me/notifications", token: token,
		body:   map[string]any{"events": map[string]any{"morning_edition": map[string]bool{"email": true}}},
		header: map[string]string{"If-Match": strconv.Quote(strconv.Itoa(stale))}}).fails(http.StatusPreconditionFailed, "edit_clash")
	e.do(call{method: "GET", path: "/v2/dream/settings", token: token}).ok(http.StatusOK, &d)
	if d.MorningEmail {
		t.Error("a stale edit turned the morning email back on after its unsubscribe")
	}
}

// Settings › Security says what the server's configuration says, and what
// a Keep from this session counts as: human_web only for the web app's
// signed requests.
func TestSecurity(t *testing.T) {
	t.Parallel()
	env := map[string]string{
		"ANTHROPIC_API_KEY":  "sk-test",
		"ANTHROPIC_BASE_URL": "https://openrouter.ai/api",
		"VOYAGE_API_KEY":     "pa-test",
		"JUDGE_STRONG_MODEL": "off",
		"DREAM_MODEL":        "off",
		"V2_DATA_RESIDENCY":  "database=neon:us-west-2,compute=fly:sjc,objects=r2,edge=cloudflare",
	}
	posture := trust.FromEnv(func(k string) (string, bool) { v, ok := env[k]; return v, ok })
	v, err := websurface.New(surfaceSecret)
	if err != nil {
		t.Fatal(err)
	}
	e := newEnv(t, v2api.WithTrust(posture), v2api.WithWebSurface(v))
	zz := e.user("zz")
	e.space(zz, policy.SpaceProject, "memax-v2")
	type security struct {
		Assurance string `json:"assurance"`
		Residency []struct {
			Holds    string `json:"holds"`
			Provider string `json:"provider"`
			Region   string `json:"region"`
		} `json:"residency"`
		Processors []struct {
			Name      string `json:"name"`
			Retention string `json:"retention"`
			Uses      []struct {
				Use           string   `json:"use"`
				Model         string   `json:"model"`
				Hosts         []string `json:"hosts"`
				MinPrecision  string   `json:"min_precision"`
				ZeroRetention bool     `json:"zero_retention"`
			} `json:"uses"`
		} `json:"processors"`
		BackupDays int `json:"backup_days"`
	}
	var s security
	e.do(call{method: "GET", path: "/v2/security", token: e.session(zz)}).ok(http.StatusOK, &s)
	if s.Assurance != "client_attested" || s.BackupDays != 7 || len(s.Residency) != 4 || s.Residency[0].Region != "us-west-2" {
		t.Errorf("security: %+v", s)
	}
	if len(s.Processors) != 2 || s.Processors[0].Name != "openrouter" || s.Processors[0].Retention != "zero" ||
		s.Processors[1].Name != "voyage" || s.Processors[1].Retention != "unconfirmed" {
		t.Fatalf("processors: %+v", s.Processors)
	}
	judge := s.Processors[0].Uses[0]
	if judge.Use != "judge" || judge.Model != "deepseek/deepseek-v4.1-flash" || judge.MinPrecision != "fp8" ||
		!judge.ZeroRetention || strings.Join(judge.Hosts, ",") != "together,baseten,coreweave,deepinfra" {
		t.Errorf("the judge's tier: %+v", judge)
	}
	for _, u := range s.Processors[0].Uses {
		if strings.HasPrefix(u.Use, "dream") || u.Use == "judge_strong" {
			t.Errorf("a tier that's off is listed: %s", u.Use)
		}
	}
	// The web app's signed request counts as a person on the web.
	e.do(call{method: "GET", path: "/v2/security", token: e.webSession(zz), sign: webSigned(zz, tamper{})}).ok(http.StatusOK, &s)
	if s.Assurance != "human_web" {
		t.Errorf("a signed web session: %s, want human_web", s.Assurance)
	}
	// It's a person's page.
	agent, _ := e.apiKey(zz, keyOpts{agent: "codex"})
	e.do(call{method: "GET", path: "/v2/security", token: agent}).fails(http.StatusForbidden, "permission_denied")
}
