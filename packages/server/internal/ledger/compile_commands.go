package ledger

import (
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// The Brief, target and compile commands (plan 25 §5.7). Like the memory
// commands, each is receipted, idempotent and decided by policy.
const (
	CommandReviseBrief       CommandName = "revise_brief"
	CommandConfigureTarget   CommandName = "configure_target"
	CommandRequestCompile    CommandName = "request_compile"
	CommandRecordCompile     CommandName = "record_compile"
	CommandRecordDelivery    CommandName = "record_delivery"
	CommandRecordObservation CommandName = "record_observation"
	CommandResolveDrift      CommandName = "resolve_drift"
)

// Limits on the Brief and on what a compile or an observation may carry.
const (
	MaxBriefTitleRunes   = 200
	MaxBriefSummaryRunes = 500
	MaxBriefSections     = 20
	MaxBriefItems        = 400
	MaxBriefHeadingRunes = 80
	MaxBriefProseRunes   = 500
	MaxBriefCites        = 20
	MaxCompileError      = 2000
	MaxCompileOutputs    = 200
	MaxDriftChanges      = 500
	MaxObserverIDRunes   = 200
)

// ReviseBrief writes a new version (B-) of the space's Brief. Every memory
// item must be a kept memory of the space; every prose line must cite at
// least one, and cite nothing forgotten or rejected. Every target of the
// space is recompiled.
type ReviseBrief struct {
	Meta
	SpaceID uuid.UUID
	// ExpectedVersion is the version the editor started from (If-Match);
	// 0 writes the space's first Brief.
	ExpectedVersion int
	Title           string
	Summary         string
	Sections        []BriefSection
}

// Name implements Command.
func (*ReviseBrief) Name() CommandName { return CommandReviseBrief }

// TargetSettingsInput changes some of a target's settings; nil fields stay.
type TargetSettingsInput struct {
	Include    *IncludeMode `json:"include,omitempty"`
	Stale      *StaleMode   `json:"stale,omitempty"`
	SizeBudget *int         `json:"size_budget,omitempty"`
	Scoped     *ScopedMode  `json:"scoped,omitempty"`
	UserOwned  *bool        `json:"user_owned,omitempty"`
}

// ConfigureTarget adds a target to a space (Target unset) or changes one.
// Either way the target is recompiled.
type ConfigureTarget struct {
	Meta
	// Target is the target to change; uuid.Nil adds one to SpaceID.
	Target uuid.UUID
	// SpaceID and Kind say where and what to add.
	SpaceID uuid.UUID
	Kind    TargetKind
	// ExpectedVersion, when set, must be the target's version (If-Match).
	ExpectedVersion int
	// Path defaults to the kind's own (AGENTS.md, CLAUDE.md, .cursor/rules).
	Path     *string
	Settings *TargetSettingsInput
	// Delivery defaults to local for files and copy for ChatGPT.
	Delivery *Delivery
	// Enabled turns compiling back on (true) or off (false).
	Enabled *bool
}

// Name implements Command.
func (*ConfigureTarget) Name() CommandName { return CommandConfigureTarget }

// RequestCompile asks for a fresh compile of one target ("Compile now").
type RequestCompile struct {
	Meta
	Target uuid.UUID
}

// Name implements Command.
func (*RequestCompile) Name() CommandName { return CommandRequestCompile }

// RecordCompile records one compile run, as Memax. The compile
// coordinator calls it after the compile service answered and the
// artifact is stored. It is refused with ErrBehind when the target moved
// past Generation meanwhile (§5.7 step 3: the job snoozes and compiles
// again), unless AllowBehind is set.
type RecordCompile struct {
	Meta
	Target uuid.UUID
	// Ref is the run's display ID, reserved with ReserveCompileRef before
	// compiling, because the compiled header carries it.
	Ref          string
	Generation   int64
	BriefID      uuid.UUID
	BriefVersion int
	InputSHA256  string
	// Error marks a failed run: the compiler refused the input. It is a
	// message about the input's shape and never holds memory text.
	Error            string
	OutputSHA256     string
	DriftSHA256      string
	ArtifactKey      string
	Bytes            int
	Lines            int
	Refs             []string
	DroppedForBudget []string
	Files            []CompiledOutput
	Warnings         []CompileWarning
	EnqueuedAt       time.Time
	StartedAt        time.Time
	CompiledAt       time.Time
	// AllowBehind records the run although the target moved on (the job
	// gave up waiting for a quiet moment); compiled_gen then stays behind
	// dirty_gen and the sweeper compiles again.
	AllowBehind bool
	// Finish, when set, runs in the transaction that records the run (see
	// Finisher): the compile job completes itself there.
	Finish Finisher `json:"-"`
}

// Name implements Command.
func (*RecordCompile) Name() CommandName { return CommandRecordCompile }

// RecordDelivery acknowledges that a compile run's output is on disk (the
// daemon wrote it) or merged (GitHub). SHA256 is the run's drift hash as
// the writer computed it from what it wrote.
type RecordDelivery struct {
	Meta
	Target uuid.UUID
	// Compile is the run, by display ID (C-0881) or id.
	Compile string
	SHA256  string
}

// Name implements Command.
func (*RecordDelivery) Name() CommandName { return CommandRecordDelivery }

// RecordObservation reports a compiled file seen changed outside Memax:
// the observed content (already stored under ArtifactKey) and what the
// compiler's parse-back made of it. A file that matches the baseline is
// no drift, and nothing is written.
type RecordObservation struct {
	Meta
	Target uuid.UUID
	Path   string
	// ObservedSHA256 is the compiler's drift hash of the content.
	ObservedSHA256 string
	ObserverKind   string
	ObserverID     string
	Commit         string
	ArtifactKey    string
	Bytes          int
	// BaseCompileID and BaseSHA256 are what the edit was compared against.
	BaseCompileID *uuid.UUID
	BaseSHA256    string
	Changes       ChangeSet
}

// Name implements Command.
func (*RecordObservation) Name() CommandName { return CommandRecordObservation }

// ResolveDrift resolves a target's open hand edits: pull them back as
// proposals, overwrite them, or stop compiling the target.
type ResolveDrift struct {
	Meta
	Target uuid.UUID
	Mode   DriftMode
	// Observation resolves one file's edit; uuid.Nil resolves every open
	// one of the target.
	Observation uuid.UUID
}

// Name implements Command.
func (*ResolveDrift) Name() CommandName { return CommandResolveDrift }

var (
	sectionKey = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,39}$`)
	sha256Hex  = regexp.MustCompile(`^[0-9a-f]{64}$`)
	commitSHA  = regexp.MustCompile(`^[0-9a-f]{7,64}$`)
	repoPath   = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)
)

func (c *ReviseBrief) validate() error {
	if c.SpaceID == uuid.Nil {
		return invalid("space_id", "say which space's Brief to revise")
	}
	if c.ExpectedVersion < 0 {
		return invalid("expected_version", "send the version you started from (If-Match)")
	}
	c.Title = strings.TrimSpace(c.Title)
	if err := checkText("title", c.Title, MaxBriefTitleRunes, true); err != nil {
		return err
	}
	c.Summary = strings.TrimSpace(c.Summary)
	if err := checkText("summary", c.Summary, MaxBriefSummaryRunes, false); err != nil {
		return err
	}
	if len(c.Sections) > MaxBriefSections {
		return invalid("sections", "a Brief has at most %d sections", MaxBriefSections)
	}
	keys := map[string]bool{}
	placed := map[string]bool{}
	items := 0
	for i := range c.Sections {
		s := &c.Sections[i]
		if !sectionKey.MatchString(s.Key) {
			return invalid("sections.key", "use a lowercase key like decisions, conventions or open")
		}
		if keys[s.Key] {
			return invalid("sections.key", "%s appears more than once", s.Key)
		}
		keys[s.Key] = true
		s.Heading = strings.TrimSpace(s.Heading)
		if err := checkLine("sections.heading", s.Heading, MaxBriefHeadingRunes, true); err != nil {
			return err
		}
		items += len(s.Items)
		if items > MaxBriefItems {
			return invalid("sections.items", "a Brief holds at most %d items", MaxBriefItems)
		}
		if s.Items == nil {
			s.Items = []BriefItem{}
		}
		for j := range s.Items {
			if err := s.Items[j].normalize(placed); err != nil {
				return err
			}
		}
	}
	if c.Sections == nil {
		c.Sections = []BriefSection{}
	}
	return nil
}

// normalize checks one item and writes its refs in canonical form (M-0219).
func (it *BriefItem) normalize(placed map[string]bool) error {
	it.Text = strings.TrimSpace(it.Text)
	if strings.TrimSpace(it.Ref) != "" {
		if it.Text != "" || len(it.Cites) > 0 {
			return invalid("sections.items", "an item is a memory ref or cited prose, not both")
		}
		ref, err := memoryRef("sections.items.ref", it.Ref)
		if err != nil {
			return err
		}
		if placed[ref] {
			return invalid("sections.items.ref", "%s is placed more than once", ref)
		}
		placed[ref] = true
		it.Ref = ref
		return nil
	}
	if err := checkLine("sections.items.text", it.Text, MaxBriefProseRunes, true); err != nil {
		return err
	}
	if len(it.Cites) == 0 {
		return invalid("sections.items.cites", "prose must cite at least one memory")
	}
	if len(it.Cites) > MaxBriefCites {
		return invalid("sections.items.cites", "cite at most %d memories on one line", MaxBriefCites)
	}
	cites := make([]string, 0, len(it.Cites))
	for _, c := range it.Cites {
		ref, err := memoryRef("sections.items.cites", c)
		if err != nil {
			return err
		}
		if !slices.Contains(cites, ref) {
			cites = append(cites, ref)
		}
	}
	it.Cites = cites
	return nil
}

// memoryRef parses a memory display ID into its canonical form.
func memoryRef(field, s string) (string, error) {
	p, n, ok := ParseRef(s)
	if !ok || p != PrefixMemory {
		return "", invalid(field, "use memory display IDs like M-0219")
	}
	return FormatRef(p, n), nil
}

// checkLine is checkText for one line: no line breaks or other control
// characters, which would let a heading or prose add lines to a file.
func checkLine(field, s string, maxRunes int, required bool) error {
	if err := checkText(field, s, maxRunes, required); err != nil {
		return err
	}
	if strings.ContainsFunc(s, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return invalid(field, "must be one line of text")
	}
	return nil
}

func (c *ConfigureTarget) validate() error {
	if c.ExpectedVersion < 0 {
		return invalid("expected_version", "send the target's version (If-Match)")
	}
	if c.Settings != nil {
		if err := c.Settings.validate(); err != nil {
			return err
		}
	}
	if c.Delivery != nil && !c.Delivery.Valid() {
		return invalid("delivery", "use local, pr, mcp or copy")
	}
	if c.Target != uuid.Nil {
		if c.Kind != "" || c.SpaceID != uuid.Nil {
			return invalid("kind", "a target's kind and space are fixed; add a new target instead")
		}
		if c.Path == nil && c.Settings == nil && c.Delivery == nil && c.Enabled == nil {
			return invalid("body", "say what to change: path, settings, delivery or enabled")
		}
		return nil
	}
	if c.SpaceID == uuid.Nil {
		return invalid("space_id", "say which space the target belongs to")
	}
	if !c.Kind.Valid() {
		return invalid("kind", "use agents_md, claude_md, cursor_mdc, chatgpt, gemini_md, copilot, windsurf or claude_rules")
	}
	if c.Enabled != nil && !*c.Enabled {
		return invalid("enabled", "a new target compiles; stop it once it exists")
	}
	return nil
}

func (s *TargetSettingsInput) validate() error {
	if s.Include != nil && *s.Include != IncludeKeptOnly && *s.Include != IncludeKeptAndOpen {
		return invalid("settings.include", "use kept_only or kept_and_open")
	}
	if s.Stale != nil && *s.Stale != StaleMark && *s.Stale != StaleOmit {
		return invalid("settings.stale", "use mark or omit")
	}
	if s.SizeBudget != nil && (*s.SizeBudget < MinSizeBudget || *s.SizeBudget > MaxSizeBudget) {
		return invalid("settings.size_budget", "use %d to %d bytes", MinSizeBudget, MaxSizeBudget)
	}
	if s.Scoped != nil && *s.Scoped != ScopedInline && *s.Scoped != ScopedOmit {
		return invalid("settings.scoped", "use inline or omit")
	}
	return nil
}

// defaultSettings are a new target's settings before any input.
func defaultSettings(k TargetKind) TargetSettings {
	s := TargetSettings{Include: IncludeKeptAndOpen, Stale: StaleMark, SizeBudget: DefaultSizeBudget}
	if k == TargetAgentsMD || k == TargetChatGPT {
		s.Scoped = ScopedInline
	}
	return s
}

// apply merges the input into s and checks it fits the kind.
func (s TargetSettings) apply(in *TargetSettingsInput, k TargetKind) (TargetSettings, error) {
	if in == nil {
		return s, nil
	}
	if in.Include != nil {
		s.Include = *in.Include
	}
	if in.Stale != nil {
		s.Stale = *in.Stale
	}
	if in.SizeBudget != nil {
		s.SizeBudget = *in.SizeBudget
	}
	if in.Scoped != nil {
		if k.role() != "canonical" && k.role() != "copy" {
			return s, invalid("settings.scoped", "only agents_md and chatgpt inline scoped facts")
		}
		s.Scoped = *in.Scoped
	}
	if in.UserOwned != nil {
		if k.role() != "shim" && *in.UserOwned {
			return s, invalid("settings.user_owned", "only the shims (claude_md, gemini_md) can live in a file you own")
		}
		s.UserOwned = *in.UserOwned
	}
	return s, nil
}

// checkTargetPath checks a path the way the compiler's adapters do: a
// repository-relative POSIX path, a file named for the tool for AGENTS.md
// and the shims, a rules directory for the scoped adapters, and nothing
// for ChatGPT.
func checkTargetPath(k TargetKind, path string) error {
	if k == TargetChatGPT {
		if path != "" {
			return invalid("path", "ChatGPT is copied out, so it takes no path")
		}
		return nil
	}
	if !ValidPath(path) {
		return invalid("path", "use a path relative to the repository root, with no .. and no leading /")
	}
	base := path[strings.LastIndexByte(path, '/')+1:]
	hasDir := func(dirs ...string) bool {
		for _, d := range dirs {
			if strings.Contains("/"+path+"/", "/"+d+"/") {
				return true
			}
		}
		return false
	}
	ok := false
	hint := ""
	switch k {
	case TargetAgentsMD:
		ok, hint = base == "AGENTS.md", "end it in AGENTS.md"
	case TargetClaudeMD:
		ok, hint = base == "CLAUDE.md" || base == "CLAUDE.local.md", "end it in CLAUDE.md or CLAUDE.local.md"
	case TargetGeminiMD:
		ok, hint = base == "GEMINI.md", "end it in GEMINI.md"
	case TargetCursorMDC:
		ok, hint = hasDir(".cursor/rules"), "put it inside .cursor/rules"
	case TargetCopilot:
		ok, hint = hasDir(".github/instructions"), "put it inside .github/instructions"
	case TargetWindsurf:
		ok, hint = hasDir(".devin/rules", ".windsurf/rules"), "put it inside .devin/rules or .windsurf/rules"
	case TargetClaudeRules:
		ok, hint = hasDir(".claude/rules"), "put it inside .claude/rules"
	}
	if !ok {
		return invalid("path", "%s: %s", k, hint)
	}
	return nil
}

// ValidPath mirrors v2.repo_path_valid and the compiler's isRepoPath.
func ValidPath(p string) bool {
	if p == "" || utf8.RuneCountInString(p) > 300 || !repoPath.MatchString(p) {
		return false
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
	}
	return true
}

func (c *RequestCompile) validate() error {
	if c.Target == uuid.Nil {
		return invalid("target", "say which target to compile")
	}
	return nil
}

func (c *RecordCompile) validate() error {
	switch {
	case c.Target == uuid.Nil:
		return invalid("target", "say which target was compiled")
	case c.Generation < 1:
		return invalid("generation", "must be the generation compiled")
	case c.BriefID == uuid.Nil || c.BriefVersion < 1:
		return invalid("brief", "say which Brief version was compiled")
	case !sha256Hex.MatchString(c.InputSHA256):
		return invalid("input_sha256", "must be a lowercase hex sha256")
	case c.EnqueuedAt.IsZero() || c.StartedAt.IsZero() || c.CompiledAt.IsZero():
		return invalid("timings", "enqueued, started and compiled times are required")
	case c.CompiledAt.Before(c.StartedAt):
		return invalid("compiled_at", "is before started_at")
	}
	if p, _, ok := ParseRef(c.Ref); !ok || p != PrefixCompile {
		return invalid("ref", "reserve a compile ID (C-0881) with ReserveCompileRef first")
	}
	c.EnqueuedAt = c.EnqueuedAt.Truncate(time.Microsecond)
	c.StartedAt = c.StartedAt.Truncate(time.Microsecond)
	c.CompiledAt = c.CompiledAt.Truncate(time.Microsecond)
	if c.Error != "" {
		return checkText("error", c.Error, MaxCompileError, true)
	}
	switch {
	case !sha256Hex.MatchString(c.OutputSHA256) || !sha256Hex.MatchString(c.DriftSHA256):
		return invalid("output_sha256", "a compiled run needs its output and drift hashes")
	case strings.TrimSpace(c.ArtifactKey) == "" || len(c.ArtifactKey) > 500:
		return invalid("artifact_key", "a compiled run needs its stored artifact")
	case c.Bytes < 0 || c.Lines < 0:
		return invalid("bytes", "must not be negative")
	case len(c.Files) > MaxCompileOutputs:
		return invalid("files", "at most %d outputs", MaxCompileOutputs)
	}
	return nil
}

func (c *RecordDelivery) validate() error {
	if c.Target == uuid.Nil {
		return invalid("target", "say which target was delivered")
	}
	if strings.TrimSpace(c.Compile) == "" {
		return invalid("compile", "say which compile run you wrote (C-0881)")
	}
	if !sha256Hex.MatchString(c.SHA256) {
		return invalid("sha256", "send the drift hash of what you wrote, as lowercase hex sha256")
	}
	return nil
}

func (c *RecordObservation) validate() error {
	switch {
	case c.Target == uuid.Nil:
		return invalid("target", "say which target's file changed")
	case !ValidPath(c.Path):
		return invalid("path", "use the file's path relative to the repository root")
	case !sha256Hex.MatchString(c.ObservedSHA256):
		return invalid("observed_sha256", "must be a lowercase hex sha256")
	case c.ObserverKind != ObserverDevice && c.ObserverKind != ObserverGitHub:
		return invalid("observer_kind", "use device or github")
	case c.Commit != "" && !commitSHA.MatchString(c.Commit):
		return invalid("commit", "use a lowercase hex commit sha")
	case c.BaseSHA256 != "" && !sha256Hex.MatchString(c.BaseSHA256):
		return invalid("base_sha256", "must be a lowercase hex sha256")
	case strings.TrimSpace(c.ArtifactKey) == "" || len(c.ArtifactKey) > 500:
		return invalid("artifact_key", "store the observed content first")
	case c.Bytes < 0:
		return invalid("bytes", "must not be negative")
	case len(c.Changes.Changes) > MaxDriftChanges:
		return invalid("changes", "a hand edit of more than %d lines can't be pulled; overwrite it or stop compiling", MaxDriftChanges)
	}
	c.ObserverID = strings.TrimSpace(c.ObserverID)
	if err := checkLine("observer_id", c.ObserverID, MaxObserverIDRunes, true); err != nil {
		return err
	}
	if c.Changes.Changes == nil {
		c.Changes.Changes = []DriftChange{}
	}
	for _, ch := range c.Changes.Changes {
		switch ch.Kind {
		case ChangeEdit, ChangeNew, ChangeRemove:
		default:
			return invalid("changes.kind", "use edit, new or remove")
		}
		for _, s := range []string{ch.OldText, ch.NewText, ch.Text} {
			if !utf8.ValidString(s) || strings.ContainsRune(s, 0) {
				return invalid("changes", "must be valid UTF-8 text")
			}
		}
	}
	return nil
}

func (c *ResolveDrift) validate() error {
	if c.Target == uuid.Nil {
		return invalid("target", "say which target's hand edit to resolve")
	}
	switch c.Mode {
	case DriftPull, DriftOverwrite, DriftStop:
		return nil
	}
	return invalid("mode", "use pull, overwrite or stop")
}
