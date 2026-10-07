package judge

import (
	"encoding/json"
	"fmt"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
)

// The manifests dep_present may name (plan 25 §5.9).
var manifests = []string{"go.mod", "package.json", "pnpm-lock.yaml", "Cargo.toml", "pyproject.toml", "requirements.txt"}

// maxConditions bounds what one proposal gets.
const maxConditions = 5

// validConditions keeps the model's conditions that its sources make
// evident, in the typed form stale detection reads (§5.9), and drops the
// rest: a condition must point at something the statement or a source
// actually names. It returns a JSON array, "[]" when none survive.
func validConditions(conds []Condition, statement string, sources []ledger.Source, now time.Time) json.RawMessage {
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

	var out []map[string]any
	seen := map[string]bool{}
	for _, c := range conds {
		if len(out) == maxConditions {
			break
		}
		var v map[string]any
		switch c.Kind {
		case "dep_present":
			m := strings.TrimSpace(c.Manifest)
			if !slices.Contains(manifests, path.Base(m)) || !mentions(path.Base(m)) || !mentions(strings.TrimSpace(c.Name)) {
				continue
			}
			v = map[string]any{"kind": c.Kind, "manifest": m, "name": strings.TrimSpace(c.Name)}
		case "file_exists", "file_unchanged":
			p := strings.TrimPrefix(strings.TrimSpace(c.Path), "./")
			if !repoPath(p) || (sourceFor(p) == nil && !mentions(p)) {
				continue
			}
			v = map[string]any{"kind": c.Kind, "path": p}
			if c.Kind == "file_unchanged" {
				src := sourceFor(p)
				if src == nil || src.ContentHash == "" {
					continue // there is nothing to compare against
				}
				v["hash"] = src.ContentHash
			}
		case "pr_state":
			if c.Number <= 0 || !slices.Contains([]string{"open", "merged", "closed"}, c.State) || strings.TrimSpace(c.Repo) == "" {
				continue
			}
			cited := slices.ContainsFunc(sources, func(s ledger.Source) bool {
				return s.Kind == ledger.SourcePR && strings.Contains(s.Ref+" "+s.URI, fmt.Sprintf("%d", c.Number))
			})
			if !cited {
				continue
			}
			v = map[string]any{"kind": c.Kind, "repo": strings.TrimSpace(c.Repo), "number": c.Number, "state": c.State}
		case "before":
			d, err := time.Parse("2006-01-02", strings.TrimSpace(c.Before))
			if err != nil || !d.After(now) || d.After(now.AddDate(5, 0, 0)) || !mentions(fmt.Sprint(d.Year())) {
				continue
			}
			v = map[string]any{"kind": c.Kind, "stale_after": d.UTC().Format(time.RFC3339)}
		default:
			continue
		}
		key, _ := json.Marshal(v)
		if seen[string(key)] {
			continue
		}
		seen[string(key)] = true
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
