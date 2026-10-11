package ledger

import (
	"bytes"
	"encoding/json"
	"slices"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger/conditions"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// Conditions on the record (the stale-detection design §1.1, §1.2): every
// command that writes a version of a memory in a project space with a
// repository reads its derived conditions again, in the same transaction,
// off the new words, the files its sources cite and its scope. They ride
// on the command that wrote the version, so they add no receipt. What a
// person, the judge or Dream attached stays.

// ActionConditionsChanged is the receipt of a change to a memory's
// conditions that writes no version (migration 059: typing what was
// written before; later, a person's own edit of them).
const ActionConditionsChanged Action = "conditions_changed"

// derives reports whether Memax reads conditions off the space's
// memories: a project space with a repository to check them against.
func (s spaceRow) derives() bool {
	return s.Kind == policy.SpaceProject && s.Repository != ""
}

// sentConditions decodes conditions a command was sent: typed, without
// unknown fields, ids and origin optional (the ledger sets them).
func sentConditions(raw json.RawMessage) ([]conditions.Condition, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" || string(raw) == "[]" {
		return nil, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var cs []conditions.Condition
	if err := dec.Decode(&cs); err != nil {
		return nil, invalid("conditions", "send typed conditions (path_exists, file_contains, dep_present, file_unchanged or pr_state) with their fields")
	}
	if len(cs) > conditions.MaxConditions {
		return nil, invalid("conditions", "a memory has at most %d conditions", conditions.MaxConditions)
	}
	return cs, nil
}

// storedConditions reads a memory's conditions. The database holds them to
// v2.conditions_valid, so they parse; anything else is a bug worth failing
// the command on.
func storedConditions(raw json.RawMessage) ([]conditions.Condition, error) {
	cs, err := conditions.Parse(raw)
	if err != nil {
		return nil, invalid("conditions", "the stored conditions don't read: %v", err)
	}
	return cs, nil
}

// attachConditions gives sent conditions their ids and origin after what
// the memory had, and validates the result.
func attachConditions(had, sent []conditions.Condition, origin conditions.Origin) ([]conditions.Condition, error) {
	out := conditions.Attach(had, sent, origin)
	if err := conditions.Validate(out); err != nil {
		return nil, invalid("conditions", "%v", err)
	}
	return out, nil
}

// conditionsFor is what a version of a memory carries: the conditions it
// had (or was given), with the derived ones read again off these words,
// the files its sources cite (nil: the ones it had conditions on, since a
// new version keeps its sources) and its scope. In a space Memax doesn't
// derive for, it is what it had.
func conditionsFor(sp spaceRow, had []conditions.Condition, statement string, files []string, scope json.RawMessage) []conditions.Condition {
	if !sp.derives() {
		return had
	}
	if files == nil {
		for _, c := range had {
			if c.Origin == conditions.OriginDerived && c.From == conditions.FromSource && c.Path != "" {
				files = append(files, c.Path)
			}
		}
	}
	derived := conditions.Derive(conditions.Input{Statement: statement, Files: files, ScopePaths: scopePaths(scope)})
	return conditions.Renew(had, derived, conditions.FromStatement, conditions.FromSource, conditions.FromScope)
}

// renewedConditions is conditionsFor over stored conditions, encoded: a new
// version's conditions in a command that changes words.
func renewedConditions(sp spaceRow, stored json.RawMessage, statement string, scope json.RawMessage) (json.RawMessage, error) {
	had, err := storedConditions(stored)
	if err != nil {
		return nil, err
	}
	return conditions.Encode(conditionsFor(sp, had, statement, nil, scope)), nil
}

// judgeConditions attaches the judge's conditions to what a proposal has,
// once: a proposal the judge already gave conditions to keeps them. Ones
// the record can't hold (past the limit, not valid) are left out rather
// than failing the verdict.
func judgeConditions(stored, sent json.RawMessage) (json.RawMessage, error) {
	had, err := storedConditions(stored)
	if err != nil {
		return nil, err
	}
	in, err := sentConditions(sent)
	if err != nil || len(in) == 0 ||
		slices.ContainsFunc(had, func(c conditions.Condition) bool { return c.Origin == conditions.OriginJudge }) {
		return stored, nil
	}
	out := had
	for _, c := range in {
		next := conditions.Attach(out, []conditions.Condition{c}, conditions.OriginJudge)
		if conditions.Validate(next) == nil {
			out = next
		}
	}
	return conditions.Encode(out), nil
}

// fileRefs are the repository files sources cite: file sources' refs
// ("go.mod:14") and their URIs when they are paths.
func fileRefs(srcs []SourceInput) []string {
	var out []string
	for _, s := range srcs {
		if s.Kind != SourceFile {
			continue
		}
		for _, r := range []string{s.Ref, s.URI} {
			if r != "" && !slices.Contains(out, r) {
				out = append(out, r)
			}
		}
	}
	return out
}
