package v2api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/handler"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// The wire shapes. Items (Memory, Receipt, Source, MemoryVersion, Space)
// are the ledger's own types, whose JSON tags follow v2.yaml; these are
// the pages, results and request bodies around them.

type spaceList struct {
	Items []ledger.Space `json:"items"`
}

type memoryPage struct {
	Items      []ledger.Memory `json:"items"`
	HasMore    bool            `json:"has_more"`
	NextCursor string          `json:"next_cursor,omitempty"`
}

type reviewPage struct {
	Items      []ledger.Memory `json:"items"`
	HasMore    bool            `json:"has_more"`
	NextCursor string          `json:"next_cursor,omitempty"`
	Total      int             `json:"total"`
}

type receiptPage struct {
	Items      []ledger.Receipt `json:"items"`
	HasMore    bool             `json:"has_more"`
	NextCursor string           `json:"next_cursor,omitempty"`
}

func toReceiptPage(p ledger.ReceiptPage) receiptPage {
	return receiptPage{Items: nonNil(p.Receipts), HasMore: p.HasMore, NextCursor: p.NextCursor}
}

type memoryDetail struct {
	Memory   *ledger.Memory         `json:"memory"`
	Versions []ledger.MemoryVersion `json:"versions"`
	Receipts receiptPage            `json:"receipts"`
	Reads    *ledger.MemoryReads    `json:"reads,omitempty"`
	// ForgetRequests wait for a person to forget it, or keep it.
	ForgetRequests []ledger.ForgetRequest `json:"forget_requests,omitempty"`
}

// commandResult is ledger.Result without Replayed: a replay returns the
// same body, and says so in the Idempotent-Replayed header instead.
type commandResult struct {
	Outcome  ledger.Outcome   `json:"outcome"`
	Policy   policy.Decision  `json:"policy"`
	Memory   *ledger.Memory   `json:"memory"`
	Receipts []ledger.Receipt `json:"receipts"`
}

// commandFields are what every command body may carry (ledger.Meta).
type commandFields struct {
	Reason     string     `json:"reason"`
	OccurredAt *time.Time `json:"occurred_at"`
	SessionRef string     `json:"session_ref"`
}

type sourceInput struct {
	Kind        ledger.SourceKind `json:"kind"`
	Ref         string            `json:"ref"`
	URI         string            `json:"uri"`
	Locator     json.RawMessage   `json:"locator"`
	External    bool              `json:"external"`
	Trust       policy.Trust      `json:"trust"`
	Quote       string            `json:"quote"`
	ContentHash string            `json:"content_hash"`
}

type rememberRequest struct {
	Statement  string                 `json:"statement"`
	Section    ledger.Section         `json:"section"`
	Kind       ledger.Kind            `json:"kind"`
	Decision   *ledger.DecisionFields `json:"decision"`
	Sources    []sourceInput          `json:"sources"`
	StaleAfter *time.Time             `json:"stale_after"`
	Conditions json.RawMessage        `json:"conditions"`
	Scope      json.RawMessage        `json:"scope"`
	ValidFrom  *time.Time             `json:"valid_from"`
	ValidTo    *time.Time             `json:"valid_to"`
	commandFields
}

func (req *rememberRequest) newMemory(spaceID uuid.UUID) ledger.NewMemory {
	nm := ledger.NewMemory{
		SpaceID: spaceID, Statement: req.Statement, Section: req.Section, Kind: req.Kind,
		Decision: req.Decision, StaleAfter: req.StaleAfter, Conditions: req.Conditions,
		Applies: req.Scope, ValidFrom: req.ValidFrom, ValidTo: req.ValidTo,
	}
	nm.Sources = toSources(req.Sources)
	return nm
}

// toSources maps the request's sources onto the ledger's. The ledger holds
// a source's class to what the actor could write itself (an agent's own
// work at most, for an agent), and keeps URL, email and issue sources
// external whatever the request says.
func toSources(in []sourceInput) []ledger.SourceInput {
	var out []ledger.SourceInput
	for _, s := range in {
		src := ledger.SourceInput{Kind: s.Kind, Ref: s.Ref, URI: s.URI, Locator: s.Locator, Quote: s.Quote,
			ContentHash: s.ContentHash, Trust: s.Trust}
		if s.External {
			src.Trust = policy.TrustExternal
		}
		out = append(out, src)
	}
	return out
}

type editRequest struct {
	Statement string         `json:"statement"`
	Section   ledger.Section `json:"section"`
	Keep      bool           `json:"keep"`
	commandFields
}

type reviewRequest struct {
	commandFields
}

type resolveConflictRequest struct {
	Choice         ledger.ConflictChoice `json:"choice"`
	Other          string                `json:"other"`
	Statement      string                `json:"statement"`
	OtherStatement string                `json:"other_statement"`
	commandFields
}

// memoriesResult is a command that changed several memories (settling a
// conflict, an undo).
type memoriesResult struct {
	Outcome  ledger.Outcome   `json:"outcome"`
	Policy   policy.Decision  `json:"policy"`
	Memory   *ledger.Memory   `json:"memory"`
	Memories []ledger.Memory  `json:"memories"`
	Receipts []ledger.Receipt `json:"receipts"`
}

// maxBody bounds a command body: 20 sources with 4000-character quotes
// fit with room to spare.
const maxBody = 1 << 20

// decodeBody reads a JSON body into dst, refusing unknown fields so a
// misspelt field is an error rather than silently ignored.
func decodeBody(w http.ResponseWriter, r *http.Request, dst any, required bool) *apiError {
	return decodeBodyMax(w, r, dst, required, maxBody)
}

// decodeBodyMax is decodeBody with its own size bound (an import's body).
func decodeBodyMax(w http.ResponseWriter, r *http.Request, dst any, required bool, limit int64) *apiError {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	dec.DisallowUnknownFields()
	err := dec.Decode(dst)
	var tooLarge *http.MaxBytesError
	var typeErr *json.UnmarshalTypeError
	switch {
	case errors.Is(err, io.EOF):
		if required {
			return invalidRequest("body", "Send a JSON body.")
		}
		return nil
	case errors.As(err, &tooLarge):
		return invalidRequest("body", fmt.Sprintf("The body is too large; send at most %d MiB.", limit>>20))
	case errors.As(err, &typeErr):
		return invalidRequest(typeErr.Field, typeErr.Field+" has the wrong type; see the API reference.")
	case err != nil && strings.HasPrefix(err.Error(), "json: unknown field "):
		field := strings.Trim(strings.TrimPrefix(err.Error(), "json: unknown field "), `"`)
		return invalidRequest(field, field+" is not a field of this request.")
	case err != nil:
		return invalidRequest("body", "The body isn't valid JSON.")
	}
	if dec.More() {
		return invalidRequest("body", "Send one JSON object.")
	}
	return nil
}

// meta builds a command's envelope.
func (p *principal) meta(scope ledger.Scope, key string, f commandFields) ledger.Meta {
	m := ledger.Meta{
		Actor: p.actor, Scope: scope, Via: p.via, IdempotencyKey: key,
		Reason: f.Reason, SessionRef: f.SessionRef,
	}
	if f.OccurredAt != nil {
		m.OccurredAt = *f.OccurredAt
	}
	return m
}

// space resolves a space key (id or slug) within the caller's scope. A
// space outside it is not found, never forbidden. A key shaped like a
// uuid that isn't one of the caller's space ids may still be a slug: V1
// named every personal space after its owner (slug = the owner's user id).
func (h *Handler) space(r *http.Request, p *principal, key string) (ledger.SpaceGrant, *apiError) {
	if id, err := uuid.Parse(key); err == nil {
		if g, ok := p.scope.Grant(id); ok {
			return g, nil
		}
	}
	// The scope carries each space's slug, read with it from the same
	// hubs rows v2.spaces shows: no round trip. A scope without slugs
	// (built some other way) asks the record.
	for _, g := range p.scope.Spaces {
		if g.Slug == key {
			return g, nil
		}
	}
	spaces, err := h.ledger.ListSpaces(r.Context(), p.scope)
	if err != nil {
		return ledger.SpaceGrant{}, h.fromLedger(r, err)
	}
	for _, sp := range spaces {
		if sp.Slug == key {
			g, _ := p.scope.Grant(sp.ID)
			return g, nil
		}
	}
	return ledger.SpaceGrant{}, notFound
}

// target resolves the {ref} of /v2/memories/{ref} and its ?space=
// context into the ref and the scope to look it up in. A display ID needs
// its space, because display IDs repeat across tenants; a uuid doesn't.
func (h *Handler) target(r *http.Request, p *principal) (string, ledger.Scope, *apiError) {
	ref := r.PathValue("ref")
	spaceKey := r.URL.Query().Get("space")
	if _, err := uuid.Parse(ref); err != nil {
		if _, _, ok := ledger.ParseRef(ref); !ok {
			return "", ledger.Scope{}, invalidRequest("ref", "Use a display ID like M-0219 or a memory id.")
		}
		if spaceKey == "" {
			return "", ledger.Scope{}, &apiError{status: http.StatusBadRequest, code: codeSpaceRequired,
				message: ref + " is unique only within its space. Add ?space= with the space's id or slug, or use the memory's id."}
		}
	}
	if spaceKey == "" {
		return ref, p.scope, nil
	}
	g, apiErr := h.space(r, p, spaceKey)
	if apiErr != nil {
		return "", ledger.Scope{}, apiErr
	}
	return ref, p.scope.Narrow(g.SpaceID), nil
}

// page reads ?cursor= and ?limit=.
func page(r *http.Request) (string, int, *apiError) {
	q := r.URL.Query()
	limit := 0
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			return "", 0, invalidRequest("limit", "limit must be a whole number from 1 to 200.")
		}
		limit = n
	}
	return q.Get("cursor"), limit, nil
}

// ifMatch reads If-Match as a memory version: `"3"`, or a bare 3.
func ifMatch(r *http.Request) (int, bool, *apiError) {
	raw := strings.TrimSpace(r.Header.Get("If-Match"))
	if raw == "" {
		return 0, false, nil
	}
	tag := raw
	if len(tag) >= 2 && tag[0] == '"' && tag[len(tag)-1] == '"' {
		tag = tag[1 : len(tag)-1]
	}
	n, err := strconv.Atoi(tag)
	if err != nil || n < 1 {
		return 0, false, invalidRequest("If-Match", `If-Match must be the memory's ETag, such as "3".`)
	}
	return n, true, nil
}

func setETag(w http.ResponseWriter, m *ledger.Memory) {
	if m != nil {
		w.Header().Set("ETag", strconv.Quote(strconv.Itoa(m.Version)))
	}
}

func writeData(w http.ResponseWriter, status int, data any) {
	handler.WriteJSON(w, status, model.ApiResponse{Data: data})
}

// nonNil makes an empty list encode as [] rather than null.
func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}
