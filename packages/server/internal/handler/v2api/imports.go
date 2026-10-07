package v2api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/lifecycle"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// Spaces, imports and bulk review: what `memax init` needs (plan 25 §7.3),
// and what the onboarding screens (Cleanup, ReviewImport) will read.

// maxImportBody bounds an import's body: 500 statements of up to 2,000
// characters, each with its sources.
const maxImportBody = 8 << 20

type createSpaceRequest struct {
	Name       string           `json:"name"`
	Slug       string           `json:"slug"`
	Kind       policy.SpaceKind `json:"kind"`
	Repository string           `json:"repository"`
}

// POST /v2/spaces
func (h *Handler) createSpace(w http.ResponseWriter, r *http.Request) {
	p, key, e := h.commandStart(r)
	if e != nil {
		writeError(w, e)
		return
	}
	var req createSpaceRequest
	if e := decodeBody(w, r, &req, true); e != nil {
		writeError(w, e)
		return
	}
	sp, replayed, err := h.ledger.CreateSpace(r.Context(), p.actor, ledger.NewSpace{
		Name: req.Name, Slug: req.Slug, Kind: req.Kind, Repository: req.Repository, Key: key,
	})
	if err != nil {
		writeError(w, h.spaceError(r, err))
		return
	}
	if replayed {
		w.Header().Set("Idempotent-Replayed", "true")
	}
	writeData(w, http.StatusCreated, sp)
}

// POST /v2/spaces/{space}:switch
func (h *Handler) switchSpace(w http.ResponseWriter, r *http.Request) {
	p, _, e := h.commandStart(r)
	if e != nil {
		writeError(w, e)
		return
	}
	g, e := h.space(r, p, r.PathValue("space"))
	if e != nil {
		writeError(w, e)
		return
	}
	sp, err := h.ledger.SwitchSpace(r.Context(), p.actor, p.scope, g.SpaceID)
	if err != nil {
		writeError(w, h.spaceError(r, err))
		return
	}
	writeData(w, http.StatusOK, sp)
}

// spaceError maps CreateSpace's and SwitchSpace's errors.
func (h *Handler) spaceError(r *http.Request, err error) *apiError {
	var refused *ledger.SpaceRefusedError
	var notes *ledger.SpaceHasNotesError
	switch {
	case errors.As(err, &refused):
		return refusal(refused.Decision)
	case errors.As(err, &notes):
		return &apiError{status: http.StatusConflict, code: codeSpaceHasNotes, message: notes.Error(),
			details: &errorDetails{Notes: notes.Notes}}
	case errors.Is(err, ledger.ErrSlugTaken):
		return &apiError{status: http.StatusConflict, code: codeSlugTaken, message: err.Error(),
			details: &errorDetails{Field: "slug"}}
	}
	return h.fromLedger(r, err)
}

type importItemInput struct {
	Key              string                 `json:"key"`
	Ref              string                 `json:"ref"`
	Location         ledger.ImportLocation  `json:"location"`
	HiddenCharacters int                    `json:"hidden_characters"`
	Statement        string                 `json:"statement"`
	Section          ledger.Section         `json:"section"`
	Kind             ledger.Kind            `json:"kind"`
	Decision         *ledger.DecisionFields `json:"decision"`
	Sources          []sourceInput          `json:"sources"`
	Scope            json.RawMessage        `json:"scope"`
}

type importRequest struct {
	Client     string              `json:"client"`
	Files      []ledger.ImportFile `json:"files"`
	Skipped    []ledger.ImportSkip `json:"skipped"`
	Items      []importItemInput   `json:"items"`
	OccurredAt *time.Time          `json:"occurred_at"`
	SessionRef string              `json:"session_ref"`
}

type importResult struct {
	Import *ledger.Import            `json:"import"`
	Items  []ledger.ImportItemResult `json:"items"`
}

// POST /v2/spaces/{space}/imports
func (h *Handler) createImport(w http.ResponseWriter, r *http.Request) {
	p, key, e := h.commandStart(r)
	if e != nil {
		writeError(w, e)
		return
	}
	var req importRequest
	if e := decodeBodyMax(w, r, &req, true, maxImportBody); e != nil {
		writeError(w, e)
		return
	}
	sp, e := h.space(r, p, r.PathValue("space"))
	if e != nil {
		writeError(w, e)
		return
	}
	items := make([]ledger.ImportItem, len(req.Items))
	for i, it := range req.Items {
		items[i] = ledger.ImportItem{Key: it.Key, Ref: it.Ref, Location: it.Location, HiddenCharacters: it.HiddenCharacters,
			NewMemory: ledger.NewMemory{Statement: it.Statement, Section: it.Section, Kind: it.Kind, Decision: it.Decision,
				Sources: toSources(it.Sources), Applies: it.Scope}}
	}
	res, err := h.ledger.Import(r.Context(), ledger.ImportRequest{
		Meta:    p.meta(p.scope.Narrow(sp.SpaceID), key, commandFields{OccurredAt: req.OccurredAt, SessionRef: req.SessionRef}),
		SpaceID: sp.SpaceID, Client: req.Client, Files: req.Files, Skipped: req.Skipped, Items: items,
	})
	if err != nil {
		writeError(w, h.fromLedger(r, err))
		return
	}
	if res.Refused != nil {
		writeError(w, refusal(*res.Refused))
		return
	}
	if res.Replayed {
		w.Header().Set("Idempotent-Replayed", "true")
	}
	w.Header().Set("Location", "/v2/spaces/"+sp.SpaceID.String()+"/imports/"+res.Import.ID.String())
	writeData(w, http.StatusCreated, importResult{Import: res.Import, Items: nonNil(res.Items)})
}

// GET /v2/spaces/{space}/imports
func (h *Handler) listImports(w http.ResponseWriter, r *http.Request) {
	p, e := h.principalFor(r)
	if e != nil {
		writeError(w, e)
		return
	}
	cursor, limit, e := page(r)
	if e != nil {
		writeError(w, e)
		return
	}
	sp, e := h.space(r, p, r.PathValue("space"))
	if e != nil {
		writeError(w, e)
		return
	}
	pg, err := h.ledger.ListImports(r.Context(), p.scope, sp.SpaceID, cursor, limit)
	if err != nil {
		writeError(w, h.fromLedger(r, err))
		return
	}
	writeData(w, http.StatusOK, pg)
}

// GET /v2/spaces/{space}/imports/{import}
func (h *Handler) getImport(w http.ResponseWriter, r *http.Request) {
	p, e := h.principalFor(r)
	if e != nil {
		writeError(w, e)
		return
	}
	sp, e := h.space(r, p, r.PathValue("space"))
	if e != nil {
		writeError(w, e)
		return
	}
	id, err := uuid.Parse(r.PathValue("import"))
	if err != nil {
		writeError(w, notFound)
		return
	}
	v, err := h.ledger.GetImport(r.Context(), p.scope, sp.SpaceID, id)
	if err != nil {
		writeError(w, h.fromLedger(r, err))
		return
	}
	writeData(w, http.StatusOK, v)
}

type settleImportRequest struct {
	Choice    ledger.ImportChoice `json:"choice"`
	Keep      string              `json:"keep"`
	Statement string              `json:"statement"`
	commandFields
}

type importConflictResult struct {
	Outcome  ledger.Outcome        `json:"outcome"`
	Policy   policy.Decision       `json:"policy"`
	Conflict ledger.ImportConflict `json:"conflict"`
	Memories []ledger.Memory       `json:"memories"`
	Receipts []ledger.Receipt      `json:"receipts"`
}

// POST /v2/spaces/{space}/imports/{import}/conflicts/{n}:settle
func (h *Handler) settleImportConflict(w http.ResponseWriter, r *http.Request) {
	p, key, e := h.commandStart(r)
	if e != nil {
		writeError(w, e)
		return
	}
	var req settleImportRequest
	if e := decodeBody(w, r, &req, true); e != nil {
		writeError(w, e)
		return
	}
	sp, e := h.space(r, p, r.PathValue("space"))
	if e != nil {
		writeError(w, e)
		return
	}
	id, err := uuid.Parse(r.PathValue("import"))
	n, nerr := strconv.Atoi(r.PathValue("n"))
	if err != nil || nerr != nil || n < 1 {
		writeError(w, notFound)
		return
	}
	res, err := h.ledger.Apply(r.Context(), &ledger.SettleImportConflict{
		Meta: p.meta(p.scope.Narrow(sp.SpaceID), key, req.commandFields), SpaceID: sp.SpaceID, Import: id, N: n,
		Choice: req.Choice, Keep: req.Keep, Statement: req.Statement,
	})
	if err != nil {
		writeError(w, h.fromLedger(r, err))
		return
	}
	if res.Outcome == ledger.OutcomeRefused {
		writeError(w, refusal(res.Policy))
		return
	}
	v, err := h.ledger.GetImport(r.Context(), p.scope, sp.SpaceID, id)
	if err != nil {
		writeError(w, h.fromLedger(r, err))
		return
	}
	var conflict ledger.ImportConflict
	for _, c := range v.Conflicts {
		if c.N == n {
			conflict = c
		}
	}
	if res.Replayed {
		w.Header().Set("Idempotent-Replayed", "true")
	}
	writeData(w, http.StatusOK, importConflictResult{Outcome: res.Outcome, Policy: res.Policy, Conflict: conflict,
		Memories: nonNil(res.Memories), Receipts: nonNil(res.Receipts)})
}

type bulkReviewRequest struct {
	Items []struct {
		Memory  string `json:"memory"`
		Version int    `json:"version"`
	} `json:"items"`
	commandFields
}

type bulkOutcome string

const (
	bulkApplied bulkOutcome = "applied"
	bulkRefused bulkOutcome = "refused"
	bulkFailed  bulkOutcome = "failed"
)

type bulkError struct {
	Code    string        `json:"code"`
	Message string        `json:"message"`
	Details *errorDetails `json:"details,omitempty"`
}

type bulkItem struct {
	Memory  string           `json:"memory"`
	Ref     string           `json:"ref,omitempty"`
	Outcome bulkOutcome      `json:"outcome"`
	State   lifecycle.Mark   `json:"state,omitempty"`
	Version int              `json:"version,omitempty"`
	Policy  *policy.Decision `json:"policy,omitempty"`
	Error   *bulkError       `json:"error,omitempty"`
}

type bulkResult struct {
	Items   []bulkItem `json:"items"`
	Applied int        `json:"applied"`
	Refused int        `json:"refused"`
	Failed  int        `json:"failed"`
}

// maxBulkReview is how many proposals one bulk keep or reject takes.
const maxBulkReview = 200

// POST /v2/spaces/{space}/memories:keep
func (h *Handler) keepMemories(w http.ResponseWriter, r *http.Request) {
	h.bulkReview(w, r, "keep", func(m ledger.Meta, ref string, version int) ledger.Command {
		return &ledger.Keep{Meta: m, Memory: ref, ExpectedVersion: version}
	})
}

// POST /v2/spaces/{space}/memories:reject
func (h *Handler) rejectMemories(w http.ResponseWriter, r *http.Request) {
	h.bulkReview(w, r, "reject", func(m ledger.Meta, ref string, version int) ledger.Command {
		return &ledger.Reject{Meta: m, Memory: ref, ExpectedVersion: version}
	})
}

// bulkReview applies one Keep (or Reject) per item, each its own command
// with its own receipt and an idempotency key derived from the request's
// and the item, so a retry applies nothing twice. One item's failure is
// that item's outcome.
func (h *Handler) bulkReview(w http.ResponseWriter, r *http.Request, verb string, build func(ledger.Meta, string, int) ledger.Command) {
	p, key, e := h.commandStart(r)
	if e != nil {
		writeError(w, e)
		return
	}
	var req bulkReviewRequest
	if e := decodeBody(w, r, &req, true); e != nil {
		writeError(w, e)
		return
	}
	if len(req.Items) == 0 || len(req.Items) > maxBulkReview {
		writeError(w, invalidRequest("items", "Send 1 to "+strconv.Itoa(maxBulkReview)+" memories."))
		return
	}
	sp, e := h.space(r, p, r.PathValue("space"))
	if e != nil {
		writeError(w, e)
		return
	}
	scope := p.scope.Narrow(sp.SpaceID)
	out := bulkResult{Items: make([]bulkItem, 0, len(req.Items))}
	seen := map[string]bool{}
	for _, it := range req.Items {
		ref := strings.TrimSpace(it.Memory)
		item := bulkItem{Memory: it.Memory}
		if ref == "" || seen[strings.ToUpper(ref)] || it.Version < 0 {
			item.Outcome, item.Error = bulkFailed, &bulkError{Code: codeInvalidRequest,
				Message: "Name each memory once, by display ID or id."}
			out.Items, out.Failed = append(out.Items, item), out.Failed+1
			continue
		}
		seen[strings.ToUpper(ref)] = true
		meta := p.meta(scope, bulkKey(key, verb, ref), req.commandFields)
		res, err := h.ledger.Apply(r.Context(), build(meta, ref, it.Version))
		switch {
		case err != nil:
			ae := h.fromLedger(r, err)
			item.Outcome, item.Error = bulkFailed, &bulkError{Code: ae.code, Message: ae.message, Details: ae.details}
			out.Failed++
		case res.Outcome == ledger.OutcomeRefused:
			d := res.Policy
			item.Outcome, item.Policy = bulkRefused, &d
			out.Refused++
		default:
			item.Outcome = bulkApplied
			out.Applied++
		}
		if res.Memory != nil {
			item.Ref, item.State, item.Version = res.Memory.Ref, res.Memory.State, res.Memory.Version
		}
		out.Items = append(out.Items, item)
	}
	writeData(w, http.StatusOK, out)
}

// bulkKey is one item's idempotency key: the verb, the memory and the
// request's key, hashed so it fits whatever the request's length.
func bulkKey(key, verb, ref string) string {
	sum := sha256.Sum256([]byte(key))
	return "bulk-" + verb + ":" + strings.ToUpper(ref) + ":" + hex.EncodeToString(sum[:16])
}
