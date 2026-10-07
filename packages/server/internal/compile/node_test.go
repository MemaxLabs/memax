package compile_test

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/compile"
	"github.com/MemaxLabs/memax/packages/server/internal/compile/compiletest"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// The Go side and the compiler agree on the adapters: every kind, the
// default set (the Phase 1 targets), the opt-in set (never a default) and
// each default path.
func TestRealServiceMatchesTheLedgersTargets(t *testing.T) {
	t.Parallel()
	client := compiletest.Client(compiletest.StartService(t))
	h, err := client.Health(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if h.Status != "ok" || h.ContractVersion != compile.ContractVersion {
		t.Fatalf("health = %+v", h)
	}
	var kinds, defaults, optIn []string
	for _, a := range h.Adapters {
		kinds = append(kinds, a.Kind)
		if a.Default {
			defaults = append(defaults, a.Kind)
		}
		if a.OptIn {
			optIn = append(optIn, a.Kind)
			if a.Default {
				t.Errorf("%s is both opt-in and a default", a.Kind)
			}
		}
		want := ledger.TargetKind(a.Kind).DefaultPath()
		got := ""
		if a.DefaultPath != nil {
			got = *a.DefaultPath
		}
		if got != want {
			t.Errorf("%s: compiler path %q, ledger %q", a.Kind, got, want)
		}
	}
	var goKinds, goDefaults []string
	for _, k := range ledger.TargetKinds {
		goKinds = append(goKinds, string(k))
	}
	for _, k := range ledger.DefaultTargetKinds {
		goDefaults = append(goDefaults, string(k))
	}
	if !slices.Equal(kinds, goKinds) || !slices.Equal(defaults, goDefaults) {
		t.Errorf("compiler kinds %v (defaults %v), ledger %v (defaults %v)", kinds, defaults, goKinds, goDefaults)
	}
	var goOptIn []string
	for _, k := range ledger.OptInTargetKinds {
		goOptIn = append(goOptIn, string(k))
		if slices.Contains(ledger.DefaultTargetKinds, k) {
			t.Errorf("%s is both opt-in and a default in the ledger", k)
		}
	}
	if !slices.Equal(optIn, goOptIn) {
		t.Errorf("compiler opt-in kinds %v, ledger %v", optIn, goOptIn)
	}
}

// The compiler's demo input, through the Go client: the same bytes as the
// compiler's golden AGENTS.md.
func TestRealServiceCompilesTheGoldenDemo(t *testing.T) {
	t.Parallel()
	client := compiletest.Client(compiletest.StartService(t))
	dir := filepath.Join(compiletest.RepoRoot(), "packages", "compiler", "test", "fixtures", "demo-memax-v2")
	raw, err := os.ReadFile(filepath.Join(dir, "input.json"))
	if err != nil {
		t.Fatal(err)
	}
	var in compile.Input
	if err := json.Unmarshal(raw, &in); err != nil {
		t.Fatal(err)
	}
	res, err := client.Compile(context.Background(), &in)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := os.ReadFile(filepath.Join(dir, "expected", "AGENTS.md"))
	var agents string
	for _, f := range res.Files {
		if f.Path == "AGENTS.md" {
			agents = f.Content
		}
	}
	if agents != string(want) {
		t.Errorf("AGENTS.md differs from the golden file:\n%s", agents)
	}
	// A refused input is a permanent InputError with every issue.
	in.Memories[0].State = "proposed"
	in.Targets[0].SizeBudget = 10
	_, err = client.Compile(context.Background(), &in)
	var ie *compile.InputError
	if !errors.As(err, &ie) || len(ie.Issues) < 2 || !strings.HasPrefix(ie.Issues[0].InstancePath, "/") {
		t.Errorf("refused input: %v", err)
	}
}

// The service takes only its token: health is public, and a client with
// no token or a wrong one is refused at once, without retries, and told
// what to set. The token never shows up in an error.
func TestRealServiceRequiresTheToken(t *testing.T) {
	t.Parallel()
	url := compiletest.StartService(t)
	if compiletest.Token() == "" {
		t.Skip("the external service under test has no token")
	}
	if _, err := compile.NewClient(url).Health(context.Background()); err != nil {
		t.Fatalf("health without a token: %v", err)
	}
	for name, client := range map[string]*compile.Client{
		"no token":    compile.NewClient(url, compile.WithAttempts(3)),
		"wrong token": compile.NewClient(url, compile.WithAttempts(3), compile.WithToken("not-"+compiletest.Token())),
	} {
		start := time.Now()
		_, err := client.Compile(context.Background(), &compile.Input{})
		if err == nil || errors.Is(err, compile.ErrUnavailable) || !strings.Contains(err.Error(), "COMPILE_SERVICE_TOKEN") {
			t.Errorf("%s: %v", name, err)
		}
		if err != nil && strings.Contains(err.Error(), compiletest.Token()) {
			t.Errorf("%s: the error names the token: %v", name, err)
		}
		if d := time.Since(start); d > time.Second {
			t.Errorf("%s: refused only after %v; a 401 isn't retried", name, d)
		}
	}
}

// WithToken sends the token as a bearer token on every request, and an
// empty one sends no Authorization header.
func TestClientSendsTheToken(t *testing.T) {
	t.Parallel()
	got := make(chan string, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got <- r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","contract_version":1,"adapters":[]}`))
	}))
	defer srv.Close()
	for _, tc := range []struct{ token, want string }{
		{" s3cret ", "Bearer s3cret"},
		{"", ""},
	} {
		if _, err := compile.NewClient(srv.URL, compile.WithToken(tc.token)).Health(context.Background()); err != nil {
			t.Fatal(err)
		}
		if h := <-got; h != tc.want {
			t.Errorf("token %q: Authorization %q, want %q", tc.token, h, tc.want)
		}
	}
}

// Connection failures are retried, then reported (the job retries).
func TestClientRetriesThenFails(t *testing.T) {
	t.Parallel()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	client := compile.NewClient("http://"+addr, compile.WithAttempts(3), compile.WithBackoff(10*time.Millisecond),
		compile.WithTimeout(200*time.Millisecond), compile.WithClientLogger(quiet))
	start := time.Now()
	_, err = client.Compile(context.Background(), &compile.Input{})
	var ie *compile.InputError
	if err == nil || errors.As(err, &ie) || !strings.Contains(err.Error(), "after 3 attempts") {
		t.Errorf("unreachable service: %v", err)
	}
	if d := time.Since(start); d < 30*time.Millisecond {
		t.Errorf("no backoff between attempts (%v)", d)
	}
	if compile.NewClient("  ") != nil {
		t.Error("an empty URL must disable the client")
	}
}

// The coordinator with the real compiler: what the files say, and a hand
// edit read back by the real parse-back.
func TestCoordinatorWithTheRealService(t *testing.T) {
	t.Parallel()
	f := newFixture(t, fixtureOpts{compiler: compiletest.Client(compiletest.StartService(t))})
	s := f.seed()
	scoped := f.apply(&ledger.Remember{Meta: f.meta(s.owner, policy.ViaWeb), NewMemory: ledger.NewMemory{
		SpaceID: s.space, Statement: "Web screens use only packages/ledger components.", Section: ledger.SectionConventions,
		Applies: json.RawMessage(`{"paths": ["packages/web/**"]}`)}}).Memory
	for _, k := range ledger.DefaultTargetKinds {
		f.run(s.targets[k])
	}
	ctx := context.Background()
	preview := func(k ledger.TargetKind) *compile.Preview {
		t.Helper()
		p, err := f.svc.Preview(ctx, f.scope(s.owner), s.targets[k].ID)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	agents := preview(ledger.TargetAgentsMD)
	body := agents.Files[0].Content
	for _, want := range []string{
		"<!-- Compiled by Memax from memax-v2-", "(" + agents.Compile.Ref + ")", "# Memax V2 engineering brief", "## Decisions",
		"- Background jobs run on River. [" + s.river.Ref + "]", "### In `packages/web/**`",
		"[" + scoped.Ref + "]", "## Open", "Fly.io or Railway is undecided. [" + s.open.Ref + "]",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("AGENTS.md lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, s.prop.Statement) || strings.Contains(body, s.external.Statement) {
		t.Errorf("a proposal or quarantined memory compiled:\n%s", body)
	}
	if c := preview(ledger.TargetClaudeMD).Files[0].Content; !strings.Contains(c, "@AGENTS.md") {
		t.Errorf("CLAUDE.md isn't a shim:\n%s", c)
	}
	cursor := preview(ledger.TargetCursorMDC)
	if len(cursor.Files) != 1 || !strings.HasPrefix(cursor.Files[0].Path, ".cursor/rules/memax-") ||
		!strings.Contains(cursor.Files[0].Content, "globs: packages/web/**") || cursor.Reads != "AGENTS.md" {
		t.Errorf("cursor = %+v", cursor)
	}
	if gpt := preview(ledger.TargetChatGPT); len(gpt.Copies) != 1 || !strings.Contains(gpt.Copies[0].Content, s.river.Statement) {
		t.Errorf("chatgpt = %+v", gpt.Copies)
	}

	// Deliver, hand-edit, observe: the real parse-back's changes.
	tg := s.targets[ledger.TargetAgentsMD]
	f.apply(&ledger.RecordDelivery{Meta: f.meta(s.owner, policy.ViaCLI), Target: tg.ID, Compile: agents.Compile.Ref, SHA256: agents.Compile.DriftSHA256})
	edited := strings.Replace(body, "pnpm workspaces only.", "pnpm workspaces only; never npm.", 1)
	res, err := f.svc.Observe(ctx, f.meta(s.owner, policy.ViaCLI), tg.ID, compile.ObserveInput{
		Path: "AGENTS.md", Content: edited, ObserverKind: ledger.ObserverDevice, ObserverID: "zz-laptop"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Observations) != 1 {
		t.Fatalf("observation = %+v", res)
	}
	ch := res.Observations[0].Changes.Changes
	if len(ch) != 1 || ch[0].Kind != ledger.ChangeEdit || ch[0].Ref != s.pnpm.Ref || ch[0].NewText != "pnpm workspaces only; never npm." {
		t.Errorf("changes = %+v", ch)
	}
	if same, err := f.svc.Observe(ctx, f.meta(s.owner, policy.ViaCLI), tg.ID, compile.ObserveInput{
		Path: "AGENTS.md", Content: edited, ObserverKind: ledger.ObserverDevice, ObserverID: "zz-laptop"}); err != nil || same.Unchanged {
		// The same edit again is a fresh observation of the same file
		// (it dismisses the first), not "unchanged": the baseline is still
		// the delivered compile.
		t.Errorf("observing the same edit again: unchanged=%v %v", same.Unchanged, err)
	}
}
