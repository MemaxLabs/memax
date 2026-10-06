package compile

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/lifecycle"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// Building a CompileInput from one snapshot of the record (plan 25
// Phase 0 carry-over, "compile service mapping"):
//
//   - only kept memories compile: lifecycle=kept maps to state "kept",
//     and a kept open question (section=open_question) to state "open" in
//     the Brief's "open" section;
//   - quarantined memories (trust external) never compile, kept or not:
//     the compiler refuses them, and leaving them out is how they stay
//     live over MCP only;
//   - superseded decisions don't compile: they stay kept, with their
//     history, and the decision that replaced them compiles instead;
//   - read scores are 0 until R- reads land (epic 1.7);
//   - scope globs and agents the compiler would refuse are dropped, with a
//     warning, rather than failing the whole file.

var (
	slugInvalid = regexp.MustCompile(`[^a-z0-9]+`)
	// The compiler's GLOB and AGENT patterns (validate.ts).
	globPattern  = regexp.MustCompile(`^[A-Za-z0-9._\-/*?\[\]!@+~]+$`)
	globWild     = regexp.MustCompile(`[*?\[\]!]+`)
	agentPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,39}$`)
	repoPathOK   = regexp.MustCompile(`^[A-Za-z0-9._\-/]+$`)
)

// The compiler's own limits on what it is given (validate.ts), in UTF-16
// code units, which is how JavaScript counts a string's length.
const (
	maxStatementUnits = 2000
	maxSpaceNameUnits = 120
)

// built is an input and what building it decided.
type built struct {
	input *Input
	// stale are the memories past stale_after at the compile time.
	stale []string
	// warnings are the memories (or parts) left out, for the run.
	warnings []ledger.CompileWarning
}

// buildInput maps a snapshot onto the compiler's input. The compile ID and
// time are filled in by the caller.
func buildInput(s *ledger.CompileSnapshot, appBase string, at time.Time) (*built, error) {
	t := s.View.Target
	if s.Brief == nil {
		return nil, fmt.Errorf("compile: space %s has no Brief", s.Space.ID)
	}
	slug := compileSlug(s.Space.Slug)
	name := strings.TrimSpace(s.Space.Name)
	if name == "" {
		name = slug
	}
	b := &built{input: &Input{
		Version: ContractVersion,
		Space: InputSpace{
			Slug: slug, Name: truncateUnits(name, maxSpaceNameUnits), Kind: string(s.Space.Kind),
			URL: spaceURL(appBase, slug),
		},
		Brief: InputBrief{ID: s.Brief.Ref, Title: s.Brief.Title, Summary: s.Brief.Summary},
	}}
	for _, sec := range s.Brief.Sections {
		items := make([]InputItem, 0, len(sec.Items))
		for _, it := range sec.Items {
			items = append(items, InputItem{Ref: it.Ref, Text: it.Text, Cites: it.Cites})
		}
		b.input.Brief.Sections = append(b.input.Brief.Sections, InputSection{Key: sec.Key, Heading: sec.Heading, Items: items})
	}
	if b.input.Brief.Sections == nil {
		b.input.Brief.Sections = []InputSection{}
	}

	b.input.Memories = []InputMemory{}
	for _, m := range s.Kept {
		if m.Lifecycle != lifecycle.Kept || m.Trust.External() {
			continue
		}
		// A superseded decision keeps its history in the record but is no
		// longer in force: agents read the one that replaced it.
		if m.Decision != nil && m.Decision.Status == ledger.DecisionSuperseded {
			continue
		}
		if utf16Len(m.Statement) > maxStatementUnits {
			b.warnings = append(b.warnings, ledger.CompileWarning{Code: "not_compiled", Ref: m.Ref,
				Message: fmt.Sprintf("%s is too long to compile. It stays live over MCP.", m.Ref)})
			continue
		}
		in := InputMemory{
			Ref: m.Ref, Statement: m.Statement, Section: string(m.Section), Kind: string(m.Kind), State: "kept",
			Trust: string(m.Trust),
		}
		if m.Section == ledger.SectionOpenQuestion {
			in.Section, in.State = "open", "open"
		}
		for _, f := range m.Flags {
			in.Flags = append(in.Flags, string(f))
		}
		if m.StaleAfter != nil {
			in.StaleAfter = m.StaleAfter.UTC().Format(time.RFC3339)
			if !m.StaleAfter.After(at) {
				b.stale = append(b.stale, m.Ref)
			}
		}
		in.Scope = b.scope(m)
		b.input.Memories = append(b.input.Memories, in)
	}
	b.input.Targets = []TargetSettings{targetSettings(t, s.Canonical)}
	return b, nil
}

// scope reads a memory's scope ({"paths": [...], "agents": [...]}),
// keeping what the compiler accepts.
func (b *built) scope(m ledger.Memory) *InputScope {
	if len(m.Applies) == 0 {
		return nil
	}
	var raw struct {
		Paths  []string `json:"paths"`
		Agents []string `json:"agents"`
	}
	if err := json.Unmarshal(m.Applies, &raw); err != nil {
		return nil
	}
	sc := &InputScope{}
	for _, g := range raw.Paths {
		if validGlob(g) {
			sc.Paths = append(sc.Paths, g)
		} else {
			b.warnings = append(b.warnings, ledger.CompileWarning{Code: "not_compiled", Ref: m.Ref,
				Message: fmt.Sprintf("Left out the path %q of %s: use a glob relative to the repository root.", g, m.Ref)})
		}
	}
	for _, a := range raw.Agents {
		if agentPattern.MatchString(a) {
			sc.Agents = append(sc.Agents, a)
		}
	}
	if len(sc.Paths) == 0 && len(sc.Agents) == 0 {
		return nil
	}
	return sc
}

// validGlob is the compiler's check on scope.paths.
func validGlob(g string) bool {
	if !globPattern.MatchString(g) || strings.HasPrefix(g, "!") {
		return false
	}
	p := globWild.ReplaceAllString(g, "x")
	if p == "" || len(p) > 300 || !repoPathOK.MatchString(p) {
		return false
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
	}
	return true
}

// targetSettings maps a target row onto the compiler's settings, setting
// only what applies to the kind.
func targetSettings(t *ledger.Target, canonical string) TargetSettings {
	ts := TargetSettings{
		Kind: string(t.Kind), Path: t.Path,
		Include: string(t.Settings.Include), Stale: string(t.Settings.Stale), SizeBudget: t.Settings.SizeBudget,
	}
	switch t.Kind {
	case ledger.TargetAgentsMD, ledger.TargetChatGPT:
		ts.Scoped = string(t.Settings.Scoped)
	default:
		if canonical != "" && canonical != "AGENTS.md" {
			ts.CanonicalPath = canonical
		}
	}
	if (t.Kind == ledger.TargetClaudeMD || t.Kind == ledger.TargetGeminiMD) && t.Settings.UserOwned {
		ts.UserOwned = true
		empty := ""
		ts.Current = &empty
	}
	return ts
}

// inputHash fingerprints what a compile reads: the input without its own
// ID and time, plus which memories are stale at that time. Two compiles
// with the same hash produce the same bytes under the same ID and time.
func inputHash(in *Input, stale []string) string {
	cp := *in
	cp.Compile = Run{}
	raw, _ := json.Marshal(struct {
		Input *Input   `json:"input"`
		Stale []string `json:"stale"`
	}{&cp, stale})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// compileSlug makes a space slug the compiler accepts: lower case, a-z,
// 0-9 and single hyphens, at most 64 characters.
func compileSlug(slug string) string {
	s := strings.Trim(slugInvalid.ReplaceAllString(strings.ToLower(slug), "-"), "-")
	if len(s) > 64 {
		s = strings.TrimRight(s[:64], "-")
	}
	if s == "" {
		return "space"
	}
	return s
}

// spaceURL is where a person edits the space's Brief.
func spaceURL(base, slug string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if u, err := url.Parse(base); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		base = "https://memax.app"
	}
	return base + "/" + slug + "/brief"
}

func utf16Len(s string) int { return len(utf16.Encode([]rune(s))) }

func truncateUnits(s string, n int) string {
	for utf16Len(s) > n {
		_, size := utf8.DecodeLastRuneInString(s)
		s = s[:len(s)-size]
	}
	return s
}

// outputs maps a compile result onto the run's outputs (without content).
func outputs(res *Result) []ledger.CompiledOutput {
	out := make([]ledger.CompiledOutput, 0, len(res.Files)+len(res.Copies))
	for _, f := range res.Files {
		out = append(out, ledger.CompiledOutput{
			Path: f.Path, SHA256: f.SHA256, DriftSHA256: f.DriftSHA256, Bytes: f.Bytes, Lines: f.Lines,
			Refs: nonNil(f.Refs), Cites: nonNil(f.Cites), DroppedForBudget: nonNil(f.DroppedForBudget), UserOwned: f.UserOwned,
		})
	}
	for _, c := range res.Copies {
		out = append(out, ledger.CompiledOutput{
			Label: c.Label, SHA256: c.SHA256, DriftSHA256: c.SHA256, Bytes: c.Bytes, Lines: c.Lines,
			Refs: nonNil(c.Refs), Cites: nonNil(c.Cites), DroppedForBudget: nonNil(c.DroppedForBudget),
		})
	}
	return out
}

// totals sums a run's outputs.
func totals(outs []ledger.CompiledOutput) (bytes, lines int, refs, dropped []string) {
	refs, dropped = []string{}, []string{}
	for _, o := range outs {
		bytes += o.Bytes
		lines += o.Lines
		for _, r := range o.Refs {
			if !slices.Contains(refs, r) {
				refs = append(refs, r)
			}
		}
		for _, r := range o.DroppedForBudget {
			if !slices.Contains(dropped, r) {
				dropped = append(dropped, r)
			}
		}
	}
	slices.Sort(refs)
	slices.Sort(dropped)
	return bytes, lines, refs, dropped
}

func warnings(res *Result, extra []ledger.CompileWarning) []ledger.CompileWarning {
	out := slices.Clone(extra)
	for _, w := range res.Warnings {
		out = append(out, ledger.CompileWarning{Code: w.Code, Message: w.Message, Path: w.Path, Ref: w.Ref})
	}
	if out == nil {
		out = []ledger.CompileWarning{}
	}
	return out
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// actor is Memax, the compile coordinator's actor on receipts.
var actor = ledger.Actor{Kind: policy.ActorMemax, Name: "Memax", Credential: policy.CredentialSession}
