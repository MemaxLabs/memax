package migrate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	gomigrate "github.com/golang-migrate/migrate/v4"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MemaxLabs/memax/packages/server/internal/sessions"
)

// sessionRotationVersion is migration 048 (hashed, rotated refresh
// tokens). Renumber it with the file if a merge moves it.
const sessionRotationVersion = 48

// TestSessionMigrationHashesLiveTokensInPlace: the people signed in before
// 048 stay signed in. Their plain-text refresh tokens are hashed where they
// are, nothing in plain text is left in the table, each session gets its
// kind, and the token each client holds still refreshes, now rotating.
func TestSessionMigrationHashesLiveTokensInPlace(t *testing.T) {
	cs := withFreshDB(t)
	m := newMigrator(t, cs)
	if err := m.Migrate(sessionRotationVersion - 1); err != nil {
		t.Fatalf("migrate to %03d: %v", sessionRotationVersion-1, err)
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, cs)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	if _, err := pool.Exec(ctx, `
		INSERT INTO users (id, email, name) VALUES ('11111111-1111-1111-1111-111111111111', 'rt@test', 'rt');
		INSERT INTO oauth_clients (client_id, client_name) VALUES ('claude', 'Claude');
		INSERT INTO oauth_grants (id, user_id, client_id, agent_name, default_permissions)
			VALUES ('44444444-4444-4444-4444-444444444444', '11111111-1111-1111-1111-111111111111', 'claude', 'claude-ai', ARRAY['memory:read']);
		INSERT INTO sessions (user_id, refresh_token, expires_at, surface) VALUES
			('11111111-1111-1111-1111-111111111111', 'live-web-token', now() + interval '20 days', 'web'),
			('11111111-1111-1111-1111-111111111111', 'live-cli-token', now() + interval '20 days', 'cli'),
			('11111111-1111-1111-1111-111111111111', 'pre-030-token', now() + interval '20 days', NULL);
		INSERT INTO sessions (user_id, refresh_token, expires_at, agent_name, grant_id) VALUES
			('11111111-1111-1111-1111-111111111111', 'live-mcp-token', now() + interval '20 days', 'claude-ai', '44444444-4444-4444-4444-444444444444')`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := m.Migrate(sessionRotationVersion); err != nil {
		t.Fatalf("migrate to %03d: %v", sessionRotationVersion, err)
	}

	want := map[string]string{"live-web-token": "web", "live-cli-token": "cli", "pre-030-token": "cli", "live-mcp-token": "mcp"}
	rows, err := pool.Query(ctx, `SELECT row_to_json(s)::text, refresh_token_hash, kind FROM sessions s`)
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]string{}
	for rows.Next() {
		var row, hash, kind string
		if err := rows.Scan(&row, &hash, &kind); err != nil {
			t.Fatal(err)
		}
		for tok := range want {
			if strings.Contains(row, tok) {
				t.Fatalf("a plain-text token survived the migration: %s", row)
			}
		}
		kinds[hash] = kind
	}
	rows.Close()
	for tok, kind := range want {
		sum := sha256.Sum256([]byte(tok))
		if got := kinds[hex.EncodeToString(sum[:])]; got != kind {
			t.Errorf("%s: kind %q, want %q (or its hash is missing)", tok, got, kind)
		}
	}

	// The token the browser or CLI holds still works, and now rotates.
	store := sessions.New(pool)
	is, err := store.Refresh(ctx, "live-cli-token", sessions.RefreshOptions{})
	if err != nil {
		t.Fatalf("refresh a migrated token: %v", err)
	}
	if is.RefreshToken == "live-cli-token" || is.Replayed {
		t.Fatal("a migrated token didn't rotate")
	}
	if _, err := store.Refresh(ctx, is.RefreshToken, sessions.RefreshOptions{}); err != nil {
		t.Fatalf("refresh the rotated token: %v", err)
	}

	// Back and forth: the down migration ends every session (hashes can't
	// be turned back into tokens), and 048 applies again on top.
	if err := m.Migrate(sessionRotationVersion - 1); err != nil {
		t.Fatalf("migrate down to %03d: %v", sessionRotationVersion-1, err)
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM sessions`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("after down: %d sessions, %v", n, err)
	}
	if err := m.Up(); err != nil && !errors.Is(err, gomigrate.ErrNoChange) {
		t.Fatalf("migrate up again: %v", err)
	}
}
