package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

func (s *PostgresStore) GetUserByCanonicalEmail(email string) (*model.User, error) {
	canonical := strings.ToLower(strings.TrimSpace(email))
	if canonical == "" {
		return nil, fmt.Errorf("empty email")
	}
	var u model.User
	err := s.pool.QueryRow(context.Background(),
		`SELECT id, COALESCE(github_id, 0), email, name, COALESCE(display_name, name, ''),
		        avatar_url, COALESCE(plan, 'free'), personal_plan_id, created_at, updated_at
		 FROM users WHERE lower(btrim(email)) = $1`,
		canonical,
	).Scan(&u.ID, &u.GitHubID, &u.Email, &u.Name, &u.DisplayName,
		&u.AvatarURL, &u.Plan, &u.PersonalPlanID, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (s *PostgresStore) GetUserByAuthIdentity(provider, providerID string) (*model.User, error) {
	var u model.User
	err := s.pool.QueryRow(context.Background(),
		`SELECT u.id, COALESCE(u.github_id, 0), u.email, u.name, COALESCE(u.display_name, u.name, ''),
		        u.avatar_url, COALESCE(u.plan, 'free'), u.personal_plan_id, u.created_at, u.updated_at
		 FROM auth_identities ai
		 JOIN users u ON u.id = ai.user_id
		 WHERE ai.provider = $1 AND ai.provider_id = $2`,
		provider, providerID,
	).Scan(&u.ID, &u.GitHubID, &u.Email, &u.Name, &u.DisplayName,
		&u.AvatarURL, &u.Plan, &u.PersonalPlanID, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (s *PostgresStore) CreateAuthIdentity(userID, provider, providerID, email, name, avatar string) error {
	res, err := s.pool.Exec(context.Background(),
		`INSERT INTO auth_identities (user_id, provider, provider_id, provider_email, provider_name, provider_avatar)
		 VALUES ($1::uuid, $2, $3, $4, $5, $6)
		 ON CONFLICT (provider, provider_id) DO NOTHING`,
		userID, provider, providerID, email, name, avatar)
	if err != nil {
		return err
	}
	// ON CONFLICT DO NOTHING means RowsAffected=0 if the identity already exists.
	// Check if the existing row belongs to a different user (TOCTOU race).
	if res.RowsAffected() == 0 {
		var existingOwner string
		lookupErr := s.pool.QueryRow(context.Background(),
			`SELECT user_id FROM auth_identities WHERE provider = $1 AND provider_id = $2`,
			provider, providerID).Scan(&existingOwner)
		if lookupErr != nil {
			// Lookup failed — we can't confirm ownership, so don't report success
			return fmt.Errorf("identity conflict check failed: %w", lookupErr)
		}
		if existingOwner != userID {
			return ErrIdentityConflict
		}
		// Same user already owns it — idempotent success
	}
	return nil
}

func (s *PostgresStore) ListAuthIdentities(userID string) ([]model.AuthIdentity, error) {
	rows, err := s.pool.Query(context.Background(),
		`SELECT id, user_id, provider, provider_id, provider_email, provider_name, created_at
		 FROM auth_identities WHERE user_id = $1::uuid ORDER BY created_at`,
		userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var identities []model.AuthIdentity
	for rows.Next() {
		var ai model.AuthIdentity
		if err := rows.Scan(&ai.ID, &ai.UserID, &ai.Provider, &ai.ProviderID,
			&ai.ProviderEmail, &ai.ProviderName, &ai.CreatedAt); err != nil {
			return nil, err
		}
		identities = append(identities, ai)
	}
	return identities, rows.Err()
}

func (s *PostgresStore) DeleteAuthIdentity(userID, provider string) error {
	_, err := s.pool.Exec(context.Background(),
		`DELETE FROM auth_identities WHERE user_id = $1::uuid AND provider = $2`,
		userID, provider)
	return err
}

func (s *PostgresStore) CreateOAuthState(ctx context.Context, state model.OAuthState) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO oauth_states (state_hash, provider, flow, user_id, client_redirect, expires_at)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		state.StateHash, state.Provider, state.Flow, state.UserID,
		state.ClientRedirect, state.ExpiresAt)
	return err
}

func (s *PostgresStore) ConsumeOAuthState(ctx context.Context, stateHash string) (*model.OAuthState, error) {
	var state model.OAuthState
	err := s.pool.QueryRow(ctx,
		`UPDATE oauth_states
		 SET consumed_at = now()
		 WHERE state_hash = $1 AND consumed_at IS NULL AND expires_at > now()
		 RETURNING provider, flow, user_id, client_redirect`,
		stateHash,
	).Scan(&state.Provider, &state.Flow, &state.UserID, &state.ClientRedirect)
	if err != nil {
		return nil, fmt.Errorf("invalid or expired OAuth state")
	}
	state.StateHash = stateHash
	return &state, nil
}

func (s *PostgresStore) CleanupExpiredOAuthStates(ctx context.Context) (int64, error) {
	res, err := s.pool.Exec(ctx,
		`DELETE FROM oauth_states WHERE expires_at < now()`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected(), nil
}

// HealAPIKeyAgent performs a compare-and-set backfill of api_keys.agent_name
// when the key is unassigned and the caller has a validated source_agent
// claim. See store.Store interface for the full contract.
//
// The UPDATE is owner-scoped, respects standalone = false, and skips revoked
// rows. If the CAS fails (no rows updated), we re-read the row to distinguish
// "someone else won with the same slug" from "someone else won with a different
// slug" — the caller uses that to decide proceed vs attribution_conflict.
func (s *PostgresStore) HealAPIKeyAgent(ctx context.Context, keyID, userID, claimedSlug string) (string, error) {
	if keyID == "" || userID == "" || claimedSlug == "" {
		return "", nil
	}

	var updated string
	err := s.pool.QueryRow(ctx,
		`UPDATE api_keys
		    SET agent_name = $1
		  WHERE id = $2::uuid
		    AND user_id = $3::uuid
		    AND agent_name = ''
		    AND standalone = false
		    AND revoked_at IS NULL
		RETURNING agent_name`,
		claimedSlug, keyID, userID,
	).Scan(&updated)
	if err == nil {
		return updated, nil
	}
	// Real DB failures (pool exhausted, syntax error, deadlock, etc.)
	// must propagate so the caller can log them. Only the "no rows
	// matched the CAS" case should fall through to the re-read path.
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("heal api_key CAS: %w", err)
	}

	// Zero rows updated — the row is now standalone / revoked / missing,
	// or someone else already won the race. Re-read (same owner scope)
	// to report the effective slug back to the caller.
	var current string
	err = s.pool.QueryRow(ctx,
		`SELECT agent_name
		   FROM api_keys
		  WHERE id = $1::uuid
		    AND user_id = $2::uuid
		    AND revoked_at IS NULL`,
		keyID, userID,
	).Scan(&current)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Revoked / wrong owner / missing. Return "" so the caller
			// falls through to existing error paths rather than treating
			// it as a conflict.
			return "", nil
		}
		return "", fmt.Errorf("heal api_key re-read: %w", err)
	}
	return current, nil
}
