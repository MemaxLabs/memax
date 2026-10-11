// Package conditions is the typed form of a memory's "stays true while"
// conditions (plan 25 §5.9; the stale-detection design §1.1): the kinds,
// their fields, one validator, and the derivation of path conditions from
// the paths a memory names.
//
// A condition is a predicate on the repository at a commit: a path is in
// the tree (path_exists), a file contains some text (file_contains), a
// manifest declares a dependency (dep_present), a file's blob is the same
// (file_unchanged), or a pull request is in a state (pr_state). The date to
// check again is the memory's stale_after, not a condition here.
//
// Each condition has an id stable within its memory ("c1", "c2", …), a
// kind, its fields and an origin: derived (Memax read it off the words, a
// source or the scope; From says which), judge, dream or person. Validate
// is the record's guard in Go; v2.conditions_valid is the same in SQL, and
// a test holds the two together. Nothing here does I/O.
package conditions

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/MemaxLabs/memax/packages/server/internal/secrets"
)

// Kind is what a condition checks.
type Kind string

// The kinds.
const (
	PathExists    Kind = "path_exists"
	FileContains  Kind = "file_contains"
	DepPresent    Kind = "dep_present"
	FileUnchanged Kind = "file_unchanged"
	PRState       Kind = "pr_state"
)

// Kinds lists every kind.
var Kinds = []Kind{PathExists, FileContains, DepPresent, FileUnchanged, PRState}

// Origin is who attached a condition.
type Origin string

// The origins.
const (
	OriginDerived Origin = "derived"
	OriginJudge   Origin = "judge"
	OriginDream   Origin = "dream"
	OriginPerson  Origin = "person"
)

// Origins lists every origin.
var Origins = []Origin{OriginDerived, OriginJudge, OriginDream, OriginPerson}

// From is where Memax read a derived condition off: the statement, a
// source the memory cites, or its scope. A rename of a path only a source
// cites follows the move; one the statement names makes the words wrong.
type From string

// The places a derived condition comes from.
const (
	FromStatement From = "statement"
	FromSource    From = "source"
	FromScope     From = "scope"
)

// Froms lists every place.
var Froms = []From{FromStatement, FromSource, FromScope}

// The pull request states pr_state checks.
var PRStates = []string{"open", "merged", "closed"}

// Manifests are the files dep_present reads (by base name).
var Manifests = []string{"go.mod", "package.json", "pyproject.toml", "Cargo.toml", "requirements.txt"}

// Limits.
const (
	// MaxConditions bounds one memory's conditions.
	MaxConditions = 8
	// MaxDerived bounds the ones Memax derives.
	MaxDerived    = 4
	MaxPathRunes  = 300
	MaxTextRunes  = 200
	MaxNameRunes  = 214
	MaxRangeRunes = 64
	MaxRepoRunes  = 200
)

// Condition is one typed condition. Only the fields of its kind are set.
type Condition struct {
	ID   string `json:"id"`
	Kind Kind   `json:"kind"`
	// path_exists: Path or Glob. file_contains and file_unchanged: Path.
	Path string `json:"path,omitempty"`
	Glob string `json:"glob,omitempty"`
	// file_contains: the text, literally and case-sensitively; as a whole
	// word when Word.
	Text string `json:"text,omitempty"`
	Word bool   `json:"word,omitempty"`
	// dep_present: a manifest path or a bare manifest name, the
	// dependency's name and, optionally, a version range it must satisfy.
	Manifest string `json:"manifest,omitempty"`
	Name     string `json:"name,omitempty"`
	Range    string `json:"range,omitempty"`
	// file_unchanged: the file's git blob id when the condition was
	// attached.
	Blob string `json:"blob,omitempty"`
	// pr_state: "owner/name", the number and the state.
	Repo   string `json:"repo,omitempty"`
	Number int    `json:"number,omitempty"`
	State  string `json:"state,omitempty"`

	Origin Origin `json:"origin"`
	From   From   `json:"from,omitempty"`
}

// Error says which condition is wrong and why, for people.
type Error struct {
	// Index is the condition's position, or -1 for the list.
	Index   int
	Message string
}

func (e *Error) Error() string {
	if e.Index < 0 {
		return e.Message
	}
	return fmt.Sprintf("condition %d: %s", e.Index+1, e.Message)
}

func fail(i int, format string, args ...any) error {
	return &Error{Index: i, Message: fmt.Sprintf(format, args...)}
}

// Parse decodes stored or sent conditions strictly (unknown fields are
// refused) and validates them. Empty input is no conditions.
func Parse(raw json.RawMessage) ([]Condition, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return []Condition{}, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var cs []Condition
	if err := dec.Decode(&cs); err != nil {
		return nil, fail(-1, "must be an array of typed conditions (path_exists, file_contains, dep_present, file_unchanged or pr_state): %v", err)
	}
	if cs == nil {
		cs = []Condition{}
	}
	return cs, Validate(cs)
}

// Encode is the stored form: a JSON array, "[]" when there are none.
func Encode(cs []Condition) json.RawMessage {
	if len(cs) == 0 {
		return json.RawMessage("[]")
	}
	b, err := json.Marshal(cs)
	if err != nil {
		// Condition holds only strings, numbers and booleans.
		panic(err)
	}
	return b
}

// Hash identifies what a condition checks: its kind and fields, not its
// id, origin or where Memax read it (SHA-256 of its JSON without those, in
// hex). A condition redefined in place (a person changes its path, a new
// version names another file) has a new hash, so its checks start over;
// one read again from new words that checks the same thing keeps them.
func Hash(c Condition) string {
	b, err := json.Marshal(struct {
		Kind     Kind   `json:"kind"`
		Path     string `json:"path,omitempty"`
		Glob     string `json:"glob,omitempty"`
		Text     string `json:"text,omitempty"`
		Word     bool   `json:"word,omitempty"`
		Manifest string `json:"manifest,omitempty"`
		Name     string `json:"name,omitempty"`
		Range    string `json:"range,omitempty"`
		Blob     string `json:"blob,omitempty"`
		Repo     string `json:"repo,omitempty"`
		Number   int    `json:"number,omitempty"`
		State    string `json:"state,omitempty"`
	}{c.Kind, c.Path, c.Glob, c.Text, c.Word, c.Manifest, c.Name, c.Range, c.Blob, c.Repo, c.Number, c.State})
	if err != nil {
		panic(err) // strings, numbers and booleans only
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

var (
	idPattern   = regexp.MustCompile(`^c[1-9][0-9]{0,3}$`)
	pathChars   = regexp.MustCompile(`^[A-Za-z0-9._@+()\[\]/-]+$`)
	globChars   = regexp.MustCompile(`^[A-Za-z0-9._\-/*?\[\]!@+~]+$`)
	repoPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
	blobPattern = regexp.MustCompile(`^([0-9a-f]{40}|[0-9a-f]{64})$`)
)

// ValidPath reports a repository-relative path in canonical form: no
// leading "/", "~" or "./", no "." or ".." segments, no empty segments,
// no backslashes or spaces, at most MaxPathRunes.
func ValidPath(p string) bool {
	if p == "" || utf8.RuneCountInString(p) > MaxPathRunes || !pathChars.MatchString(p) {
		return false
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
	}
	return true
}

// ValidGlob reports a glob in the compiler's scope syntax, relative to the
// repository root.
func ValidGlob(g string) bool {
	if g == "" || utf8.RuneCountInString(g) > MaxPathRunes || !globChars.MatchString(g) {
		return false
	}
	for _, seg := range strings.Split(g, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
	}
	return true
}

// ValidManifest reports a manifest dep_present can read: a path ending in
// one of Manifests, or a bare manifest name.
func ValidManifest(m string) bool {
	return ValidPath(m) && slices.Contains(Manifests, path.Base(m))
}

func line(s string, maxRunes int) bool {
	if s == "" || !utf8.ValidString(s) || utf8.RuneCountInString(s) > maxRunes {
		return false
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

// Validate checks every rule the record holds conditions to: at most
// MaxConditions, unique ids, a known kind with exactly its fields, a known
// origin (From only on derived ones, and required there), canonical paths,
// the glob subset, the lengths, and no credential in file_contains' text.
func Validate(cs []Condition) error {
	if len(cs) > MaxConditions {
		return fail(-1, "a memory has at most %d conditions", MaxConditions)
	}
	ids := map[string]bool{}
	for i, c := range cs {
		if !idPattern.MatchString(c.ID) {
			return fail(i, "its id must be c1, c2, …")
		}
		if ids[c.ID] {
			return fail(i, "id %s is used twice", c.ID)
		}
		ids[c.ID] = true
		if !slices.Contains(Origins, c.Origin) {
			return fail(i, "origin must be derived, judge, dream or person")
		}
		switch {
		case c.Origin == OriginDerived && !slices.Contains(Froms, c.From):
			return fail(i, "a derived condition says where it came from: statement, source or scope")
		case c.Origin != OriginDerived && c.From != "":
			return fail(i, "only a derived condition says where it came from")
		}
		if err := validateKind(i, c); err != nil {
			return err
		}
	}
	return nil
}

func validateKind(i int, c Condition) error {
	// unset reports whether every field outside the kind's own is empty.
	unset := func(fields ...string) bool {
		for _, f := range fields {
			switch f {
			case "path":
				if c.Path != "" {
					return false
				}
			case "glob":
				if c.Glob != "" {
					return false
				}
			case "text":
				if c.Text != "" || c.Word {
					return false
				}
			case "dep":
				if c.Manifest != "" || c.Name != "" || c.Range != "" {
					return false
				}
			case "blob":
				if c.Blob != "" {
					return false
				}
			case "pr":
				if c.Repo != "" || c.Number != 0 || c.State != "" {
					return false
				}
			}
		}
		return true
	}
	switch c.Kind {
	case PathExists:
		switch {
		case !unset("text", "dep", "blob", "pr"):
			return fail(i, "path_exists takes a path or a glob, nothing else")
		case (c.Path == "") == (c.Glob == ""):
			return fail(i, "path_exists takes exactly one of path and glob")
		case c.Path != "" && !ValidPath(c.Path):
			return fail(i, "path must be relative to the repository root, with no .. or leading /")
		case c.Glob != "" && !ValidGlob(c.Glob):
			return fail(i, "glob must use the scope glob syntax, relative to the repository root")
		}
	case FileContains:
		switch {
		case !unset("glob", "dep", "blob", "pr"):
			return fail(i, "file_contains takes a path, text and word, nothing else")
		case !ValidPath(c.Path):
			return fail(i, "path must be relative to the repository root, with no .. or leading /")
		case !line(c.Text, MaxTextRunes):
			return fail(i, "text must be one line of 1 to %d characters", MaxTextRunes)
		case len(secrets.DetectCredentials(c.Text)) > 0:
			return fail(i, "text looks like a credential; Memax never stores secrets")
		}
	case DepPresent:
		switch {
		case !unset("path", "glob", "text", "blob", "pr"):
			return fail(i, "dep_present takes a manifest, a name and a range, nothing else")
		case !ValidManifest(c.Manifest):
			return fail(i, "manifest must be one of %s, or a path to one", strings.Join(Manifests, ", "))
		case !line(c.Name, MaxNameRunes) || strings.ContainsAny(c.Name, " \t"):
			return fail(i, "name must be the dependency's name, at most %d characters", MaxNameRunes)
		case c.Range != "" && !line(c.Range, MaxRangeRunes):
			return fail(i, "range must be at most %d characters", MaxRangeRunes)
		}
	case FileUnchanged:
		switch {
		case !unset("glob", "text", "dep", "pr"):
			return fail(i, "file_unchanged takes a path and a blob, nothing else")
		case !ValidPath(c.Path):
			return fail(i, "path must be relative to the repository root, with no .. or leading /")
		case !blobPattern.MatchString(c.Blob):
			return fail(i, "blob must be the file's git blob id")
		}
	case PRState:
		switch {
		case !unset("path", "glob", "text", "dep", "blob"):
			return fail(i, "pr_state takes a repo, a number and a state, nothing else")
		case utf8.RuneCountInString(c.Repo) > MaxRepoRunes || !repoPattern.MatchString(c.Repo):
			return fail(i, "repo must be owner/name")
		case c.Number < 1:
			return fail(i, "number must be the pull request's number")
		case !slices.Contains(PRStates, c.State):
			return fail(i, "state must be open, merged or closed")
		}
	default:
		return fail(i, "kind must be path_exists, file_contains, dep_present, file_unchanged or pr_state")
	}
	return nil
}

// Same reports whether two conditions check the same thing (their kind and
// fields), whatever their ids and origins.
func Same(a, b Condition) bool {
	a.ID, b.ID, a.Origin, b.Origin, a.From, b.From = "", "", "", "", "", ""
	return a == b
}

// freeIDs hands out the lowest ids none of cs holds: an id a condition no
// longer uses can be used again, since what a check learned is keyed to a
// condition's fields too (a redefined condition starts over).
func freeIDs(cs []Condition) func() string {
	used := map[string]bool{}
	for _, c := range cs {
		used[c.ID] = true
	}
	n := 0
	return func() string {
		for {
			n++
			id := "c" + strconv.Itoa(n)
			if !used[id] {
				used[id] = true
				return id
			}
		}
	}
}

// Attach gives conditions a person (or the judge, or Dream) sends their
// ids and origin, after what the memory has: one that checks the same as a
// condition it has is that one, and the others take the lowest free ids.
func Attach(had, sent []Condition, origin Origin) []Condition {
	out := slices.Clone(had)
	next := freeIDs(out)
	for _, c := range sent {
		c.Origin, c.From = origin, ""
		if slices.ContainsFunc(out, func(o Condition) bool { return Same(o, c) }) {
			continue
		}
		c.ID = next()
		out = append(out, c)
	}
	return out
}

// Renew is a new version's conditions: what the memory had, with its
// derived conditions from the places in `places` replaced by `derived`.
// Conditions that check the same thing keep their id, so what a check
// learned about them stands; new ones take the lowest free ids; and
// nothing past MaxConditions is added. Conditions a person, the judge or Dream
// attached always stay.
func Renew(had, derived []Condition, places ...From) []Condition {
	var out []Condition
	var replaced []Condition
	for _, c := range had {
		if c.Origin == OriginDerived && slices.Contains(places, c.From) {
			replaced = append(replaced, c)
			continue
		}
		out = append(out, c)
	}
	// Ids stay with what checks the same thing; the others take the lowest
	// free ones, so undoing an edit gives back the ids it had.
	keep := slices.Clone(out)
	for _, d := range derived {
		if i := slices.IndexFunc(replaced, func(o Condition) bool { return Same(o, d) }); i >= 0 {
			keep = append(keep, replaced[i])
		}
	}
	next := freeIDs(keep)
	for _, d := range derived {
		if len(out) == MaxConditions {
			break
		}
		if slices.ContainsFunc(out, func(o Condition) bool { return Same(o, d) }) {
			continue
		}
		d.Origin = OriginDerived
		if i := slices.IndexFunc(replaced, func(o Condition) bool { return Same(o, d) }); i >= 0 {
			d.ID = replaced[i].ID
		} else {
			d.ID = next()
		}
		out = append(out, d)
	}
	if out == nil {
		out = []Condition{}
	}
	return out
}
