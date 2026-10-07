package ledger

import (
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// What a V1 memory becomes (plan 25 §10, D13), case by case.
func TestClassifyV1(t *testing.T) {
	t.Parallel()
	text := func(content string) v1Memory {
		return v1Memory{ID: uuid.New(), OwnerID: uuid.New(), Title: "t", Content: content, Length: len([]rune(content)),
			ContentType: "text", State: "active", Kind: "semantic", ByType: "human"}
	}
	with := func(m v1Memory, f func(*v1Memory)) v1Memory { f(&m); return m }
	long := strings.Repeat("word ", 500)
	cases := []struct {
		name        string
		m           v1Memory
		personal    bool
		author      string
		trust       policy.Trust
		disposition NoteDisposition
		hold        NoteHold
		section     Section
	}{
		{"a person's fact", text("We use pnpm."), false, "person", policy.TrustPerson, NoteCandidate, "", SectionConventions},
		{"a person's rationale", with(text("We chose River."), func(m *v1Memory) { m.Kind = "rationale" }), false,
			"person", policy.TrustPerson, NoteCandidate, "", SectionDecisions},
		{"a personal space's words", text("I like tabs."), true, "person", policy.TrustPerson, NoteCandidate, "", SectionPreferences},
		{"markdown", with(text("- Use pnpm."), func(m *v1Memory) { m.ContentType = "markdown" }), false,
			"person", policy.TrustPerson, NoteCandidate, "", SectionConventions},
		{"an agent's, by slug", with(text("CI is slow."), func(m *v1Memory) { m.BySlug = "claude-code" }), false,
			"agent", policy.TrustAgentOwnWork, NoteFold, "", ""},
		{"an agent's, by legacy source agent", with(text("CI is slow."), func(m *v1Memory) { m.SourceAgent = "cursor" }), false,
			"agent", policy.TrustAgentOwnWork, NoteFold, "", ""},
		{"a hook's capture", with(text("Session summary."), func(m *v1Memory) { m.CreatedVia = "hook" }), false,
			"agent", policy.TrustAgentOwnWork, NoteFold, "", ""},
		{"extraction", with(text("From CLAUDE.md."), func(m *v1Memory) { m.CreatedVia = "extraction" }), false,
			"agent", policy.TrustAgentOwnWork, NoteFold, "", ""},
		{"a long document", with(text(""), func(m *v1Memory) { m.Content, m.Length = "", len(long) }), false,
			"person", policy.TrustPerson, NoteFold, HoldLong, ""},
		{"a credential", text("The key is AKIAIOSFODNN7EXAMPLE."), false, "person", policy.TrustPerson, NoteOnly, HoldSecret, ""},
		{"an agent's credential", with(text("ghp_abcdefghijklmnopqrstuvwxyz0123456789"), func(m *v1Memory) { m.BySlug = "codex" }), false,
			"agent", policy.TrustAgentOwnWork, NoteOnly, HoldSecret, ""},
		{"archived", with(text("Old."), func(m *v1Memory) { m.State = "archived" }), false,
			"person", policy.TrustPerson, NoteOnly, HoldArchived, ""},
		{"a web page", with(text("A page."), func(m *v1Memory) { m.Source, m.ContentType = "url", "html" }), false,
			"person", policy.TrustExternal, NoteFold, HoldExternal, ""},
		{"a link by path", with(text("A link."), func(m *v1Memory) { m.SourcePath = "https://example.test" }), false,
			"person", policy.TrustExternal, NoteFold, HoldExternal, ""},
		{"a PDF", with(text("Extracted text."), func(m *v1Memory) { m.ContentType = "pdf" }), false,
			"person", policy.TrustPerson, NoteFold, HoldFormat, ""},
		{"text beside an attachment", with(text("See the file."), func(m *v1Memory) { m.Attachments = 1 }), false,
			"person", policy.TrustPerson, NoteCandidate, "", SectionConventions},
		{"still processing", with(text("Half done."), func(m *v1Memory) { m.State = "processing" }), false,
			"person", policy.TrustPerson, NoteOnly, HoldFormat, ""},
		{"a title only", with(text(""), func(m *v1Memory) { m.Title = "Use pnpm" }), false,
			"person", policy.TrustPerson, NoteCandidate, "", SectionConventions},
	}
	for _, c := range cases {
		got := classifyV1(c.m, c.personal)
		if got.AuthorKind != c.author || got.Trust != c.trust || got.Disposition != c.disposition || got.Hold != c.hold || got.Section != c.section {
			t.Errorf("%s: %s/%s/%s/%s/%s, want %s/%s/%s/%s/%s", c.name, got.AuthorKind, got.Trust, got.Disposition, got.Hold, got.Section,
				c.author, c.trust, c.disposition, c.hold, c.section)
		}
		if got.Disposition == NoteCandidate && got.Statement == "" {
			t.Errorf("%s: a candidate without a statement", c.name)
		}
	}
}

func TestRepoKeyAndConfigs(t *testing.T) {
	t.Parallel()
	for _, u := range []string{"https://github.com/Acme/Web.git", "git@github.com:acme/web", "acme/web", "github.com/acme/web/",
		"ssh://git@github.com/acme/web.git"} {
		if got := RepoKey(u); got != "acme/web" {
			t.Errorf("RepoKey(%q) = %q", u, got)
		}
	}
	belongs := []struct {
		scope string
		kind  policy.SpaceKind
		repo  string
		want  bool
	}{
		{"global", policy.SpacePersonal, "", true},
		{"profile:work", policy.SpacePersonal, "", true},
		{"global", policy.SpaceProject, "acme/web", false},
		{"project:https://github.com/acme/web", policy.SpaceProject, "acme/web", true},
		{"project:https://github.com/acme/web", policy.SpaceTeam, "acme/web", true},
		{"project:https://github.com/acme/api", policy.SpaceProject, "acme/web", false},
		{"project:https://github.com/acme/web", policy.SpaceProject, "", false},
		{"project:https://github.com/acme/web", policy.SpacePersonal, "acme/web", false},
	}
	for _, b := range belongs {
		if got := ConfigBelongs(b.scope, b.kind, b.repo); got != b.want {
			t.Errorf("ConfigBelongs(%q, %s, %q) = %v", b.scope, b.kind, b.repo, got)
		}
	}
	targets := map[string][]TargetKind{
		"CLAUDE.md":                       {TargetAgentsMD, TargetClaudeMD},
		"./AGENTS.md":                     {TargetAgentsMD},
		"GEMINI.md":                       {TargetAgentsMD, TargetGeminiMD},
		".cursor/rules/web.mdc":           {TargetAgentsMD, TargetCursorMDC},
		".cursorrules":                    {TargetAgentsMD, TargetCursorMDC},
		".github/copilot-instructions.md": {TargetAgentsMD},
		".windsurfrules":                  {TargetAgentsMD},
		".claude/rules/api.md":            {TargetAgentsMD},
		"SOUL.md":                         nil,
	}
	for p, want := range targets {
		if got := ConfigTargets(p); !slices.Equal(got, want) {
			t.Errorf("ConfigTargets(%q) = %v, want %v", p, got, want)
		}
	}
}
