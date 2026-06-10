package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// CreateEmailOTPCode inserts a freshly issued code. Caller is
// responsible for hashing the code and canonicalizing the email.
func (s *PostgresStore) CreateEmailOTPCode(ctx context.Context, code model.EmailOTPCode) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO email_otp_codes (
			email, code_hash, purpose, max_attempts,
			client_redirect, invite_token, request_ip, user_agent, expires_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		code.Email, code.CodeHash, code.Purpose, code.MaxAttempts,
		code.ClientRedirect, code.InviteToken, code.RequestIP, code.UserAgent,
		code.ExpiresAt,
	)
	return err
}

// GetActiveEmailOTPCode returns the most recent unconsumed, unexpired
// row for the given canonical email. Returns pgx.ErrNoRows when no
// active row exists.
func (s *PostgresStore) GetActiveEmailOTPCode(ctx context.Context, email string) (*model.EmailOTPCode, error) {
	var c model.EmailOTPCode
	err := s.pool.QueryRow(ctx,
		`SELECT id::text, email, code_hash, purpose, attempts, max_attempts,
		        client_redirect, invite_token, request_ip, user_agent,
		        created_at, expires_at, consumed_at
		   FROM email_otp_codes
		  WHERE email = $1
		    AND consumed_at IS NULL
		    AND expires_at > now()
		  ORDER BY created_at DESC
		  LIMIT 1`,
		email,
	).Scan(
		&c.ID, &c.Email, &c.CodeHash, &c.Purpose, &c.Attempts, &c.MaxAttempts,
		&c.ClientRedirect, &c.InviteToken, &c.RequestIP, &c.UserAgent,
		&c.CreatedAt, &c.ExpiresAt, &c.ConsumedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrEmailOTPCodeNotFound
		}
		return nil, err
	}
	return &c, nil
}

// IncrementEmailOTPCodeAttempts atomically bumps the attempts counter
// and returns the new value. Used after a wrong-code submission so
// the handler can cap attempts at max_attempts.
func (s *PostgresStore) IncrementEmailOTPCodeAttempts(ctx context.Context, id string) (int, error) {
	var attempts int
	err := s.pool.QueryRow(ctx,
		`UPDATE email_otp_codes
		    SET attempts = attempts + 1
		  WHERE id = $1::uuid
		RETURNING attempts`,
		id,
	).Scan(&attempts)
	if err != nil {
		return 0, err
	}
	return attempts, nil
}

// ConsumeEmailOTPCode marks a code as used. Idempotent: re-consuming
// a row is a no-op (we still write consumed_at = now() but the verify
// path checks consumed_at before calling us).
func (s *PostgresStore) ConsumeEmailOTPCode(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE email_otp_codes
		    SET consumed_at = now()
		  WHERE id = $1::uuid
		    AND consumed_at IS NULL`,
		id,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	return err
}

// CountRecentEmailOTPCodesForEmail counts rows created after `since`
// for the given canonical email. Drives per-email rate limiting.
func (s *PostgresStore) CountRecentEmailOTPCodesForEmail(ctx context.Context, email string, since time.Time) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx,
		`SELECT count(*)::int
		   FROM email_otp_codes
		  WHERE email = $1 AND created_at >= $2`,
		email, since,
	).Scan(&n)
	return n, err
}

// CountRecentEmailOTPCodesForIP counts rows created after `since`
// from the given IP. Drives per-IP rate limiting.
func (s *PostgresStore) CountRecentEmailOTPCodesForIP(ctx context.Context, ip string, since time.Time) (int, error) {
	if ip == "" {
		return 0, nil
	}
	var n int
	err := s.pool.QueryRow(ctx,
		`SELECT count(*)::int
		   FROM email_otp_codes
		  WHERE request_ip = $1 AND created_at >= $2`,
		ip, since,
	).Scan(&n)
	return n, err
}

// CleanupExpiredEmailOTPCodes deletes rows past their expires_at.
// Returns the number of rows removed. Safe to run on a cron.
func (s *PostgresStore) CleanupExpiredEmailOTPCodes(ctx context.Context) (int64, error) {
	res, err := s.pool.Exec(ctx,
		`DELETE FROM email_otp_codes WHERE expires_at < now()`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected(), nil
}
