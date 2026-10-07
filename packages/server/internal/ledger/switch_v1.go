package ledger

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// What the switch reads of V1 (plan 25 §10). Like ResolveUserScope and the
// V1 credential backfill, these reads run as the login role, before any
// ledger transaction: V1's tables are the switch's input, never the V2
// record, and the switch never writes them (only hubs.v2_enabled_at, the
// switch itself, and a team hub's kind when it becomes a project space).

// v1SystemUser is V1's system actor, which owns the onboarding seeds.
var v1SystemUser = uuid.MustParse("00000000-0000-0000-0000-00000000ada7")

// v1Memory is a V1 memory as the switch classifies it. Content is read
// only when it is short enough to be one statement (the candidates, which
// are also scanned for credentials); longer notes go to Dream whole.
type v1Memory struct {
	ID          uuid.UUID
	OwnerID     uuid.UUID
	Title       string
	Content     string
	Length      int
	ContentType string
	Source      string
	SourcePath  string
	State       string
	Kind        string
	ByType      string
	BySlug      string
	SourceAgent string
	CreatedVia  string
	Initiation  string
	CreatedAt   time.Time
	Attachments int
	Repo        string
}

// readV1Memories reads a hub's V1 memories, oldest first, leaving out the
// onboarding seeds (no one's notes: the disposition research says to
// exclude them) and anything already numbered as a note of this space.
func readV1Memories(ctx context.Context, db Querier, hubID uuid.UUID, skip map[uuid.UUID]bool) ([]v1Memory, error) {
	rows, err := db.Query(ctx, `
		SELECT m.id, m.owner_id, m.title,
		       CASE WHEN char_length(m.content) <= $3 THEN m.content ELSE '' END, char_length(m.content),
		       m.content_type, m.source, m.source_path, m.state, m.kind,
		       m.created_by_type, m.created_by_slug, m.source_agent, m.created_via, m.initiation_type, m.created_at,
		       (SELECT count(*) FROM public.memory_attachments a WHERE a.memory_id = m.id),
		       COALESCE(m.project_context ->> 'repo', '')
		  FROM public.memories m
		 WHERE m.hub_id = $1 AND m.source_kind IS DISTINCT FROM 'onboarding-seed' AND m.owner_id <> $2
		 ORDER BY m.created_at, m.id`, hubID, v1SystemUser, MaxStatementRunes)
	if err != nil {
		return nil, fmt.Errorf("ledger: read V1 memories: %w", err)
	}
	all, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (v1Memory, error) {
		var m v1Memory
		err := r.Scan(&m.ID, &m.OwnerID, &m.Title, &m.Content, &m.Length, &m.ContentType, &m.Source, &m.SourcePath,
			&m.State, &m.Kind, &m.ByType, &m.BySlug, &m.SourceAgent, &m.CreatedVia, &m.Initiation, &m.CreatedAt,
			&m.Attachments, &m.Repo)
		return m, err
	})
	if err != nil {
		return nil, fmt.Errorf("ledger: read V1 memories: %w", err)
	}
	if len(skip) == 0 {
		return all, nil
	}
	out := all[:0]
	for _, m := range all {
		if !skip[m.ID] {
			out = append(out, m)
		}
	}
	return out, nil
}

// classifiedNote is a V1 memory as a note, with the statement a candidate
// offers for bulk keep.
type classifiedNote struct {
	noteRow
	Statement string
	Section   Section
}

// classifyV1 decides what a V1 memory becomes (plan 25 §10, D13):
//
//   - who wrote it: an agent when V1 recorded one (created_by, the source
//     agent, an agent's surface: hooks, extraction, chat), else the person;
//   - its trust: what an agent wrote is its own work, a person's words are
//     theirs, and a web page or an email is external;
//   - a person's own short V1 memory, active and text, is a bulk-keep
//     candidate; a longer one, a file or a page is folded by Dream; one
//     archived in V1, or holding a credential, stays a note.
func classifyV1(m v1Memory, personal bool) classifiedNote {
	agent := firstNonEmptyStr(m.BySlug, m.SourceAgent)
	c := classifiedNote{noteRow: noteRow{ID: m.ID, Origin: NoteFromMemory, V1ID: m.ID, AuthorID: m.OwnerID}}
	switch {
	case m.ByType == "agent", agent != "", slices.Contains([]string{"hook", "extraction", "chat"}, m.CreatedVia),
		m.Initiation == "agent_proactive", m.Initiation == "agent_automatic":
		c.AuthorKind = "agent"
		c.Agent = truncateRunes(strings.TrimSpace(agent), MaxAgentSlug)
	default:
		c.AuthorKind = "person"
	}
	external := v1External(m)
	switch {
	case external:
		c.Trust = policy.TrustExternal
	case c.AuthorKind == "person":
		c.Trust = policy.TrustPerson
	default:
		c.Trust = policy.TrustAgentOwnWork
	}
	statement := strings.TrimSpace(m.Content)
	if statement == "" && m.Length == 0 {
		statement = strings.TrimSpace(m.Title)
	}
	short := m.Length <= MaxStatementRunes && utf8.RuneCountInString(statement) <= MaxStatementRunes
	switch {
	case m.State == "archived":
		c.Disposition, c.Hold = NoteOnly, HoldArchived
	case short && len(findSecrets(m.Title, statement)) > 0:
		c.Disposition, c.Hold = NoteOnly, HoldSecret
	case c.AuthorKind == "agent":
		c.Disposition = NoteFold
	case external:
		c.Disposition, c.Hold = NoteFold, HoldExternal
	case !v1Text(m) || m.Attachments > 0:
		c.Disposition, c.Hold = NoteFold, HoldFormat
	case !short:
		c.Disposition, c.Hold = NoteFold, HoldLong
	case statement == "" || m.State != "active":
		c.Disposition, c.Hold = NoteOnly, HoldFormat
	default:
		c.Disposition, c.Statement, c.Section = NoteCandidate, statement, v1Section(m.Kind, personal)
	}
	return c
}

// v1External reports whether a V1 memory holds third-party content: a page
// fetched from the web, a link, an email.
func v1External(m v1Memory) bool {
	switch strings.ToLower(m.Source) {
	case "url", "link", "email", "web_clip", "web":
		return true
	}
	switch strings.ToLower(m.ContentType) {
	case "html", "url", "link", "text/html":
		return true
	}
	p := strings.ToLower(m.SourcePath)
	return strings.HasPrefix(p, "http://") || strings.HasPrefix(p, "https://")
}

// v1Text reports whether a V1 memory is text a person typed or pasted.
func v1Text(m v1Memory) bool {
	switch strings.ToLower(m.ContentType) {
	case "", "text", "markdown", "text/plain", "text/markdown", "md":
		return true
	}
	return false
}

// v1Section maps V1's memory kind onto a V2 section: a rationale is a
// decision's (the statement stays a fact: only a person makes a decision);
// a personal space's words are preferences; everything else a convention.
func v1Section(kind string, personal bool) Section {
	switch {
	case kind == "rationale":
		return SectionDecisions
	case kind == "procedural":
		return SectionConventions
	case personal:
		return SectionPreferences
	}
	return SectionConventions
}

func firstNonEmptyStr(vs ...string) string {
	for _, v := range vs {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// v1Persona is a V1 persona (personal spaces only).
type v1Persona struct {
	ID          uuid.UUID
	OwnerID     uuid.UUID
	SourceAgent string
	Path        string
	Name        string
	Secrets     bool
}

func readV1Personas(ctx context.Context, db Querier, owner uuid.UUID) ([]v1Persona, error) {
	rows, err := db.Query(ctx, `
		SELECT id, owner_id, source_agent, source_file_path, name, content
		  FROM public.personas WHERE owner_id = $1 ORDER BY created_at, id`, owner)
	if err != nil {
		return nil, fmt.Errorf("ledger: read V1 personas: %w", err)
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (v1Persona, error) {
		var p v1Persona
		var content string
		err := r.Scan(&p.ID, &p.OwnerID, &p.SourceAgent, &p.Path, &p.Name, &content)
		p.Secrets = len(findSecrets(p.Name, content)) > 0
		return p, err
	})
}

// personaNote is a persona as a note: its identity is an agent's, for
// Dream to fold into proposals in the personal space.
func personaNote(p v1Persona) noteRow {
	r := noteRow{ID: noteID(NoteFromPersona, p.ID), Origin: NoteFromPersona, V1ID: p.ID, AuthorKind: "agent",
		AuthorID: p.OwnerID, Agent: truncateRunes(strings.TrimSpace(p.SourceAgent), MaxAgentSlug),
		Trust: policy.TrustAgentOwnWork, Disposition: NoteFold}
	if p.Secrets {
		r.Disposition, r.Hold = NoteOnly, HoldSecret
	}
	return r
}

// v1Config is a V1 agent config (a synced agent file).
type v1Config struct {
	ID      uuid.UUID
	OwnerID uuid.UUID
	Agent   string
	Path    string
	Scope   string
	Secrets bool
}

// readV1Configs reads the owner's synced agent files that belong to the
// space: the global and profile ones in a personal space, a repository's
// in the project (or team) space compiling for it.
func readV1Configs(ctx context.Context, db Querier, owner uuid.UUID, kind policy.SpaceKind, repository string) ([]v1Config, error) {
	rows, err := db.Query(ctx, `
		SELECT id, owner_id, agent, file_path, scope, content
		  FROM public.agent_configs WHERE owner_id = $1 ORDER BY created_at, id`, owner)
	if err != nil {
		return nil, fmt.Errorf("ledger: read V1 agent configs: %w", err)
	}
	all, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (v1Config, error) {
		var c v1Config
		var content string
		err := r.Scan(&c.ID, &c.OwnerID, &c.Agent, &c.Path, &c.Scope, &content)
		c.Secrets = len(findSecrets(content)) > 0
		return c, err
	})
	if err != nil {
		return nil, fmt.Errorf("ledger: read V1 agent configs: %w", err)
	}
	var out []v1Config
	for _, c := range all {
		if ConfigBelongs(c.Scope, kind, repository) {
			out = append(out, c)
		}
	}
	return out, nil
}

// ConfigBelongs reports whether a V1 agent config of this scope ("global",
// "profile:<name>", "project:<url>") belongs to a space: global and profile
// files to the personal space, a project's files to the space compiling
// for its repository. V1's two-way config sync stops for a file once the
// space it belongs to is on V2.
func ConfigBelongs(scope string, kind policy.SpaceKind, repository string) bool {
	switch {
	case scope == "global" || strings.HasPrefix(scope, "profile:"):
		return kind == policy.SpacePersonal
	case strings.HasPrefix(scope, "project:"):
		return kind != policy.SpacePersonal && repository != "" &&
			RepoKey(strings.TrimPrefix(scope, "project:")) == RepoKey(repository)
	}
	return false
}

// RepoKey is "owner/name" for any spelling of a repository's URL
// (https://github.com/Acme/Web.git, git@github.com:acme/web, acme/web).
func RepoKey(url string) string {
	s := strings.ToLower(strings.TrimSpace(url))
	s = strings.TrimSuffix(strings.TrimSuffix(s, "/"), ".git")
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	if i := strings.Index(s, "@"); i >= 0 && !strings.Contains(s[:i], "/") {
		s = s[i+1:]
	}
	s = strings.ReplaceAll(s, ":", "/")
	parts := strings.FieldsFunc(s, func(r rune) bool { return r == '/' })
	if len(parts) < 2 {
		return strings.Join(parts, "/")
	}
	return parts[len(parts)-2] + "/" + parts[len(parts)-1]
}

// configNote is an agent config as a note: the person's file (a
// repository's at the repository's trust), for Dream to fold. A file
// holding a credential stays a note.
func configNote(c v1Config) noteRow {
	trust := policy.TrustPerson
	if strings.HasPrefix(c.Scope, "project:") {
		trust = policy.TrustRepository
	}
	r := noteRow{ID: noteID(NoteFromAgentConfig, c.ID), Origin: NoteFromAgentConfig, V1ID: c.ID, AuthorKind: "person",
		AuthorID: c.OwnerID, Agent: truncateRunes(strings.TrimSpace(c.Agent), MaxAgentSlug), Trust: trust,
		Disposition: NoteFold}
	if c.Secrets {
		r.Disposition, r.Hold = NoteOnly, HoldSecret
	}
	return r
}

// ConfigTargets are the compile targets a V1 agent file stands for (D2:
// AGENTS.md is canonical, CLAUDE.md a shim importing it, Cursor rules for
// scoped facts).
func ConfigTargets(filePath string) []TargetKind {
	p := strings.ToLower(strings.TrimPrefix(strings.ReplaceAll(filePath, "\\", "/"), "./"))
	base := path.Base(p)
	switch {
	case base == "claude.md" || base == "claude.local.md":
		return []TargetKind{TargetAgentsMD, TargetClaudeMD}
	case base == "agents.md":
		return []TargetKind{TargetAgentsMD}
	case base == "gemini.md":
		return []TargetKind{TargetAgentsMD, TargetGeminiMD}
	case base == ".cursorrules" || strings.Contains(p, ".cursor/rules"):
		return []TargetKind{TargetAgentsMD, TargetCursorMDC}
	case strings.HasSuffix(p, "copilot-instructions.md") || strings.Contains(p, ".github/instructions"):
		return []TargetKind{TargetAgentsMD, TargetCopilot}
	case base == ".windsurfrules" || strings.Contains(p, ".windsurf/rules") || strings.Contains(p, ".devin/rules"):
		return []TargetKind{TargetAgentsMD, TargetWindsurf}
	case strings.Contains(p, ".claude/rules"):
		return []TargetKind{TargetAgentsMD, TargetClaudeRules}
	}
	return nil
}

// v1Member is a person in a V1 hub, with their role mapped onto V2's.
type v1Member struct {
	PersonID  uuid.UUID
	Name      string
	V1Role    string
	Role      policy.Role
	CanForget bool
}

func readV1Members(ctx context.Context, db Querier, hubID uuid.UUID) ([]v1Member, error) {
	rows, err := db.Query(ctx, `
		SELECT u.id, COALESCE(NULLIF(u.display_name, ''), NULLIF(u.name, ''), ''),
		       CASE WHEN h.owner_id = u.id THEN 'owner' ELSE COALESCE(m.role, 'viewer') END
		  FROM public.hubs h
		  JOIN public.users u ON u.id = h.owner_id OR u.id IN (SELECT user_id FROM public.hub_members WHERE hub_id = h.id)
		  LEFT JOIN public.hub_members m ON m.hub_id = h.id AND m.user_id = u.id
		 WHERE h.id = $1
		 ORDER BY (h.owner_id = u.id) DESC, m.joined_at, u.id`, hubID)
	if err != nil {
		return nil, fmt.Errorf("ledger: read V1 members: %w", err)
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (v1Member, error) {
		var m v1Member
		err := r.Scan(&m.PersonID, &m.Name, &m.V1Role)
		m.Role, m.CanForget = policy.RoleFromV1(m.V1Role)
		return m, err
	})
}

// v1Gate is a decision gate waiting on V1's board (before epic 1.11 moved
// gates to the V2 record).
type v1Gate struct {
	SlotID   uuid.UUID
	SlotKey  string
	Question string
	Context  string
	Options  []string
	Agent    string
}

func readV1Gates(ctx context.Context, db Querier, hubID uuid.UUID) ([]v1Gate, error) {
	rows, err := db.Query(ctx, `
		SELECT s.id, s.slot_key, s.payload
		  FROM public.board_slots s JOIN public.boards b ON b.id = s.board_id
		 WHERE b.hub_id = $1 AND s.kind = 'decision_gate' AND s.state IN ('fresh', 'seen')
		 ORDER BY s.created_at, s.id`, hubID)
	if err != nil {
		return nil, fmt.Errorf("ledger: read V1 gates: %w", err)
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (v1Gate, error) {
		var g v1Gate
		var payload []byte
		if err := r.Scan(&g.SlotID, &g.SlotKey, &payload); err != nil {
			return g, err
		}
		var p struct {
			Question string `json:"question"`
			Context  string `json:"context"`
			Options  []struct {
				Label string `json:"label"`
			} `json:"options"`
			SourceAgent string `json:"source_agent"`
		}
		_ = json.Unmarshal(payload, &p)
		g.Question, g.Context, g.Agent = strings.TrimSpace(p.Question), strings.TrimSpace(p.Context), strings.TrimSpace(p.SourceAgent)
		for _, o := range p.Options {
			if l := strings.TrimSpace(o.Label); l != "" {
				g.Options = append(g.Options, l)
			}
		}
		return g, nil
	})
}

// v1HubFacts are the rest of what the preview says about a V1 hub.
type v1HubFacts struct {
	Plan      string
	DreamRuns int
	Agents    int // V1's connected agents of the owner (the Agents page)
	Repo      string
}

func readV1HubFacts(ctx context.Context, db Querier, hubID uuid.UUID) (v1HubFacts, error) {
	var f v1HubFacts
	rows, err := db.Query(ctx, `
		SELECT COALESCE(NULLIF(h.plan, ''), NULLIF(u.plan, ''), u.personal_plan_id, ''),
		       (SELECT count(*) FROM public.dream_runs d WHERE d.hub_id = h.id),
		       (SELECT count(*) FROM public.connected_agents a WHERE a.owner_id = h.owner_id AND a.status = 'active'),
		       COALESCE((SELECT m.project_context ->> 'repo' FROM public.memories m
		                  WHERE m.hub_id = h.id AND COALESCE(m.project_context ->> 'repo', '') <> ''
		                  GROUP BY 1 ORDER BY count(*) DESC, 1 LIMIT 1), '')
		  FROM public.hubs h JOIN public.users u ON u.id = h.owner_id
		 WHERE h.id = $1`, hubID)
	if err != nil {
		return f, fmt.Errorf("ledger: read V1 hub: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		if err := rows.Scan(&f.Plan, &f.DreamRuns, &f.Agents, &f.Repo); err != nil {
			return f, fmt.Errorf("ledger: read V1 hub: %w", err)
		}
	}
	return f, rows.Err()
}
