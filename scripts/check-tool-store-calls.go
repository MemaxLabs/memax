//go:build ignore

// check-tool-store-calls is a CI lint that enforces the tool-wrapper
// security pattern documented in
// docs/plans/24-agent-runtime-lucid-and-chat.md "Tool registry":
//
//   1. Every tool-wrapper file under packages/server/internal/agent/tools/
//      must resolve the session scope via *ScopeResolver.Resolve before
//      calling the wrapper's underlying service or store. A wrapper that
//      bypasses Resolve loses the runtime authorization check that pins
//      the agent to its session's allowed hubs.
//
//   2. Every wrapper that calls Resolve must also guard against an
//      empty HubIDs result (the user lost access to every hub in the
//      session's scope between session-create and now). Without this
//      guard, the empty slice falls through into store.VisibilityScope,
//      which the store interprets as "owner-only reads" — a fail-open
//      that a former hub member can use to recall their own content
//      after the hub access was revoked.
//
//   3. Every wrapper that accepts a hub_id argument from the model
//      MUST validate it via scope.IsHubAllowed before narrowing the
//      query. Without this, the model could pass a hub_id outside
//      the session's scope and the wrapper would happily query it.
//      The hub-arg detector matches `<recv>.HubID` for any receiver
//      OTHER than scope-derived names ("scope", "resolved",
//      "sessionScope") so the rule fires regardless of arg-struct
//      naming convention (`args.HubID`, `req.HubID`, `params.HubID`,
//      `input.HubID`).
//
//   4. Every wrapper that sets a sink struct's OwnerID field MUST
//      also set its HubIDs field UNCONDITIONALLY in the same
//      function — empty HubIDs falls through to owner-only store
//      reads (the exact fail-open the lint exists to prevent).
//      The rule fires regardless of struct type; the OwnerID/
//      HubIDs field-name pair is the security signal. Three
//      observable shapes are covered:
//
//        (a) inline literal — flagged if missing HubIDs:
//            `return service.Run(Req{OwnerID: scope.OwnerID})`
//        (b) bound literal completed by assignment — SAFE:
//            `req := Req{OwnerID: scope.OwnerID}
//             req.HubIDs = scope.HubIDs`
//        (c) bound zero-value mutated — flagged if HubIDs is
//            absent or only conditional:
//            `req := Req{}; req.OwnerID = ...
//             if narrow { req.HubIDs = ... }   // CONDITIONAL — UNSAFE`
//
//      The lint's HubIDs-completeness check operates ONLY on the
//      function body's top-level statement list. Assignments
//      nested inside if/for/switch bodies do NOT count as
//      complete because they're conditional — HubIDs MUST be set
//      on every path that reaches the store call.
//
//      Order matters too: HubIDs MUST be set BEFORE the first
//      service/store call that consumes the sink. A wrapper that
//      mutates OwnerID, calls the service, then mutates HubIDs is
//      flagged — the call already failed open at the moment of
//      invocation, regardless of what gets assigned afterwards.
//
// Why a Go AST lint and not a regex grep: AST gives us per-function
// granularity (a file with multiple wrappers, one of which forgets a
// guard, is detectable; a regex would just say "the substring exists
// somewhere"). It also tolerates whitespace/comment changes that
// would break grep.
//
// Run via:
//
//	go run scripts/check-tool-store-calls.go            # production lint
//	go run scripts/check-tool-store-calls.go --self-test # internal regression test
//
// Exit code 0 = clean; non-zero = violations printed to stderr. Wired
// into pnpm lint at the root.
//
// Future scope: when more tools land (push_memory, forget_memory,
// etc.), they each need to follow the same pattern; this lint catches
// regressions automatically. To opt a file out (rare — only for tools
// that genuinely don't touch the store), add the comment
// `//memax:lint-skip:tool-store` on its own line near the top.
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

const (
	toolsDir   = "packages/server/internal/agent/tools"
	skipMarker = "//memax:lint-skip:tool-store"
)

// violation is one rule miss for one function (or whole-file when the
// rule is file-level). file:line for IDE jump-to-definition.
type violation struct {
	file string
	line int
	rule string
	msg  string
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--self-test" {
		runSelfTest()
		return
	}

	root, err := repoRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "check-tool-store-calls: %v\n", err)
		os.Exit(2)
	}

	dir := filepath.Join(root, toolsDir)
	if _, statErr := os.Stat(dir); os.IsNotExist(statErr) {
		// Tools directory hasn't been created yet — lint passes
		// silently. The lint becomes meaningful as soon as the dir
		// exists; not having tools yet shouldn't block CI on a
		// pre-Phase-1 branch.
		os.Exit(0)
	}

	violations, err := lintDir(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "check-tool-store-calls: %v\n", err)
		os.Exit(2)
	}

	if len(violations) == 0 {
		fmt.Println("check-tool-store-calls: ok")
		return
	}

	fmt.Fprintln(os.Stderr, "check-tool-store-calls: violations found:")
	for _, v := range violations {
		fmt.Fprintf(os.Stderr, "  %s:%d [%s] %s\n", v.file, v.line, v.rule, v.msg)
	}
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Every tool wrapper under internal/agent/tools/ must:")
	fmt.Fprintln(os.Stderr, "  1. Call ScopeResolver.Resolve(ctx, session) before any service/store call")
	fmt.Fprintln(os.Stderr, "  2. Reject empty scope.HubIDs (do NOT fall through to owner-only reads)")
	fmt.Fprintln(os.Stderr, "  3. Validate caller-supplied hub_id args via scope.IsHubAllowed")
	fmt.Fprintln(os.Stderr, "  4. Pass HubIDs into every sink struct that carries OwnerID (fail-open guard)")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "See docs/plans/24-agent-runtime-lucid-and-chat.md \"Tool registry\".")
	os.Exit(1)
}

// repoRoot walks up from cwd looking for the repo's go.work or
// pnpm-workspace.yaml so the lint runs equally from any subdirectory.
// Falls back to cwd if neither is found, since the lint's relative
// path resolution against toolsDir would still work from repo root.
func repoRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for d := cwd; d != "/" && d != ""; d = filepath.Dir(d) {
		for _, marker := range []string{"go.work", "pnpm-workspace.yaml", ".git"} {
			if _, err := os.Stat(filepath.Join(d, marker)); err == nil {
				return d, nil
			}
		}
	}
	return cwd, nil
}

// lintDir walks dir non-recursively and lints each *.go file that is
// not a test, internal helpers, or explicitly opted out. Tool wrappers
// are flat — one per file — by convention; if a future structure
// nests wrappers in subdirectories, swap to filepath.WalkDir here.
func lintDir(dir string) ([]violation, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var all []violation
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		path := filepath.Join(dir, name)
		fileViolations, err := lintFile(path)
		if err != nil {
			return nil, err
		}
		all = append(all, fileViolations...)
	}
	return all, nil
}

// lintFile parses one Go source and applies the three rules. It
// returns a slice of violations rather than panicking on the first so
// authors fixing many at once see them all in one CI run.
func lintFile(path string) ([]violation, error) {
	fset := token.NewFileSet()
	src, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	if hasFileSkipMarker(src) {
		return nil, nil
	}

	rel := relPath(path)

	// File-level signals. The wrapper handler is typically a method
	// named `execute` on a struct that holds resolver + service +
	// session refs. We detect by scanning function bodies for the
	// signal calls; the lint doesn't care which struct the method
	// is bound to.
	hasResolveCall := false
	hasEmptyHubsGuard := false
	hasHubArgValidation := false
	hasHubIDArg := false

	// suspectFuncs: functions that call into a service-shaped method
	// (e.g. h.service.AgentRecall(...)) or into a store-shaped method
	// (e.g. h.store.SomeReadAccess(...)) and therefore MUST be
	// preceded by a Resolve call within the same function. We capture
	// each suspect's position so the violation message can pinpoint
	// the exact line.
	type suspect struct {
		funcName string
		line     int
	}
	var suspects []suspect
	// suspects4: per-function rule-4 violations (sink-struct
	// composite literal sets OwnerID but not HubIDs). Captured here
	// so they accumulate across functions and the final emission
	// loop can append them after the file-level rules run.
	var suspects4 []violation

	ast.Inspect(src, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			return true
		}
		funcName := fn.Name.Name

		var localResolve, localGuard, localIsHubAllowed, localServiceCall bool
		var serviceCallLine int

		ast.Inspect(fn.Body, func(child ast.Node) bool {
			call, ok := child.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			methodName := sel.Sel.Name

			switch methodName {
			case "Resolve":
				// Heuristic: any call to .Resolve(ctx, ...) where the
				// receiver name suggests a resolver. We don't check
				// the receiver's static type because the lint runs
				// without type info; a misnamed `httpResolver.Resolve`
				// would be a false positive but isn't observed in
				// this codebase.
				if recvLooksLikeResolver(sel.X) {
					localResolve = true
					hasResolveCall = true
				}
			case "IsHubAllowed":
				localIsHubAllowed = true
				hasHubArgValidation = true
			}

			// Service / store call detection. We treat any call like
			// `h.<field>.<Method>(...)` as a candidate suspect when
			// <field> is "service" or "store" — the two field names
			// the project's tool wrappers consistently use. New field
			// names (e.g. "client", "repo") would extend this list.
			if isLikelyServiceCall(sel) {
				localServiceCall = true
				if serviceCallLine == 0 {
					serviceCallLine = fset.Position(call.Pos()).Line
				}
			}
			return true
		})

		// Empty-HubIDs guard. We accept any of these patterns within
		// the function body:
		//   if len(scope.HubIDs) == 0 { ... }
		//   if len(scope.HubIDs) < 1 { ... }
		// The strict shape lets us distinguish "you guarded the
		// scope" from "you happened to use scope.HubIDs somewhere."
		ast.Inspect(fn.Body, func(child ast.Node) bool {
			binary, ok := child.(*ast.BinaryExpr)
			if !ok {
				return true
			}
			if !isLenScopeHubIDs(binary.X) && !isLenScopeHubIDs(binary.Y) {
				return true
			}
			switch binary.Op {
			case token.EQL, token.LSS, token.LEQ, token.GTR, token.GEQ:
				localGuard = true
				hasEmptyHubsGuard = true
			}
			return true
		})

		// Detect hub_id arg by scanning local field selectors of the
		// form `<recv>.HubID`. The receiver name varies across
		// wrappers — `args.HubID`, `req.HubID`, `params.HubID`,
		// `input.HubID`, etc. — so we accept any identifier OTHER
		// than the resolved scope itself. The `scope` exclusion is
		// load-bearing: a wrapper might use `scope.HubID` (singular)
		// or future code may add the field to the SessionScope struct;
		// either way, scope-derived identifiers do not represent
		// model-supplied input and don't need IsHubAllowed validation.
		ast.Inspect(fn.Body, func(child ast.Node) bool {
			sel, ok := child.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if sel.Sel.Name != "HubID" {
				return true
			}
			ident, ok := sel.X.(*ast.Ident)
			if !ok {
				return true
			}
			switch ident.Name {
			case "scope", "resolved", "sessionScope":
				// scope-derived; not a model-supplied arg.
				// Note: bare `s` was excluded earlier as a defensive
				// scope alias, but `s.HubID` is also plausibly a
				// decoded request var. Codex v6 flagged the over-
				// exclusion; removed since no production wrapper
				// actually uses `s` for the resolved scope.
				return true
			}
			hasHubIDArg = true
			return true
		})

		// Rule-4: sink-shape (the exact fail-open class this lint
		// exists to prevent). A wrapper can pass rules 1–3 and still
		// drop scope.HubIDs by giving the sink struct an OwnerID
		// without HubIDs. The store reads owner-only when HubIDs is
		// empty; the security signal is the FIELD-NAME PAIR, not
		// the struct type — so we ignore types and check pairs
		// across the observable shapes:
		//
		//   (a) **inline keyed composite literal** —
		//       `return service.Run(Req{OwnerID: ...})` (missing HubIDs).
		//   (b) **bound literal then mutation** —
		//       `req := Req{OwnerID: ...}; req.HubIDs = scope.HubIDs`
		//       — SAFE; the assignment completes the sink.
		//   (c) **mutation only** —
		//       `req := Req{}; req.OwnerID = ...` without a sibling
		//       `req.HubIDs = ...` at the SAME control level.
		//
		// To handle all three correctly, the lint runs ONE unified
		// per-receiver pair check at the FUNCTION'S TOP LEVEL ONLY.
		// We treat a HubIDs / OwnerID set as "complete" only when
		// it's a direct child of the function body — assignments
		// nested in if-bodies, for-bodies, switch-cases, etc. are
		// NOT considered complete because they're conditional. This
		// keeps a wrapper like
		//   `req.OwnerID = ...; if narrow { req.HubIDs = ... }`
		// failing the rule, which is correct: HubIDs MUST be
		// unconditional on every path that reaches the store call.
		//
		// Inline literals (no receiver to track) are handled in a
		// separate AST walk below; literals BOUND to a top-level
		// receiver are skipped during that walk so the (b) shape
		// doesn't false-positive.
		topOwnerSet := map[string]int{}
		topHubsSet := map[string]int{} // recv → first-set line (for order check)
		// boundLiteralPositions: token positions of composite
		// literals that appear as the RHS of a top-level
		// `recv := X{...}` (or `recv := &X{...}`) binding. The
		// recursive literal walk below skips these so a binding
		// whose receiver later has a top-level HubIDs assignment
		// doesn't double-flag.
		boundLiteralPositions := map[token.Pos]struct{}{}

		litFields := func(cl *ast.CompositeLit) (hasOwner, hasHubs bool) {
			for _, el := range cl.Elts {
				kv, ok := el.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				key, ok := kv.Key.(*ast.Ident)
				if !ok {
					continue
				}
				switch key.Name {
				case "OwnerID":
					hasOwner = true
				case "HubIDs":
					hasHubs = true
				}
			}
			return
		}

		// Walk only the function body's TOP-LEVEL statements. Anything
		// inside an if/for/switch is excluded so conditional-only
		// assignments don't suppress the rule.
		for _, stmt := range fn.Body.List {
			assign, ok := stmt.(*ast.AssignStmt)
			if !ok {
				continue
			}
			// Shape: `recv := X{...}` or `recv := &X{...}` —
			// track the binding so the literal's positional flag
			// can be suppressed if HubIDs lands at top level later.
			if len(assign.Lhs) == 1 && len(assign.Rhs) == 1 {
				if ident, ok := assign.Lhs[0].(*ast.Ident); ok {
					var cl *ast.CompositeLit
					switch rhs := assign.Rhs[0].(type) {
					case *ast.CompositeLit:
						cl = rhs
					case *ast.UnaryExpr:
						if rhs.Op == token.AND {
							if inner, ok := rhs.X.(*ast.CompositeLit); ok {
								cl = inner
							}
						}
					}
					if cl != nil {
						boundLiteralPositions[cl.Pos()] = struct{}{}
						hasOwner, hasHubs := litFields(cl)
						if hasOwner {
							if _, exists := topOwnerSet[ident.Name]; !exists {
								topOwnerSet[ident.Name] = fset.Position(cl.Pos()).Line
							}
						}
						if hasHubs {
							if _, exists := topHubsSet[ident.Name]; !exists {
								topHubsSet[ident.Name] = fset.Position(cl.Pos()).Line
							}
						}
					}
				}
			}
			// Shape: `recv.OwnerID = ...` / `recv.HubIDs = ...` at
			// top level. Multi-LHS like `recv.OwnerID, recv.HubIDs
			// = ..., ...` lands here too via the per-LHS loop.
			for _, lhs := range assign.Lhs {
				sel, ok := lhs.(*ast.SelectorExpr)
				if !ok {
					continue
				}
				recv, ok := sel.X.(*ast.Ident)
				if !ok {
					continue
				}
				switch sel.Sel.Name {
				case "OwnerID":
					if _, exists := topOwnerSet[recv.Name]; !exists {
						topOwnerSet[recv.Name] = fset.Position(sel.Pos()).Line
					}
				case "HubIDs":
					if _, exists := topHubsSet[recv.Name]; !exists {
						topHubsSet[recv.Name] = fset.Position(sel.Pos()).Line
					}
				}
			}
		}

		// Per-receiver pair check. Receivers with OwnerID set at top
		// level but no HubIDs set at top level fail the rule.
		// AND — the HubIDs assignment must come BEFORE the first
		// service/store call: a wrapper that calls the service with
		// the half-built sink and only completes HubIDs afterwards
		// has already failed open at that call site. The service-
		// call line for the function is `serviceCallLine` (captured
		// during the suspect walk above); 0 means no service call
		// observed.
		for recv, ownerLine := range topOwnerSet {
			hubsLine, ok := topHubsSet[recv]
			if ok && (serviceCallLine == 0 || hubsLine <= serviceCallLine) {
				continue
			}
			var msg string
			if ok && serviceCallLine > 0 && hubsLine > serviceCallLine {
				msg = fmt.Sprintf("%s sets %s.HubIDs (line %d) AFTER the first service/store call (line %d) — the call sees an incomplete sink and falls through to owner-only reads", funcName, recv, hubsLine, serviceCallLine)
			} else {
				msg = fmt.Sprintf("%s sets %s.OwnerID at top level without a top-level %s.HubIDs assignment — empty HubIDs falls through to owner-only reads (conditional or nested HubIDs assignment is unsafe)", funcName, recv, recv)
			}
			suspects4 = append(suspects4, violation{
				file: rel, line: ownerLine, rule: "rule-4",
				msg:  msg,
			})
		}

		// Inline literals. Walk all composite literals; flag any
		// that has OwnerID-without-HubIDs and is NOT the RHS of a
		// top-level receiver binding (those are evaluated above
		// via the per-receiver pair check). Catches the common
		// `return service.Run(Req{OwnerID: ...})` shape.
		ast.Inspect(fn.Body, func(child ast.Node) bool {
			cl, ok := child.(*ast.CompositeLit)
			if !ok {
				return true
			}
			if _, bound := boundLiteralPositions[cl.Pos()]; bound {
				return true
			}
			hasOwner, hasHubs := litFields(cl)
			if !hasOwner || hasHubs {
				return true
			}
			suspects4 = append(suspects4, violation{
				file: rel, line: fset.Position(cl.Pos()).Line, rule: "rule-4",
				msg: fmt.Sprintf("%s constructs a sink literal with OwnerID but no HubIDs — empty HubIDs falls through to owner-only reads (fail-open)", funcName),
			})
			return true
		})

		// Per-function suspect tally — a function that makes a
		// service/store call MUST resolve scope locally OR delegate
		// to a function that does.
		if localServiceCall && !localResolve {
			suspects = append(suspects, suspect{funcName: funcName, line: serviceCallLine})
		}

		// Quiet the unused-var warning for localGuard/localIsHubAllowed.
		_ = localGuard
		_ = localIsHubAllowed

		return true
	})

	var v []violation
	if !hasResolveCall {
		v = append(v, violation{
			file: rel, line: 1, rule: "rule-1",
			msg: "no ScopeResolver.Resolve call found in file (every tool wrapper must resolve session scope before calling the store)",
		})
	}
	if hasResolveCall && !hasEmptyHubsGuard {
		v = append(v, violation{
			file: rel, line: 1, rule: "rule-2",
			msg: "no `len(scope.HubIDs) == 0` guard found (resolved scope MUST be checked for empty hubs to avoid fail-open owner-only reads)",
		})
	}
	if hasHubIDArg && !hasHubArgValidation {
		v = append(v, violation{
			file: rel, line: 1, rule: "rule-3",
			msg: "tool accepts an args.HubID but never calls scope.IsHubAllowed (caller-supplied hub_id must be validated against the resolved scope)",
		})
	}
	for _, s := range suspects {
		v = append(v, violation{
			file: rel, line: s.line, rule: "rule-1",
			msg: fmt.Sprintf("%s makes a service/store call without a same-function Resolve — extract the unsafe call behind a method that calls Resolve first", s.funcName),
		})
	}
	v = append(v, suspects4...)

	return v, nil
}

// recvLooksLikeResolver returns true when `expr.Resolve(...)` is
// plausibly a ScopeResolver call. Receivers like `h.resolver`,
// `cfg.Resolver`, or just a local var named `resolver` are accepted.
// A future strict mode would type-check, but for lint precision in a
// small target dir this heuristic is sufficient.
func recvLooksLikeResolver(x ast.Expr) bool {
	switch v := x.(type) {
	case *ast.Ident:
		return strings.Contains(strings.ToLower(v.Name), "resolver") || v.Name == "r"
	case *ast.SelectorExpr:
		return strings.Contains(strings.ToLower(v.Sel.Name), "resolver")
	}
	return false
}

// isLikelyServiceCall returns true for selector expressions that look
// like store/service method invocations bound to wrapper-struct fields.
// Pattern: `h.<service|store|client|repo>.SomeMethod(...)`.
func isLikelyServiceCall(sel *ast.SelectorExpr) bool {
	inner, ok := sel.X.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	field := strings.ToLower(inner.Sel.Name)
	switch field {
	case "service", "store", "client", "repo", "members":
		return true
	}
	return false
}

// isLenScopeHubIDs matches `len(scope.HubIDs)` so the guard rule can
// detect `if len(scope.HubIDs) == 0` regardless of which side of the
// binary expression the len(...) call sits on.
func isLenScopeHubIDs(e ast.Expr) bool {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return false
	}
	ident, ok := call.Fun.(*ast.Ident)
	if !ok || ident.Name != "len" {
		return false
	}
	if len(call.Args) != 1 {
		return false
	}
	sel, ok := call.Args[0].(*ast.SelectorExpr)
	if !ok {
		return false
	}
	if sel.Sel.Name != "HubIDs" {
		return false
	}
	if x, ok := sel.X.(*ast.Ident); ok && x.Name == "scope" {
		return true
	}
	return false
}

// hasFileSkipMarker returns true if the file's leading comments
// contain the explicit lint-skip directive. The marker MUST be on
// its own line and exact-string match — partial matches don't count
// so a doc-comment that mentions the skip syntax in passing doesn't
// accidentally suppress the lint.
func hasFileSkipMarker(f *ast.File) bool {
	for _, group := range f.Comments {
		for _, c := range group.List {
			if strings.TrimSpace(c.Text) == skipMarker {
				return true
			}
		}
	}
	return false
}

// relPath returns the path relative to the repo root when possible
// so violation lines look like `packages/server/internal/agent/tools/recall.go:42`
// rather than absolute paths.
func relPath(path string) string {
	if root, err := repoRoot(); err == nil {
		if rel, err := filepath.Rel(root, path); err == nil {
			return rel
		}
	}
	return path
}

// ----- self-test --------------------------------------------------
//
// Synthetic tool-wrapper fixtures + a harness that asserts the lint
// catches each rule violation. Lives in the same file as the lint so
// the rule + its proof of detection move together — a future
// loosening of any rule that breaks the harness fails CI loudly.

// Fixtures mirror production tool-wrapper shape: a concrete resolver
// field (h.resolver), a concrete service/store field (h.service), and
// a session descriptor (h.session). Synthetic resolver/service types
// keep the lint's heuristics testable without depending on the real
// agent package — the lint runs against the AST, not types.

const fixturePreamble = `package tools

import "context"

type fakeScope struct{ HubIDs []string; OwnerID string }

func (s fakeScope) IsHubAllowed(id string) bool {
	for _, h := range s.HubIDs {
		if h == id {
			return true
		}
	}
	return false
}

type fakeResolver struct{}

func (r *fakeResolver) Resolve(ctx context.Context, s any) (fakeScope, error) {
	return fakeScope{}, nil
}

type fakeService struct{}

func (s *fakeService) Run(scope fakeScope, hub string) error { return nil }
`

const fixtureGoodWrapper = fixturePreamble + `
type goodTool struct {
	resolver *fakeResolver
	service  *fakeService
	session  any
}

type goodArgs struct{ HubID string }

func (h *goodTool) execute(ctx context.Context, args goodArgs) error {
	scope, err := h.resolver.Resolve(ctx, h.session)
	if err != nil {
		return err
	}
	if len(scope.HubIDs) == 0 {
		return nil
	}
	target := scope.HubIDs[0]
	if args.HubID != "" {
		if !scope.IsHubAllowed(args.HubID) {
			return nil
		}
		target = args.HubID
	}
	return h.service.Run(scope, target)
}
`

const fixtureMissingResolve = fixturePreamble + `
type bad1 struct {
	service *fakeService
}

func (h *bad1) execute(_ context.Context) error {
	return h.service.Run(fakeScope{}, "")
}
`

const fixtureMissingGuard = fixturePreamble + `
type bad2 struct {
	resolver *fakeResolver
	service  *fakeService
	session  any
}

func (h *bad2) execute(ctx context.Context) error {
	scope, err := h.resolver.Resolve(ctx, h.session)
	if err != nil {
		return err
	}
	return h.service.Run(scope, "")
}
`

const fixtureMissingHubValidation = fixturePreamble + `
type bad3 struct {
	resolver *fakeResolver
	service  *fakeService
	session  any
}

type bad3Args struct{ HubID string }

func (h *bad3) execute(ctx context.Context, args bad3Args) error {
	scope, err := h.resolver.Resolve(ctx, h.session)
	if err != nil {
		return err
	}
	if len(scope.HubIDs) == 0 {
		return nil
	}
	return h.service.Run(scope, args.HubID)
}
`

// Rule-3 also fires on req.HubID, params.HubID, input.HubID, etc.
// — any receiver name OTHER than scope-derived. This fixture proves
// the relaxation by using `req` instead of `args`.
const fixtureMissingHubValidationReqAlias = fixturePreamble + `
type bad3req struct {
	resolver *fakeResolver
	service  *fakeService
	session  any
}

type bad3reqInput struct{ HubID string }

func (h *bad3req) execute(ctx context.Context, req bad3reqInput) error {
	scope, err := h.resolver.Resolve(ctx, h.session)
	if err != nil {
		return err
	}
	if len(scope.HubIDs) == 0 {
		return nil
	}
	return h.service.Run(scope, req.HubID)
}
`

// Rule-4 (a): keyed composite literal sets OwnerID but not HubIDs.
// The lint must catch this composite-literal shape regardless of
// struct type.
const fixtureSinkMissingHubIDs = fixturePreamble + `
type bad4SinkRequest struct {
	OwnerID string
	HubIDs  []string
}

type bad4 struct {
	resolver *fakeResolver
	session  any
}

type bad4Args struct{ HubID string }

func (h *bad4) callSink(req bad4SinkRequest) error { return nil }

func (h *bad4) execute(ctx context.Context, args bad4Args) error {
	scope, err := h.resolver.Resolve(ctx, h.session)
	if err != nil {
		return err
	}
	if len(scope.HubIDs) == 0 {
		return nil
	}
	if args.HubID != "" && !scope.IsHubAllowed(args.HubID) {
		return nil
	}
	// Forgot HubIDs! This is the fail-open we exist to catch.
	return h.callSink(bad4SinkRequest{OwnerID: scope.OwnerID})
}
`

// Rule-4 (combined safe): wrapper binds a literal that sets
// OwnerID (no HubIDs in the literal), then completes the sink
// with a top-level `req.HubIDs = ...` assignment. Lint MUST NOT
// flag this (the assignment completes the safe shape).
const fixtureSinkCombinedSafe = fixturePreamble + `
type combinedSinkRequest struct {
	OwnerID string
	HubIDs  []string
}

type combinedSafe struct {
	resolver *fakeResolver
	session  any
}

func (h *combinedSafe) callSink(req combinedSinkRequest) error { return nil }

func (h *combinedSafe) execute(ctx context.Context) error {
	scope, err := h.resolver.Resolve(ctx, h.session)
	if err != nil {
		return err
	}
	if len(scope.HubIDs) == 0 {
		return nil
	}
	req := combinedSinkRequest{OwnerID: scope.OwnerID}
	req.HubIDs = scope.HubIDs
	return h.callSink(req)
}
`

// Rule-4 (conditional unsafe): wrapper sets OwnerID
// unconditionally but sets HubIDs only inside an if-branch.
// Empty-HubIDs paths fall through to owner-only reads. Lint MUST
// flag this — top-level-only completeness check is what catches it.
const fixtureSinkConditionalUnsafe = fixturePreamble + `
type condSinkRequest struct {
	OwnerID string
	HubIDs  []string
}

type condUnsafe struct {
	resolver *fakeResolver
	session  any
}

type condArgs struct{ Narrow bool }

func (h *condUnsafe) callSink(req condSinkRequest) error { return nil }

func (h *condUnsafe) execute(ctx context.Context, args condArgs) error {
	scope, err := h.resolver.Resolve(ctx, h.session)
	if err != nil {
		return err
	}
	if len(scope.HubIDs) == 0 {
		return nil
	}
	req := condSinkRequest{}
	req.OwnerID = scope.OwnerID
	if args.Narrow {
		req.HubIDs = scope.HubIDs // CONDITIONAL — must fail
	}
	return h.callSink(req)
}
`

// Rule-4 (mutation only — both fields at top level): zero-value
// init, both OwnerID and HubIDs assigned at top level. Lint MUST
// NOT flag.
const fixtureSinkMutationSafe = fixturePreamble + `
type mutSinkRequest struct {
	OwnerID string
	HubIDs  []string
}

type mutSafe struct {
	resolver *fakeResolver
	session  any
}

func (h *mutSafe) callSink(req mutSinkRequest) error { return nil }

func (h *mutSafe) execute(ctx context.Context) error {
	scope, err := h.resolver.Resolve(ctx, h.session)
	if err != nil {
		return err
	}
	if len(scope.HubIDs) == 0 {
		return nil
	}
	req := mutSinkRequest{}
	req.OwnerID = scope.OwnerID
	req.HubIDs = scope.HubIDs
	return h.callSink(req)
}
`

// Rule-4 (order-sensitive unsafe): wrapper sets OwnerID, calls
// the service while the sink is still incomplete, then sets
// HubIDs afterwards. The service call has already failed open;
// the post-hoc HubIDs assignment doesn't help. Lint MUST flag
// this — the order check ensures HubIDs lands BEFORE the first
// service call.
const fixtureSinkOrderUnsafe = fixturePreamble + `
type orderSinkRequest struct {
	OwnerID string
	HubIDs  []string
}

type orderUnsafe struct {
	resolver *fakeResolver
	service  *fakeService
	session  any
}

func (h *orderUnsafe) callSink(req orderSinkRequest) error { return nil }

func (h *orderUnsafe) execute(ctx context.Context) error {
	scope, err := h.resolver.Resolve(ctx, h.session)
	if err != nil {
		return err
	}
	if len(scope.HubIDs) == 0 {
		return nil
	}
	req := orderSinkRequest{}
	req.OwnerID = scope.OwnerID
	// First service call sees req with HubIDs unset — fail-open.
	_ = h.service.Run(scope, "")
	req.HubIDs = scope.HubIDs
	return h.callSink(req)
}
`

// Rule-4 (b): post-construction mutation. Wrapper builds a
// zero-value sink, sets OwnerID via assignment, never assigns
// HubIDs. Composite-literal scan misses this; the assignment
// scan catches it.
const fixtureSinkMutationMissingHubIDs = fixturePreamble + `
type bad4mSinkRequest struct {
	OwnerID string
	HubIDs  []string
}

type bad4m struct {
	resolver *fakeResolver
	session  any
}

type bad4mArgs struct{ HubID string }

func (h *bad4m) callSink(req bad4mSinkRequest) error { return nil }

func (h *bad4m) execute(ctx context.Context, args bad4mArgs) error {
	scope, err := h.resolver.Resolve(ctx, h.session)
	if err != nil {
		return err
	}
	if len(scope.HubIDs) == 0 {
		return nil
	}
	if args.HubID != "" && !scope.IsHubAllowed(args.HubID) {
		return nil
	}
	req := bad4mSinkRequest{}
	req.OwnerID = scope.OwnerID
	// Forgot req.HubIDs assignment! The assignment-shape lint
	// catches this evasion path.
	return h.callSink(req)
}
`

const fixtureSkipMarker = `//memax:lint-skip:tool-store
package tools

type opted struct{}

func (o *opted) execute() {}
`

func runSelfTest() {
	type tcase struct {
		name     string
		body     string
		expected []string // substrings expected somewhere in violation rule/msg
	}
	cases := []tcase{
		{name: "good.go", body: fixtureGoodWrapper, expected: nil},
		{name: "bad_no_resolve.go", body: fixtureMissingResolve, expected: []string{"rule-1"}},
		{name: "bad_no_guard.go", body: fixtureMissingGuard, expected: []string{"rule-2"}},
		{name: "bad_no_hub_validation.go", body: fixtureMissingHubValidation, expected: []string{"rule-3"}},
		{name: "bad_no_hub_validation_req.go", body: fixtureMissingHubValidationReqAlias, expected: []string{"rule-3"}},
		{name: "bad_sink_missing_hubids.go", body: fixtureSinkMissingHubIDs, expected: []string{"rule-4"}},
		{name: "bad_sink_mutation_missing_hubids.go", body: fixtureSinkMutationMissingHubIDs, expected: []string{"rule-4"}},
		{name: "bad_sink_conditional.go", body: fixtureSinkConditionalUnsafe, expected: []string{"rule-4"}},
		{name: "bad_sink_order.go", body: fixtureSinkOrderUnsafe, expected: []string{"rule-4"}},
		{name: "good_sink_combined.go", body: fixtureSinkCombinedSafe, expected: nil},
		{name: "good_sink_mutation.go", body: fixtureSinkMutationSafe, expected: nil},
		{name: "skipped.go", body: fixtureSkipMarker, expected: nil},
	}

	failed := 0
	for _, tc := range cases {
		dir, err := os.MkdirTemp("", "lint-test-*")
		if err != nil {
			fmt.Fprintf(os.Stderr, "tempdir: %v\n", err)
			os.Exit(2)
		}
		path := filepath.Join(dir, tc.name)
		if err := os.WriteFile(path, []byte(tc.body), 0o644); err != nil {
			os.RemoveAll(dir)
			fmt.Fprintf(os.Stderr, "write %s: %v\n", path, err)
			os.Exit(2)
		}
		violations, err := lintDir(dir)
		os.RemoveAll(dir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "lint %s: %v\n", tc.name, err)
			failed++
			continue
		}

		if len(tc.expected) == 0 {
			if len(violations) != 0 {
				fmt.Fprintf(os.Stderr, "FAIL %s: expected clean, got %d violations:\n", tc.name, len(violations))
				for _, v := range violations {
					fmt.Fprintf(os.Stderr, "  - %s\n", v.msg)
				}
				failed++
				continue
			}
			fmt.Printf("PASS %s\n", tc.name)
			continue
		}

		missing := []string{}
		for _, expect := range tc.expected {
			found := false
			for _, v := range violations {
				if strings.Contains(v.rule, expect) || strings.Contains(v.msg, expect) {
					found = true
					break
				}
			}
			if !found {
				missing = append(missing, expect)
			}
		}
		if len(missing) > 0 {
			fmt.Fprintf(os.Stderr, "FAIL %s: missing expected violations %v; got:\n", tc.name, missing)
			for _, v := range violations {
				fmt.Fprintf(os.Stderr, "  - [%s] %s\n", v.rule, v.msg)
			}
			failed++
			continue
		}
		fmt.Printf("PASS %s\n", tc.name)
	}

	if failed > 0 {
		fmt.Fprintf(os.Stderr, "\n%d self-test case(s) failed\n", failed)
		os.Exit(1)
	}
	fmt.Println("\nAll lint self-tests passed.")
}
