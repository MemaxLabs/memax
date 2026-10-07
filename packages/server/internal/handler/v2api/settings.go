package v2api

import (
	"errors"
	"net/http"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
	"github.com/MemaxLabs/memax/packages/server/internal/trust"
)

// A person's settings beyond Dream's (Phase 2 epic 2.6): their
// notification preferences (Settings › Notifications) and what this Memax
// says about its own security (Settings › Security). Both belong to a
// person: an agent's credential gets 403.

// NotificationDelivery says which emails this deployment sends today, so
// the page can tell a choice that takes effect from one kept for later.
type NotificationDelivery struct {
	// MorningEmail: Dream's morning email goes out (DREAM_EMAIL, with an
	// email provider configured).
	MorningEmail bool
}

// WithNotifications sets what the deployment delivers. Without it nothing
// is said to be sent.
func WithNotifications(d NotificationDelivery) Option {
	return func(h *Handler) { h.delivery = d }
}

// WithTrust sets what Settings › Security says about this deployment
// (trust.FromEnv). Without it the posture is empty: no processors, the
// default residency and backup window.
func WithTrust(p trust.Posture) Option {
	return func(h *Handler) { h.posture = &p }
}

// personOnly refuses an agent's credential: these are a person's settings.
func personOnly(p *principal, what string) *apiError {
	if p.actor.Kind == policy.ActorPerson {
		return nil
	}
	return &apiError{status: http.StatusForbidden, code: codePermissionDenied,
		message: what + " are a person's. Sign in on the web or with the CLI."}
}

// notificationsView is the settings with what this deployment sends.
func (h *Handler) notificationsView(s ledger.NotificationSettings) ledger.NotificationSettings {
	for i := range s.Events {
		s.Events[i].EmailSent = s.Events[i].Event == ledger.NotifyMorningEdition && h.delivery.MorningEmail
	}
	return s
}

// GET /v2/me/notifications
func (h *Handler) getNotificationSettings(w http.ResponseWriter, r *http.Request) {
	p, e := h.principalFor(r)
	if e != nil {
		writeError(w, e)
		return
	}
	if e := personOnly(p, "Notification settings"); e != nil {
		writeError(w, e)
		return
	}
	s, err := h.ledger.GetNotificationSettings(r.Context(), p.scope)
	if err != nil {
		writeError(w, h.fromLedger(r, err))
		return
	}
	setVersionETag(w, s.Version)
	writeData(w, http.StatusOK, h.notificationsView(s))
}

type notificationChoiceRequest struct {
	Email bool `json:"email"`
}

type quietHoursRequest struct {
	On           *bool   `json:"on"`
	From         *string `json:"from"`
	Until        *string `json:"until"`
	GatesThrough *bool   `json:"gates_through"`
}

type notificationSettingsRequest struct {
	Events          map[ledger.NotificationEvent]notificationChoiceRequest `json:"events"`
	QuietHours      *quietHoursRequest                                     `json:"quiet_hours"`
	ReviewAfterDays *int                                                   `json:"review_after_days"`
}

// PATCH /v2/me/notifications
func (h *Handler) updateNotificationSettings(w http.ResponseWriter, r *http.Request) {
	p, key, e := h.commandStart(r)
	if e != nil {
		writeError(w, e)
		return
	}
	if e := personOnly(p, "Notification settings"); e != nil {
		writeError(w, e)
		return
	}
	version, ok, e := ifMatch(r)
	if e != nil {
		writeError(w, invalidRequest("If-Match", `If-Match must be the settings' ETag, such as "3".`))
		return
	}
	if !ok {
		writeError(w, &apiError{status: http.StatusPreconditionRequired, code: codePreconditionRequired,
			message: `Send If-Match with the version you read (the settings' ETag, such as "3"), so a change made elsewhere since, such as an unsubscribe, isn't undone.`})
		return
	}
	var req notificationSettingsRequest
	if e := decodeBody(w, r, &req, true); e != nil {
		writeError(w, e)
		return
	}
	ch := ledger.NotificationChange{ReviewAfterDays: req.ReviewAfterDays}
	if len(req.Events) > 0 {
		ch.Email = make(map[ledger.NotificationEvent]bool, len(req.Events))
		for ev, c := range req.Events {
			ch.Email[ev] = c.Email
		}
	}
	if q := req.QuietHours; q != nil {
		ch.QuietOn, ch.QuietFrom, ch.QuietUntil, ch.GatesThrough = q.On, q.From, q.Until, q.GatesThrough
	}
	s, replayed, err := h.ledger.UpdateNotificationSettings(r.Context(), p.scope, ch, version, key)
	var clash *ledger.EditClashError
	if errors.As(err, &clash) {
		writeError(w, &apiError{status: http.StatusPreconditionFailed, code: codeEditClash,
			message: "Your notification settings changed since you opened them (perhaps an unsubscribe link, or another tab). Reload them and try again.",
			details: &errorDetails{ExpectedVersion: clash.Expected, CurrentVersion: clash.Current}})
		return
	}
	if err != nil {
		writeError(w, h.fromLedger(r, err))
		return
	}
	if replayed {
		w.Header().Set("Idempotent-Replayed", "true")
	}
	setVersionETag(w, s.Version)
	writeData(w, http.StatusOK, h.notificationsView(s))
}

// securityView is Settings › Security's data: this session's assurance and
// the deployment's posture.
type securityView struct {
	// Assurance is what a Keep from this session counts as: human_web only
	// for the web app's own signed requests (internal/websurface).
	Assurance  policy.Assurance  `json:"assurance"`
	Residency  []trust.Place     `json:"residency"`
	Processors []trust.Processor `json:"processors"`
	BackupDays int               `json:"backup_days"`
}

// GET /v2/security
func (h *Handler) getSecurity(w http.ResponseWriter, r *http.Request) {
	p, e := h.principalFor(r)
	if e != nil {
		writeError(w, e)
		return
	}
	if e := personOnly(p, "Security settings"); e != nil {
		writeError(w, e)
		return
	}
	posture := trust.Posture{Residency: trust.ParseResidency(""), BackupDays: ledger.DefaultBackupDays}
	if h.posture != nil {
		posture = *h.posture
	}
	assurance := policy.AssuranceClientAttested
	if p.via == policy.ViaWeb {
		assurance = policy.AssuranceHumanWeb
	}
	out := securityView{Assurance: assurance, Residency: nonNil(posture.Residency),
		Processors: nonNil(posture.Processors), BackupDays: posture.BackupDays}
	for i := range out.Processors {
		out.Processors[i].Uses = nonNil(out.Processors[i].Uses)
	}
	writeData(w, http.StatusOK, out)
}
