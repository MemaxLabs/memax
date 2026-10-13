package conditions

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
)

func TestValidate(t *testing.T) {
	t.Parallel()
	good := []Condition{
		{ID: "c1", Kind: PathExists, Path: "packages/server/internal/ask/config.go", Origin: OriginDerived, From: FromStatement},
		{ID: "c2", Kind: PathExists, Glob: "packages/web/**", Origin: OriginDerived, From: FromScope},
		{ID: "c3", Kind: FileContains, Path: "packages/server/internal/ask/config.go", Text: "TierHaiku", Word: true, Origin: OriginDerived, From: FromStatement},
		{ID: "c4", Kind: DepPresent, Manifest: "go.mod", Name: "github.com/riverqueue/river", Origin: OriginJudge},
		{ID: "c5", Kind: FileUnchanged, Path: "docs/adr/004-queues.md", Blob: strings.Repeat("9f", 20), Origin: OriginPerson},
		{ID: "c6", Kind: PRState, Repo: "MemaxLabs/memax", Number: 198, State: "merged", Origin: OriginDream},
		{ID: "c7", Kind: DepPresent, Manifest: "packages/web/package.json", Name: "@memaxlabs/ledger", Range: "^2.0.0", Origin: OriginPerson},
		{ID: "c8", Kind: PathExists, Path: "app/(ledger)/[space]/page.tsx", Origin: OriginPerson},
	}
	if err := Validate(good); err != nil {
		t.Fatalf("the design's examples: %v", err)
	}
	bad := map[string]Condition{
		"no id":                  {Kind: PathExists, Path: "go.mod", Origin: OriginPerson},
		"an id of another shape": {ID: "x1", Kind: PathExists, Path: "go.mod", Origin: OriginPerson},
		"unknown kind":           {ID: "c1", Kind: "file_exists", Path: "go.mod", Origin: OriginPerson},
		"unknown origin":         {ID: "c1", Kind: PathExists, Path: "go.mod", Origin: "agent"},
		"derived, from where":    {ID: "c1", Kind: PathExists, Path: "go.mod", Origin: OriginDerived},
		"from on a person's":     {ID: "c1", Kind: PathExists, Path: "go.mod", Origin: OriginPerson, From: FromStatement},
		"path and glob":          {ID: "c1", Kind: PathExists, Path: "go.mod", Glob: "*.go", Origin: OriginPerson},
		"neither":                {ID: "c1", Kind: PathExists, Origin: OriginPerson},
		"absolute":               {ID: "c1", Kind: PathExists, Path: "/etc/passwd", Origin: OriginPerson},
		"up and out":             {ID: "c1", Kind: PathExists, Path: "../secrets", Origin: OriginPerson},
		"home":                   {ID: "c1", Kind: PathExists, Path: "~/.ssh/id_rsa", Origin: OriginPerson},
		"dot segment":            {ID: "c1", Kind: PathExists, Path: "./go.mod", Origin: OriginPerson},
		"backslash":              {ID: "c1", Kind: PathExists, Path: `src\main.go`, Origin: OriginPerson},
		"a space":                {ID: "c1", Kind: PathExists, Path: "my file.go", Origin: OriginPerson},
		"glob outside":           {ID: "c1", Kind: PathExists, Glob: "../**", Origin: OriginPerson},
		"glob characters":        {ID: "c1", Kind: PathExists, Glob: "src/{a,b}/*.go", Origin: OriginPerson},
		"extra field":            {ID: "c1", Kind: PathExists, Path: "go.mod", Text: "x", Origin: OriginPerson},
		"empty text":             {ID: "c1", Kind: FileContains, Path: "go.mod", Origin: OriginPerson},
		"long text":              {ID: "c1", Kind: FileContains, Path: "go.mod", Text: strings.Repeat("x", 201), Origin: OriginPerson},
		"two lines":              {ID: "c1", Kind: FileContains, Path: "go.mod", Text: "a\nb", Origin: OriginPerson},
		"a secret":               {ID: "c1", Kind: FileContains, Path: ".env", Text: "AKIAIOSFODNN7EXAMPLE", Origin: OriginPerson},
		"not a manifest":         {ID: "c1", Kind: DepPresent, Manifest: "deps.txt", Name: "river", Origin: OriginPerson},
		"no name":                {ID: "c1", Kind: DepPresent, Manifest: "go.mod", Origin: OriginPerson},
		"a name with spaces":     {ID: "c1", Kind: DepPresent, Manifest: "go.mod", Name: "river queue", Origin: OriginPerson},
		"blob of another kind":   {ID: "c1", Kind: FileUnchanged, Path: "a.md", Blob: "sha256:abc", Origin: OriginPerson},
		"repo without owner":     {ID: "c1", Kind: PRState, Repo: "memax", Number: 1, State: "open", Origin: OriginPerson},
		"no number":              {ID: "c1", Kind: PRState, Repo: "a/b", State: "open", Origin: OriginPerson},
		"unknown state":          {ID: "c1", Kind: PRState, Repo: "a/b", Number: 3, State: "draft", Origin: OriginPerson},
	}
	for name, c := range bad {
		if err := Validate([]Condition{c}); err == nil {
			t.Errorf("%s: %+v validated", name, c)
		}
	}
	if err := Validate(append(good, Condition{ID: "c9", Kind: PathExists, Path: "x.go", Origin: OriginPerson})); err == nil {
		t.Error("nine conditions validated")
	}
	if err := Validate([]Condition{good[0], good[0]}); err == nil {
		t.Error("a repeated id validated")
	}
}

func TestParseIsStrict(t *testing.T) {
	t.Parallel()
	if cs, err := Parse(nil); err != nil || len(cs) != 0 {
		t.Errorf("nothing: %v %v", cs, err)
	}
	if cs, err := Parse(json.RawMessage(`[]`)); err != nil || len(cs) != 0 {
		t.Errorf("[]: %v %v", cs, err)
	}
	if _, err := Parse(json.RawMessage(`[{"id":"c1","kind":"path_exists","path":"go.mod","origin":"person","note":"x"}]`)); err == nil {
		t.Error("an unknown field parsed")
	}
	if _, err := Parse(json.RawMessage(`["go.mod is there"]`)); err == nil {
		t.Error("a sentence parsed")
	}
	cs, err := Parse(json.RawMessage(`[{"id":"c1","kind":"path_exists","path":"go.mod","origin":"person"}]`))
	if err != nil || len(cs) != 1 || string(Encode(cs)) != `[{"id":"c1","kind":"path_exists","path":"go.mod","origin":"person"}]` {
		t.Errorf("round trip: %v %s", err, Encode(cs))
	}
}

func TestDerive(t *testing.T) {
	t.Parallel()
	type want = []Condition
	p := func(path string, from From) Condition {
		return Condition{Kind: PathExists, Path: path, Origin: OriginDerived, From: from}
	}
	cases := []struct {
		name string
		in   Input
		want want
	}{
		{"a backticked file, and an identifier in it",
			Input{Statement: "Ask's tiers are set in `packages/server/internal/ask/config.go`, as `TierHaiku`."},
			want{p("packages/server/internal/ask/config.go", FromStatement),
				{Kind: FileContains, Path: "packages/server/internal/ask/config.go", Text: "TierHaiku", Word: true, Origin: OriginDerived, From: FromStatement}}},
		{"a bare folder under a known top folder",
			Input{Statement: "Jobs live in packages/server/internal/queue."},
			want{p("packages/server/internal/queue", FromStatement)}},
		{"a bare file by its extension",
			Input{Statement: "Versions are pinned in pnpm-lock.yaml and go.mod."},
			want{p("pnpm-lock.yaml", FromStatement), p("go.mod", FromStatement)}},
		{"a Go module and a scoped npm package",
			Input{Statement: "Background jobs run on `github.com/riverqueue/river`; the web uses @tanstack/react-query@5."},
			want{{Kind: DepPresent, Manifest: "go.mod", Name: "github.com/riverqueue/river", Origin: OriginDerived, From: FromStatement},
				{Kind: DepPresent, Manifest: "package.json", Name: "@tanstack/react-query", Origin: OriginDerived, From: FromStatement}}},
		{"prose that only looks like paths",
			Input{Statement: "Agents read/propose/write; deploys go to Fly.io and Next.js serves memax.app/docs, e.g. version 1.2.3 and https://example.com/a.go."},
			want{}},
		{"Memax's own files and agents' files are never conditions",
			Input{Statement: "Conventions live in `AGENTS.md`, CLAUDE.md and `.cursor/rules/web.mdc`.", Files: []string{"CLAUDE.md:12", "AGENTS.md:40"}},
			want{}},
		{"code files the sources cite, after the statement's",
			Input{Statement: "The judge runs at temperature 0.", Files: []string{"packages/server/internal/judge/config.go:12", "go.mod:14", "CLAUDE.md:3"}},
			want{p("packages/server/internal/judge/config.go", FromSource), p("go.mod", FromSource)}},
		{"the scope's globs",
			Input{Statement: "Run tests with `pnpm test`.", ScopePaths: []string{"packages/web/**", "../outside/**"}},
			want{{Kind: PathExists, Glob: "packages/web/**", Origin: OriginDerived, From: FromScope}}},
		{"an identifier with two files names no file",
			Input{Statement: "`TierHaiku` moved from `a/config.go` to `a/tiers.go`."},
			want{p("a/config.go", FromStatement), p("a/tiers.go", FromStatement)}},
		{"at most four",
			Input{Statement: "See a/1.go, a/2.go, a/3.go, a/4.go and a/5.go."},
			want{p("a/1.go", FromStatement), p("a/2.go", FromStatement), p("a/3.go", FromStatement), p("a/4.go", FromStatement)}},
		{"a plain word in backticks is no identifier",
			Input{Statement: "Use `pnpm` in `go.mod`."},
			want{p("go.mod", FromStatement)}},
	}
	for _, c := range cases {
		got := Derive(c.in)
		if len(got) != len(c.want) {
			t.Errorf("%s: got %+v, want %+v", c.name, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%s: [%d] got %+v, want %+v", c.name, i, got[i], c.want[i])
			}
		}
	}
}

func TestRenewKeepsIDsAndWhatOthersAttached(t *testing.T) {
	t.Parallel()
	had := []Condition{
		{ID: "c1", Kind: PathExists, Path: "a/config.go", Origin: OriginDerived, From: FromStatement},
		{ID: "c2", Kind: DepPresent, Manifest: "go.mod", Name: "github.com/riverqueue/river", Origin: OriginPerson},
		{ID: "c3", Kind: PathExists, Path: "go.mod", Origin: OriginDerived, From: FromSource},
	}
	// The words now name the old file and a new one.
	derived := Derive(Input{Statement: "Tiers moved from `a/config.go` to `a/tiers.go`.", Files: []string{"go.mod"}})
	got := Renew(had, derived, FromStatement, FromSource, FromScope)
	if err := Validate(got); err != nil {
		t.Fatal(err)
	}
	ids := map[string]string{}
	for _, c := range got {
		ids[c.Path+c.Name] = c.ID
	}
	if ids["a/config.go"] != "c1" || ids["github.com/riverqueue/river"] != "c2" || ids["go.mod"] != "c3" || ids["a/tiers.go"] != "c4" {
		t.Errorf("ids %v", ids)
	}
	// Words that name nothing: the derived ones go, the person's stays.
	got = Renew(had, nil, FromStatement, FromSource, FromScope)
	if len(got) != 1 || got[0].ID != "c2" {
		t.Errorf("renewed to nothing: %+v", got)
	}
	// A new derived condition takes the lowest free id.
	got = Renew(got, Derive(Input{Statement: "See `b/x.go`."}), FromStatement)
	if len(got) != 2 || got[1].ID != "c1" {
		t.Errorf("after dropping: %+v", got)
	}
	// Attach: a person's conditions take the lowest free ids, and a repeat is one.
	sent := []Condition{{Kind: PathExists, Path: "b/x.go"}, {Kind: PRState, Repo: "a/b", Number: 7, State: "merged", Origin: OriginJudge, ID: "c1"}}
	got = Attach(got, sent, OriginPerson)
	if len(got) != 3 || got[2].ID != "c3" || got[2].Origin != OriginPerson {
		t.Errorf("attached %+v", got)
	}
}

func TestHashIsWhatItChecks(t *testing.T) {
	a := Condition{ID: "c1", Kind: PathExists, Path: "internal/ask/config.go", Origin: OriginDerived, From: FromStatement}
	b := Condition{ID: "c4", Kind: PathExists, Path: "internal/ask/config.go", Origin: OriginPerson}
	if Hash(a) != Hash(b) {
		t.Error("the id, origin and where it was read aren't what a condition checks")
	}
	if len(Hash(a)) != 64 {
		t.Errorf("hash = %q", Hash(a))
	}
	for _, c := range []Condition{
		{Kind: PathExists, Path: "internal/ask/tiers.go"},
		{Kind: PathExists, Glob: "internal/ask/config.go"},
		{Kind: FileContains, Path: "internal/ask/config.go", Text: "ASK_MODEL"},
		{Kind: FileContains, Path: "internal/ask/config.go", Text: "ASK_MODEL", Word: true},
	} {
		if Hash(c) == Hash(a) {
			t.Errorf("%+v hashes as %+v", c, a)
		}
	}
	// Pinned: daemons cache it, so the encoding never changes silently.
	if got := Hash(Condition{Kind: PathExists, Path: "go.mod"}); got != hashOf(`{"kind":"path_exists","path":"go.mod"}`) {
		t.Errorf("hash of go.mod = %s", got)
	}
}

func hashOf(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
