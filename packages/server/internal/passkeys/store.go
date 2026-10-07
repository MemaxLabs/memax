package passkeys

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Passkey is one of a person's passkeys, as they see it. Nothing in it is
// secret.
type Passkey struct {
	ID         uuid.UUID
	Name       string
	CreatedAt  time.Time
	LastUsedAt *time.Time
	// BackupEligible: the authenticator syncs it (iCloud Keychain, Google
	// Password Manager, a password manager); BackedUp: it is synced now.
	BackupEligible bool
	BackedUp       bool
	Transports     []string
	// AAGUID names the authenticator's make when it says (nil otherwise),
	// and Provider is its name when Memax knows it.
	AAGUID   *uuid.UUID
	Provider string
}

// record is a stored credential.
type record struct {
	Passkey
	credentialID []byte
	publicKey    []byte
	signCount    uint32
	format       string
}

const recordColumns = `id, name, created_at, last_used_at, backup_eligible, backed_up, transports, aaguid,
	credential_id, public_key, sign_count, attestation_format`

func scanRecord(row pgx.Row) (*record, error) {
	var r record
	var count int64
	if err := row.Scan(&r.ID, &r.Name, &r.CreatedAt, &r.LastUsedAt, &r.BackupEligible, &r.BackedUp, &r.Transports,
		&r.AAGUID, &r.credentialID, &r.publicKey, &count, &r.format); err != nil {
		return nil, err
	}
	r.signCount = uint32(min(max(count, 0), int64(^uint32(0))))
	if r.AAGUID != nil && *r.AAGUID == uuid.Nil {
		r.AAGUID = nil
	}
	if r.AAGUID != nil {
		r.Provider = ProviderName(*r.AAGUID)
	}
	return &r, nil
}

// credential is the record as go-webauthn verifies with it.
func (r *record) credential() webauthn.Credential {
	flags := protocol.FlagUserPresent | protocol.FlagUserVerified
	if r.BackupEligible {
		flags |= protocol.FlagBackupEligible
	}
	if r.BackedUp {
		flags |= protocol.FlagBackupState
	}
	transports := make([]protocol.AuthenticatorTransport, 0, len(r.Transports))
	for _, t := range r.Transports {
		transports = append(transports, protocol.AuthenticatorTransport(t))
	}
	var aaguid []byte
	if r.AAGUID != nil {
		aaguid = r.AAGUID[:]
	}
	return webauthn.Credential{
		ID: r.credentialID, PublicKey: r.publicKey, AttestationType: "none", AttestationFormat: r.format,
		Transport: transports, Flags: webauthn.NewCredentialFlags(flags),
		Authenticator: webauthn.Authenticator{AAGUID: aaguid, SignCount: r.signCount},
	}
}

// inPerson runs fn in a transaction as memax_v2 scoped to person (uuid.Nil:
// nobody, for sign-in). RLS shows it only that person's passkeys and
// challenges.
func (s *Service) inPerson(ctx context.Context, person uuid.UUID, fn func(pgx.Tx) error) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("passkeys: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	p := ""
	if person != uuid.Nil {
		p = person.String()
	}
	if _, err := tx.Exec(ctx, `SELECT set_config('role', $1, true), set_config('app.person_id', $2, true)`, Role, p); err != nil {
		return fmt.Errorf("passkeys: scope: %w", err)
	}
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("passkeys: commit: %w", err)
	}
	return nil
}

func loadRecords(ctx context.Context, tx pgx.Tx, person uuid.UUID) ([]*record, error) {
	rows, err := tx.Query(ctx, `SELECT `+recordColumns+` FROM v2.passkeys WHERE person_id = $1 ORDER BY created_at, id`, person)
	if err != nil {
		return nil, fmt.Errorf("passkeys: list: %w", err)
	}
	defer rows.Close()
	var out []*record
	for rows.Next() {
		r, err := scanRecord(rows)
		if err != nil {
			return nil, fmt.Errorf("passkeys: list: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// List returns the person's passkeys, oldest first.
func (s *Service) List(ctx context.Context, person uuid.UUID) ([]Passkey, error) {
	var out []Passkey
	err := s.inPerson(ctx, person, func(tx pgx.Tx) error {
		recs, err := loadRecords(ctx, tx, person)
		for _, r := range recs {
			out = append(out, r.Passkey)
		}
		return err
	})
	return out, err
}

// Has reports whether the person has a passkey, in one round trip: the
// API asks it for every command a person sends, beside resolving who they
// are.
func (s *Service) Has(ctx context.Context, person uuid.UUID) (bool, error) {
	b := &pgx.Batch{}
	b.Queue(`BEGIN READ ONLY`)
	b.Queue(`SELECT set_config('role', $1, true), set_config('app.person_id', $2, true)`, Role, person.String())
	b.Queue(`SELECT EXISTS (SELECT 1 FROM v2.passkeys WHERE person_id = $1)`, person)
	b.Queue(`COMMIT`)
	br := s.pool.SendBatch(ctx, b)
	var has bool
	_, err := br.Exec()
	if err == nil {
		_, err = br.Exec()
	}
	if err == nil {
		err = br.QueryRow().Scan(&has)
	}
	if err == nil {
		_, err = br.Exec()
	}
	if cerr := br.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return false, fmt.Errorf("passkeys: has: %w", err)
	}
	return has, nil
}

// CleanName trims a passkey's name and checks it: 1 to MaxNameRunes
// characters of text, no control characters.
func CleanName(name string) (string, bool) {
	name = strings.Join(strings.Fields(name), " ")
	if name == "" || !utf8.ValidString(name) || utf8.RuneCountInString(name) > MaxNameRunes {
		return "", false
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return "", false
		}
	}
	return name, true
}

// Rename names one of the person's passkeys.
func (s *Service) Rename(ctx context.Context, person, id uuid.UUID, name string) (*Passkey, error) {
	var out *Passkey
	err := s.inPerson(ctx, person, func(tx pgx.Tx) error {
		r, err := scanRecord(tx.QueryRow(ctx, `UPDATE v2.passkeys SET name = $3 WHERE id = $1 AND person_id = $2
			RETURNING `+recordColumns, id, person, name))
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("passkeys: rename: %w", err)
		}
		out = &r.Passkey
		return nil
	})
	return out, err
}

// Remove takes one of the person's passkeys off their account. Its
// authenticator still holds it; it no longer signs in or checks anything.
func (s *Service) Remove(ctx context.Context, person, id uuid.UUID) (*Passkey, error) {
	var out *Passkey
	err := s.inPerson(ctx, person, func(tx pgx.Tx) error {
		// memax_v2 never deletes in v2: v2.remove_passkeys does, for the
		// person in scope only.
		r, err := scanRecord(tx.QueryRow(ctx, `SELECT `+recordColumns+` FROM v2.remove_passkeys($1)`, id))
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("passkeys: remove: %w", err)
		}
		out = &r.Passkey
		return nil
	})
	return out, err
}

// RemoveAll takes every passkey off the person's account, and their open
// challenges with them (forgetting the account). It returns how many
// passkeys there were.
func (s *Service) RemoveAll(ctx context.Context, person uuid.UUID) (int, error) {
	var n int
	err := s.inPerson(ctx, person, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM v2.remove_passkeys(NULL)`).Scan(&n); err != nil {
			return fmt.Errorf("passkeys: remove all: %w", err)
		}
		if _, err := tx.Exec(ctx, `SELECT v2.clear_passkey_challenges($1, true)`, s.now()); err != nil {
			return fmt.Errorf("passkeys: remove challenges: %w", err)
		}
		return nil
	})
	return n, err
}
