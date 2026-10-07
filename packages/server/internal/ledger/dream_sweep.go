package ledger

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// Dream's schedule, settings and morning email (migration 046). None of it
// is the record: schedules and email sends are bookkeeping, settings are a
// person's preferences, so none of it carries receipts. The cross-space
// parts run as DreamSweeperRole, whose policies are keyed on app.sweep =
// 'dream' and apply to that role only.

// DreamSweeperRole is the role Dream's sweep and its email fan-out run as.
const DreamSweeperRole = "memax_v2_dream_sweeper"

// DreamSpaceArgs is the River job that runs Dream on one space for one
// slot. It is unique by args over every state River keeps, so a slot runs
// once however often the sweep or a retry queues it; an edition is unique
// per (space, slot) besides.
type DreamSpaceArgs struct {
	SpaceID uuid.UUID `json:"space_id"`
	Slot    time.Time `json:"slot"`
	// Trigger is schedule or manual; RequestedBy is the person who asked.
	Trigger     DreamTrigger `json:"trigger"`
	RequestedBy uuid.UUID    `json:"requested_by,omitzero"`
}

// Kind implements river.JobArgs.
func (DreamSpaceArgs) Kind() string { return "dream_space" }

// InsertOpts implements river.JobArgsWithInsertOpts.
func (DreamSpaceArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: QueueDream, MaxAttempts: 3, UniqueOpts: river.UniqueOpts{ByArgs: true}}
}

// SweepSpace is a V2 space the sweep looks at: its owner's zone, how busy
// it was this week among its owner's spaces, and its schedule, if any.
type SweepSpace struct {
	SpaceID, TenantID, OwnerID uuid.UUID
	// TimeZone is the owner's zone ("UTC" until Memax knows it).
	TimeZone string
	// Busy counts the week's changes by people and agents; BusyRank is its
	// place among the owner's V2 spaces (1 is the busiest).
	Busy, BusyRank int
	// The schedule, when the space has one.
	HasSchedule      bool
	Cadence          string
	ScheduleTimeZone string
	DueAt            time.Time
	LastSlot         *time.Time
}

// SweepDecision is what the sweep does with a space: its cadence, the zone
// and next due time to store, and the slot to run now, if one came due.
type SweepDecision struct {
	Cadence  string
	TimeZone string
	DueAt    time.Time
	// Slot, when set, queues a run for it.
	Slot *time.Time
}

// DreamSweep is the catch-up sweep (plan 25 §5.17: no River Pro). In one
// transaction, as DreamSweeperRole, it finds V2 spaces with no schedule,
// a due one, or one computed for another zone or owner, asks decide what
// each should be, stores it, and queues dream_space for every slot that
// came due. A space that missed several nights runs once, for the latest.
// It returns the spaces it queued.
func (l *Ledger) DreamSweep(ctx context.Context, now time.Time, limit int, decide func(SweepSpace) SweepDecision, jobs Jobs) ([]DreamSpaceArgs, error) {
	if l == nil {
		return nil, ErrDisabled
	}
	tx, loginRole, err := l.beginRole(ctx, DreamSweeperRole, Scope{}, pgx.TxOptions{AccessMode: pgx.ReadWrite})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT set_config('app.sweep', 'dream', true)`); err != nil {
		return nil, fmt.Errorf("ledger: dream sweep: %w", err)
	}
	rows, err := tx.Query(ctx, `
		SELECT h.id, h.tenant_id, h.owner_id, COALESCE(ds.time_zone, 'UTC'),
		       s.space_id IS NOT NULL, COALESCE(s.cadence, ''), COALESCE(s.time_zone, ''), COALESCE(s.due_at, $1), s.last_slot
		  FROM public.hubs h
		  LEFT JOIN v2.dream_schedules s ON s.space_id = h.id
		  LEFT JOIN v2.dream_settings ds ON ds.person_id = h.owner_id
		 WHERE h.v2_enabled_at IS NOT NULL AND h.tenant_id IS NOT NULL
		   AND (s.space_id IS NULL OR s.due_at <= $1 OR s.owner_id <> h.owner_id
		        OR s.time_zone <> COALESCE(ds.time_zone, 'UTC'))
		 ORDER BY s.due_at NULLS FIRST, h.id
		 LIMIT $2`, now, max(limit, 1))
	if err != nil {
		return nil, fmt.Errorf("ledger: dream sweep: %w", err)
	}
	spaces, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (SweepSpace, error) {
		var s SweepSpace
		err := r.Scan(&s.SpaceID, &s.TenantID, &s.OwnerID, &s.TimeZone, &s.HasSchedule, &s.Cadence, &s.ScheduleTimeZone,
			&s.DueAt, &s.LastSlot)
		return s, err
	})
	if err != nil {
		return nil, fmt.Errorf("ledger: dream sweep: %w", err)
	}
	if len(spaces) == 0 {
		return nil, tx.Commit(ctx)
	}
	if err := rankBusy(ctx, tx, now, spaces); err != nil {
		return nil, err
	}
	var queued []DreamSpaceArgs
	for _, s := range spaces {
		if s.HasSchedule {
			// Another sweep may hold it: skip it, it has it.
			var due time.Time
			err := tx.QueryRow(ctx, `SELECT due_at FROM v2.dream_schedules WHERE space_id = $1 FOR UPDATE SKIP LOCKED`, s.SpaceID).Scan(&due)
			if errNoRows(err) {
				continue
			}
			if err != nil {
				return nil, fmt.Errorf("ledger: dream sweep: %w", err)
			}
			s.DueAt = due
		}
		d := decide(s)
		if d.Cadence != "nightly" && d.Cadence != "weekly" {
			return nil, fmt.Errorf("ledger: dream sweep: cadence %q", d.Cadence)
		}
		var lastSlot any
		if d.Slot != nil {
			lastSlot = d.Slot.UTC()
			queued = append(queued, DreamSpaceArgs{SpaceID: s.SpaceID, Slot: d.Slot.UTC(), Trigger: DreamScheduled})
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO v2.dream_schedules AS s (space_id, tenant_id, owner_id, cadence, time_zone, due_at, last_slot, last_queued_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, CASE WHEN $7::timestamptz IS NULL THEN NULL ELSE now() END)
			ON CONFLICT (space_id) DO UPDATE
			   SET owner_id = EXCLUDED.owner_id, cadence = EXCLUDED.cadence, time_zone = EXCLUDED.time_zone,
			       due_at = EXCLUDED.due_at, last_slot = COALESCE(EXCLUDED.last_slot, s.last_slot),
			       last_queued_at = COALESCE(EXCLUDED.last_queued_at, s.last_queued_at), updated_at = now()`,
			s.SpaceID, s.TenantID, s.OwnerID, d.Cadence, d.TimeZone, d.DueAt, lastSlot); err != nil {
			return nil, fmt.Errorf("ledger: dream sweep: %w", err)
		}
	}
	if len(queued) > 0 && jobs != nil {
		params := make([]river.InsertManyParams, len(queued))
		for i, q := range queued {
			params[i] = river.InsertManyParams{Args: q}
		}
		// River's tables are the login role's (jobs.go); the insert is the
		// transaction's last statement.
		if _, err := tx.Exec(ctx, `SELECT set_config('role', $1, true)`, loginRole); err != nil {
			return nil, fmt.Errorf("ledger: dream sweep: %w", err)
		}
		if _, err := jobs.InsertManyTx(ctx, tx, params); err != nil {
			return nil, fmt.Errorf("ledger: dream sweep: queue %d run(s): %w", len(params), err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, mapDBError(fmt.Errorf("ledger: dream sweep: commit: %w", err))
	}
	return queued, nil
}

// rankBusy counts each space's changes this week and ranks it among its
// owner's V2 spaces (D9: Pro dreams nightly on the five busiest).
func rankBusy(ctx context.Context, tx pgx.Tx, now time.Time, spaces []SweepSpace) error {
	var owners []uuid.UUID
	for _, s := range spaces {
		if !slices.Contains(owners, s.OwnerID) {
			owners = append(owners, s.OwnerID)
		}
	}
	rows, err := tx.Query(ctx, `
		SELECT h.id, h.owner_id,
		       (SELECT count(*) FROM v2.receipts r
		         WHERE r.space_id = h.id AND r.recorded_at > $2 AND r.actor_kind IN ('person', 'agent'))
		  FROM public.hubs h
		 WHERE h.v2_enabled_at IS NOT NULL AND h.owner_id = ANY ($1)`, owners, now.Add(-7*24*time.Hour))
	if err != nil {
		return fmt.Errorf("ledger: dream sweep: rank spaces: %w", err)
	}
	type busy struct {
		space, owner uuid.UUID
		n            int
	}
	all, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (busy, error) {
		var b busy
		err := r.Scan(&b.space, &b.owner, &b.n)
		return b, err
	})
	if err != nil {
		return fmt.Errorf("ledger: dream sweep: rank spaces: %w", err)
	}
	slices.SortFunc(all, func(a, b busy) int {
		if a.n != b.n {
			return b.n - a.n
		}
		return strings.Compare(a.space.String(), b.space.String())
	})
	rank := map[uuid.UUID]int{}
	seen := map[uuid.UUID]int{}
	for _, b := range all {
		seen[b.owner]++
		rank[b.space] = seen[b.owner]
	}
	for i := range spaces {
		for _, b := range all {
			if b.space == spaces[i].SpaceID {
				spaces[i].Busy = b.n
			}
		}
		spaces[i].BusyRank = rank[spaces[i].SpaceID]
	}
	return nil
}

// QueueDreamRun queues a manual run of one space (run now), as the
// sweeper's job, after policy and the caller's rate limit allow it. It
// records no receipt: asking changes nothing; the edition it leads to is
// receipted, and names the person who asked.
func (l *Ledger) QueueDreamRun(ctx context.Context, scope Scope, actor Actor, via policy.Via, spaceID uuid.UUID, jobs Jobs) (DreamSpaceArgs, policy.Decision, error) {
	if l == nil {
		return DreamSpaceArgs{}, policy.Decision{}, ErrDisabled
	}
	grant, ok := scope.Grant(spaceID)
	if !ok {
		return DreamSpaceArgs{}, policy.Decision{}, ErrNotFound
	}
	args := DreamSpaceArgs{SpaceID: spaceID, Slot: l.now().UTC().Truncate(time.Second), Trigger: DreamManual, RequestedBy: actor.ID}
	var dec policy.Decision
	tx, loginRole, err := l.begin(ctx, scope.Narrow(spaceID), pgx.ReadWrite)
	if err != nil {
		return DreamSpaceArgs{}, policy.Decision{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	sp, err := loadSpace(ctx, tx, spaceID)
	if err != nil {
		return DreamSpaceArgs{}, policy.Decision{}, err
	}
	dec = policy.Decide(toPolicyActor(actor, via, grant), policy.ActionRunDream, policy.Object{}, sp.policy())
	if dec.Effect == policy.EffectRefuse {
		return DreamSpaceArgs{}, dec, nil
	}
	if jobs != nil {
		if err := asLoginRole(ctx, tx, loginRole, func() error {
			_, err := jobs.InsertManyTx(ctx, tx, []river.InsertManyParams{{Args: args}})
			return err
		}); err != nil {
			return DreamSpaceArgs{}, policy.Decision{}, fmt.Errorf("ledger: queue a Dream run: %w", err)
		}
	}
	return args, dec, tx.Commit(ctx)
}

// RecentEditions counts a space's editions since a time, by trigger: the
// plan's run caps and run now's rate limit read it.
func (l *Ledger) RecentEditions(ctx context.Context, scope Scope, spaceID uuid.UUID, since time.Time) (scheduled, manual int, err error) {
	if l == nil {
		return 0, 0, ErrDisabled
	}
	err = l.Read(ctx, scope.Narrow(spaceID), func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			SELECT count(*) FILTER (WHERE trigger = 'schedule'), count(*) FILTER (WHERE trigger = 'manual')
			  FROM v2.dream_editions WHERE space_id = $1 AND finished_at > $2`, spaceID, since).Scan(&scheduled, &manual)
	})
	return scheduled, manual, err
}

// ---------------------------------------------------------------------
// A person's settings
// ---------------------------------------------------------------------

// DreamSettings are a person's Dream settings.
type DreamSettings struct {
	TimeZone string `json:"time_zone"`
	// TimeZoneSource is default (Memax doesn't know it yet: UTC), observed
	// (from their app's clock) or set (by them).
	TimeZoneSource string `json:"time_zone_source"`
	MorningEmail   bool   `json:"morning_email"`
}

// The time zone sources.
const (
	ZoneDefault  = "default"
	ZoneObserved = "observed"
	ZoneSet      = "set"
)

// GetDreamSettings reads the scope's person's settings (the defaults when
// they have none).
func (l *Ledger) GetDreamSettings(ctx context.Context, scope Scope) (DreamSettings, error) {
	if l == nil {
		return DreamSettings{}, ErrDisabled
	}
	if scope.PersonID == uuid.Nil {
		return DreamSettings{}, ErrNotFound
	}
	out := DreamSettings{TimeZone: "UTC", TimeZoneSource: ZoneDefault, MorningEmail: true}
	err := l.Read(ctx, scope, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `SELECT time_zone, time_zone_source, morning_email FROM v2.dream_settings WHERE person_id = $1`,
			scope.PersonID).Scan(&out.TimeZone, &out.TimeZoneSource, &out.MorningEmail)
		if errNoRows(err) {
			return nil
		}
		return err
	})
	return out, err
}

// UpdateDreamSettings changes the scope's person's settings: a zone they
// set, or the morning email on or off. A nil field stays.
func (l *Ledger) UpdateDreamSettings(ctx context.Context, scope Scope, zone *string, email *bool) (DreamSettings, error) {
	if l == nil {
		return DreamSettings{}, ErrDisabled
	}
	if scope.PersonID == uuid.Nil {
		return DreamSettings{}, ErrNotFound
	}
	if zone != nil {
		z := strings.TrimSpace(*zone)
		if _, err := time.LoadLocation(z); err != nil || z == "" || z == "Local" || len(z) > 64 {
			return DreamSettings{}, invalid("time_zone", "use an IANA time zone like America/Vancouver or Asia/Shanghai")
		}
		zone = &z
	}
	err := l.writePerson(ctx, scope, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO v2.dream_settings AS d (person_id, time_zone, time_zone_source, morning_email)
			VALUES ($1, COALESCE($2, 'UTC'), CASE WHEN $2::text IS NULL THEN 'default' ELSE 'set' END, COALESCE($3, true))
			ON CONFLICT (person_id) DO UPDATE
			   SET time_zone = COALESCE($2, d.time_zone),
			       time_zone_source = CASE WHEN $2::text IS NULL THEN d.time_zone_source ELSE 'set' END,
			       morning_email = COALESCE($3, d.morning_email), updated_at = now()`, scope.PersonID, zone, email)
		return err
	})
	if err != nil {
		return DreamSettings{}, err
	}
	return l.GetDreamSettings(ctx, scope)
}

// ObserveTimeZone records the zone a person's app reports (X-Timezone),
// unless they set one themselves. It is how Dream learns the person's
// local night without asking; never assuming Pacific, or any zone.
func (l *Ledger) ObserveTimeZone(ctx context.Context, scope Scope, zone string) error {
	if l == nil || scope.PersonID == uuid.Nil {
		return nil
	}
	zone = strings.TrimSpace(zone)
	if _, err := time.LoadLocation(zone); err != nil || zone == "" || zone == "Local" || len(zone) > 64 {
		return nil // a client's bad header changes nothing
	}
	return l.writePerson(ctx, scope, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO v2.dream_settings AS d (person_id, time_zone, time_zone_source) VALUES ($1, $2, 'observed')
			ON CONFLICT (person_id) DO UPDATE SET time_zone = $2, time_zone_source = 'observed', updated_at = now()
			 WHERE d.time_zone_source <> 'set' AND d.time_zone <> $2`, scope.PersonID, zone)
		return err
	})
}

// writePerson runs fn in a write transaction scoped to the person.
func (l *Ledger) writePerson(ctx context.Context, scope Scope, fn func(pgx.Tx) error) error {
	tx, _, err := l.begin(ctx, Scope{PersonID: scope.PersonID}, pgx.ReadWrite)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(tx); err != nil {
		return mapDBError(err)
	}
	return tx.Commit(ctx)
}

// UnsubscribeDreamEmail turns off the morning email for whoever the token
// in that email belongs to (RFC 8058 one-click). It says nothing about
// whether the token matched.
func (l *Ledger) UnsubscribeDreamEmail(ctx context.Context, token string) error {
	if l == nil {
		return ErrDisabled
	}
	tx, _, err := l.begin(ctx, Scope{}, pgx.ReadWrite)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var matched bool
	if err := tx.QueryRow(ctx, `SELECT v2.unsubscribe_dream_email($1)`, token).Scan(&matched); err != nil {
		return fmt.Errorf("ledger: unsubscribe: %w", err)
	}
	if matched {
		l.log.Info("dream: morning email turned off by its unsubscribe link")
	}
	return tx.Commit(ctx)
}

// ---------------------------------------------------------------------
// The morning email's recipients
// ---------------------------------------------------------------------

// DreamRecipient is a person an edition's morning email goes to.
type DreamRecipient struct {
	PersonID uuid.UUID
	Email    string
	Name     string
	// Token is their one-click unsubscribe token.
	Token string
	// TimeZone is theirs, for the edition's date ("UTC" until known).
	TimeZone string
	// Sent is set when this edition's email already went to them.
	Sent bool
}

// DreamEmailSpace is what the email says about the space.
type DreamEmailSpace struct {
	Name, Slug string
}

// DreamRecipients lists who gets an edition's morning email: the people
// who may keep in the space (they are who "needs you" means), with the
// email on. It reads members and addresses as DreamSweeperRole.
func (l *Ledger) DreamRecipients(ctx context.Context, spaceID, editionID uuid.UUID) (DreamEmailSpace, []DreamRecipient, error) {
	if l == nil {
		return DreamEmailSpace{}, nil, ErrDisabled
	}
	var sp DreamEmailSpace
	var out []DreamRecipient
	err := l.asDreamSweeper(ctx, func(tx pgx.Tx) error {
		var rulesJSON []byte
		var owner uuid.UUID
		err := tx.QueryRow(ctx, `SELECT name, COALESCE(slug, ''), owner_id, rules FROM public.hubs WHERE id = $1`, spaceID).
			Scan(&sp.Name, &sp.Slug, &owner, &rulesJSON)
		if errNoRows(err) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("ledger: email recipients: %w", err)
		}
		var rules policy.Rules
		if err := json.Unmarshal(rulesJSON, &rules); err != nil {
			return fmt.Errorf("ledger: email recipients: rules: %w", err)
		}
		rows, err := tx.Query(ctx, `
			SELECT u.id, COALESCE(m.role, 'owner') FROM public.users u
			  LEFT JOIN public.hub_members m ON m.hub_id = $1 AND m.user_id = u.id
			 WHERE u.id = $2 OR m.hub_id = $1`, spaceID, owner)
		if err != nil {
			return fmt.Errorf("ledger: email recipients: %w", err)
		}
		type member struct {
			id   uuid.UUID
			role string
		}
		members, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (member, error) {
			var m member
			err := r.Scan(&m.id, &m.role)
			return m, err
		})
		if err != nil {
			return fmt.Errorf("ledger: email recipients: %w", err)
		}
		var keepers []uuid.UUID
		for _, m := range members {
			role, _ := policy.RoleFromV1(m.role)
			if m.id == owner {
				role = policy.RoleOwner
			}
			if role == policy.RoleOwner || (role == policy.RoleMember && rules.KeepBy() == policy.WhoMembers) {
				keepers = append(keepers, m.id)
			}
		}
		if len(keepers) == 0 {
			return nil
		}
		if _, err := tx.Exec(ctx, `INSERT INTO v2.dream_settings (person_id) SELECT unnest($1::uuid[]) ON CONFLICT DO NOTHING`,
			keepers); err != nil {
			return fmt.Errorf("ledger: email recipients: %w", err)
		}
		rows, err = tx.Query(ctx, `
			SELECT u.id, u.email, COALESCE(NULLIF(u.display_name, ''), u.name), d.unsubscribe_token, d.time_zone,
			       EXISTS (SELECT 1 FROM v2.dream_email_sends s WHERE s.edition_id = $2 AND s.person_id = u.id)
			  FROM public.users u JOIN v2.dream_settings d ON d.person_id = u.id
			 WHERE u.id = ANY ($1) AND d.morning_email AND u.email <> ''
			 ORDER BY u.id`, keepers, editionID)
		if err != nil {
			return fmt.Errorf("ledger: email recipients: %w", err)
		}
		out, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (DreamRecipient, error) {
			var d DreamRecipient
			err := r.Scan(&d.PersonID, &d.Email, &d.Name, &d.Token, &d.TimeZone, &d.Sent)
			return d, err
		})
		return err
	})
	return sp, out, err
}

// MarkDreamEmailSent records that an edition's email went to a person.
func (l *Ledger) MarkDreamEmailSent(ctx context.Context, spaceID, editionID, personID uuid.UUID, messageID string) error {
	if l == nil {
		return ErrDisabled
	}
	return l.asDreamSweeper(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO v2.dream_email_sends (edition_id, person_id, space_id, message_id) VALUES ($1, $2, $3, $4)
			ON CONFLICT DO NOTHING`, editionID, personID, spaceID, nullText(truncateRunes(messageID, 200)))
		return err
	})
}

// asDreamSweeper runs fn as DreamSweeperRole with app.sweep set.
func (l *Ledger) asDreamSweeper(ctx context.Context, fn func(pgx.Tx) error) error {
	tx, _, err := l.beginRole(ctx, DreamSweeperRole, Scope{}, pgx.TxOptions{AccessMode: pgx.ReadWrite})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT set_config('app.sweep', 'dream', true)`); err != nil {
		return fmt.Errorf("ledger: dream sweeper: %w", err)
	}
	if err := fn(tx); err != nil {
		return mapDBError(err)
	}
	return tx.Commit(ctx)
}
