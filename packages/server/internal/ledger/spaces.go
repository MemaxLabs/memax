package ledger

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// Spaces are V1's hubs (plan 25 §5.4), so creating one and switching one
// to the V2 record write public.hubs as the login role, as V1 and
// internal/spacemode do. A space isn't part of its own record: neither
// writes a receipt, and the space's first receipt is its first change.
// Who may is policy's (policy.DecideCreateSpace, DecideSwitchSpace).

// NewSpace is what CreateSpace needs.
type NewSpace struct {
	Name string
	// Slug is optional: without one, the name's slug is used, with a
	// number added when it is taken. A slug the caller names must be free.
	Slug string
	Kind policy.SpaceKind
	// Repository is the repository the space compiles for ("owner/name").
	Repository string
}

// The space limits.
const (
	MaxSpaceName       = 80
	MaxSpaceRepository = 200
)

// spaceSlug is V1's hub slug rule, so a V2 space is a valid hub to V1.
var spaceSlug = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{2,48}[a-z0-9]$`)

// reservedSlugs are V1's reserved hub slugs and the web app's reserved
// top-level routes (packages/web/src/lib/ui-gate.ts), so a space's URL
// never shadows a page.
var reservedSlugs = map[string]bool{}

func init() {
	for _, s := range strings.Fields(`
		personal shared team teams public private internal external all everyone anyone new me my user users
		admin api auth dashboard dev docs help home hub hubs inbox login logout memories memory recall register
		settings signup support topic topics waitlist memax memaxlabs root test demo staging prod production
		null undefined true false
		setup signin device join oauth pricing security h agents brain discover dreams invite privacy pulse
		share terms images assets static account billing blog changelog download legal mcp signout status v1 v2`) {
		reservedSlugs[s] = true
	}
}

// ErrSlugTaken: the slug the caller named belongs to another space.
var ErrSlugTaken = errors.New("ledger: the slug is taken")

// SpaceHasNotesError: a space switches to the V2 record on its own only
// while it holds no V1 memories; one that does switches in the app, with
// the import cleanup (plan 25 §10, epic 2.8).
type SpaceHasNotesError struct {
	Slug  string
	Notes int
}

func (e *SpaceHasNotesError) Error() string {
	return fmt.Sprintf("%s holds %d V1 %s. Switch it to V2 in the app, where they are cleaned up first.",
		e.Slug, e.Notes, plural(e.Notes, "memory", "memories"))
}

// SpaceRefusedError is a policy refusal of a space change.
type SpaceRefusedError struct{ Decision policy.Decision }

func (e *SpaceRefusedError) Error() string { return e.Decision.Message }

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// slugify makes a slug out of a name: lower case, letters and digits,
// hyphens between words.
func slugify(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			dash = false
			b.WriteRune(r)
		default:
			dash = true
		}
	}
	s := b.String()
	if len(s) > 44 {
		s = strings.TrimRight(s[:44], "-")
	}
	return s
}

func (n *NewSpace) validate() error {
	n.Name = strings.TrimSpace(n.Name)
	if err := checkText("name", n.Name, MaxSpaceName, true); err != nil {
		return err
	}
	n.Slug = strings.ToLower(strings.TrimSpace(n.Slug))
	if n.Slug != "" && !spaceSlug.MatchString(n.Slug) {
		return invalid("slug", "use 4 to 50 lowercase letters, digits and hyphens, starting and ending with a letter or digit")
	}
	if n.Slug != "" && reservedSlugs[n.Slug] {
		return &slugTakenError{slug: n.Slug}
	}
	if n.Kind == "" {
		n.Kind = policy.SpaceProject
	}
	if !n.Kind.Valid() {
		return invalid("kind", "use project")
	}
	n.Repository = strings.TrimSpace(n.Repository)
	return checkText("repository", n.Repository, MaxSpaceRepository, false)
}

// slugTakenError wraps ErrSlugTaken with the slug.
type slugTakenError struct{ slug string }

func (e *slugTakenError) Error() string {
	return fmt.Sprintf("%s is taken. Choose another slug, or leave it out and Memax picks one.", e.slug)
}
func (e *slugTakenError) Is(target error) bool { return target == ErrSlugTaken }

// CreateSpace creates a space on the V2 record, owned by the person, and
// returns it with the person's role (owner). It refuses with
// *SpaceRefusedError when policy does.
func (l *Ledger) CreateSpace(ctx context.Context, actor Actor, in NewSpace) (*Space, error) {
	if l == nil {
		return nil, ErrDisabled
	}
	if err := in.validate(); err != nil {
		return nil, err
	}
	if actor.Kind != policy.ActorPerson || actor.ID == uuid.Nil {
		d := policy.DecideCreateSpace(policy.Actor{Kind: actor.Kind, Credential: actor.Credential}, in.Kind, 0)
		return nil, &SpaceRefusedError{Decision: d}
	}
	tx, err := l.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("ledger: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// One person creates one space at a time, so the fair-use count can't
	// be raced past.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('v2.create_space:' || $1::text, 0))`, actor.ID); err != nil {
		return nil, fmt.Errorf("ledger: create space: %w", err)
	}
	var owned int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM public.hubs WHERE owner_id = $1 AND space_kind = 'project'`, actor.ID).Scan(&owned); err != nil {
		return nil, fmt.Errorf("ledger: create space: %w", err)
	}
	if d := policy.DecideCreateSpace(policy.Actor{Kind: actor.Kind, Credential: actor.Credential}, in.Kind, owned); d.Effect == policy.EffectRefuse {
		return nil, &SpaceRefusedError{Decision: d}
	}

	id := newID()
	now := l.now().UTC().Truncate(time.Microsecond)
	candidates := []string{in.Slug}
	if in.Slug == "" {
		base := slugify(in.Name)
		if len(base) < 4 {
			base = strings.Trim(base+"-space", "-")
		}
		if !reservedSlugs[base] && spaceSlug.MatchString(base) {
			candidates = []string{base}
		} else {
			candidates = nil
		}
		for i := 2; i <= 9; i++ {
			candidates = append(candidates, base+"-"+strconv.Itoa(i))
		}
		candidates = append(candidates, base+"-"+id.String()[:8])
	}
	var slug string
	for _, c := range candidates {
		if !spaceSlug.MatchString(c) || reservedSlugs[c] {
			continue
		}
		tag, err := tx.Exec(ctx, `
			INSERT INTO public.hubs (id, name, slug, hub_type, owner_id, space_kind, repository, v2_enabled_at, created_at, updated_at)
			VALUES ($1, $2, $3, 'team', $4, $5, $6, $7, $7, $7)
			ON CONFLICT (slug) DO NOTHING`,
			id, in.Name, c, actor.ID, string(in.Kind), nullText(in.Repository), now)
		if err != nil {
			return nil, fmt.Errorf("ledger: create space: %w", err)
		}
		if tag.RowsAffected() == 1 {
			slug = c
			break
		}
	}
	if slug == "" {
		if in.Slug != "" {
			return nil, &slugTakenError{slug: in.Slug}
		}
		return nil, fmt.Errorf("ledger: create space: no free slug for %q", in.Name)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO public.hub_members (hub_id, user_id, role) VALUES ($1, $2, 'owner')`, id, actor.ID); err != nil {
		return nil, fmt.Errorf("ledger: create space: %w", err)
	}
	var sp Space
	if err := tx.QueryRow(ctx, `SELECT id, tenant_id, slug, name, space_kind, COALESCE(repository, ''), v2_enabled_at FROM public.hubs WHERE id = $1`, id).
		Scan(&sp.ID, &sp.TenantID, &sp.Slug, &sp.Name, &sp.Kind, &sp.Repository, &sp.V2EnabledAt); err != nil {
		return nil, fmt.Errorf("ledger: create space: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, mapDBError(fmt.Errorf("ledger: create space: %w", err))
	}
	sp.Role = policy.RoleOwner
	l.log.Info("ledger: space created", "space_id", sp.ID.String(), "slug", sp.Slug, "kind", string(sp.Kind))
	return &sp, nil
}

// SwitchSpace moves a space in the actor's scope to the V2 record, if it
// holds no V1 memories (*SpaceHasNotesError otherwise). Only its owner
// may. A space already on V2 is returned as it is.
func (l *Ledger) SwitchSpace(ctx context.Context, actor Actor, scope Scope, spaceID uuid.UUID) (*Space, error) {
	if l == nil {
		return nil, ErrDisabled
	}
	grant, ok := scope.Grant(spaceID)
	if !ok {
		return nil, ErrNotFound
	}
	pa := policy.Actor{Kind: actor.Kind, Credential: actor.Credential, Role: grant.Role}
	if d := policy.DecideSwitchSpace(pa); d.Effect == policy.EffectRefuse {
		return nil, &SpaceRefusedError{Decision: d}
	}
	tx, err := l.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("ledger: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var sp Space
	err = tx.QueryRow(ctx, `
		SELECT id, tenant_id, slug, name, space_kind, COALESCE(repository, ''), v2_enabled_at
		  FROM public.hubs WHERE id = $1 FOR UPDATE`, spaceID).
		Scan(&sp.ID, &sp.TenantID, &sp.Slug, &sp.Name, &sp.Kind, &sp.Repository, &sp.V2EnabledAt)
	if errNoRows(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("ledger: switch space: %w", err)
	}
	sp.Role = grant.Role
	if sp.V2EnabledAt != nil {
		return &sp, nil
	}
	var notes int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM public.memories WHERE hub_id = $1`, spaceID).Scan(&notes); err != nil {
		return nil, fmt.Errorf("ledger: switch space: %w", err)
	}
	if notes > 0 {
		return nil, &SpaceHasNotesError{Slug: sp.Slug, Notes: notes}
	}
	now := l.now().UTC().Truncate(time.Microsecond)
	if _, err := tx.Exec(ctx, `UPDATE public.hubs SET v2_enabled_at = $2 WHERE id = $1 AND v2_enabled_at IS NULL`, spaceID, now); err != nil {
		return nil, fmt.Errorf("ledger: switch space: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("ledger: switch space: %w", err)
	}
	sp.V2EnabledAt = &now
	l.log.Info("ledger: space switched to V2", "space_id", sp.ID.String(), "slug", sp.Slug)
	return &sp, nil
}
