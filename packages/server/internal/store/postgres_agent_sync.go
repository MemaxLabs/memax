package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// --- Agent Configs ---

func (s *PostgresStore) UpsertAgentConfig(config *model.AgentConfig) error {
	ctx := context.Background()
	_, err := s.pool.Exec(ctx,
		`INSERT INTO agent_configs (id, owner_id, agent, file_path, scope, content, content_hash, version, created_at, updated_at)
		VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (owner_id, agent, file_path, scope)
		DO UPDATE SET content = $6, content_hash = $7, version = agent_configs.version + 1, updated_at = $10`,
		config.ID, config.OwnerID, config.Agent, config.FilePath, config.Scope,
		config.Content, config.ContentHash, config.Version, config.CreatedAt, config.UpdatedAt,
	)
	return err
}

func (s *PostgresStore) GetAgentConfig(id string, ownerID string) (*model.AgentConfig, error) {
	ctx := context.Background()
	row := s.pool.QueryRow(ctx,
		`SELECT id, owner_id, agent, file_path, scope, content, content_hash, version, created_at, updated_at
		FROM agent_configs WHERE id = $1::uuid AND owner_id = $2::uuid`, id, ownerID)
	return scanAgentConfig(row)
}

func (s *PostgresStore) GetAgentConfigByPath(agent, filePath, scope, ownerID string) (*model.AgentConfig, error) {
	ctx := context.Background()
	row := s.pool.QueryRow(ctx,
		`SELECT id, owner_id, agent, file_path, scope, content, content_hash, version, created_at, updated_at
		FROM agent_configs WHERE agent = $1 AND file_path = $2 AND scope = $3 AND owner_id = $4::uuid`,
		agent, filePath, scope, ownerID)
	return scanAgentConfig(row)
}

func (s *PostgresStore) ListAgentConfigs(ownerID string) ([]model.AgentConfig, error) {
	ctx := context.Background()
	rows, err := s.pool.Query(ctx,
		`SELECT id, owner_id, agent, file_path, scope, content, content_hash, version, created_at, updated_at
		FROM agent_configs WHERE owner_id = $1::uuid ORDER BY agent, file_path`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var configs []model.AgentConfig
	for rows.Next() {
		var c model.AgentConfig
		if err := rows.Scan(&c.ID, &c.OwnerID, &c.Agent, &c.FilePath, &c.Scope,
			&c.Content, &c.ContentHash, &c.Version, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		configs = append(configs, c)
	}
	return configs, rows.Err()
}

func (s *PostgresStore) DeleteAgentConfig(id string, ownerID string) error {
	ctx := context.Background()
	_, err := s.pool.Exec(ctx,
		`DELETE FROM agent_configs WHERE id = $1::uuid AND owner_id = $2::uuid`, id, ownerID)
	return err
}

func (s *PostgresStore) ListAgentConfigSyncStates(ownerID string, deviceID string) ([]model.AgentConfigSyncState, error) {
	ctx := context.Background()
	rows, err := s.pool.Query(ctx,
		`SELECT owner_id, device_id, agent, file_path, scope, COALESCE(local_path, ''), last_seen_version, last_seen_hash, suppressed, updated_at
		FROM agent_config_sync_states
		WHERE owner_id = $1::uuid AND device_id = $2
		ORDER BY agent, scope, file_path`, ownerID, deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var states []model.AgentConfigSyncState
	for rows.Next() {
		var state model.AgentConfigSyncState
		if err := rows.Scan(&state.OwnerID, &state.DeviceID, &state.Agent, &state.FilePath, &state.Scope,
			&state.LocalPath, &state.LastSeenVersion, &state.LastSeenHash, &state.Suppressed, &state.UpdatedAt); err != nil {
			return nil, err
		}
		states = append(states, state)
	}
	return states, rows.Err()
}

func (s *PostgresStore) UpsertAgentConfigSyncState(state *model.AgentConfigSyncState) error {
	ctx := context.Background()
	_, err := s.pool.Exec(ctx,
		`INSERT INTO agent_config_sync_states (owner_id, device_id, agent, file_path, scope, local_path, last_seen_version, last_seen_hash, suppressed, updated_at)
		VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (owner_id, device_id, agent, file_path, scope)
		DO UPDATE SET local_path = EXCLUDED.local_path,
			last_seen_version = EXCLUDED.last_seen_version,
			last_seen_hash = EXCLUDED.last_seen_hash,
			suppressed = EXCLUDED.suppressed,
			updated_at = EXCLUDED.updated_at`,
		state.OwnerID, state.DeviceID, state.Agent, state.FilePath, state.Scope,
		nullIfEmpty(state.LocalPath), state.LastSeenVersion, state.LastSeenHash, state.Suppressed, state.UpdatedAt)
	return err
}

func (s *PostgresStore) ListAgentConfigTombstones(ownerID string) ([]model.AgentConfigTombstone, error) {
	ctx := context.Background()
	rows, err := s.pool.Query(ctx,
		`SELECT owner_id, agent, file_path, scope, version, deleted_at, COALESCE(deleted_content, ''), COALESCE(deleted_content_hash, ''), content_expires_at
		FROM agent_config_tombstones
		WHERE owner_id = $1::uuid
		ORDER BY agent, scope, file_path`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tombstones []model.AgentConfigTombstone
	for rows.Next() {
		var tombstone model.AgentConfigTombstone
		if err := rows.Scan(&tombstone.OwnerID, &tombstone.Agent, &tombstone.FilePath, &tombstone.Scope,
			&tombstone.Version, &tombstone.DeletedAt, &tombstone.DeletedContent, &tombstone.DeletedContentHash, &tombstone.ContentExpiresAt); err != nil {
			return nil, err
		}
		tombstones = append(tombstones, tombstone)
	}
	return tombstones, rows.Err()
}

func (s *PostgresStore) GetAgentConfigTombstone(agent, filePath, scope, ownerID string) (*model.AgentConfigTombstone, error) {
	ctx := context.Background()
	row := s.pool.QueryRow(ctx,
		`SELECT owner_id, agent, file_path, scope, version, deleted_at, COALESCE(deleted_content, ''), COALESCE(deleted_content_hash, ''), content_expires_at
		FROM agent_config_tombstones
		WHERE owner_id = $1::uuid AND agent = $2 AND file_path = $3 AND scope = $4`,
		ownerID, agent, filePath, scope)
	var tombstone model.AgentConfigTombstone
	if err := row.Scan(&tombstone.OwnerID, &tombstone.Agent, &tombstone.FilePath, &tombstone.Scope,
		&tombstone.Version, &tombstone.DeletedAt, &tombstone.DeletedContent, &tombstone.DeletedContentHash, &tombstone.ContentExpiresAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("agent config tombstone not found: %s/%s/%s", agent, filePath, scope)
		}
		return nil, err
	}
	return &tombstone, nil
}

func (s *PostgresStore) CreateAgentConfigTombstone(tombstone *model.AgentConfigTombstone) error {
	ctx := context.Background()
	_, err := s.pool.Exec(ctx,
		`INSERT INTO agent_config_tombstones (owner_id, agent, file_path, scope, version, deleted_at, deleted_content, deleted_content_hash, content_expires_at)
		VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (owner_id, agent, file_path, scope)
		DO UPDATE SET version = EXCLUDED.version, deleted_at = EXCLUDED.deleted_at,
			deleted_content = EXCLUDED.deleted_content,
			deleted_content_hash = EXCLUDED.deleted_content_hash,
			content_expires_at = EXCLUDED.content_expires_at`,
		tombstone.OwnerID, tombstone.Agent, tombstone.FilePath, tombstone.Scope, tombstone.Version, tombstone.DeletedAt,
		nullIfEmpty(tombstone.DeletedContent), nullIfEmpty(tombstone.DeletedContentHash), tombstone.ContentExpiresAt)
	return err
}

func (s *PostgresStore) DeleteAgentConfigTombstone(agent, filePath, scope, ownerID string) error {
	ctx := context.Background()
	_, err := s.pool.Exec(ctx,
		`DELETE FROM agent_config_tombstones WHERE owner_id = $1::uuid AND agent = $2 AND file_path = $3 AND scope = $4`,
		ownerID, agent, filePath, scope)
	return err
}

func (s *PostgresStore) PurgeExpiredAgentConfigTombstoneContent(now time.Time) error {
	ctx := context.Background()
	_, err := s.pool.Exec(ctx,
		`UPDATE agent_config_tombstones
		SET deleted_content = NULL,
			deleted_content_hash = NULL,
			content_expires_at = NULL
		WHERE content_expires_at IS NOT NULL
		  AND content_expires_at <= $1
		  AND deleted_content IS NOT NULL`,
		now)
	return err
}

func (s *PostgresStore) CountExtractedMemories(ownerID string) (map[string]int, error) {
	ctx := context.Background()
	rows, err := s.pool.Query(ctx,
		`SELECT REPLACE(source_path, 'config:', ''), COUNT(*)
		FROM memories
		WHERE owner_id = $1::uuid AND source_path LIKE 'config:%' AND state = 'active'
		GROUP BY source_path`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := make(map[string]int)
	for rows.Next() {
		var configID string
		var count int
		if err := rows.Scan(&configID, &count); err != nil {
			return nil, err
		}
		counts[configID] = count
	}
	return counts, rows.Err()
}

func scanAgentConfig(row interface{ Scan(dest ...any) error }) (*model.AgentConfig, error) {
	var c model.AgentConfig
	err := row.Scan(&c.ID, &c.OwnerID, &c.Agent, &c.FilePath, &c.Scope,
		&c.Content, &c.ContentHash, &c.Version, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func scanMemoryAttachment(row interface{ Scan(dest ...any) error }) (*model.MemoryAttachment, error) {
	var a model.MemoryAttachment
	err := row.Scan(
		&a.ID, &a.MemoryID, &a.OwnerID, &a.Kind, &a.Filename,
		&a.ContentType, &a.SizeBytes, &a.SHA256, &a.StorageKey, &a.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func nullIfEmpty(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

// ── Connected Agents ────────────────────────────────────────────────

func (s *PostgresStore) UpsertConnectedAgent(agent *model.ConnectedAgent) error {
	_, err := s.pool.Exec(context.Background(), `
		INSERT INTO connected_agents (owner_id, agent_name, display_name, icon)
		VALUES ($1::uuid, $2, $3, $4)
		ON CONFLICT (owner_id, agent_name) DO UPDATE SET
			display_name = CASE WHEN connected_agents.display_name = '' THEN EXCLUDED.display_name ELSE connected_agents.display_name END,
			icon = CASE WHEN connected_agents.icon = '' THEN EXCLUDED.icon ELSE connected_agents.icon END,
			updated_at = now()`,
		agent.OwnerID, agent.AgentName, agent.DisplayName, agent.Icon)
	return err
}

func (s *PostgresStore) GetConnectedAgent(ownerID string, agentName string) (*model.ConnectedAgent, error) {
	// Memax is the always-on platform agent — synthesized for every
	// user, never stored in connected_agents. Return the same shape
	// the list path produces so the agent detail route + any other
	// per-slug lookup stay consistent.
	if agentName == memaxBuiltInAgentSlug {
		now := time.Now().UTC()
		return &model.ConnectedAgent{
			ID:          "memax-builtin-" + ownerID,
			OwnerID:     ownerID,
			AgentName:   memaxBuiltInAgentSlug,
			DisplayName: "",
			Icon:        "",
			Status:      "active",
			CreatedAt:   now,
			UpdatedAt:   now,
		}, nil
	}
	var a model.ConnectedAgent
	err := s.pool.QueryRow(context.Background(), `
		SELECT id, owner_id, agent_name, display_name, icon, status, created_at, updated_at
		FROM connected_agents WHERE owner_id = $1::uuid AND agent_name = $2`,
		ownerID, agentName).Scan(
		&a.ID, &a.OwnerID, &a.AgentName, &a.DisplayName, &a.Icon, &a.Status, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// memaxBuiltInAgentSlug is the reserved agent_name for the platform's
// always-on actor. It is never inserted into `connected_agents`
// (Memax has no API key, no OAuth grant, no per-user row). The List
// and Get paths synthesize it from `memories` whose `created_by_slug`
// matches this slug — keeping a single source of truth: every
// consumer of the agents API sees Memax as a regular connected agent
// without the frontend having to inject parallel UI.
const memaxBuiltInAgentSlug = "memax"

// listMemaxBuiltInAgent synthesizes the always-on Memax agent for
// `ownerID`. Stats (memory_count, last_active_at, etc.) are computed
// from `memories` joined on the reserved slug; if the user has no
// system-pushed memories yet, the agent still appears with zero
// stats so the agents grid + settings list stay consistent across
// every signup phase.
func (s *PostgresStore) listMemaxBuiltInAgent(ctx context.Context, ownerID string) (*model.ConnectedAgentWithStats, error) {
	var memoryCount int
	var lastActive *time.Time
	if err := s.pool.QueryRow(ctx, `
		SELECT
			COUNT(*),
			MAX(created_at)
		FROM memories
		WHERE owner_id = $1::uuid
		  AND COALESCE(NULLIF(created_by_slug, ''), NULLIF(source_agent, '')) = $2
		  AND state != 'archived'`,
		ownerID, memaxBuiltInAgentSlug).Scan(&memoryCount, &lastActive); err != nil {
		return nil, fmt.Errorf("count memax memories for %s: %w", ownerID, err)
	}
	// CreatedAt is a stable anchor so the row's ordering doesn't
	// jitter across requests. We pick the epoch zero so Memax sorts
	// before any user-connected agent in the same UTC reading; the
	// handler/UI doesn't depend on this beyond consistency.
	now := time.Now().UTC()
	return &model.ConnectedAgentWithStats{
		ConnectedAgent: model.ConnectedAgent{
			ID:        "memax-builtin-" + ownerID,
			OwnerID:   ownerID,
			AgentName: memaxBuiltInAgentSlug,
			// Empty DisplayName / Icon — frontend resolves the
			// branded identity via AGENT_IDENTITIES.memax (signature
			// pink Bot). Setting display_name="Memax" here would race
			// the frontend's identity lookup on rename flows; leaving
			// it empty makes the frontend the single source of truth
			// for the visible label.
			DisplayName: "",
			Icon:        "",
			Status:      "active",
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		KeyCount:       0,
		ConfigCount:    0,
		MemoryCount:    memoryCount,
		NeedsReconnect: false,
		LastActiveAt:   lastActive,
	}, nil
}

func (s *PostgresStore) ListConnectedAgentsWithStats(ownerID string) ([]model.ConnectedAgentWithStats, error) {
	rows, err := s.pool.Query(context.Background(), `
		SELECT ca.id, ca.owner_id, ca.agent_name, ca.display_name, ca.icon, ca.status,
		       ca.created_at, ca.updated_at,
		       COALESCE(k.key_count, 0),
		       COALESCE(c.config_count, 0),
		       COALESCE(m.memory_count, 0),
		       COALESCE(g.needs_reconnect, FALSE),
		       -- last_active_at = GREATEST of real activity sources only.
		       -- Previously this also folded in ca.updated_at, but that is
		       -- bumped by EnsureConnectedAgent on config/registration
		       -- events (PATCHing agent_name on a key, pushing a config,
		       -- etc.) and was falsely labelling freshly-configured
		       -- agents as "Active just now". Activity should come from
		       -- grant last_used or a real usage_event. NULL when the
		       -- agent has never authenticated or produced an event --
		       -- agent-status.ts already renders that as "configured".
		       GREATEST(k.last_active_at, a.created_at),
		       a.created_at,
		       a.operation,
		       a.summary
		FROM connected_agents ca
		LEFT JOIN (
			SELECT agent_name, user_id, COUNT(*) AS key_count, MAX(last_active_at) AS last_active_at
			FROM (
				SELECT agent_name, user_id, last_used AS last_active_at
				FROM api_keys
				WHERE user_id = $1::uuid AND agent_name != ''
				UNION ALL
				SELECT agent_name, user_id, last_used AS last_active_at
				FROM oauth_grants
				WHERE user_id = $1::uuid AND agent_name != '' AND revoked_at IS NULL
			) grants
			GROUP BY agent_name, user_id
		) k ON k.agent_name = ca.agent_name AND k.user_id = ca.owner_id
		LEFT JOIN (
			SELECT agent, owner_id, COUNT(*) AS config_count
			FROM agent_configs WHERE owner_id = $1::uuid
			GROUP BY agent, owner_id
		) c ON c.agent = ca.agent_name AND c.owner_id = ca.owner_id
		LEFT JOIN (
			SELECT COALESCE(NULLIF(created_by_slug, ''), NULLIF(source_agent, '')) AS agent_slug, owner_id, COUNT(*) AS memory_count
			FROM memories
			WHERE owner_id = $1::uuid
			  AND COALESCE(NULLIF(created_by_slug, ''), NULLIF(source_agent, '')) IS NOT NULL
			  AND state != 'archived'
			GROUP BY agent_slug, owner_id
		) m ON m.agent_slug = ca.agent_name AND m.owner_id = ca.owner_id
		LEFT JOIN (
			SELECT agent_name, user_id, BOOL_OR(agent_name = '' OR agent_name = 'unknown') AS needs_reconnect
			FROM oauth_grants
			WHERE user_id = $1::uuid AND revoked_at IS NULL
			GROUP BY agent_name, user_id
		) g ON g.agent_name = ca.agent_name AND g.user_id = ca.owner_id
		LEFT JOIN LATERAL (
			SELECT ue.created_at, ue.operation, NULLIF(COALESCE(ue.metadata->>'summary', ''), '') AS summary
			FROM usage_events ue
			WHERE ue.user_id = ca.owner_id
			  AND ue.agent_name = ca.agent_name
			ORDER BY ue.created_at DESC
			LIMIT 1
		) a ON TRUE
		WHERE ca.owner_id = $1::uuid
		ORDER BY COALESCE(a.created_at, k.last_active_at, ca.created_at) DESC`,
		ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var agents []model.ConnectedAgentWithStats
	for rows.Next() {
		var a model.ConnectedAgentWithStats
		var lastActive *time.Time
		var lastObserved *time.Time
		var lastOperation *string
		var lastSummary *string
		if err := rows.Scan(
			&a.ID, &a.OwnerID, &a.AgentName, &a.DisplayName, &a.Icon, &a.Status,
			&a.CreatedAt, &a.UpdatedAt,
			&a.KeyCount, &a.ConfigCount, &a.MemoryCount, &a.NeedsReconnect, &lastActive,
			&lastObserved, &lastOperation, &lastSummary,
		); err != nil {
			return nil, err
		}
		a.LastActiveAt = lastActive
		a.LastObservedAt = lastObserved
		a.LastOperation = lastOperation
		a.LastActivitySummary = lastSummary
		agents = append(agents, a)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Prepend the always-on Memax agent so every consumer of the
	// agents list (grid, settings, /agents/<slug> via Get) sees the
	// same shape. Memory_count / last_active_at come from the
	// memories table joined on the reserved slug — the user's
	// seed-memory pushes (and future Dreams summaries) accrue here
	// automatically. Failure to synthesize is non-fatal — log and
	// fall back to the original list so a transient query problem
	// doesn't blank the agents tab.
	memax, err := s.listMemaxBuiltInAgent(context.Background(), ownerID)
	if err != nil {
		// We deliberately don't fail the whole listing on a memax
		// synthesis error — the user's real agents are more
		// important than the platform marker. Surface for debugging
		// via slog at the caller's discretion; here we just return
		// the unsynthesized list.
		return agents, nil
	}
	return append([]model.ConnectedAgentWithStats{*memax}, agents...), nil
}

func (s *PostgresStore) UpdateConnectedAgent(agent *model.ConnectedAgent) error {
	_, err := s.pool.Exec(context.Background(), `
		UPDATE connected_agents SET display_name = $3, icon = $4, updated_at = now()
		WHERE owner_id = $1::uuid AND agent_name = $2`,
		agent.OwnerID, agent.AgentName, agent.DisplayName, agent.Icon)
	return err
}

// CountAgentApiKeys returns the number of active API keys scoped to the
// given agent. Used by the disconnect handler to report how many keys a
// cascade is about to revoke without trusting after-the-fact RowsAffected.
func (s *PostgresStore) CountAgentApiKeys(ownerID string, agentName string) (int, error) {
	var n int
	err := s.pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM api_keys WHERE user_id = $1::uuid AND agent_name = $2`,
		ownerID, agentName).Scan(&n)
	return n, err
}

// CountAgentConfigs returns the number of synced configs for the given
// agent. Used by the disconnect handler alongside CountAgentApiKeys to
// preview the cascade counts.
func (s *PostgresStore) CountAgentConfigs(ownerID string, agentName string) (int, error) {
	var n int
	err := s.pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM agent_configs WHERE owner_id = $1::uuid AND agent = $2`,
		ownerID, agentName).Scan(&n)
	return n, err
}

func (s *PostgresStore) DeleteConnectedAgent(ownerID string, agentName string) error {
	tx, err := s.pool.Begin(context.Background())
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())

	// Revoke all API keys and OAuth grants for this agent.
	_, err = tx.Exec(context.Background(),
		`DELETE FROM api_keys WHERE user_id = $1::uuid AND agent_name = $2`,
		ownerID, agentName)
	if err != nil {
		return fmt.Errorf("delete api_keys: %w", err)
	}
	_, err = tx.Exec(context.Background(),
		`UPDATE oauth_grants SET revoked_at = now(), updated_at = now()
		WHERE user_id = $1::uuid AND agent_name = $2 AND revoked_at IS NULL`,
		ownerID, agentName)
	if err != nil {
		return fmt.Errorf("revoke oauth_grants: %w", err)
	}

	// Create tombstones for configs (so other devices know to delete locally)
	_, err = tx.Exec(context.Background(), `
		INSERT INTO agent_config_tombstones (owner_id, agent, file_path, scope, version, deleted_at, deleted_content, deleted_content_hash, content_expires_at)
		SELECT owner_id, agent, file_path, scope, version + 1, now(), content, content_hash, now() + interval '30 days'
		FROM agent_configs WHERE owner_id = $1::uuid AND agent = $2`,
		ownerID, agentName)
	if err != nil {
		return fmt.Errorf("create config tombstones: %w", err)
	}

	// Delete configs
	_, err = tx.Exec(context.Background(),
		`DELETE FROM agent_configs WHERE owner_id = $1::uuid AND agent = $2`,
		ownerID, agentName)
	if err != nil {
		return fmt.Errorf("delete agent_configs: %w", err)
	}

	// Delete sync states
	_, err = tx.Exec(context.Background(),
		`DELETE FROM agent_config_sync_states WHERE owner_id = $1::uuid AND agent = $2`,
		ownerID, agentName)
	if err != nil {
		return fmt.Errorf("delete sync states: %w", err)
	}

	// Delete the connected agent itself
	_, err = tx.Exec(context.Background(),
		`DELETE FROM connected_agents WHERE owner_id = $1::uuid AND agent_name = $2`,
		ownerID, agentName)
	if err != nil {
		return fmt.Errorf("delete connected_agent: %w", err)
	}

	return tx.Commit(context.Background())
}
