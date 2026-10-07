// Package devseed seeds the `memax-v2` demo space of the V2 handoff
// (docs/v2/handoff/screens/INDEX.md, "Demo data"): Ziyang (ZZ, owner) and
// Jiahao (JY, member), Claude Code (CC, at Write) and Codex (CX, at
// Propose), the memories the screens show under their own IDs (M-0219
// River, M-0102 MCP transport, M-0098 problem+json, M-0071 pnpm, M-0187
// stale, the open question M-0431 against M-0174, …), the Brief B-0043
// and the four default targets, compiling as C-0881 onwards.
//
// The V2 record is written only through ledger commands, the way any
// client writes it. Only the V1 rows the record hangs off (users, the
// space's hub and memberships, the agents' OAuth grants) are inserted
// directly, because V2 has no commands for them yet.
//
// Seeding is idempotent: rows have fixed ids, and every command carries a
// fixed idempotency key, so a second run replays instead of duplicating.
// It refuses to run when MEMAX_ENV is production.
package devseed

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// ErrProduction is returned when MEMAX_ENV says production.
var ErrProduction = errors.New("devseed: refusing to seed demo data into production (MEMAX_ENV)")

// SpaceSlug is the demo space's slug.
const SpaceSlug = "memax-v2"

// Options configure a seed.
type Options struct {
	// Env is MEMAX_ENV. production (or prod) is refused.
	Env string
	// SkipBriefAndTargets seeds the memories only (the gate test writes
	// the Brief and the targets itself, through the API).
	SkipBriefAndTargets bool
}

// Demo is what was seeded.
type Demo struct {
	ZZ, JY  uuid.UUID
	Space   uuid.UUID
	Tenant  uuid.UUID
	CC, CX  *ledger.Connection
	Memory  map[string]*ledger.Memory // by display ID
	Brief   *ledger.Brief
	Targets map[ledger.TargetKind]*ledger.Target
}

// ns fixes every seeded id.
var ns = uuid.MustParse("6d656d61-782d-7632-2d64-656d6f736565") // "memax-v2-demosee"

func id(name string) uuid.UUID { return uuid.NewSHA1(ns, []byte(name)) }

// The demo's people.
var (
	zzID = id("user:zz")
	jyID = id("user:jy")
)

// memorySeed is one demo memory.
type memorySeed struct {
	seq       int64
	by        string // zz, jy, cc, cx
	propose   bool   // a proposal, waiting in Review
	section   ledger.Section
	kind      ledger.Kind
	statement string
	at        string // when it happened, 2026-MM-DD
	sources   []ledger.SourceInput
	stale     string // stale_after, if set
	scope     string // the memory's scope JSON, if any
}

// demoMemories are the screens' memories, in ID order.
var demoMemories = []memorySeed{
	{seq: 1, by: "zz", section: ledger.SectionDecisions, at: "2026-09-28",
		statement: "Memax V2 is the context layer every agent on the team reads from."},
	{seq: 12, by: "zz", section: ledger.SectionDecisions, kind: ledger.KindDecision, at: "2026-09-28",
		statement: "Agents at Propose send writes to Review; agents at Write keep directly."},
	{seq: 71, by: "jy", section: ledger.SectionConventions, at: "2026-08-29",
		statement: "pnpm workspaces only. Never run `npm install` at the root."},
	{seq: 98, by: "jy", section: ledger.SectionDecisions, kind: ledger.KindDecision, at: "2026-09-12",
		statement: "API errors are RFC 9457 problem+json, never bare strings."},
	{seq: 102, by: "zz", section: ledger.SectionDecisions, kind: ledger.KindDecision, at: "2026-09-30",
		statement: "Remote MCP is stateless streamable HTTP with OAuth 2.1, per spec 2026-07-28."},
	{seq: 112, by: "zz", section: ledger.SectionConventions, at: "2026-09-03",
		statement: "Every write tool returns a receipt ID the caller can cite."},
	{seq: 174, by: "jy", section: ledger.SectionDecisions, kind: ledger.KindDecision, at: "2026-09-21",
		statement: "Deploy the v2 API to Railway."},
	{seq: 187, by: "cc", section: ledger.SectionDecisions, kind: ledger.KindDecision, at: "2026-09-18",
		statement: "Ask memax answers with the Haiku tier.", stale: "2026-10-01T00:00:00Z",
		sources: []ledger.SourceInput{{Kind: ledger.SourcePR, Ref: "PR #198", URI: "https://github.com/MemaxLabs/memax/pull/198"}}},
	{seq: 219, by: "zz", section: ledger.SectionDecisions, kind: ledger.KindDecision, at: "2026-10-02",
		statement: "Background jobs run on River, Postgres-backed. We do not use Temporal.",
		sources:   []ledger.SourceInput{{Kind: ledger.SourcePR, Ref: "PR #212", URI: "https://github.com/MemaxLabs/memax/pull/212"}}},
	{seq: 430, by: "cx", propose: true, section: ledger.SectionConventions, at: "2026-10-05",
		statement: "MCP write tools must ask for confirmation with input_required."},
	{seq: 431, by: "cx", propose: true, section: ledger.SectionDecisions, kind: ledger.KindDecision, at: "2026-10-05",
		statement: "Deploy the v2 API to Fly.io."},
	{seq: 436, by: "cc", section: ledger.SectionConventions, at: "2026-10-04",
		statement: "Compile adapters live in packages/compiler, one per target.",
		sources:   []ledger.SourceInput{{Kind: ledger.SourceFile, Ref: "packages/compiler/README.md", URI: "packages/compiler/README.md"}}},
	{seq: 441, by: "zz", section: ledger.SectionConventions, at: "2026-10-04",
		statement: "Run tests with `pnpm test`."},
	{seq: 442, by: "zz", section: ledger.SectionConventions, at: "2026-10-04",
		statement: "Web screens use only packages/ledger components; no Tailwind under (ledger).",
		scope:     `{"paths": ["packages/web/**"]}`},
}

// DemoBrief is the Brief B-0043 the screens show.
func DemoBrief() (title, summary string, sections []ledger.BriefSection) {
	return "Memax V2 engineering brief", "What every agent on this project reads before it writes code.",
		[]ledger.BriefSection{
			{Key: "overview", Heading: "What this is", Items: []ledger.BriefItem{
				{Text: "Memax V2 is the context layer every agent on the team reads from: a cited record of decisions and conventions that compiles into each tool's files and answers live over MCP.",
					Cites: []string{"M-0001"}},
				{Text: "Writes from agents go to Review unless the agent is trusted to write.", Cites: []string{"M-0012"}},
			}},
			{Key: "decisions", Heading: "Decisions", Items: []ledger.BriefItem{
				{Ref: "M-0219"}, {Ref: "M-0102"}, {Ref: "M-0098"}, {Ref: "M-0187"},
			}},
			{Key: "conventions", Heading: "Conventions", Items: []ledger.BriefItem{
				{Ref: "M-0071"}, {Ref: "M-0112"}, {Ref: "M-0436"}, {Ref: "M-0441"},
			}},
			{Key: "open", Heading: "Open", Items: []ledger.BriefItem{
				{Text: "Fly.io or Railway for the v2 API is undecided. Ask before changing deploy config.",
					Cites: []string{"M-0431", "M-0174"}},
			}},
		}
}

// SeedMemaxV2 seeds the demo space. pool is the app's login pool; l is a
// ledger on it.
func SeedMemaxV2(ctx context.Context, pool *pgxpool.Pool, l *ledger.Ledger, o Options) (*Demo, error) {
	switch strings.ToLower(strings.TrimSpace(o.Env)) {
	case "production", "prod":
		return nil, ErrProduction
	}
	if l == nil || pool == nil {
		return nil, errors.New("devseed: needs a database")
	}
	d := &Demo{Memory: map[string]*ledger.Memory{}, Targets: map[ledger.TargetKind]*ledger.Target{}}
	var err error
	if d.ZZ, err = upsertUser(ctx, pool, zzID, "ziyang@example.com", "Ziyang Zeng"); err != nil {
		return nil, err
	}
	if d.JY, err = upsertUser(ctx, pool, jyID, "jiahao@example.com", "Jiahao Ye"); err != nil {
		return nil, err
	}
	if d.Space, err = upsertSpace(ctx, pool, d.ZZ, d.JY); err != nil {
		return nil, err
	}
	zzScope, err := l.UserScope(ctx, d.ZZ)
	if err != nil {
		return nil, err
	}
	zzScope = zzScope.Narrow(d.Space)
	jyScope, err := l.UserScope(ctx, d.JY)
	if err != nil {
		return nil, err
	}
	jyScope = jyScope.Narrow(d.Space)
	g, _ := zzScope.Grant(d.Space)
	d.Tenant = g.TenantID

	apply := func(cmd ledger.Command) (ledger.Result, error) {
		res, err := l.Apply(ctx, cmd)
		if err != nil {
			return res, fmt.Errorf("devseed: %s: %w", cmd.Name(), err)
		}
		if res.Outcome == ledger.OutcomeRefused {
			return res, fmt.Errorf("devseed: %s refused: %s", cmd.Name(), res.Policy.Message)
		}
		return res, nil
	}
	webMeta := func(user uuid.UUID, scope ledger.Scope, key string, at time.Time) ledger.Meta {
		return ledger.Meta{Actor: ledger.Actor{Kind: policy.ActorPerson, ID: user, Credential: policy.CredentialSession},
			Scope: scope, Via: policy.ViaWeb, IdempotencyKey: "devseed:" + key, OccurredAt: at}
	}

	// The agents: Claude Code at Write, Codex at Propose, as ZZ connected
	// them on the web.
	connect := func(name string, agent ledger.AgentKind, level policy.Autonomy) (*ledger.Connection, error) {
		grant, err := upsertGrant(ctx, pool, d.ZZ, name, string(agent))
		if err != nil {
			return nil, err
		}
		res, err := apply(&ledger.ConnectAgent{Meta: webMeta(d.ZZ, zzScope, "connect:"+name, day("2026-09-01")),
			Credential: ledger.CredentialOAuthGrant, CredentialID: grant, Agent: agent,
			Spaces: []ledger.SpaceAutonomy{{SpaceID: d.Space, Autonomy: level}}})
		if err != nil {
			return nil, err
		}
		return res.Connection, nil
	}
	if d.CC, err = connect("cc", ledger.AgentClaudeCode, policy.AutonomyWrite); err != nil {
		return nil, err
	}
	if d.CX, err = connect("cx", ledger.AgentCodex, policy.AutonomyPropose); err != nil {
		return nil, err
	}
	agentMeta := func(c *ledger.Connection, key string, at time.Time) (ledger.Meta, error) {
		got, err := l.ConnectionForCredential(ctx, zzScope, c.Credential.Kind, c.Credential.ID)
		if err != nil {
			return ledger.Meta{}, err
		}
		return ledger.Meta{
			Actor: ledger.Actor{Kind: policy.ActorAgent, ID: c.ID, Name: c.DisplayName, Agent: string(c.Agent),
				Credential: c.Credential.Kind.Policy()},
			Scope: zzScope.WithConnection(got, policy.AutonomyWrite), Via: policy.ViaMCP,
			SessionRef: string(c.Agent) + "-demo", IdempotencyKey: "devseed:" + key, OccurredAt: at,
		}, nil
	}

	for _, m := range demoMemories {
		if err := l.AdvanceCounter(ctx, zzScope, d.Space, ledger.PrefixMemory, m.seq); err != nil {
			return nil, err
		}
		nm := ledger.NewMemory{SpaceID: d.Space, Statement: m.statement, Section: m.section, Kind: m.kind, Sources: m.sources}
		if m.scope != "" {
			nm.Applies = json.RawMessage(m.scope)
		}
		if m.stale != "" {
			t, err := time.Parse(time.RFC3339, m.stale)
			if err != nil {
				return nil, err
			}
			nm.StaleAfter = &t
		}
		key := ledger.FormatRef(ledger.PrefixMemory, m.seq)
		var cmd ledger.Command
		switch m.by {
		case "zz":
			cmd = &ledger.Remember{Meta: webMeta(d.ZZ, zzScope, key, day(m.at)), NewMemory: nm}
		case "jy":
			cmd = &ledger.Remember{Meta: webMeta(d.JY, jyScope, key, day(m.at)), NewMemory: nm}
		case "cc", "cx":
			c := d.CC
			if m.by == "cx" {
				c = d.CX
			}
			meta, err := agentMeta(c, key, day(m.at))
			if err != nil {
				return nil, err
			}
			cmd = &ledger.Propose{Meta: meta, NewMemory: nm}
		}
		res, err := apply(cmd)
		if err != nil {
			return nil, err
		}
		if res.Memory.Ref != key {
			return nil, fmt.Errorf("devseed: %s was written as %s; the tenant already had later IDs", key, res.Memory.Ref)
		}
		if wantKept := !m.propose; wantKept != (res.Memory.State != "proposed") {
			return nil, fmt.Errorf("devseed: %s is %s", key, res.Memory.State)
		}
		d.Memory[key] = res.Memory
	}
	if o.SkipBriefAndTargets {
		return d, nil
	}

	if err := l.AdvanceCounter(ctx, zzScope, d.Space, ledger.PrefixBrief, 43); err != nil {
		return nil, err
	}
	title, summary, sections := DemoBrief()
	res, err := apply(&ledger.ReviseBrief{Meta: webMeta(d.ZZ, zzScope, "brief", day("2026-10-05")),
		SpaceID: d.Space, Title: title, Summary: summary, Sections: sections})
	if err != nil {
		return nil, err
	}
	d.Brief = res.Brief
	if err := l.AdvanceCounter(ctx, zzScope, d.Space, ledger.PrefixCompile, 881); err != nil {
		return nil, err
	}
	for _, k := range ledger.DefaultTargetKinds {
		res, err := apply(&ledger.ConfigureTarget{Meta: webMeta(d.ZZ, zzScope, "target:"+string(k), day("2026-10-05")),
			SpaceID: d.Space, Kind: k})
		if err != nil {
			return nil, err
		}
		d.Targets[k] = res.Target
	}
	return d, nil
}

// day is noon UTC on a demo date.
func day(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t.Add(12 * time.Hour)
}

func upsertUser(ctx context.Context, pool *pgxpool.Pool, want uuid.UUID, email, name string) (uuid.UUID, error) {
	if _, err := pool.Exec(ctx, `INSERT INTO users (id, email, name) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`,
		want, email, name); err != nil {
		return uuid.Nil, fmt.Errorf("devseed: user %s: %w", email, err)
	}
	var got uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM users WHERE email = $1`, email).Scan(&got); err != nil {
		return uuid.Nil, fmt.Errorf("devseed: user %s: %w", email, err)
	}
	return got, nil
}

func upsertSpace(ctx context.Context, pool *pgxpool.Pool, owner, member uuid.UUID) (uuid.UUID, error) {
	space := id("space:" + SpaceSlug)
	if _, err := pool.Exec(ctx, `
		INSERT INTO hubs (id, name, slug, hub_type, owner_id, space_kind, repository)
		VALUES ($1, $2, $3, 'team', $4, 'project', 'MemaxLabs/memax') ON CONFLICT DO NOTHING`,
		space, SpaceSlug, SpaceSlug, owner); err != nil {
		return uuid.Nil, fmt.Errorf("devseed: space: %w", err)
	}
	var got uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM hubs WHERE slug = $1 AND owner_id = $2`, SpaceSlug, owner).Scan(&got); err != nil {
		return uuid.Nil, fmt.Errorf("devseed: space %s exists and isn't the demo's: %w", SpaceSlug, err)
	}
	// On the V2 record, as POST /v2/spaces makes spaces (migration 033):
	// MCP serves it through the ledger, and its people see the V2 UI
	// (internal/v2ui). A space seeded before this keeps its first time.
	if _, err := pool.Exec(ctx, `UPDATE hubs SET v2_enabled_at = COALESCE(v2_enabled_at, now()) WHERE id = $1`, got); err != nil {
		return uuid.Nil, fmt.Errorf("devseed: space on V2: %w", err)
	}
	for _, m := range []struct {
		user uuid.UUID
		role string
	}{{owner, "owner"}, {member, "contributor"}} {
		if _, err := pool.Exec(ctx, `INSERT INTO hub_members (hub_id, user_id, role) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`,
			got, m.user, m.role); err != nil {
			return uuid.Nil, fmt.Errorf("devseed: membership: %w", err)
		}
	}
	return got, nil
}

func upsertGrant(ctx context.Context, pool *pgxpool.Pool, user uuid.UUID, name, agent string) (uuid.UUID, error) {
	grant := id("grant:" + name)
	if _, err := pool.Exec(ctx, `INSERT INTO oauth_clients (client_id, client_name) VALUES ('memax-devseed', 'Memax demo') ON CONFLICT DO NOTHING`); err != nil {
		return uuid.Nil, fmt.Errorf("devseed: oauth client: %w", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO oauth_grants (id, user_id, client_id, agent_name, hub_scope_mode, default_permissions)
		VALUES ($1, $2, 'memax-devseed', $3, 'all_accessible', '{memory:read,memory:write}') ON CONFLICT DO NOTHING`,
		grant, user, agent); err != nil {
		return uuid.Nil, fmt.Errorf("devseed: grant %s: %w", name, err)
	}
	return grant, nil
}
