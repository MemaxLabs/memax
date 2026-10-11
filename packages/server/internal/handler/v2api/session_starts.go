package v2api

import (
	"net/http"
	"regexp"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/compile"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// Cloud session starts (cloud warm-start design §3.4): a cloud agent's
// sandbox names the compile each committed file holds, and the answer is
// what changed in the record since. The ledger reads the runs, forgets,
// gates and notices and counts the loads (ledger.StartSession); the
// compile service diffs the words from its stored artifacts
// (compile.Service.Delta). The lines sent are a digest read, recorded
// after the response.

// sessionStartRequest is SessionStartRequest. The commit and the branch
// are checked and dropped: they select nothing, and aren't stored.
type sessionStartRequest struct {
	Agent      string                  `json:"agent"`
	SessionRef string                  `json:"session_ref"`
	Source     string                  `json:"source"`
	Files      []sessionStartFileInput `json:"files"`
	Commit     string                  `json:"commit"`
	Branch     string                  `json:"branch"`
	LoadedAt   *time.Time              `json:"loaded_at"`
}

type sessionStartFileInput struct {
	Path        string `json:"path"`
	Compile     string `json:"compile"`
	DriftSHA256 string `json:"drift_sha256"`
}

type sessionStart struct {
	Space     sessionStartSpace    `json:"space"`
	Files     []sessionStartFile   `json:"files"`
	Forgotten []sessionStartForget `json:"forgotten"`
	Gates     []sessionStartGate   `json:"gates"`
	Notices   []ledger.Notice      `json:"notices"`
	Handoffs  []sessionStartHandle `json:"handoffs"`
}

type sessionStartSpace struct {
	ID   uuid.UUID `json:"id"`
	Slug string    `json:"slug"`
}

type sessionStartFile struct {
	Path       string              `json:"path"`
	Here       string              `json:"here"`
	Latest     string              `json:"latest"`
	HandEdited bool                `json:"hand_edited"`
	Added      []compile.DeltaLine `json:"added"`
	Changed    []compile.DeltaLine `json:"changed"`
	Gone       []string            `json:"gone"`
	More       int                 `json:"more"`
}

type sessionStartForget struct {
	Ref     string    `json:"ref"`
	With    []string  `json:"with"`
	At      time.Time `json:"at"`
	InFiles bool      `json:"in_files"`
}

type sessionStartGate struct {
	Ref       string    `json:"ref"`
	Question  string    `json:"question"`
	Agent     string    `json:"agent,omitempty"`
	ExpiresAt time.Time `json:"expires_at"`
}

// sessionStartHandle is a handoff addressed to the session's branch:
// always none until handoffs ship (plan 25 Phase 4.4).
type sessionStartHandle struct {
	Ref string `json:"ref"`
}

// POST /v2/spaces/{space}/session-starts
func (h *Handler) startSession(w http.ResponseWriter, r *http.Request) {
	p, key, e := h.commandStart(r)
	if e != nil {
		writeError(w, e)
		return
	}
	var req sessionStartRequest
	if e := decodeBody(w, r, &req, true); e != nil {
		writeError(w, e)
		return
	}
	if e := req.check(); e != nil {
		writeError(w, e)
		return
	}
	sp, e := h.space(r, p, r.PathValue("space"))
	if e != nil {
		writeError(w, e)
		return
	}
	if h.compile == nil {
		writeError(w, &apiError{status: http.StatusServiceUnavailable, code: codeUnavailable,
			message: "Compiling isn't configured on this server, so there is no compile to compare with."})
		return
	}
	start := ledger.SessionStart{Actor: p.actor, Scope: p.scope, Via: p.via, SpaceID: sp.SpaceID,
		Agent: req.Agent, SessionRef: req.SessionRef, Source: ledger.StartSource(req.Source), IdempotencyKey: key}
	if p.connection != nil {
		start.Connection = p.connection.ID
	}
	if req.LoadedAt != nil {
		start.LoadedAt = *req.LoadedAt
	}
	for _, f := range req.Files {
		start.Files = append(start.Files, ledger.StartFile{Path: f.Path, Compile: f.Compile, DriftSHA256: f.DriftSHA256})
	}
	v, err := h.ledger.StartSession(r.Context(), start)
	if err != nil {
		writeError(w, h.fromLedger(r, err))
		return
	}

	out := sessionStart{Space: sessionStartSpace{ID: sp.SpaceID, Slug: sp.Slug}, Files: []sessionStartFile{},
		Forgotten: []sessionStartForget{}, Gates: []sessionStartGate{}, Notices: nonNil(v.Notices),
		Handoffs: []sessionStartHandle{}}
	var sent []uuid.UUID
	for _, f := range v.Files {
		d, err := h.compile.Delta(r.Context(), f.Here, f.Latest, f.Path, v.ForgottenRefs)
		if err != nil {
			if r.Context().Err() != nil {
				writeError(w, h.fromLedger(r, r.Context().Err()))
				return
			}
			// The file's compiles stand; only their words are missing.
			h.log.WarnContext(r.Context(), "v2: a session start's delta couldn't be read", "space_id", sp.SpaceID.String(),
				"here", f.Here.Ref, "latest", f.Latest.Ref, "error", err)
			d = compile.FileDelta{Added: []compile.DeltaLine{}, Changed: []compile.DeltaLine{}, Gone: []string{}}
		}
		out.Files = append(out.Files, sessionStartFile{Path: f.Path, Here: f.Here.Ref, Latest: f.Latest.Ref,
			HandEdited: f.HandEdited, Added: d.Added, Changed: d.Changed, Gone: d.Gone, More: d.More})
		for _, ls := range [][]compile.DeltaLine{d.Added, d.Changed} {
			for _, l := range ls {
				for _, ref := range l.Refs {
					if id, ok := v.MemoryIDs[ref]; ok && !slices.Contains(sent, id) {
						sent = append(sent, id)
					}
				}
			}
		}
	}
	for _, f := range v.Forgotten {
		out.Forgotten = append(out.Forgotten, sessionStartForget{Ref: f.Ref, With: nonNil(f.With), At: f.At, InFiles: f.InFiles})
	}
	for _, g := range v.Gates {
		out.Gates = append(out.Gates, sessionStartGate{Ref: g.Ref, Question: g.Question, Agent: g.Agent, ExpiresAt: g.ExpiresAt})
	}
	writeData(w, http.StatusOK, out)

	// The lines sent are a digest read: an agent's, where it is connected
	// and this start counted.
	if v.Counted && len(sent) > 0 {
		h.recordStartRead(p, sp, req.SessionRef, sent)
	}
}

var commitPattern = regexp.MustCompile(`^[0-9a-f]{7,64}$`)

// check is what the spec says of a start beyond its fields' types.
func (req *sessionStartRequest) check() *apiError {
	if n := len(req.Files); n < 1 || n > ledger.MaxStartFiles {
		return invalidRequest("files", "Name the compiled files the session loaded: 1 to 8 of them.")
	}
	if req.Commit != "" && !commitPattern.MatchString(req.Commit) {
		return invalidRequest("commit", "Send the commit as 7 to 64 lowercase hex characters.")
	}
	if len(req.Branch) > 255 {
		return invalidRequest("branch", "The branch is longer than 255 characters.")
	}
	return nil
}

// recordStartRead hands the start's digest read to the recorder.
func (h *Handler) recordStartRead(p *principal, g ledger.SpaceGrant, session string, memories []uuid.UUID) {
	if h.reads == nil || p.actor.Kind != policy.ActorAgent || p.connection == nil ||
		g.AgentStatus == policy.AgentNotConnected {
		return
	}
	h.reads.Record(ledger.ReadEvent{
		SpaceID: g.SpaceID, TenantID: g.TenantID, Reader: ledger.ReaderAgent, ConnectionID: p.connection.ID,
		PersonID: p.scope.PersonID, Agent: string(p.connection.Agent), Kind: ledger.ReadDigest, Via: p.via,
		SessionRef: session, Memories: memories, At: h.now(),
	})
}
