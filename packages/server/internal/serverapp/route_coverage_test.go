package serverapp

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// TestProtectedRoutesAreMounted regression-guards the class of bug
// where a handler gets registered on an auth-guarded inner mux
// (either the shared `protected` sub-mux or a dedicated
// authMiddleware(protected) handoff) but the corresponding root-
// mux mount is forgotten. Symptom: a 404 on the real endpoint
// while tsc, go vet, and unit tests all pass — the handler
// compiles and works when invoked directly, but traffic never
// reaches it.
//
// We've already paid for this regression twice:
//   - /v1/usage 404 (commit 63b03b4b)
//   - /v1/auth/api-keys PATCH 405 (the motivation for
//     TestAPIKeyPatchRouteIsReachable in routes_test.go)
//
// Static analysis over routes.go:
//  1. Collect every HandleFunc call registering a path under /v1/
//     anywhere on the `protected` mux (direct calls via the
//     literal identifier, plus helper-local `mux.HandleFunc`
//     calls inside helpers that receive `protected` as their
//     `mux` parameter).
//  2. Collect every `root.Handle("/v1/...", X(protected))` mount,
//     where X is any wrapping middleware (covers both
//     `withAuth(protected)` and ad-hoc mounts like
//     `authMiddleware(protected)`).
//  3. Assert every registered path sits under at least one
//     mounted prefix.
//
// Static analysis is the right scope here — we're not proving the
// handler behaves correctly, just proving mux wiring reaches it.
// A request-based end-to-end check would need every handler's
// dependency graph to stand up and would reject for reasons
// unrelated to the mount question.
func TestProtectedRoutesAreMounted(t *testing.T) {
	t.Parallel()

	src, err := os.ReadFile("routes.go")
	if err != nil {
		t.Fatalf("read routes.go: %v", err)
	}
	body := string(src)

	registered := extractProtectedRoutes(body)
	if len(registered) == 0 {
		t.Fatal("no protected routes found — either routes.go moved or the regex is broken")
	}
	mountedPrefixes := extractMountedPrefixes(body)
	if len(mountedPrefixes) == 0 {
		t.Fatal("no mounted prefixes found — regex likely broken")
	}

	for _, r := range registered {
		if !hasMountCovering(r.path, mountedPrefixes) {
			t.Errorf(
				"route %s %s is registered on an auth-guarded mux but no matching root mount exists. Expected either `root.Handle(\"%s\", withAuth(protected))` in registerProtectedMounts OR an ad-hoc `root.Handle(\"%s\", yourMiddleware(protected))` elsewhere in routes.go. See commit 63b03b4b for what this bug looks like in prod (/v1/usage → 404).",
				r.method, r.path, deriveSuggestedMount(r.path), deriveSuggestedMount(r.path),
			)
		}
	}
}

// TestProtectedRoutesCoverageParseSanity makes sure the regexes used
// by the coverage test haven't started returning empty results due
// to a formatting change in routes.go (e.g. a refactor inserts a
// newline between `HandleFunc` and its arg list and the regex
// stops matching). Without this guard the real coverage test
// above would silently pass on zero routes.
func TestProtectedRoutesCoverageParseSanity(t *testing.T) {
	t.Parallel()

	src, err := os.ReadFile("routes.go")
	if err != nil {
		t.Fatalf("read routes.go: %v", err)
	}
	body := string(src)

	routes := extractProtectedRoutes(body)
	mounts := extractMountedPrefixes(body)

	// Lower bounds picked to be comfortably below current counts
	// (~40 routes and ~20 mounts today) but above "parse fell
	// through to empty."
	if len(routes) < 10 {
		t.Errorf("extractProtectedRoutes returned only %d routes — regex likely mis-parsing", len(routes))
	}
	if len(mounts) < 5 {
		t.Errorf("extractMountedPrefixes returned only %d mounts — regex likely mis-parsing", len(mounts))
	}
}

type protectedRoute struct {
	method string
	path   string
}

var (
	// Matches `<ident>.HandleFunc("METHOD /v1/...",` where ident
	// is any of the protected-mux variable names this file uses.
	// `protected` is the direct shared sub-mux. `mux` appears
	// inside helper functions (registerMemoryRoutes,
	// registerKnowledgeRoutes, registerAccountRoutes,
	// registerAgentSyncRoutes) that take the protected mux as
	// their `mux` parameter — callers pass `protected` in
	// registerRoutes, so their `mux.HandleFunc` calls need the
	// same mount coverage. We accept the over-match for helpers
	// whose `mux` is actually root (registerAdmin*,
	// registerWaitlist*, registerMCP*, registerPlans*,
	// registerHubRoutes GET-invite) by post-filtering: root-
	// mounted paths show up directly in the mount-list check
	// below and pass trivially.
	reProtectedRoute = regexp.MustCompile(`\b(protected|mux)\.HandleFunc\(\s*"(GET|POST|PUT|PATCH|DELETE)\s+(/v1/[^"]*)"`)
	// Matches `root.Handle("/v1/...", <any-expr>(protected))`.
	// Covers `withAuth(protected)` (registerProtectedMounts) AND
	// ad-hoc mounts like `deps.authMiddleware(protected)` used
	// in registerAuthRoutes. Expression match is greedy but
	// path-bounded so we can't accidentally swallow adjacent
	// calls. Trailing `/` in the mount path acts as a prefix
	// mount under Go's ServeMux.
	reMountedPrefix = regexp.MustCompile(`root\.Handle\(\s*"(/v1/[^"]*)"\s*,\s*[\w.]+\(\s*protected\s*\)\s*\)`)
	// Matches `mux.Handle("/v1/...", withAuth(X))` inside
	// registerProtectedMounts's `root *http.ServeMux` parameter
	// named `root`. Kept separate so the regex is easy to read.
	// (Already covered by reMountedPrefix since `root` is the
	// first arg there.)
	_ = reMountedPrefix
	// Matches `mux.HandleFunc("METHOD /v1/...",` calls at the
	// top level of registerRoutes (NOT inside a protected-only
	// helper). These routes are registered directly on root and
	// are by construction reachable — they don't need a mount.
	// We use this to filter out false positives from the broad
	// reProtectedRoute match above.
	reDirectRootRoute = regexp.MustCompile(`mux\.HandleFunc\(\s*"(GET|POST|PUT|PATCH|DELETE)\s+(/v1/[^"]*)"`)
)

// extractProtectedRoutes returns every /v1/... path registered on
// the protected mux (either directly via `protected.HandleFunc`
// or transitively via a helper receiving the protected mux as
// its `mux` parameter). We post-filter out root-direct routes by
// checking which paths also have a direct root.Handle mount —
// since those wouldn't need a mount-prefix check.
func extractProtectedRoutes(body string) []protectedRoute {
	matches := reProtectedRoute.FindAllStringSubmatch(body, -1)
	// Track routes that are registered directly on root via
	// root.HandleFunc (`mux.HandleFunc` inside a root-helper).
	// If a path appears ONLY as a root-direct registration, it's
	// not a protected route and doesn't need a mount check.
	directRootPaths := map[string]bool{}
	// Paths registered outside /v1/ (not our concern) and paths
	// in registerAdminRoutes / registerWaitlistRoutes / similar
	// that attach root-level are identified by the absence of a
	// mount — but we can't tell from regex alone. So we lean on
	// a different signal: `root.Handle(...)` without `(protected)`
	// inside. Those are direct root mounts for the same path.
	for _, m := range regexp.MustCompile(`root\.Handle\(\s*"(/v1/[^"]*)"\s*,\s*[^)]+\)`).FindAllStringSubmatch(body, -1) {
		// We don't bother parsing whether it's protected-wrapped
		// here — this is a "registered on root directly" signal.
		directRootPaths[m[1]] = true
	}

	out := make([]protectedRoute, 0, len(matches))
	for _, m := range matches {
		ident, method, path := m[1], m[2], m[3]
		// If the helper's `mux` is root (registerAdminRoutes,
		// registerWaitlistRoutes, etc.), the path will ALSO have
		// a direct root-handle registration covering it. Such
		// paths are reachable trivially. Skip them.
		if ident == "mux" && pathHasDirectRootRegistration(path, body) {
			continue
		}
		out = append(out, protectedRoute{method: method, path: path})
	}
	return out
}

// pathHasDirectRootRegistration returns true if the path appears in
// a `mux.HandleFunc` call inside a register* helper whose `mux`
// parameter is bound to the root mux (not the protected sub-mux).
// We identify this via the registerRoutes call site: helpers
// invoked as `registerFoo(mux, ...)` — where the arg is literally
// `mux` (the root mux in registerRoutes' scope) — are root
// helpers. Helpers invoked as `registerFoo(protected, ...)` are
// protected helpers.
//
// This function errs on the side of "path IS root" when
// ambiguous, so we skip the mount check. The sanity test above
// guards against the regex returning empty, so missing a real
// protected route here would be caught by a downstream mount
// failure.
func pathHasDirectRootRegistration(path string, body string) bool {
	// Find which helper registered this path. Collect indices.
	rePathInRegister := regexp.MustCompile(
		`mux\.HandleFunc\(\s*"(?:GET|POST|PUT|PATCH|DELETE)\s+` + regexp.QuoteMeta(path) + `"`,
	)
	loc := rePathInRegister.FindStringIndex(body)
	if loc == nil {
		return false
	}
	// Find the enclosing `func registerXxx(` above this index.
	funcStart := strings.LastIndex(body[:loc[0]], "\nfunc register")
	if funcStart < 0 {
		return false
	}
	// Extract the helper name: the token between "func " and "(".
	nameMatch := regexp.MustCompile(`^func (register\w+)\(`).FindStringSubmatch(body[funcStart+1:])
	if len(nameMatch) < 2 {
		return false
	}
	helperName := nameMatch[1]
	// Is this helper invoked with `protected` as its first arg in
	// registerRoutes? If so, it's a protected helper and this
	// path IS a protected route. Otherwise assume root.
	rePass := regexp.MustCompile(
		regexp.QuoteMeta(helperName) + `\(\s*(\w+)`,
	)
	passMatch := rePass.FindStringSubmatch(body)
	if len(passMatch) < 2 {
		return false
	}
	firstArg := passMatch[1]
	return firstArg != "protected"
}

func extractMountedPrefixes(body string) []string {
	matches := reMountedPrefix.FindAllStringSubmatch(body, -1)
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		out = append(out, m[1])
	}
	return out
}

// hasMountCovering reports whether any mount in `mounts` would
// route `path` to the protected mux. Go's http.ServeMux rules:
//   - an exact pattern (no trailing slash) matches only that path
//   - a prefix pattern (trailing slash) matches itself and any
//     path that starts with it
func hasMountCovering(path string, mounts []string) bool {
	for _, m := range mounts {
		if strings.HasSuffix(m, "/") {
			if strings.HasPrefix(path, m) {
				return true
			}
		} else {
			if path == m {
				return true
			}
		}
	}
	return false
}

// deriveSuggestedMount returns the mount line we'd suggest adding
// when a path isn't covered. For `/v1/usage` we suggest exact; for
// `/v1/usage/something` we suggest `/v1/usage/` prefix.
func deriveSuggestedMount(path string) string {
	segments := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(segments) >= 2 {
		return "/" + segments[0] + "/" + segments[1]
	}
	return path
}
