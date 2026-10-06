package v2api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/MemaxLabs/memax/packages/server/internal/ask"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// WithAsk serves POST /v2/spaces/{space}/ask from the Ask service. Without
// it (or with a nil one) the route answers 503 unavailable.
func WithAsk(s *ask.Service) Option { return func(h *Handler) { h.asker = s } }

type askRequest struct {
	Question string `json:"question"`
}

// POST /v2/spaces/{space}/ask
//
// Everything that can refuse or fail before the answer starts (the
// credential, the body, the space, policy and the plan limit, the search)
// is an ordinary JSON error. Then the stream begins, and only an error
// event can say the model failed (spec: AskEvent).
func (h *Handler) askSpace(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	p, e := h.principalFor(r)
	if e != nil {
		writeError(w, e)
		return
	}
	var req askRequest
	if e := decodeBody(w, r, &req, true); e != nil {
		writeError(w, e)
		return
	}
	question := strings.TrimSpace(req.Question)
	switch n := utf8.RuneCountInString(question); {
	case n == 0:
		writeError(w, invalidRequest("question", "Say what you want to know."))
		return
	case n > ask.MaxQuestionRunes:
		writeError(w, invalidRequest("question", "A question is at most 2000 characters."))
		return
	}
	sp, e := h.space(r, p, r.PathValue("space"))
	if e != nil {
		writeError(w, e)
		return
	}
	if h.asker == nil {
		writeError(w, &apiError{status: http.StatusServiceUnavailable, code: codeUnavailable,
			message: "Ask isn't configured on this server."})
		return
	}
	name, err := h.spaceName(r, p, sp)
	if err != nil {
		writeError(w, h.fromLedger(r, err))
		return
	}
	prep, err := h.asker.Prepare(r.Context(), ask.Request{
		Actor: p.actor, Via: p.via, Scope: p.scope, Space: sp, SpaceName: name, Question: question,
		Uncounted: p.impersonated, Started: started,
	})
	var refused *ask.Refusal
	switch {
	case errors.As(err, &refused):
		e := refusal(refused.Decision)
		if refused.Decision.Code == policy.CodeAskLimit {
			at := refused.ResetsAt
			e.details.Limit, e.details.Current, e.details.ResetAt = refused.Limit, refused.Limit, &at
		}
		writeError(w, e)
		return
	case errors.Is(err, ask.ErrSearchTimeout):
		writeError(w, &apiError{status: http.StatusServiceUnavailable, code: codeBusy, retryAfter: 1,
			message: "Searching the space took too long. Ask again in a moment.", details: &errorDetails{RetryAfter: 1}})
		return
	case err != nil:
		writeError(w, h.fromLedger(r, err))
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	// Proxies that buffer (nginx) pass the events through as they come.
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	rc := http.NewResponseController(w)
	h.asker.Answer(r.Context(), prep, func(event string, data any) error {
		b, err := json.Marshal(data)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b); err != nil {
			return err
		}
		return rc.Flush()
	})
}

// spaceName is the space's name, which the answer's instructions use.
func (h *Handler) spaceName(r *http.Request, p *principal, sp ledger.SpaceGrant) (string, error) {
	spaces, err := h.ledger.ListSpaces(r.Context(), p.scope.Narrow(sp.SpaceID))
	if err != nil {
		return "", err
	}
	for _, s := range spaces {
		if s.ID == sp.SpaceID {
			return s.Name, nil
		}
	}
	return "", ledger.ErrNotFound
}
