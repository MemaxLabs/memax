package judge

import (
	"encoding/json"
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/conditions"
)

// maxConditions bounds what one proposal gets.
const maxConditions = 5

// validConditions keeps the model's conditions that its sources make
// evident, in the record's typed form (internal/ledger/conditions), and
// drops the rest: a condition must point at something the statement or a
// source actually names. The model answers in its own schema (file_exists,
// before, …): file_exists is path_exists here; file_unchanged and before
// are left out until the judge can attach them properly (a file_unchanged
// needs the file's git blob id, and before is the memory's stale_after,
// not a condition). It returns a JSON array, "[]" when none survive; the
// ledger gives them ids and the judge's origin.
func validConditions(conds []Condition, statement string, sources []ledger.Source) json.RawMessage {
	evidence := strings.ToLower(statement)
	for _, s := range sources {
		evidence += "\n" + strings.ToLower(s.Ref+" "+s.URI+" "+s.Quote)
	}
	mentions := func(s string) bool { return s != "" && strings.Contains(evidence, strings.ToLower(s)) }
	sourceFor := func(p string) *ledger.Source {
		for i, s := range sources {
			if strings.TrimSuffix(s.URI, "/") == p || strings.HasPrefix(s.Ref, p+":") || s.Ref == p {
				return &sources[i]
			}
		}
		return nil
	}

	var out []conditions.Condition
	for _, c := range conds {
		if len(out) == maxConditions {
			break
		}
		var v conditions.Condition
		switch c.Kind {
		case "dep_present":
			m := strings.TrimSpace(c.Manifest)
			if !slices.Contains(conditions.Manifests, path.Base(m)) || !mentions(path.Base(m)) || !mentions(strings.TrimSpace(c.Name)) {
				continue
			}
			v = conditions.Condition{Kind: conditions.DepPresent, Manifest: m, Name: strings.TrimSpace(c.Name)}
		case "file_exists":
			p := strings.TrimPrefix(strings.TrimSpace(c.Path), "./")
			if !repoPath(p) || conditions.Excluded(p) || (sourceFor(p) == nil && !mentions(p)) {
				continue
			}
			v = conditions.Condition{Kind: conditions.PathExists, Path: p}
		case "pr_state":
			if c.Number <= 0 || !slices.Contains(conditions.PRStates, c.State) || strings.TrimSpace(c.Repo) == "" {
				continue
			}
			cited := slices.ContainsFunc(sources, func(s ledger.Source) bool {
				return s.Kind == ledger.SourcePR && strings.Contains(s.Ref+" "+s.URI, fmt.Sprintf("%d", c.Number))
			})
			if !cited {
				continue
			}
			v = conditions.Condition{Kind: conditions.PRState, Repo: strings.TrimSpace(c.Repo), Number: c.Number, State: c.State}
		default:
			continue
		}
		v.ID, v.Origin = "c1", conditions.OriginJudge
		if conditions.Validate([]conditions.Condition{v}) != nil ||
			slices.ContainsFunc(out, func(o conditions.Condition) bool { return conditions.Same(o, v) }) {
			continue
		}
		v.ID, v.Origin = "", ""
		out = append(out, v)
	}
	if len(out) == 0 {
		return json.RawMessage("[]")
	}
	b, err := json.Marshal(out)
	if err != nil {
		return json.RawMessage("[]")
	}
	return b
}

// repoPath reports a repository-relative path: no "..", not absolute.
func repoPath(p string) bool {
	if p == "" || len(p) > 300 || strings.HasPrefix(p, "/") || strings.Contains(p, "\\") {
		return false
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
	}
	return true
}
