package store

import (
	"context"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

func (s *PostgresStore) UpsertPersona(p *model.Persona) error {
	ctx := context.Background()
	_, err := s.pool.Exec(ctx,
		`INSERT INTO personas (id, owner_id, source_agent, source_scope, source_file_path, name, content, content_hash, version, created_at, updated_at)
		VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (owner_id, source_agent, source_scope, source_file_path)
		DO UPDATE SET name = $6, content = $7, content_hash = $8, version = personas.version + 1, updated_at = $11`,
		p.ID, p.OwnerID, p.SourceAgent, p.SourceScope, p.SourceFilePath,
		p.Name, p.Content, p.ContentHash, p.Version, p.CreatedAt, p.UpdatedAt,
	)
	return err
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
