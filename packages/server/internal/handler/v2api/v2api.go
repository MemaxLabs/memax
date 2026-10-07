// Package v2api serves the /v2 HTTP API on top of internal/ledger.
//
// The contract is packages/server/openapi/v2.yaml, written first. Every
// route here is an operation there (TestRoutesMatchSpec), and every
// handler test runs its requests and responses through the spec
// (internal/contract), so an undocumented status, header or field fails
// the build rather than a client.
//
// The handlers are thin: they map the request's credential onto a ledger
// actor and scope (principal.go, the one place that happens), decode the
// body, call the ledger and encode its answer. They never touch V1 store
// methods for V2 data, and every response goes through handler.WriteJSON
// and the model.ApiResponse envelope.
package v2api

import (
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/ask"
	"github.com/MemaxLabs/memax/packages/server/internal/compile"
	"github.com/MemaxLabs/memax/packages/server/internal/deviceauth"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/receiptchain"
	"github.com/MemaxLabs/memax/packages/server/internal/sessions"
	"github.com/MemaxLabs/memax/packages/server/internal/trust"
	"github.com/MemaxLabs/memax/packages/server/internal/websurface"
)

// Handler serves /v2.
type Handler struct {
	ledger *ledger.Ledger
	// compile serves what sits around a compile (the preview, hand edits,
	// drift): artifacts and parse-back, with every record read and write
	// through the ledger. Nil when compiling isn't configured.
	compile *compile.Service
	log     *slog.Logger
	now     func() time.Time
	seen    seenTracker
	// web verifies the web app's signed requests; nil is disabled.
	web       *websurface.Verifier
	webWarned atomic.Int64
	// reads records agents' reads off the request path; nil records none.
	reads ledger.ReadRecorder
	// receiptKeys are the public keys checkpoints are signed with.
	receiptKeys receiptchain.Keyring
	// drafts embeds Remember's draft for the near-duplicate check (near.go);
	// nil checks exact repeats only. near rate-limits the check.
	drafts DraftEmbedder
	near   nearLimiter
	// asker answers ⌘K Ask (ask.go); nil answers 503.
	asker *ask.Service
	// dream queues run-now (dream.go); nil answers 503 there.
	dream *dreamDeps
	// exports rate-limits exports (export.go).
	exports exportLimiter
	// devices confirms the CLI's device codes (device.go); nil answers
	// 503. deviceMisses slows down guessing codes.
	devices      *deviceauth.Store
	deviceMisses deviceMisses
	// delivery says which notification emails this deployment sends, and
	// posture what Settings › Security says about it (settings.go).
	delivery NotificationDelivery
	posture  *trust.Posture
	// sessions lists and signs out a person's sessions (sessions.go); nil
	// answers 503.
	sessions *sessions.Store
}

// Option configures a Handler.
type Option func(*Handler)

// WithClock replaces time.Now (tests).
func WithClock(now func() time.Time) Option { return func(h *Handler) { h.now = now } }

// WithWebSurface lets requests signed by the web app's proxy count as made
// on the web (assurance human_web). nil, the default, disables it: every
// request is client-attested.
func WithWebSurface(v *websurface.Verifier) Option { return func(h *Handler) { h.web = v } }

// WithCompile serves the preview, observation and drift endpoints from the
// compile coordinator. Without it (or with a nil one) they answer 503.
func WithCompile(s *compile.Service) Option { return func(h *Handler) { h.compile = s } }

// New returns the /v2 handler. A nil ledger (no database) is allowed:
// every route then answers 503 unavailable, so the API says what is
// missing instead of 404ing.
func New(l *ledger.Ledger, log *slog.Logger, opts ...Option) *Handler {
	if log == nil {
		log = slog.Default()
	}
	h := &Handler{ledger: l, log: log, now: time.Now}
	for _, o := range opts {
		o(h)
	}
	return h
}

// Route is one operation of v2.yaml.
type Route struct {
	Method string
	// Path is the spec's path template, e.g. /v2/memories/{ref}:keep.
	Path        string
	OperationID string
	serve       func(*Handler, http.ResponseWriter, *http.Request)
}

var routes = []Route{
	{"GET", "/v2/spaces", "listSpaces", (*Handler).listSpaces},
	{"POST", "/v2/spaces", "createSpace", (*Handler).createSpace},
	{"GET", "/v2/spaces/{space}/switch", "getSpaceSwitch", (*Handler).getSpaceSwitch},
	{"POST", "/v2/spaces/{space}:switch", "switchSpace", (*Handler).switchSpace},
	{"GET", "/v2/spaces/{space}/v1-dream-runs", "listV1DreamRuns", (*Handler).listV1DreamRuns},
	{"GET", "/v2/spaces/{space}/notes", "searchNotes", (*Handler).searchNotes},
	{"GET", "/v2/spaces/{space}/notes/{note}", "getNote", (*Handler).getNote},
	{"GET", "/v2/spaces/{space}/notes/{note}/forget-preview", "previewForgetNote", (*Handler).previewForgetNote},
	{"POST", "/v2/spaces/{space}/notes/{note}:forget", "forgetNote", (*Handler).forgetNote},
	{"POST", "/v2/spaces/{space}:export", "exportSpace", (*Handler).exportSpace},
	{"POST", "/v2/spaces/{space}/memories", "rememberMemory", (*Handler).remember},
	{"GET", "/v2/spaces/{space}/memories", "listMemories", (*Handler).listMemories},
	{"POST", "/v2/spaces/{space}/memories:near-duplicates", "findNearDuplicates", (*Handler).findNearDuplicates},
	{"POST", "/v2/spaces/{space}/memories:keep", "keepMemories", (*Handler).keepMemories},
	{"POST", "/v2/spaces/{space}/memories:reject", "rejectMemories", (*Handler).rejectMemories},
	{"POST", "/v2/spaces/{space}/imports", "createImport", (*Handler).createImport},
	{"GET", "/v2/spaces/{space}/imports", "listImports", (*Handler).listImports},
	{"GET", "/v2/spaces/{space}/imports/{import}", "getImport", (*Handler).getImport},
	{"POST", "/v2/spaces/{space}/imports/{import}/conflicts/{n}:settle", "settleImportConflict", (*Handler).settleImportConflict},
	{"POST", "/v2/spaces/{space}/ask", "askSpace", (*Handler).askSpace},
	{"GET", "/v2/spaces/{space}/review", "listReview", (*Handler).listReview},
	{"GET", "/v2/spaces/{space}/receipts", "listReceipts", (*Handler).listReceipts},
	{"GET", "/v2/spaces/{space}/checkpoints", "listCheckpoints", (*Handler).listCheckpoints},
	{"GET", "/v2/spaces/{space}/reads", "listReads", (*Handler).listReads},
	{"POST", "/v2/spaces/{space}/compile-loads", "recordCompileLoad", (*Handler).recordCompileLoad},
	{"GET", "/v2/memories/{ref}", "getMemory", (*Handler).getMemory},
	{"POST", "/v2/memories/{ref}:keep", "keepMemory", (*Handler).keep},
	{"POST", "/v2/memories/{ref}:edit", "editMemory", (*Handler).edit},
	{"POST", "/v2/memories/{ref}:reject", "rejectMemory", (*Handler).reject},
	{"GET", "/v2/memories/{ref}/conflict", "getConflict", (*Handler).getConflict},
	{"POST", "/v2/memories/{ref}:resolve-conflict", "resolveConflict", (*Handler).resolveConflict},
	{"POST", "/v2/memories/{ref}:forget", "forgetMemory", (*Handler).forgetMemory},
	{"POST", "/v2/memories/{ref}:request-forget", "requestForget", (*Handler).requestForget},
	{"POST", "/v2/memories/{ref}:decline-forget", "declineForget", (*Handler).declineForget},
	{"GET", "/v2/memories/{ref}/forget-preview", "previewForget", (*Handler).previewForget},
	{"GET", "/v2/memories/{ref}/tombstone", "getTombstone", (*Handler).getTombstone},
	{"GET", "/v2/spaces/{space}/tombstones", "listTombstones", (*Handler).listTombstones},
	{"POST", "/v2/receipts/{receipt}:undo", "undoReceipt", (*Handler).undoReceipt},
	{"GET", "/v2/agents", "listAgents", (*Handler).listAgents},
	{"GET", "/v2/spaces/{space}/agents", "listSpaceAgents", (*Handler).listSpaceAgents},
	{"GET", "/v2/agents/{agent}", "getAgent", (*Handler).getAgent},
	{"PATCH", "/v2/agents/{agent}/spaces/{space}", "setAgentAutonomy", (*Handler).setAgentAutonomy},
	{"POST", "/v2/agents/{agent}:pause", "pauseAgent", (*Handler).pauseAgent},
	{"POST", "/v2/agents/{agent}:resume", "resumeAgent", (*Handler).resumeAgent},
	{"POST", "/v2/agents/{agent}:disconnect", "disconnectAgent", (*Handler).disconnectAgent},
	{"GET", "/v2/spaces/{space}/brief", "getBrief", (*Handler).getBrief},
	{"POST", "/v2/spaces/{space}/brief", "reviseBrief", (*Handler).reviseBrief},
	{"GET", "/v2/spaces/{space}/brief/versions", "listBriefVersions", (*Handler).listBriefVersions},
	{"GET", "/v2/spaces/{space}/targets", "listTargets", (*Handler).listTargets},
	{"POST", "/v2/spaces/{space}/targets", "createTarget", (*Handler).createTarget},
	{"PATCH", "/v2/targets/{target}", "configureTarget", (*Handler).configureTarget},
	{"POST", "/v2/targets/{target}:compile", "compileTarget", (*Handler).compileTarget},
	{"GET", "/v2/targets/{target}/preview", "getTargetPreview", (*Handler).getTargetPreview},
	{"GET", "/v2/targets/{target}/runs", "listCompileRuns", (*Handler).listCompileRuns},
	{"POST", "/v2/targets/{target}/observations", "recordObservation", (*Handler).recordObservation},
	{"POST", "/v2/targets/{target}/deliveries", "recordDelivery", (*Handler).recordDelivery},
	{"GET", "/v2/targets/{target}/drift", "getDrift", (*Handler).getDrift},
	{"POST", "/v2/targets/{target}/drift:pull", "pullDrift", (*Handler).pullDrift},
	{"POST", "/v2/targets/{target}/drift:overwrite", "overwriteDrift", (*Handler).overwriteDrift},
	{"POST", "/v2/targets/{target}/drift:stop", "stopDrift", (*Handler).stopDrift},
	{"GET", "/v2/spaces/{space}/gates", "listGates", (*Handler).listGates},
	{"POST", "/v2/spaces/{space}/gates", "requestDecision", (*Handler).requestDecision},
	{"GET", "/v2/gates/{ref}", "getGate", (*Handler).getGate},
	{"POST", "/v2/gates/{ref}:answer", "answerGate", (*Handler).answerGate},
	{"POST", "/v2/gates/{ref}:withdraw", "withdrawGate", (*Handler).withdrawGate},
	{"GET", "/v2/spaces/{space}/dream/editions", "listEditions", (*Handler).listEditions},
	{"GET", "/v2/spaces/{space}/dream/editions/{edition}", "getEdition", (*Handler).getEdition},
	{"GET", "/v2/spaces/{space}/dream/editions/{edition}/actions", "listDreamActions", (*Handler).listDreamActions},
	{"POST", "/v2/spaces/{space}/dream/editions/{edition}:undo", "undoEdition", (*Handler).undoEdition},
	{"POST", "/v2/spaces/{space}/dream:run", "runDream", (*Handler).runDream},
	{"POST", "/v2/dream/actions/{action}:undo", "undoDreamAction", (*Handler).undoDreamAction},
	{"POST", "/v2/memories/{ref}:restore", "restoreMemory", (*Handler).restoreMemory},
	{"GET", "/v2/dream/settings", "getDreamSettings", (*Handler).getDreamSettings},
	{"PATCH", "/v2/dream/settings", "updateDreamSettings", (*Handler).updateDreamSettings},
	{"POST", "/v2/dream/email:unsubscribe", "unsubscribeDreamEmail", (*Handler).unsubscribeDreamEmail},
	{"GET", "/v2/me/notifications", "getNotificationSettings", (*Handler).getNotificationSettings},
	{"PATCH", "/v2/me/notifications", "updateNotificationSettings", (*Handler).updateNotificationSettings},
	{"GET", "/v2/security", "getSecurity", (*Handler).getSecurity},
	{"GET", "/v2/notices", "listNotices", (*Handler).listNotices},
	{"POST", "/v2/notices:ack", "ackNotices", (*Handler).ackNotices},
	{"POST", "/v2/device-authorizations:lookup", "lookupDeviceAuthorization", (*Handler).lookupDevice},
	{"POST", "/v2/device-authorizations:approve", "approveDeviceAuthorization", (*Handler).approveDevice},
	{"POST", "/v2/device-authorizations:deny", "denyDeviceAuthorization", (*Handler).denyDevice},
	{"GET", "/v2/sessions", "listSessions", (*Handler).listSessions},
	{"POST", "/v2/sessions/{session}:revoke", "revokeSession", (*Handler).revokeSession},
	{"POST", "/v2/sessions:revoke-others", "revokeOtherSessions", (*Handler).revokeOtherSessions},
}

// Routes lists every /v2 operation this package serves, named as in
// v2.yaml.
func Routes() []Route { return slices.Clone(routes) }

// Mount registers /v2 on mux behind wrap, the same auth middleware chain
// /v1 uses (RequireAuth → HubContext → AuthorizeHTTP → RateLimit → Meter).
//
// The few public routes (publicRoutes: the morning email's one-click
// unsubscribe, whose token is the credential) are mounted outside wrap,
// behind public when it is given (an IP rate limit).
func (h *Handler) Mount(mux *http.ServeMux, wrap func(http.Handler) http.Handler, public ...func(http.Handler) http.Handler) {
	mux.Handle("/v2/", wrap(h.routes()))
	for _, rt := range routes {
		if !publicRoutes[rt.OperationID] {
			continue
		}
		var hd http.Handler = h.serve(rt)
		for _, p := range public {
			hd = p(hd)
		}
		mux.Handle(rt.Method+" "+rt.Path, hd)
	}
}

// publicRoutes are the operations served without a credential (v2.yaml
// says `security: []` for each).
var publicRoutes = map[string]bool{"unsubscribeDreamEmail": true}

// routes builds the /v2 mux. ServeMux wildcards must fill a whole path
// segment, so the custom-method routes (/v2/memories/{ref}:keep) share
// one pattern, POST /v2/memories/{ref}, and dispatch on the suffix. Every
// path also gets a method-less pattern that answers 405 in the envelope,
// and anything else under /v2/ answers 404 in the envelope.
func (h *Handler) routes() *http.ServeMux {
	mux := http.NewServeMux()
	verbs := map[string]map[string]Route{} // "POST /v2/memories/{ref}" → "keep" → route
	allowed := map[string][]string{}       // "/v2/memories/{ref}" → methods
	for _, rt := range routes {
		path, verb, _ := strings.Cut(rt.Path, "}:")
		if verb != "" {
			path += "}"
		}
		pattern := rt.Method + " " + path
		if !slices.Contains(allowed[path], rt.Method) {
			allowed[path] = append(allowed[path], rt.Method)
		}
		if verb == "" {
			mux.HandleFunc(pattern, h.serve(rt))
			continue
		}
		if verbs[pattern] == nil {
			verbs[pattern] = map[string]Route{}
		}
		verbs[pattern][verb] = rt
	}
	for pattern, byVerb := range verbs {
		path := strings.TrimPrefix(pattern, "POST ")
		param := path[strings.LastIndexByte(path, '{')+1 : len(path)-1]
		others := slices.DeleteFunc(slices.Clone(allowed[path]), func(m string) bool { return m == http.MethodPost })
		mux.HandleFunc(pattern, h.dispatch(param, others, byVerb))
	}
	for path, methods := range allowed {
		mux.HandleFunc(path, methodNotAllowed(methods))
	}
	mux.HandleFunc("/v2/", func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, &apiError{status: http.StatusNotFound, code: codeNotFound,
			message: "There is no such /v2 endpoint. See https://docs.memax.app for the API reference."})
	})
	return mux
}

func (h *Handler) serve(rt Route) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.ledger == nil {
			writeError(w, &apiError{status: http.StatusServiceUnavailable, code: codeUnavailable,
				message: "The V2 record needs a database, and this server has none. Set DATABASE_URL."})
			return
		}
		rt.serve(h, w, r)
	}
}

// dispatch serves the custom methods of one pattern: it splits the
// wildcard ("M-0219:keep") into the value and the verb. A POST with no
// verb is 405, allowing the path's other methods.
func (h *Handler) dispatch(param string, others []string, byVerb map[string]Route) http.HandlerFunc {
	verbs := make([]string, 0, len(byVerb))
	for v := range byVerb {
		verbs = append(verbs, ":"+v)
	}
	slices.Sort(verbs)
	slices.Sort(others)
	return func(w http.ResponseWriter, r *http.Request) {
		raw := r.PathValue(param)
		i := strings.LastIndexByte(raw, ':')
		if i < 0 {
			w.Header().Set("Allow", strings.Join(others, ", "))
			writeError(w, &apiError{status: http.StatusMethodNotAllowed, code: codeMethodNotAllowed,
				message: "POST one of its commands instead: " + strings.Join(verbs, ", ") + "."})
			return
		}
		rt, ok := byVerb[raw[i+1:]]
		if !ok {
			writeError(w, &apiError{status: http.StatusNotFound, code: codeNotFound,
				message: "Unknown command " + raw[i:] + ". Use " + strings.Join(verbs, ", ") + "."})
			return
		}
		r.SetPathValue(param, raw[:i])
		h.serve(rt)(w, r)
	}
}

func methodNotAllowed(methods []string) http.HandlerFunc {
	sorted := slices.Clone(methods)
	slices.Sort(sorted)
	allow := strings.Join(sorted, ", ")
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Allow", allow)
		writeError(w, &apiError{status: http.StatusMethodNotAllowed, code: codeMethodNotAllowed,
			message: r.Method + " isn't supported here. Use " + allow + "."})
	}
}
