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
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
)

// Handler serves /v2.
type Handler struct {
	ledger *ledger.Ledger
	log    *slog.Logger
	now    func() time.Time
	seen   seenTracker
}

// Option configures a Handler.
type Option func(*Handler)

// WithClock replaces time.Now (tests).
func WithClock(now func() time.Time) Option { return func(h *Handler) { h.now = now } }

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
	{"POST", "/v2/spaces/{space}/memories", "rememberMemory", (*Handler).remember},
	{"GET", "/v2/spaces/{space}/memories", "listMemories", (*Handler).listMemories},
	{"GET", "/v2/spaces/{space}/review", "listReview", (*Handler).listReview},
	{"GET", "/v2/spaces/{space}/receipts", "listReceipts", (*Handler).listReceipts},
	{"GET", "/v2/memories/{ref}", "getMemory", (*Handler).getMemory},
	{"POST", "/v2/memories/{ref}:keep", "keepMemory", (*Handler).keep},
	{"POST", "/v2/memories/{ref}:edit", "editMemory", (*Handler).edit},
	{"POST", "/v2/memories/{ref}:reject", "rejectMemory", (*Handler).reject},
}

// Routes lists every /v2 operation this package serves, named as in
// v2.yaml.
func Routes() []Route { return slices.Clone(routes) }

// Mount registers /v2 on mux behind wrap, the same auth middleware chain
// /v1 uses (RequireAuth → HubContext → AuthorizeHTTP → RateLimit → Meter).
func (h *Handler) Mount(mux *http.ServeMux, wrap func(http.Handler) http.Handler) {
	mux.Handle("/v2/", wrap(h.routes()))
}

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
