package ledger

// The repository scope for personal facts (personal-layer design §3.3).
//
// A memory's scope says where it applies: `paths` (globs in a repository,
// from a scoped rule), `agents` (the agents it is for) and `repos`, the
// repositories it applies in, as "owner/name" keys (RepoKey). memax init
// sets `repos` on what a Personal space imports from files that belong to
// one project: that repository's Claude Code auto memory and its
// CLAUDE.local.md. A home file (a personal space compiled into a machine's
// user-level agent files) leaves such a memory out, since it would load in
// every project; it stays live over MCP. A person widens one to everywhere
// on its page (ScopeMemory with no repositories), and Memax's backfill
// (cmd/v2-backfill-repo-scope) ties what an earlier init imported to its
// repository (ScopeMemory as Memax, which only narrows).

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger/lifecycle"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// CommandScopeMemory names ScopeMemory.
const CommandScopeMemory CommandName = "scope_memory"

// ActionScoped is the receipt of a scope change.
const ActionScoped Action = "scoped"

// MaxScopeRepos bounds scope.repos.
const MaxScopeRepos = 20

// maxRepoKey bounds one repository key, in bytes (keys are ASCII).
const maxRepoKey = 200

// repoKeyPattern is a normalised key: "owner/name", lowercase.
var repoKeyPattern = regexp.MustCompile(`^[a-z0-9._-]+/[a-z0-9._-]+$`)

// scopeKeys are the scope's fields (the spec's MemoryScope).
var scopeKeys = []string{"paths", "agents", "repos"}

// ScopeMemory sets the repositories a memory applies in (scope.repos);
// none is everywhere. Its paths and agents, and its words, stay as they
// are. Only a person may let a memory reach further (policy.ActionScope);
// a change to a kept memory recompiles the space's targets.
type ScopeMemory struct {
	Meta
	// Memory is a display ID ("M-0219") or a uuid.
	Memory string
	// ExpectedVersion, when set, must be the memory's version (If-Match).
	ExpectedVersion int
	// Repos are repository keys in any spelling RepoKey reads
	// ("Acme/Web", "git@github.com:acme/web.git"); stored as "acme/web",
	// sorted, once each.
	Repos []string
}

// Name implements Command.
func (*ScopeMemory) Name() CommandName { return CommandScopeMemory }

func (c *ScopeMemory) validate() error {
	if err := validateTarget(c.Memory, c.ExpectedVersion, false); err != nil {
		return err
	}
	repos, err := normaliseRepos(c.Repos)
	if err != nil {
		return err
	}
	c.Repos = repos
	return nil
}

// ScopeRepos reads scope.repos from a memory's scope: nil when it names
// none, or the scope doesn't parse.
func ScopeRepos(raw json.RawMessage) []string {
	var s struct {
		Repos []string `json:"repos"`
	}
	if len(bytes.TrimSpace(raw)) == 0 || json.Unmarshal(raw, &s) != nil || len(s.Repos) == 0 {
		return nil
	}
	return s.Repos
}

// normaliseRepos reads each key as RepoKey does, sorted and once each, and
// refuses what isn't a repository.
func normaliseRepos(in []string) ([]string, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make([]string, 0, len(in))
	for _, r := range in {
		k := RepoKey(r)
		if len(k) > maxRepoKey || !repoKeyPattern.MatchString(k) || slices.ContainsFunc(strings.Split(k, "/"), func(s string) bool {
			return s == "." || s == ".."
		}) {
			return nil, invalid("scope.repos", "name each repository as owner/name, such as acme/web (got %q)", truncateRunes(r, 80))
		}
		out = append(out, k)
	}
	slices.Sort(out)
	out = slices.Compact(out)
	if len(out) > MaxScopeRepos {
		return nil, invalid("scope.repos", "name at most %d repositories", MaxScopeRepos)
	}
	return out, nil
}

// normaliseScope checks a scope object's fields (paths, agents and repos,
// each a list of strings; a null one is left out) and normalises its
// repositories. A scope with nothing to change keeps its bytes.
func normaliseScope(raw json.RawMessage) (json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, invalid("scope", "must be an object")
	}
	changed := false
	for k, v := range fields {
		if !slices.Contains(scopeKeys, k) {
			return nil, invalid("scope", "says where a memory applies with paths, agents and repos; %q isn't one of them", truncateRunes(k, 40))
		}
		if bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			delete(fields, k)
			changed = true
			continue
		}
		var list []string
		if err := json.Unmarshal(v, &list); err != nil {
			return nil, invalid("scope."+k, "must be a list of strings")
		}
		if k != "repos" {
			continue
		}
		repos, err := normaliseRepos(list)
		if err != nil {
			return nil, err
		}
		changed = true
		if len(repos) == 0 {
			delete(fields, k)
			continue
		}
		b, err := json.Marshal(repos)
		if err != nil {
			return nil, err
		}
		fields[k] = b
	}
	if !changed {
		return raw, nil
	}
	return json.Marshal(fields)
}

// withRepos is the scope with its repositories set (left out when there
// are none) and every other field as it was.
func withRepos(raw json.RawMessage, repos []string) (json.RawMessage, error) {
	fields := map[string]json.RawMessage{}
	if len(bytes.TrimSpace(raw)) > 0 {
		if err := json.Unmarshal(raw, &fields); err != nil {
			return nil, fmt.Errorf("ledger: read scope: %w", err)
		}
	}
	delete(fields, "repos")
	if len(repos) > 0 {
		b, err := json.Marshal(repos)
		if err != nil {
			return nil, err
		}
		fields["repos"] = b
	}
	return json.Marshal(fields)
}

// widens reports whether a memory scoped to before reaches places it
// didn't once scoped to after: it named repositories, and now names none
// (everywhere) or another one.
func widens(before, after []string) bool {
	if len(before) == 0 {
		return false
	}
	if len(after) == 0 {
		return true
	}
	for _, r := range after {
		if !slices.Contains(before, r) {
			return true
		}
	}
	return false
}

// scopeMemory is ScopeMemory: one scoped receipt and the memory's new
// scope; a kept memory's targets recompile, since what they hold changed.
func (w *writer) scopeMemory(ctx context.Context, c *ScopeMemory) (Result, error) {
	mem, grant, sp, replay, err := w.open(ctx, c.Memory)
	if err != nil || replay != nil {
		return deref(replay), err
	}
	if c.ExpectedVersion != 0 && c.ExpectedVersion != mem.Version {
		return Result{}, &EditClashError{Ref: mem.Ref, Expected: c.ExpectedVersion, Current: mem.Version}
	}
	before := ScopeRepos(mem.Applies)
	obj := w.object(mem, false)
	obj.Widens = widens(before, c.Repos)
	dec := policy.Decide(w.policyActor(grant), policy.ActionScope, obj, sp.policy())
	if dec.Effect == policy.EffectRefuse {
		return refused(dec), nil
	}
	// Where a memory applies matters while it can compile: a proposal, a
	// kept memory, or a faded one a Restore brings back.
	switch mem.Lifecycle {
	case lifecycle.Proposed, lifecycle.Kept, lifecycle.Faded:
	default:
		return Result{}, &TransitionError{Ref: mem.Ref, Err: &lifecycle.TransitionError{From: mem.state(), Verb: "scope",
			Message: fmt.Sprintf("%s is %s; only a proposal, a kept memory or a faded one changes where it applies.", mem.Ref, mem.Lifecycle)}}
	}
	// As for an edit, the same scope again is nothing to do (a retry with
	// the same key replays instead).
	if slices.Equal(before, c.Repos) {
		where := "everywhere"
		if len(before) > 0 {
			where = "in " + strings.Join(before, ", ")
		}
		return Result{}, invalid("repos", "%s already applies %s; there is nothing to change", mem.Ref, where)
	}
	applies, err := withRepos(mem.Applies, c.Repos)
	if err != nil {
		return Result{}, err
	}
	rc := w.receipt(sp, mem.ID, mem.Ref, ActionScoped, mem.streamVersion+1, w.meta.Reason)
	if err := insertReceipt(ctx, w.tx, &rc); err != nil {
		return Result{}, err
	}
	// Nothing reads the update's result: it goes out with the next
	// statement (execDeferred).
	if err := execDeferred(ctx, w.tx, `
		UPDATE v2.memories
		   SET scope = $3, stream_version = $4, last_receipt_id = $5, updated_at = now()
		 WHERE id = $1 AND space_id = $2`,
		mem.ID, sp.ID, []byte(applies), rc.StreamVersion, rc.ID); err != nil {
		return Result{}, fmt.Errorf("ledger: update scope: %w", err)
	}
	if mem.Lifecycle == lifecycle.Kept {
		if err := w.markDirty(ctx, sp.ID); err != nil {
			return Result{}, err
		}
	}
	return w.finish(ctx, Result{Outcome: OutcomeApplied, Policy: dec, Receipts: []Receipt{rc}}, mem.ID)
}
