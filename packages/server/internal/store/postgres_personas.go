package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// UpsertPersona writes a persona and records an immutable revision row in
// the same transaction. Unchanged content (same hash) is a no-op — no
// version bump, no revision — so repeated applies/syncs stay quiet.
func (s *PostgresStore) UpsertPersona(p *model.Persona) error {
	ctx := context.Background()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	row := tx.QueryRow(ctx,
		`INSERT INTO personas (id, owner_id, source_agent, source_scope, source_file_path, name, content, content_hash, version, created_at, updated_at)
		VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (owner_id, source_agent, source_scope, source_file_path)
		DO UPDATE SET name = $6, content = $7, content_hash = $8, version = personas.version + 1, updated_at = $11
		WHERE personas.content_hash <> EXCLUDED.content_hash
		RETURNING id, version`,
		p.ID, p.OwnerID, p.SourceAgent, p.SourceScope, p.SourceFilePath,
		p.Name, p.Content, p.ContentHash, p.Version, p.CreatedAt, p.UpdatedAt,
	)
	var id string
	var version int
	if err := row.Scan(&id, &version); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil // unchanged content — no-op
		}
		return err
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO persona_revisions (persona_id, owner_id, version, content, content_hash)
		VALUES ($1::uuid, $2::uuid, $3, $4, $5)
		ON CONFLICT (persona_id, version) DO NOTHING`,
		id, p.OwnerID, version, p.Content, p.ContentHash,
	); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *PostgresStore) ListPersonas(ownerID string) ([]model.Persona, error) {
	ctx := context.Background()
	rows, err := s.pool.Query(ctx,
		`SELECT id, owner_id, source_agent, source_scope, source_file_path, name, content, content_hash, version, created_at, updated_at
		FROM personas WHERE owner_id = $1::uuid
		ORDER BY updated_at DESC`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var personas []model.Persona
	for rows.Next() {
		var p model.Persona
		if err := rows.Scan(&p.ID, &p.OwnerID, &p.SourceAgent, &p.SourceScope, &p.SourceFilePath,
			&p.Name, &p.Content, &p.ContentHash, &p.Version, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		personas = append(personas, p)
	}
	return personas, rows.Err()
}

func (s *PostgresStore) GetPersona(id string, ownerID string) (*model.Persona, error) {
	ctx := context.Background()
	row := s.pool.QueryRow(ctx,
		`SELECT id, owner_id, source_agent, source_scope, source_file_path, name, content, content_hash, version, created_at, updated_at
		FROM personas WHERE id = $1::uuid AND owner_id = $2::uuid`, id, ownerID)
	var p model.Persona
	if err := row.Scan(&p.ID, &p.OwnerID, &p.SourceAgent, &p.SourceScope, &p.SourceFilePath,
		&p.Name, &p.Content, &p.ContentHash, &p.Version, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *PostgresStore) DeletePersona(id string, ownerID string) error {
	ctx := context.Background()
	_, err := s.pool.Exec(ctx,
		`DELETE FROM personas WHERE id = $1::uuid AND owner_id = $2::uuid`, id, ownerID)
	return err
}

func (s *PostgresStore) DeletePersonaBySource(agent, filePath, scope, ownerID string) error {
	ctx := context.Background()
	_, err := s.pool.Exec(ctx,
		`DELETE FROM personas
		WHERE source_agent = $1 AND source_file_path = $2 AND source_scope = $3 AND owner_id = $4::uuid`,
		agent, filePath, scope, ownerID)
	return err
}

// ListPersonaRevisions returns revision metadata newest-first. Content is
// intentionally omitted — fetch a single revision for the full body.
func (s *PostgresStore) ListPersonaRevisions(personaID, ownerID string) ([]model.PersonaRevision, error) {
	ctx := context.Background()
	rows, err := s.pool.Query(ctx,
		`SELECT id, persona_id, owner_id, version, content_hash, created_at
		FROM persona_revisions
		WHERE persona_id = $1::uuid AND owner_id = $2::uuid
		ORDER BY version DESC`, personaID, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var revisions []model.PersonaRevision
	for rows.Next() {
		var rev model.PersonaRevision
		if err := rows.Scan(&rev.ID, &rev.PersonaID, &rev.OwnerID, &rev.Version, &rev.ContentHash, &rev.CreatedAt); err != nil {
			return nil, err
		}
		revisions = append(revisions, rev)
	}
	return revisions, rows.Err()
}

func (s *PostgresStore) GetPersonaRevision(personaID string, version int, ownerID string) (*model.PersonaRevision, error) {
	ctx := context.Background()
	row := s.pool.QueryRow(ctx,
		`SELECT id, persona_id, owner_id, version, content, content_hash, created_at
		FROM persona_revisions
		WHERE persona_id = $1::uuid AND version = $2 AND owner_id = $3::uuid`,
		personaID, version, ownerID)
	var rev model.PersonaRevision
	if err := row.Scan(&rev.ID, &rev.PersonaID, &rev.OwnerID, &rev.Version, &rev.Content, &rev.ContentHash, &rev.CreatedAt); err != nil {
		return nil, err
	}
	return &rev, nil
}
