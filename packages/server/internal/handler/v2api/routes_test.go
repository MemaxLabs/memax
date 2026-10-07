package v2api_test

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/MemaxLabs/memax/packages/server/internal/deviceauth"
	"github.com/MemaxLabs/memax/packages/server/internal/handler/v2api"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/lifecycle"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
	"github.com/MemaxLabs/memax/packages/server/internal/sessions"
	"github.com/MemaxLabs/memax/packages/server/internal/trust"
)

// TestRoutesMatchSpec holds the Go route table and v2.yaml equal: every
// operation in the spec is served, and nothing is served that the spec
// doesn't describe.
func TestRoutesMatchSpec(t *testing.T) {
	t.Parallel()
	served := map[string]string{}
	for _, rt := range v2api.Routes() {
		served[rt.Method+" "+rt.Path] = rt.OperationID
	}
	documented := map[string]string{}
	for _, op := range spec.Operations() {
		documented[op.Method+" "+op.Path] = op.ID
	}
	for k, id := range documented {
		if got, ok := served[k]; !ok {
			t.Errorf("%s (%s) is in v2.yaml but not served: add it to v2api.routes", k, id)
		} else if got != id {
			t.Errorf("%s is %s in v2.yaml but %s in v2api.routes", k, id, got)
		}
	}
	for k := range served {
		if _, ok := documented[k]; !ok {
			t.Errorf("%s is served but not in v2.yaml: write the spec first", k)
		}
	}
}

// sampleRequests is one valid request per operation. A new operation
// needs one here, so TestEveryRouteIsServed can prove it is reachable.
var sampleRequests = map[string]struct {
	path   string
	body   string
	header map[string]string
}{
	"listSpaces":         {path: "/v2/spaces"},
	"rememberMemory":     {path: "/v2/spaces/memax-v2/memories", body: `{"statement":"x","section":"decisions"}`},
	"listMemories":       {path: "/v2/spaces/memax-v2/memories?state=kept&state=proposed&limit=10"},
	"findNearDuplicates": {path: "/v2/spaces/memax-v2/memories:near-duplicates", body: `{"statement":"Use pnpm catalogs"}`},
	"askSpace":           {path: "/v2/spaces/memax-v2/ask", body: `{"question":"Why River?"}`},
	"listReview":         {path: "/v2/spaces/memax-v2/review"},
	"listReceipts":       {path: "/v2/spaces/memax-v2/receipts?memory=M-0001"},
	"listReads":          {path: "/v2/spaces/memax-v2/reads"},
	"listCheckpoints":    {path: "/v2/spaces/memax-v2/checkpoints?limit=5"},
	"recordCompileLoad":  {path: "/v2/spaces/memax-v2/compile-loads", body: `{"compile":"C-0001","agent":"claude-code"}`},
	"getMemory":          {path: "/v2/memories/M-0001?space=memax-v2"},
	"keepMemory":         {path: "/v2/memories/M-0001:keep?space=memax-v2", header: map[string]string{"If-Match": `"1"`}},
	"editMemory":         {path: "/v2/memories/M-0001:edit?space=memax-v2", body: `{"statement":"y"}`, header: map[string]string{"If-Match": `"1"`}},
	"rejectMemory":       {path: "/v2/memories/0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b:reject", body: `{"reason":"duplicate"}`},
	"getConflict":        {path: "/v2/memories/M-0431/conflict?space=memax-v2&with=M-0174"},
	"resolveConflict":    {path: "/v2/memories/M-0431:resolve-conflict?space=memax-v2", body: `{"choice":"keep_this"}`},
	"undoReceipt":        {path: "/v2/receipts/" + sampleID + ":undo"},
	"listAgents":         {path: "/v2/agents"},
	"listSpaceAgents":    {path: "/v2/spaces/memax-v2/agents"},
	"getAgent":           {path: "/v2/agents/0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b"},
	"setAgentAutonomy":   {path: "/v2/agents/0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b/spaces/memax-v2", body: `{"autonomy":"write"}`},
	"pauseAgent":         {path: "/v2/agents/0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b:pause"},
	"resumeAgent":        {path: "/v2/agents/0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b:resume", body: `{"reason":"done testing"}`},
	"disconnectAgent":    {path: "/v2/agents/0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b:disconnect"},

	"getBrief":          {path: "/v2/spaces/memax-v2/brief"},
	"reviseBrief":       {path: "/v2/spaces/memax-v2/brief", body: `{"title":"Brief","sections":[{"key":"decisions","heading":"Decisions","items":[{"ref":"M-0219"}]}]}`, header: map[string]string{"If-Match": `"1"`}},
	"listBriefVersions": {path: "/v2/spaces/memax-v2/brief/versions?limit=5"},
	"listTargets":       {path: "/v2/spaces/memax-v2/targets"},
	"createTarget":      {path: "/v2/spaces/memax-v2/targets", body: `{"kind":"agents_md"}`},
	"configureTarget":   {path: "/v2/targets/" + sampleID, body: `{"enabled":false}`, header: map[string]string{"Idempotency-Key": "k-configure"}},
	"compileTarget":     {path: "/v2/targets/" + sampleID + ":compile"},
	"getTargetPreview":  {path: "/v2/targets/" + sampleID + "/preview"},
	"listCompileRuns":   {path: "/v2/targets/" + sampleID + "/runs"},
	"recordObservation": {path: "/v2/targets/" + sampleID + "/observations", body: `{"path":"AGENTS.md","content":"# Brief\n"}`},
	"recordDelivery":    {path: "/v2/targets/" + sampleID + "/deliveries", body: `{"compile":"C-0881","sha256":"` + strings.Repeat("a", 64) + `"}`},
	"getDrift":          {path: "/v2/targets/" + sampleID + "/drift"},
	"pullDrift":         {path: "/v2/targets/" + sampleID + "/drift:pull"},
	"overwriteDrift":    {path: "/v2/targets/" + sampleID + "/drift:overwrite", body: `{"reason":"the record wins"}`},
	"stopDrift":         {path: "/v2/targets/" + sampleID + "/drift:stop"},

	"listGates":                  {path: "/v2/spaces/memax-v2/gates?status=waiting&status=expired"},
	"requestDecision":            {path: "/v2/spaces/memax-v2/gates", body: `{"question":"Which deploy target?","options":[{"label":"Fly.io"},{"label":"Railway"}]}`},
	"getGate":                    {path: "/v2/gates/G-0012?space=memax-v2"},
	"answerGate":                 {path: "/v2/gates/G-0012:answer?space=memax-v2", body: `{"option":1}`, header: map[string]string{"If-Match": `"1"`}},
	"withdrawGate":               {path: "/v2/gates/" + sampleID + ":withdraw"},
	"lookupDeviceAuthorization":  {path: "/v2/device-authorizations:lookup", body: `{"user_code":"WQRT-4821"}`},
	"approveDeviceAuthorization": {path: "/v2/device-authorizations:approve", body: `{"user_code":"WQRT-4821"}`},
	"denyDeviceAuthorization":    {path: "/v2/device-authorizations:deny", body: `{"user_code":"WQRT-4821"}`},
	"listSessions":               {path: "/v2/sessions"},
	"revokeSession":              {path: "/v2/sessions/" + sampleID + ":revoke"},
	"revokeOtherSessions":        {path: "/v2/sessions:revoke-others"},

	"forgetMemory":         {path: "/v2/memories/M-0001:forget?space=memax-v2", body: `{"note":"personal","carries":["M-0002"]}`, header: map[string]string{"If-Match": `"1"`}},
	"requestForget":        {path: "/v2/memories/M-0001:request-forget?space=memax-v2", body: `{"reason":"a test value"}`},
	"declineForget":        {path: "/v2/memories/" + sampleID + ":decline-forget"},
	"previewForget":        {path: "/v2/memories/M-0001/forget-preview?space=memax-v2"},
	"getTombstone":         {path: "/v2/memories/M-0001/tombstone?space=memax-v2"},
	"listTombstones":       {path: "/v2/spaces/memax-v2/tombstones?limit=5"},
	"listNotices":          {path: "/v2/notices"},
	"ackNotices":           {path: "/v2/notices:ack", body: `{"ids":["` + sampleID + `"]}`},
	"createSpace":          {path: "/v2/spaces", body: `{"name":"Acme web","repository":"acme/web"}`},
	"switchSpace":          {path: "/v2/spaces/personal:switch", body: `{"to":"v2","kind":"project","repository":"acme/web"}`},
	"getSpaceSwitch":       {path: "/v2/spaces/personal/switch"},
	"listV1DreamRuns":      {path: "/v2/spaces/memax-v2/v1-dream-runs"},
	"searchNotes":          {path: "/v2/spaces/memax-v2/notes?q=pnpm&limit=5"},
	"getNote":              {path: "/v2/spaces/memax-v2/notes/N-0042"},
	"previewForgetNote":    {path: "/v2/spaces/memax-v2/notes/N-0042/forget-preview"},
	"forgetNote":           {path: "/v2/spaces/memax-v2/notes/N-0042:forget", body: `{"note":"mine","carries":["M-0007"]}`},
	"exportSpace":          {path: "/v2/spaces/memax-v2:export"},
	"keepMemories":         {path: "/v2/spaces/memax-v2/memories:keep", body: `{"items":[{"memory":"M-0001","version":1}]}`},
	"rejectMemories":       {path: "/v2/spaces/memax-v2/memories:reject", body: `{"items":[{"memory":"M-0001"}]}`},
	"createImport":         {path: "/v2/spaces/memax-v2/imports", body: `{"items":[{"key":"a","location":"repository","statement":"Use pnpm.","section":"conventions"}]}`},
	"listImports":          {path: "/v2/spaces/memax-v2/imports"},
	"getImport":            {path: "/v2/spaces/memax-v2/imports/" + sampleID},
	"settleImportConflict": {path: "/v2/spaces/memax-v2/imports/" + sampleID + "/conflicts/1:settle", body: `{"choice":"keep_all"}`},
	// Dream.
	"listEditions":            {path: "/v2/spaces/memax-v2/dream/editions?limit=5", header: map[string]string{"X-Timezone": "America/Vancouver"}},
	"getEdition":              {path: "/v2/spaces/memax-v2/dream/editions/D-0214"},
	"listDreamActions":        {path: "/v2/spaces/memax-v2/dream/editions/latest/actions?kind=fade"},
	"undoEdition":             {path: "/v2/spaces/memax-v2/dream/editions/214:undo", body: `{"kind":"fade"}`},
	"runDream":                {path: "/v2/spaces/memax-v2/dream:run"},
	"undoDreamAction":         {path: "/v2/dream/actions/" + sampleID + ":undo"},
	"restoreMemory":           {path: "/v2/memories/M-0001:restore?space=memax-v2"},
	"getDreamSettings":        {path: "/v2/dream/settings"},
	"updateDreamSettings":     {path: "/v2/dream/settings", body: `{"time_zone":"America/Vancouver"}`},
	"unsubscribeDreamEmail":   {path: "/v2/dream/email:unsubscribe?token=0123456789abcdef0123456789abcdef"},
	"getNotificationSettings": {path: "/v2/me/notifications"},
	"updateNotificationSettings": {path: "/v2/me/notifications", body: `{"quiet_hours":{"from":"21:00"}}`,
		header: map[string]string{"If-Match": `"1"`}},
	"getSecurity": {path: "/v2/security"},
}

const sampleID = "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b"

// TestEveryRouteIsServed sends a valid request for every operation to a
// /v2 handler with no ledger. Each must reach its handler (503
// unavailable, documented for every operation) rather than fall through
// to 404 or 405 in the router.
func TestEveryRouteIsServed(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	v2api.New(nil, quiet).Mount(mux, func(h http.Handler) http.Handler { return h })
	srv := spec.Handler(t, mux)
	for _, op := range spec.Operations() {
		sample, ok := sampleRequests[op.ID]
		if !ok {
			t.Errorf("%s has no sample request in sampleRequests", op.ID)
			continue
		}
		r := httptest.NewRequest(op.Method, sample.path, strings.NewReader(sample.body))
		if sample.body != "" {
			r.Header.Set("Content-Type", "application/json")
		}
		if op.Method == http.MethodPost || op.Method == http.MethodPatch {
			r.Header.Set("Idempotency-Key", "k-"+op.ID)
		}
		for k, v := range sample.header {
			r.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, r)
		if code := errorCode(t, rec.Body.Bytes()); rec.Code != http.StatusServiceUnavailable || code != "unavailable" {
			t.Errorf("%s %s = %d %s, want 503 unavailable from its handler", op.Method, sample.path, rec.Code, code)
		}
	}
}

// TestRoutingErrorsUseTheEnvelope: the router's own answers (unknown
// paths, methods and commands) are JSON errors like everything else.
func TestRoutingErrorsUseTheEnvelope(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	v2api.New(nil, quiet).Mount(mux, func(h http.Handler) http.Handler { return h })
	cases := []struct {
		method, path string
		status       int
		code, allow  string
	}{
		{"GET", "/v2/nowhere", 404, "not_found", ""},
		{"DELETE", "/v2/spaces", 405, "method_not_allowed", "GET, POST"},
		{"PUT", "/v2/spaces/x/memories", 405, "method_not_allowed", "GET, POST"},
		{"POST", "/v2/memories/M-0001", 405, "method_not_allowed", "GET"},
		{"POST", "/v2/memories/M-0001:bury", 404, "not_found", ""},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(c.method, c.path, nil))
		if code := errorCode(t, rec.Body.Bytes()); rec.Code != c.status || code != c.code {
			t.Errorf("%s %s = %d %s, want %d %s", c.method, c.path, rec.Code, code, c.status, c.code)
		}
		if got := rec.Header().Get("Allow"); got != c.allow {
			t.Errorf("%s %s: Allow = %q, want %q", c.method, c.path, got, c.allow)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
			t.Errorf("%s %s: Content-Type %q", c.method, c.path, ct)
		}
	}
}

func errorCode(t *testing.T, body []byte) string {
	t.Helper()
	var env struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Errorf("not a JSON error envelope: %s", body)
	}
	return env.Error.Code
}

// specEnum returns the enum of a component schema.
func specEnum(t *testing.T, name string) []string {
	t.Helper()
	schemas, _ := spec.Doc()["components"].(map[string]any)["schemas"].(map[string]any)
	sch, ok := schemas[name].(map[string]any)
	if !ok {
		t.Fatalf("v2.yaml has no schema %s", name)
	}
	var out []string
	for _, v := range sch["enum"].([]any) {
		out = append(out, v.(string))
	}
	return out
}

func strs[T ~string](vs []T) []string {
	out := make([]string, len(vs))
	for i, v := range vs {
		out[i] = string(v)
	}
	return out
}

func sameSet(t *testing.T, what string, spec, code []string) {
	t.Helper()
	a, b := slices.Clone(spec), slices.Clone(code)
	sort.Strings(a)
	sort.Strings(b)
	if !slices.Equal(a, b) {
		t.Errorf("%s: v2.yaml has %v, the code has %v", what, a, b)
	}
}

// TestEnumsMatchTheLedger keeps every enum in v2.yaml equal to the Go
// vocabulary it mirrors, so a new state or source kind can't reach the
// wire undocumented (or be documented and never sent).
func TestEnumsMatchTheLedger(t *testing.T) {
	t.Parallel()
	sameSet(t, "Section", specEnum(t, "Section"), strs(ledger.Sections))
	sameSet(t, "MemoryKind", specEnum(t, "MemoryKind"), []string{string(ledger.KindFact), string(ledger.KindDecision)})
	sameSet(t, "State", specEnum(t, "State"), strs(lifecycle.Marks))
	sameSet(t, "Lifecycle", specEnum(t, "Lifecycle"), strs(lifecycle.Lifecycles))
	sameSet(t, "Flag", specEnum(t, "Flag"), strs(lifecycle.AllFlags))
	sameSet(t, "Trust", specEnum(t, "Trust"), strs(policy.Trusts))
	sameSet(t, "SourceKind", specEnum(t, "SourceKind"), strs(ledger.SourceKinds))
	sameSet(t, "ActorKind", specEnum(t, "ActorKind"), strs(policy.ActorKinds))
	sameSet(t, "Via", specEnum(t, "Via"), strs(policy.Vias))
	sameSet(t, "Assurance", specEnum(t, "Assurance"), []string{string(policy.AssuranceHumanWeb), string(policy.AssuranceClientAttested)})
	sameSet(t, "SpaceKind", specEnum(t, "SpaceKind"), []string{string(policy.SpacePersonal), string(policy.SpaceProject), string(policy.SpaceTeam)})
	sameSet(t, "Role", specEnum(t, "Role"), []string{string(policy.RoleOwner), string(policy.RoleMember), string(policy.RoleViewer)})
	sameSet(t, "Autonomy", specEnum(t, "Autonomy"), strs(policy.Autonomies))
	sameSet(t, "AgentKind", specEnum(t, "AgentKind"), strs(ledger.AgentKinds))
	sameSet(t, "AgentSurface", specEnum(t, "AgentSurface"), strs(ledger.AgentSurfaces))
	sameSet(t, "AgentState", specEnum(t, "AgentState"), strs(ledger.ConnectionStates))
	sameSet(t, "CredentialKind", specEnum(t, "CredentialKind"), strs(ledger.CredentialKinds))
	sameSet(t, "Outcome", specEnum(t, "Outcome"), []string{string(ledger.OutcomeApplied), string(ledger.OutcomeProposed), string(ledger.OutcomeNeedsConfirmation)})
	sameSet(t, "PolicyEffect", specEnum(t, "PolicyEffect"),
		[]string{string(policy.EffectApply), string(policy.EffectPropose), string(policy.EffectConfirm), string(policy.EffectRefuse)})
	sameSet(t, "PolicyCode", specEnum(t, "PolicyCode"), append(append(append(stringConsts(t, "../../ledger/policy/policy.go", "Code"),
		stringConsts(t, "../../ledger/policy/spaces.go", "Code")...),
		stringConsts(t, "../../ledger/policy/devices.go", "Code")...),
		stringConsts(t, "../../ledger/policy/sessions.go", "Code")...))
	sameSet(t, "SessionSurface", specEnum(t, "SessionSurface"), []string{
		string(sessions.KindWeb), string(sessions.KindCLI), string(sessions.KindDevice), string(sessions.KindMCP)})
	sameSet(t, "DeviceAuthorizationState", specEnum(t, "DeviceAuthorizationState"), []string{
		string(deviceauth.StatePending), string(deviceauth.StateApproved), string(deviceauth.StateSignedIn),
		string(deviceauth.StateDenied), string(deviceauth.StateExpired)})
	// rate_limited (codeRateLimited) also comes from the rate-limit
	// middleware in front of /v2.
	sameSet(t, "ErrorCode", specEnum(t, "ErrorCode"), stringConsts(t, "errors.go", "code"))
	sameSet(t, "DuplicateMatch", specEnum(t, "DuplicateMatch"), []string{string(ledger.MatchExact), string(ledger.MatchNear)})

	// The Brief, targets and compiles.
	sameSet(t, "TargetKind", specEnum(t, "TargetKind"), strs(ledger.TargetKinds))
	sameSet(t, "Delivery", specEnum(t, "Delivery"), strs(ledger.Deliveries))
	sameSet(t, "SyncState", specEnum(t, "SyncState"), strs(ledger.ShownSyncStates))
	sameSet(t, "IncludeMode", specEnum(t, "IncludeMode"), strs([]ledger.IncludeMode{ledger.IncludeKeptOnly, ledger.IncludeKeptAndOpen}))
	sameSet(t, "StaleMode", specEnum(t, "StaleMode"), strs([]ledger.StaleMode{ledger.StaleMark, ledger.StaleOmit}))
	sameSet(t, "ScopedMode", specEnum(t, "ScopedMode"), strs([]ledger.ScopedMode{ledger.ScopedInline, ledger.ScopedOmit}))
	sameSet(t, "CompileStatus", specEnum(t, "CompileStatus"), strs(ledger.CompileStatuses))
	sameSet(t, "ObservationStatus", specEnum(t, "ObservationStatus"), strs(ledger.ObservationStatuses))
	sameSet(t, "ObserverKind", specEnum(t, "ObserverKind"), []string{ledger.ObserverDevice, ledger.ObserverGitHub})
	sameSet(t, "DriftMode", specEnum(t, "DriftMode"), strs([]ledger.DriftMode{ledger.DriftPull, ledger.DriftOverwrite, ledger.DriftStop}))
	sameSet(t, "ChangeKind", specEnum(t, "ChangeKind"), []string{ledger.ChangeEdit, ledger.ChangeNew, ledger.ChangeRemove})
	sameSet(t, "ChangeOutcome", specEnum(t, "ChangeOutcome"),
		[]string{ledger.OutcomeChangeProposed, ledger.OutcomeChangeReview, ledger.OutcomeChangeSkipped})

	// The judge, links, conflicts and Undo.
	sameSet(t, "LinkKind", specEnum(t, "LinkKind"), strs(ledger.LinkKinds))
	sameSet(t, "LinkDirection", specEnum(t, "LinkDirection"), []string{ledger.LinkOut, ledger.LinkIn})
	sameSet(t, "Relation", specEnum(t, "Relation"), strs(ledger.Relations))
	sameSet(t, "JudgeStage", specEnum(t, "JudgeStage"), strs(ledger.JudgeStages))
	sameSet(t, "VerdictOutcome", specEnum(t, "VerdictOutcome"), strs(ledger.VerdictOutcomes))
	sameSet(t, "JudgeState", specEnum(t, "JudgeState"), []string{ledger.JudgeWorking, ledger.JudgeJudged, ledger.JudgeFailed})
	sameSet(t, "ModelTier", specEnum(t, "ModelTier"), []string{ledger.TierPrimary, ledger.TierFallback, ledger.TierStrong})
	sameSet(t, "ConflictChoice", specEnum(t, "ConflictChoice"), strs(ledger.ConflictChoices))
	sameSet(t, "ConflictChange", specEnum(t, "ConflictChange"), ledger.ConflictChanges)
	sameSet(t, "UndoRefusal", specEnum(t, "UndoRefusal"), ledger.UndoRefusals)

	// Decision gates.
	sameSet(t, "GateStatus", specEnum(t, "GateStatus"), strs(ledger.GateStatuses))

	// Imports.
	sameSet(t, "ImportLocation", specEnum(t, "ImportLocation"), []string{string(ledger.ImportRepository), string(ledger.ImportHome), string(ledger.ImportV1)})
	// Switch to V2 and notes (migration 048).
	sameSet(t, "SwitchState", specEnum(t, "SwitchState"), []string{ledger.SwitchStateV1, ledger.SwitchStateRunning,
		ledger.SwitchStateSwitched, ledger.SwitchStateFailed, ledger.SwitchStateOff})
	sameSet(t, "SwitchStep", specEnum(t, "SwitchStep"), append(slices.Clone(ledger.SwitchSteps), ledger.SwitchStepDone))
	sameSet(t, "NoteOrigin", specEnum(t, "NoteOrigin"), strs(ledger.NoteOrigins))
	sameSet(t, "NoteDisposition", specEnum(t, "NoteDisposition"), strs(ledger.NoteDispositions))
	sameSet(t, "NoteHold", specEnum(t, "NoteHold"), strs(ledger.NoteHolds))
	sameSet(t, "ImportOutcome", specEnum(t, "ImportOutcome"), strs(ledger.ImportOutcomes))
	sameSet(t, "ImportSkipReason", specEnum(t, "ImportSkipReason"), strs(ledger.ImportSkipReasons))
	sameSet(t, "ImportCheckState", specEnum(t, "ImportCheckState"), ledger.ImportCheckStates)
	sameSet(t, "ImportHeld", specEnum(t, "ImportHeld"), ledger.ImportHelds)
	sameSet(t, "ImportChoice", specEnum(t, "ImportChoice"), strs(ledger.ImportChoices))

	// Settings (migration 050) and the Security page.
	sameSet(t, "NotificationEvent", specEnum(t, "NotificationEvent"), strs(ledger.NotificationEvents))
	sameSet(t, "DataHolds", specEnum(t, "DataHolds"), trust.Holds)
	sameSet(t, "Retention", specEnum(t, "Retention"), trust.Retentions)
	sameSet(t, "SubprocessorName", specEnum(t, "SubprocessorName"), trust.ProcessorNames)
	sameSet(t, "SubprocessorUseKind", specEnum(t, "SubprocessorUseKind"), trust.Uses)
}

// stringConsts parses a Go file for string constants whose names start
// with prefix: the policy codes and the error codes are plain constants,
// not lists, so the test reads them from the source.
func stringConsts(t *testing.T, path, prefix string) []string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	var out []string
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, s := range gd.Specs {
			vs := s.(*ast.ValueSpec)
			for i, name := range vs.Names {
				if !strings.HasPrefix(name.Name, prefix) || i >= len(vs.Values) {
					continue
				}
				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				v, err := strconv.Unquote(lit.Value)
				if err != nil {
					t.Fatal(err)
				}
				out = append(out, v)
			}
		}
	}
	if len(out) == 0 {
		t.Fatalf("no %s* constants in %s", prefix, path)
	}
	return out
}

// TestReceiptEnumsMatchTheSchema keeps ReceiptAction and ObjectKind equal
// to the CHECK constraints on v2.receipts, which admit the verbs of
// commands later epics add. Later migrations replace a constraint, so the
// last migration that defines it wins.
func TestReceiptEnumsMatchTheSchema(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob("../../../migrations/*.up.sql")
	if err != nil || len(files) == 0 {
		t.Fatalf("migrations: %v", err)
	}
	sort.Strings(files)
	check := func(constraint string) []string {
		var m [][]byte
		re := regexp.MustCompile(`(?s)CONSTRAINT ` + constraint + ` CHECK \(\w+ IN\s*\((.*?)\)\)`)
		for _, f := range files {
			sql, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			if found := re.FindSubmatch(sql); found != nil {
				m = found
			}
		}
		if m == nil {
			t.Fatalf("no migration defines %s", constraint)
		}
		var out []string
		for _, q := range regexp.MustCompile(`'([a-z_]+)'`).FindAllSubmatch(m[1], -1) {
			out = append(out, string(q[1]))
		}
		return out
	}
	sameSet(t, "ReceiptAction", specEnum(t, "ReceiptAction"), check("receipts_action_check"))
	sameSet(t, "ObjectKind", specEnum(t, "ObjectKind"), check("receipts_object_kind_check"))
	// The judge's verdicts (migration 035).
	sameSet(t, "JudgeStage", specEnum(t, "JudgeStage"), check("judge_verdicts_stage_check"))
	sameSet(t, "Relation", specEnum(t, "Relation"), check("judge_verdicts_verdict_check"))
	sameSet(t, "VerdictOutcome", specEnum(t, "VerdictOutcome"), check("judge_verdicts_outcome_check"))
	// Notes and the switch (migration 048).
	sameSet(t, "NoteDisposition", specEnum(t, "NoteDisposition"), check("note_refs_disposition_check"))
	sameSet(t, "NoteOrigin", specEnum(t, "NoteOrigin"), check("note_refs_origin_check"))
	sameSet(t, "SwitchStep", specEnum(t, "SwitchStep"), check("space_switches_step_check"))
}
