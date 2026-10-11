package ledger

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger/lifecycle"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// ProjectBoundFileKinds are the files memax init sends a Personal space
// whose statements belong to the repository init ran in: that
// repository's Claude Code auto memory (~/.claude/projects/<repo>/memory)
// and its CLAUDE.local.md. Init sets scope.repos on their statements; the
// CLI's list is packages/cli/src/lib/init/files.ts PROJECT_BOUND_KINDS.
var ProjectBoundFileKinds = []string{"claude_local", "claude_memory"}

// RepoScopeOptions scopes BackfillRepoScope.
type RepoScopeOptions struct {
	// Users limits it to these people's Personal spaces. Empty is every
	// Personal space on the V2 record.
	Users []uuid.UUID
	// DryRun plans and reports, and writes nothing.
	DryRun bool
}

// RepoScopeChange is one memory the backfill ties to its repositories.
type RepoScopeChange struct {
	SpaceID uuid.UUID `json:"space_id"`
	Space   string    `json:"space"`
	Memory  string    `json:"memory"`
	Repos   []string  `json:"repos"`
	// Files are the files init imported it from ("CLAUDE.local.md").
	Files []string `json:"files"`
	// Outcome is scoped, already (a replay), refused or failed; "planned"
	// in a dry run.
	Outcome string `json:"outcome"`
}

// RepoScopeReport counts what BackfillRepoScope found and did. Every
// memory an init import created in a Personal space is counted once.
type RepoScopeReport struct {
	DryRun  bool `json:"dry_run"`
	Spaces  int  `json:"spaces"`
	Imports int  `json:"imports"`
	// Scoped are tied to their repositories now (planned, in a dry run).
	Scoped int `json:"scoped"`
	// Already name repositories, or someone chose where they apply (a
	// scoped receipt): the backfill leaves them as they are.
	Already int `json:"already"`
	// Everywhere also came from a file that applies everywhere
	// (~/.claude/CLAUDE.md, ~/.codex/AGENTS.md, …).
	Everywhere int `json:"everywhere"`
	// NoRepository came from a run whose repository Memax doesn't know: no
	// project space was imported in the same run, or it names none.
	NoRepository int `json:"no_repository"`
	// NotLive are rejected, merged or forgotten.
	NotLive int               `json:"not_live"`
	Failed  int               `json:"failed"`
	Changes []RepoScopeChange `json:"changes"`
}

// BackfillRepoScope sets scope.repos on what an earlier memax init brought
// into a Personal space from project-bound files (ProjectBoundFileKinds)
// without it (personal-layer design §3.3): each such memory applies only in
// the repository whose project space the same init run imported into (init
// keys each import "<run>:<space>:<batch>"). A memory that also came from a
// file that applies everywhere stays everywhere; one someone already chose
// a scope for (it names repositories, or has a scoped receipt) is left as
// it is. Each change is one ScopeMemory as Memax (via system, with a
// reason), which only narrows, so the words never change.
//
// Each Personal space is read and written under its owner's scope, so
// row-level security holds as for any of their requests. It is idempotent:
// a memory it scoped names repositories afterwards, and its idempotency key
// is the memory's. Run it with cmd/v2-backfill-repo-scope.
func (l *Ledger) BackfillRepoScope(ctx context.Context, opts RepoScopeOptions) (RepoScopeReport, error) {
	report := RepoScopeReport{DryRun: opts.DryRun, Changes: []RepoScopeChange{}}
	if l == nil {
		return report, ErrDisabled
	}
	spaces, err := personalSpaces(ctx, l.pool, opts.Users)
	if err != nil {
		return report, err
	}
	for _, ps := range spaces {
		scope, err := ResolveUserScope(ctx, l.pool, ps.owner)
		if err != nil {
			return report, err
		}
		grant, ok := scope.Grant(ps.id)
		if !ok {
			continue
		}
		report.Spaces++
		plan, err := l.planRepoScope(ctx, scope, ps.owner, grant, &report)
		if err != nil {
			return report, err
		}
		for _, c := range plan {
			if opts.DryRun {
				c.Outcome = "planned"
				report.Scoped++
				report.Changes = append(report.Changes, c)
				continue
			}
			c.Outcome = l.applyRepoScope(ctx, scope.Narrow(ps.id), c)
			switch c.Outcome {
			case "scoped":
				report.Scoped++
			case "already":
				report.Already++
			default:
				report.Failed++
			}
			report.Changes = append(report.Changes, c)
		}
	}
	l.log.Info("ledger: backfilled repository scopes", "dry_run", opts.DryRun, "spaces", report.Spaces,
		"imports", report.Imports, "scoped", report.Scoped, "already", report.Already, "everywhere", report.Everywhere,
		"no_repository", report.NoRepository, "not_live", report.NotLive, "failed", report.Failed)
	return report, nil
}

// applyRepoScope writes one change as Memax.
func (l *Ledger) applyRepoScope(ctx context.Context, scope Scope, c RepoScopeChange) string {
	files := c.Files
	more := ""
	if len(files) > 3 {
		files, more = files[:3], fmt.Sprintf(" and %d more", len(c.Files)-3)
	}
	res, err := l.Apply(ctx, &ScopeMemory{
		Meta: Meta{
			Actor: Actor{Kind: policy.ActorMemax, Name: "Memax", Credential: policy.CredentialSession}, Scope: scope,
			Via: policy.ViaSystem, IdempotencyKey: "repo-scope:" + c.Memory + ":" + c.SpaceID.String(),
			Reason: fmt.Sprintf("Applies only in %s: memax init imported it from that repository's own notes (%s%s).",
				strings.Join(c.Repos, ", "), strings.Join(files, ", "), more),
		},
		Memory: c.Memory, Repos: c.Repos,
	})
	var ve *ValidationError
	switch {
	case errors.As(err, &ve) && ve.Field == "repos":
		// It names these repositories already (changed since the plan).
		return "already"
	case err != nil:
		l.log.Error("ledger: backfill repository scope failed", "space_id", c.SpaceID.String(), "memory", c.Memory, "error", err)
		return "failed"
	case res.Outcome == OutcomeRefused:
		l.log.Error("ledger: backfill repository scope refused", "space_id", c.SpaceID.String(), "memory", c.Memory, "policy", res.Policy.Code)
		return "refused"
	case res.Replayed:
		return "already"
	}
	return "scoped"
}

type personalSpace struct{ id, owner uuid.UUID }

// personalSpaces lists the Personal spaces on the V2 record, as the login
// role: like ResolveUserScope, this is identity data scopes are built from.
func personalSpaces(ctx context.Context, db Querier, users []uuid.UUID) ([]personalSpace, error) {
	if users == nil {
		users = []uuid.UUID{}
	}
	rows, err := db.Query(ctx, `
		SELECT h.id, h.owner_id FROM public.hubs h
		 WHERE h.space_kind = 'personal' AND h.v2_enabled_at IS NOT NULL
		   AND (cardinality($1::uuid[]) = 0 OR h.owner_id = ANY ($1))
		 ORDER BY h.owner_id, h.id`, users)
	if err != nil {
		return nil, fmt.Errorf("ledger: list personal spaces: %w", err)
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (personalSpace, error) {
		var s personalSpace
		return s, r.Scan(&s.id, &s.owner)
	})
}

// scopeImport is one of the person's init imports.
type scopeImport struct {
	id, space uuid.UUID
	run       string
	files     []ImportFile
}

// scopeItem is one statement of a Personal import that names a memory.
type scopeItem struct {
	importID uuid.UUID
	ref      string
	outcome  string
	memory   uuid.UUID
}

// planRepoScope reads one Personal space's init imports, and the project
// imports of the same runs, under the owner's scope, and decides each
// memory those imports created.
func (l *Ledger) planRepoScope(ctx context.Context, scope Scope, owner uuid.UUID, grant SpaceGrant, report *RepoScopeReport) ([]RepoScopeChange, error) {
	var (
		imports []scopeImport
		repos   = map[uuid.UUID]string{} // space → its repository key
		items   []scopeItem
		mems    = map[uuid.UUID]*scopeMemoryRow{}
	)
	err := l.Read(ctx, scope, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT i.id, i.space_id, i.idempotency_key, i.files FROM v2.imports i
			 WHERE i.space_id = ANY ($1) AND i.origin = 'init' AND i.actor_kind = 'person' AND i.actor_id = $2
			 ORDER BY i.created_at, i.id`, scope.SpaceIDs(), owner)
		if err != nil {
			return fmt.Errorf("ledger: read imports: %w", err)
		}
		imports, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (scopeImport, error) {
			var im scopeImport
			var key string
			var files []byte
			if err := r.Scan(&im.id, &im.space, &key, &files); err != nil {
				return im, err
			}
			if run, _, ok := strings.Cut(key, ":"); ok && strings.HasPrefix(run, "init-") {
				im.run = run
			}
			if err := json.Unmarshal(files, &im.files); err != nil {
				return im, fmt.Errorf("import %s files: %w", im.id, err)
			}
			return im, nil
		})
		if err != nil {
			return fmt.Errorf("ledger: read imports: %w", err)
		}
		rows, err = tx.Query(ctx, `SELECT id, COALESCE(repository, '') FROM v2.spaces WHERE kind <> 'personal'`)
		if err != nil {
			return fmt.Errorf("ledger: read spaces: %w", err)
		}
		for rows.Next() {
			var id uuid.UUID
			var repo string
			if err := rows.Scan(&id, &repo); err != nil {
				rows.Close()
				return err
			}
			if k := RepoKey(repo); repoKeyPattern.MatchString(k) {
				repos[id] = k
			}
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("ledger: read spaces: %w", err)
		}
		rows, err = tx.Query(ctx, `
			SELECT ii.import_id, ii.ref, ii.outcome, ii.memory_id FROM v2.import_items ii
			  JOIN v2.imports i ON i.id = ii.import_id AND i.space_id = ii.space_id
			 WHERE ii.space_id = $1 AND ii.memory_id IS NOT NULL
			   AND i.origin = 'init' AND i.actor_kind = 'person' AND i.actor_id = $2
			 ORDER BY ii.import_id, ii.position`, grant.SpaceID, owner)
		if err != nil {
			return fmt.Errorf("ledger: read import items: %w", err)
		}
		items, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (scopeItem, error) {
			var it scopeItem
			return it, r.Scan(&it.importID, &it.ref, &it.outcome, &it.memory)
		})
		if err != nil {
			return fmt.Errorf("ledger: read import items: %w", err)
		}
		var ids []uuid.UUID
		for _, it := range items {
			ids = append(ids, it.memory)
		}
		rows, err = tx.Query(ctx, `
			SELECT m.id, m.seq, m.lifecycle, m.scope,
			       EXISTS (SELECT 1 FROM v2.receipts r WHERE r.space_id = m.space_id AND r.object_id = m.id AND r.action = 'scoped')
			  FROM v2.memories m WHERE m.space_id = $1 AND m.id = ANY ($2)`, grant.SpaceID, ids)
		if err != nil {
			return fmt.Errorf("ledger: read memories: %w", err)
		}
		for rows.Next() {
			m := &scopeMemoryRow{}
			var seq int64
			if err := rows.Scan(&m.id, &seq, &m.lifecycle, &m.scope, &m.chosen); err != nil {
				rows.Close()
				return err
			}
			m.ref = FormatRef(PrefixMemory, seq)
			mems[m.id] = m
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}

	// Each run's repositories: the project spaces it imported into, and the
	// kinds of the files it sent each space (only a run's first batch lists
	// them).
	runRepos := map[string][]string{}
	kinds := map[string]map[string]string{} // run+space → path → kind
	byID := map[uuid.UUID]scopeImport{}
	for _, im := range imports {
		byID[im.id] = im
		if im.space == grant.SpaceID {
			report.Imports++
		}
		if im.run == "" {
			continue
		}
		if r, ok := repos[im.space]; ok && !slices.Contains(runRepos[im.run], r) {
			runRepos[im.run] = append(runRepos[im.run], r)
		}
		k := im.run + "\x00" + im.space.String()
		if kinds[k] == nil {
			kinds[k] = map[string]string{}
		}
		for _, f := range im.files {
			kinds[k][f.Path] = f.Kind
		}
	}

	// Each memory an import created, with every statement that named it.
	type decided struct {
		created bool
		items   []scopeItem
	}
	per := map[uuid.UUID]*decided{}
	var order []uuid.UUID
	for _, it := range items {
		d := per[it.memory]
		if d == nil {
			d = &decided{}
			per[it.memory] = d
			order = append(order, it.memory)
		}
		d.created = d.created || it.outcome == string(ImportProposed)
		d.items = append(d.items, it)
	}
	var plan []RepoScopeChange
	for _, id := range order {
		d, m := per[id], mems[id]
		if !d.created || m == nil {
			// Written some other way (Remember, an earlier import's
			// proposal elsewhere): where it applies isn't init's to say.
			continue
		}
		switch {
		case m.lifecycle != lifecycle.Proposed && m.lifecycle != lifecycle.Kept && m.lifecycle != lifecycle.Faded:
			report.NotLive++
			continue
		case m.chosen || len(ScopeRepos(m.scope)) > 0:
			report.Already++
			continue
		}
		var tied, files []string
		everywhere, unknown := false, false
		for _, it := range d.items {
			im := byID[it.importID]
			file := it.ref
			if i := strings.LastIndex(file, ":"); i > 0 {
				file = file[:i]
			}
			if !slices.Contains(ProjectBoundFileKinds, fileKind(kinds[im.run+"\x00"+im.space.String()], file)) {
				everywhere = true
				break
			}
			if !slices.Contains(files, file) {
				files = append(files, file)
			}
			rs := runRepos[im.run]
			if len(rs) == 0 {
				unknown = true
			}
			for _, r := range rs {
				if !slices.Contains(tied, r) {
					tied = append(tied, r)
				}
			}
		}
		switch {
		case everywhere:
			report.Everywhere++
		case unknown || len(tied) == 0:
			report.NoRepository++
		default:
			slices.Sort(tied)
			if len(tied) > MaxScopeRepos {
				tied = tied[:MaxScopeRepos]
			}
			plan = append(plan, RepoScopeChange{SpaceID: grant.SpaceID, Space: grant.Slug, Memory: m.ref, Repos: tied, Files: files})
		}
	}
	return plan, nil
}

// scopeMemoryRow is what planRepoScope reads of a memory.
type scopeMemoryRow struct {
	id        uuid.UUID
	ref       string
	lifecycle lifecycle.Lifecycle
	scope     json.RawMessage
	// chosen: someone already changed where it applies (a scoped receipt).
	chosen bool
}

// fileKind is a file's kind as its import listed it, or as its label
// says: init labels CLAUDE.local.md and Claude Code's auto memory the same
// way every time (packages/cli/src/lib/init/files.ts).
func fileKind(listed map[string]string, label string) string {
	if k, ok := listed[label]; ok {
		return k
	}
	switch {
	case label == "CLAUDE.local.md":
		return "claude_local"
	case strings.HasPrefix(label, "~/.claude/projects/") && strings.Contains(label, "/memory/"):
		return "claude_memory"
	}
	return ""
}
