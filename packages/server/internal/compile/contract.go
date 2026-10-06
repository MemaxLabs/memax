package compile

import (
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
)

// The compiler's contract (packages/compiler/src/types.ts), as the
// compile service speaks it over HTTP. CONTRACT_VERSION there is 1.

// ContractVersion is the compiler contract version this package speaks.
const ContractVersion = 1

// Input is the compiler's CompileInput.
type Input struct {
	Version  int              `json:"version"`
	Compile  Run              `json:"compile"`
	Space    InputSpace       `json:"space"`
	Brief    InputBrief       `json:"brief"`
	Memories []InputMemory    `json:"memories"`
	Targets  []TargetSettings `json:"targets"`
}

// Run names a compile run in the compiled header.
type Run struct {
	ID string `json:"id"`
	At string `json:"at"`
}

// InputSpace is the space as the header names it.
type InputSpace struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
	Kind string `json:"kind"`
	URL  string `json:"url"`
}

// InputBrief is the Brief version to compile.
type InputBrief struct {
	ID       string         `json:"id"`
	Title    string         `json:"title"`
	Summary  string         `json:"summary,omitempty"`
	Sections []InputSection `json:"sections"`
}

// InputSection is one Brief section.
type InputSection struct {
	Key     string      `json:"key"`
	Heading string      `json:"heading"`
	Items   []InputItem `json:"items"`
}

// InputItem is a memory ref or cited prose.
type InputItem struct {
	Ref   string   `json:"ref,omitempty"`
	Text  string   `json:"text,omitempty"`
	Cites []string `json:"cites,omitempty"`
}

// InputMemory is one memory that may compile.
type InputMemory struct {
	Ref        string      `json:"ref"`
	Statement  string      `json:"statement"`
	Section    string      `json:"section"`
	Kind       string      `json:"kind"`
	State      string      `json:"state"`
	Flags      []string    `json:"flags,omitempty"`
	Trust      string      `json:"trust,omitempty"`
	Scope      *InputScope `json:"scope,omitempty"`
	ReadScore  float64     `json:"read_score"`
	StaleAfter string      `json:"stale_after,omitempty"`
}

// InputScope is where a memory applies.
type InputScope struct {
	Paths  []string `json:"paths,omitempty"`
	Agents []string `json:"agents,omitempty"`
}

// TargetSettings is one target to render. The compiler refuses settings
// that don't apply to the kind, so only the kind's own are set.
type TargetSettings struct {
	Kind          string   `json:"kind"`
	Path          string   `json:"path,omitempty"`
	Include       string   `json:"include,omitempty"`
	Stale         string   `json:"stale,omitempty"`
	SizeBudget    int      `json:"size_budget,omitempty"`
	Sections      []string `json:"sections,omitempty"`
	Scoped        string   `json:"scoped,omitempty"`
	CanonicalPath string   `json:"canonical_path,omitempty"`
	UserOwned     bool     `json:"user_owned,omitempty"`
	Current       *string  `json:"current,omitempty"`
}

// Result is the compiler's CompileResult.
type Result struct {
	Version    int            `json:"version"`
	CompileID  string         `json:"compile_id"`
	CompiledAt string         `json:"compiled_at"`
	BriefID    string         `json:"brief_id"`
	Space      string         `json:"space"`
	Files      []File         `json:"files"`
	Copies     []Copy         `json:"copies"`
	Targets    []TargetResult `json:"targets"`
	Warnings   []Warning      `json:"warnings"`
}

// Text is what files and copies share.
type Text struct {
	Target           string   `json:"target"`
	Content          string   `json:"content"`
	Bytes            int      `json:"bytes"`
	Lines            int      `json:"lines"`
	SHA256           string   `json:"sha256"`
	Refs             []string `json:"refs"`
	Cites            []string `json:"cites"`
	DroppedForBudget []string `json:"dropped_for_budget"`
}

// File is a file to write.
type File struct {
	Text
	Path        string `json:"path"`
	DriftSHA256 string `json:"drift_sha256"`
	UserOwned   bool   `json:"user_owned"`
}

// Copy is text for a person to copy out.
type Copy struct {
	Text
	Label string `json:"label"`
}

// TargetResult summarises one target.
type TargetResult struct {
	Kind             string   `json:"kind"`
	Path             *string  `json:"path"`
	Delivery         string   `json:"delivery"`
	Reads            *string  `json:"reads"`
	Files            []string `json:"files"`
	Refs             []string `json:"refs"`
	DroppedForBudget []string `json:"dropped_for_budget"`
}

// Warning is a compiler warning.
type Warning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Target  string `json:"target,omitempty"`
	Path    string `json:"path,omitempty"`
	Ref     string `json:"ref,omitempty"`
}

// Issue is one problem with an input the compiler refused.
type Issue struct {
	// InstancePath is a JSON pointer into the input, e.g. /memories/3/state.
	InstancePath string `json:"instancePath"`
	Message      string `json:"message"`
}

// LastFile is the file parse-back compares against: the last compiled
// (or accepted) content and, when known, its drift hash.
type LastFile struct {
	Content     string `json:"content"`
	DriftSHA256 string `json:"drift_sha256,omitempty"`
}

// ParseBackRequest asks what a hand edit means.
type ParseBackRequest struct {
	Last    LastFile `json:"last"`
	Current string   `json:"current"`
}

// ParseBackResult is the compiler's ChangeSet, plus whether the file
// drifted from the last delivered hash, and the current content's drift
// hash.
type ParseBackResult struct {
	ledger.ChangeSet
	Drifted     bool   `json:"drifted"`
	DriftSHA256 string `json:"drift_sha256"`
}

// Health is the compile service's health answer.
type Health struct {
	Status          string    `json:"status"`
	ContractVersion int       `json:"contract_version"`
	Adapters        []Adapter `json:"adapters"`
}

// Adapter describes one compiler adapter.
type Adapter struct {
	Kind        string  `json:"kind"`
	Version     int     `json:"version"`
	Role        string  `json:"role"`
	DefaultPath *string `json:"default_path"`
	Default     bool    `json:"default"`
}
