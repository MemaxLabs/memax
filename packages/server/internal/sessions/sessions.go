// Package sessions keeps people's sign-in sessions and their refresh
// tokens (plan 25 §5.15 and §5.16 "Tokens"; migration 049).
//
// # Sessions
//
// A session is one sign-in: the web app, the CLI (a loopback login, or
// the email code without a redirect), a device code (RFC 8628), or an MCP
// client's OAuth grant. Its access tokens are short JWTs (an hour) that
// name it in their sid claim; its refresh token is how the client gets
// the next one. A person lists their sessions and revokes any of them
// (/v2/sessions): revoking ends the refresh token at once, and the access
// token it last minted within that token's hour.
//
// # Refresh tokens are hashed
//
// A refresh token is 256 random bits, and the database keeps only its
// SHA-256 (hex). That is enough because the token is uniformly random:
// nobody can guess one, so there is no dictionary to precompute and
// nothing for a slow, salted password hash (bcrypt, Argon2) to slow down,
// while the lookup stays one unique-index probe. A copy of the database
// (a backup, a leaked replica) holds no token anyone can present. Unlike
// an HMAC with a server key, a hash also survives rotating JWT_SECRET.
//
// # Refresh tokens rotate, and reuse revokes the session
//
// Every refresh retires the token it was given and issues the next one:
// a session is a refresh-token family, and only its newest token works.
// A retired token's hash is kept (session_retired_tokens). Presenting one
// again means two clients hold the same session, which a stolen token
// looks like, so the whole session is revoked: the attacker's copy and
// the person's both stop working, and the person signs in again.
//
// Two honest clients do present the same token, though: two browser tabs
// whose access tokens expire together, the CLI's daemon and an MCP server
// sharing ~/.memax/credentials.json, a retry after the answer to a
// refresh was lost. So within a short grace window (ReuseGrace) of its
// retirement, a retired token yields the session's current refresh token
// again (and a fresh access token), and every racer ends up holding the
// same newest token. For that, a retired token's row carries its
// successor sealed (AES-256-GCM) under a key derived from the retired
// token itself, which is never stored: only a client that held the
// retired token can open it. The seal is wiped once the window passes, so
// a database copy plus an old token yields nothing later.
//
// # Existing tokens
//
// Migration 049 hashed the plain-text tokens in place: every client keeps
// the token it has, and its next refresh rotates it. Every published
// memax CLI stores the refresh token /v1/auth/refresh returns, so old CLIs
// keep working through rotation.
package sessions

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Kind is what signed in.
type Kind string

const (
	// KindWeb: the web app (its code was delivered to the web app's origin).
	KindWeb Kind = "web"
	// KindCLI: the memax CLI's loopback login, or tokens returned directly.
	KindCLI Kind = "cli"
	// KindDevice: a device code, confirmed by the person on the web.
	KindDevice Kind = "device"
	// KindMCP: an MCP client's OAuth grant.
	KindMCP Kind = "mcp"
)

// Reason is why a session ended before it expired.
type Reason string

const (
	ReasonSignedOut     Reason = "signed_out"
	ReasonRevoked       Reason = "revoked"
	ReasonRevokedOthers Reason = "revoked_others"
	ReasonReuseDetected Reason = "reuse_detected"
	ReasonGrantRevoked  Reason = "grant_revoked"
)

const (
	// ReuseGrace is how long a retired refresh token still yields its
	// session's current token: long enough for refreshes that raced (tabs,
	// processes sharing a credentials file) and a retry after a lost
	// answer, short enough that a stolen token used later is caught.
	ReuseGrace = 60 * time.Second
	// RetiredKeep is how long a retired token's hash is kept for reuse
	// detection. Older ones are unknown tokens.
	RetiredKeep = 14 * 24 * time.Hour
	// EndedKeep is how long a session is kept after it expired or was
	// revoked.
	EndedKeep = 30 * 24 * time.Hour
	// TouchEvery is how often a session's last use is written at most.
	TouchEvery = 5 * time.Minute
	// maxWalk bounds following a retired token's successors to the head.
	maxWalk = 8
)

var (
	// ErrUnknown: no session has that refresh token (or it was retired long
	// ago).
	ErrUnknown = errors.New("sessions: unknown refresh token")
	// ErrExpired: the session expired.
	ErrExpired = errors.New("sessions: session expired")
	// ErrRevoked: the session was revoked.
	ErrRevoked = errors.New("sessions: session revoked")
	// ErrReused: a retired refresh token was presented after the grace
	// window; the session is now revoked.
	ErrReused = errors.New("sessions: refresh token reused; session revoked")
	// ErrGrantRevoked: returned by a refresh's Accept to end the session
	// because the OAuth grant behind it no longer holds.
	ErrGrantRevoked = errors.New("sessions: the session's grant is no longer valid")
	// ErrNotFound: no such session for that person.
	ErrNotFound = errors.New("sessions: no such session")
)

// Where is where a client is, when known.
type Where struct {
	IP        string
	City      string
	UserAgent string
}

// Session is one sign-in.
type Session struct {
	ID            uuid.UUID
	UserID        uuid.UUID
	Kind          Kind
	Surface       string // the assurance claim: web, cli or "" (before migration 030)
	AgentName     string
	GrantID       string // the OAuth grant, for an MCP session
	Client        string
	UserAgent     string
	CreatedIP     string
	CreatedCity   string
	LastIP        string
	LastCity      string
	CreatedAt     time.Time
	LastUsedAt    time.Time
	RotatedAt     time.Time
	ExpiresAt     time.Time
	Generation    int
	RevokedAt     *time.Time
	RevokedReason Reason
}

// Start is a new session.
type Start struct {
	UserID    uuid.UUID
	Kind      Kind
	Surface   string
	AgentName string
	GrantID   string
	Client    string
	Where     Where
	// TTL is how long the session lives (30 days for people's sessions).
	TTL time.Duration
}

// Issued is a session and the refresh token its client keeps.
type Issued struct {
	Session      Session
	RefreshToken string
	// Replayed: the token presented was retired within the grace window, so
	// this is the session's current token again, not a new one.
	Replayed bool
}

// Store keeps sessions in Postgres.
type Store struct {
	pool  *pgxpool.Pool
	now   func() time.Time
	grace time.Duration
	log   *slog.Logger

	reapMu   sync.Mutex
	lastReap time.Time

	touchMu sync.Mutex
	touched map[uuid.UUID]time.Time
	wg      sync.WaitGroup
}

// Option configures a Store.
type Option func(*Store)

// WithClock replaces time.Now (tests).
func WithClock(now func() time.Time) Option { return func(s *Store) { s.now = now } }

// WithGrace replaces ReuseGrace (tests).
func WithGrace(d time.Duration) Option { return func(s *Store) { s.grace = d } }

// WithLogger sets the logger.
func WithLogger(l *slog.Logger) Option { return func(s *Store) { s.log = l } }

// New returns a Store on pool.
func New(pool *pgxpool.Pool, opts ...Option) *Store {
	s := &Store{pool: pool, now: time.Now, grace: ReuseGrace, log: slog.Default()}
	for _, o := range opts {
		o(s)
	}
	return s
}

// Now is the store's clock.
func (s *Store) Now() time.Time { return s.now() }

// HashToken is the stored form of a refresh token: its SHA-256, in hex.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// newToken is a refresh token: 256 random bits, in hex.
func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("sessions: random token: %w", err)
	}
	return hex.EncodeToString(b), nil
}

const sessionColumns = `s.id, s.user_id, s.kind, COALESCE(s.surface, ''), COALESCE(s.agent_name, ''),
	COALESCE(s.grant_id::text, ''), s.client, s.user_agent, s.created_ip, s.created_city,
	s.last_ip, s.last_city, s.created_at, s.last_used_at, s.rotated_at, s.expires_at,
	s.generation, s.revoked_at, COALESCE(s.revoked_reason, '')`

func scanSession(row pgx.Row) (*Session, string, error) {
	var (
		ss     Session
		kind   string
		reason string
		hash   string
	)
	err := row.Scan(&ss.ID, &ss.UserID, &kind, &ss.Surface, &ss.AgentName, &ss.GrantID, &ss.Client,
		&ss.UserAgent, &ss.CreatedIP, &ss.CreatedCity, &ss.LastIP, &ss.LastCity, &ss.CreatedAt,
		&ss.LastUsedAt, &ss.RotatedAt, &ss.ExpiresAt, &ss.Generation, &ss.RevokedAt, &reason, &hash)
	if err != nil {
		return nil, "", err
	}
	ss.Kind, ss.RevokedReason = Kind(kind), Reason(reason)
	return &ss, hash, nil
}

// Issue starts a session and returns its first refresh token.
func (s *Store) Issue(ctx context.Context, st Start) (*Issued, error) {
	switch st.Kind {
	case KindWeb, KindCLI, KindDevice, KindMCP:
	default:
		return nil, fmt.Errorf("sessions: unknown kind %q", st.Kind)
	}
	if st.TTL <= 0 {
		return nil, errors.New("sessions: a session needs a lifetime")
	}
	token, err := newToken()
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	w := clean(st.Where)
	row := s.pool.QueryRow(ctx, `
		WITH s AS (
			INSERT INTO sessions (user_id, refresh_token_hash, expires_at, agent_name, grant_id, surface, kind,
				client, user_agent, created_ip, created_city, last_ip, last_city, created_at, last_used_at, rotated_at)
			VALUES ($1, $2, $3, $4, NULLIF($5, '')::uuid, NULLIF($6, ''), $7, $8, $9, $10, $11, $10, $11, $12, $12, $12)
			RETURNING *
		)
		SELECT `+sessionColumns+`, s.refresh_token_hash FROM s`,
		st.UserID, HashToken(token), now.Add(st.TTL), st.AgentName, st.GrantID, st.Surface, string(st.Kind),
		trim(st.Client, 200), w.UserAgent, w.IP, w.City, now)
	ss, _, err := scanSession(row)
	if err != nil {
		return nil, fmt.Errorf("sessions: issue: %w", err)
	}
	s.reap(ctx)
	return &Issued{Session: *ss, RefreshToken: token}, nil
}

// RefreshOptions are the optional parts of a refresh.
type RefreshOptions struct {
	// Where the client is now.
	Where Where
	// Accept, when set, is asked about the session before it is refreshed.
	// An error refuses the refresh and is returned as is; ErrGrantRevoked
	// also revokes the session.
	Accept func(Session) error
}

// Refresh trades a refresh token for the session's next one (rotation).
// It returns ErrUnknown, ErrExpired, ErrRevoked or ErrReused (which has
// revoked the session) when the token can't be refreshed.
func (s *Store) Refresh(ctx context.Context, token string, o RefreshOptions) (*Issued, error) {
	if token == "" {
		return nil, ErrUnknown
	}
	hash := HashToken(token)
	var out *Issued
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		ss, _, err := scanSession(tx.QueryRow(ctx,
			`SELECT `+sessionColumns+`, s.refresh_token_hash FROM sessions s WHERE s.refresh_token_hash = $1 FOR UPDATE`, hash))
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			out, err = s.retired(ctx, tx, token, hash, o)
			return err
		case err != nil:
			return err
		}
		if err := s.usable(ctx, tx, ss, o); err != nil {
			return err
		}
		next, err := newToken()
		if err != nil {
			return err
		}
		sealed, err := seal(token, next, ss.ID)
		if err != nil {
			return err
		}
		now := s.now().UTC()
		if _, err := tx.Exec(ctx, `INSERT INTO session_retired_tokens (token_hash, session_id, generation, retired_at, successor_sealed)
			VALUES ($1, $2, $3, $4, $5)`, hash, ss.ID, ss.Generation, now, sealed); err != nil {
			return fmt.Errorf("retire: %w", err)
		}
		w := clean(o.Where)
		if _, err := tx.Exec(ctx, `UPDATE sessions SET refresh_token_hash = $2, generation = generation + 1, rotated_at = $3,
				last_used_at = GREATEST(last_used_at, $3),
				last_ip = CASE WHEN $4 = '' THEN last_ip ELSE $4 END,
				last_city = CASE WHEN $4 = '' THEN last_city ELSE $5 END
			WHERE id = $1`, ss.ID, HashToken(next), now, w.IP, w.City); err != nil {
			return fmt.Errorf("rotate: %w", err)
		}
		ss.Generation++
		ss.RotatedAt, ss.LastUsedAt = now, now
		if w.IP != "" {
			ss.LastIP, ss.LastCity = w.IP, w.City
		}
		out = &Issued{Session: *ss, RefreshToken: next}
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.reap(ctx)
	return out, nil
}

// inTx runs fn in a transaction. An error fn marks with commitThen commits
// what fn did (a revocation) and is returned after; any other rolls back.
func (s *Store) inTx(ctx context.Context, fn func(pgx.Tx) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	err = fn(tx)
	var c *committed
	if errors.As(err, &c) {
		if cerr := tx.Commit(ctx); cerr != nil {
			return cerr
		}
		return c.err
	}
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// usable checks a session can be refreshed, revoking it when its grant no
// longer holds.
func (s *Store) usable(ctx context.Context, tx pgx.Tx, ss *Session, o RefreshOptions) error {
	if ss.RevokedAt != nil {
		return ErrRevoked
	}
	if !s.now().Before(ss.ExpiresAt) {
		return ErrExpired
	}
	if o.Accept == nil {
		return nil
	}
	err := o.Accept(*ss)
	if errors.Is(err, ErrGrantRevoked) {
		if e := revokeTx(ctx, tx, ss.ID, ReasonGrantRevoked, s.now()); e != nil {
			return e
		}
		return commitThen(ErrGrantRevoked)
	}
	return err
}

// commitThen marks an error that must not roll the transaction back: the
// revocation it made stands, and the caller still gets err.
func commitThen(err error) error { return &committed{err} }

type committed struct{ err error }

func (c *committed) Error() string { return c.err.Error() }

// retired handles a token that isn't any session's current one: a retired
// token within the grace window yields the session's current token; after
// it, the session is revoked.
func (s *Store) retired(ctx context.Context, tx pgx.Tx, token, hash string, o RefreshOptions) (*Issued, error) {
	var (
		sessionID uuid.UUID
		retiredAt time.Time
		sealed    []byte
	)
	err := tx.QueryRow(ctx, `SELECT session_id, retired_at, successor_sealed FROM session_retired_tokens WHERE token_hash = $1`,
		hash).Scan(&sessionID, &retiredAt, &sealed)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrUnknown
	}
	if err != nil {
		return nil, err
	}
	ss, current, err := scanSession(tx.QueryRow(ctx,
		`SELECT `+sessionColumns+`, s.refresh_token_hash FROM sessions s WHERE s.id = $1 FOR UPDATE`, sessionID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrUnknown
	}
	if err != nil {
		return nil, err
	}
	if ss.RevokedAt != nil {
		return nil, ErrRevoked
	}
	now := s.now()
	if now.Sub(retiredAt) <= s.grace && len(sealed) > 0 {
		head, ok := s.walk(ctx, tx, token, sealed, ss.ID, current, now)
		if ok {
			if err := s.usable(ctx, tx, ss, o); err != nil {
				return nil, err
			}
			if _, err := tx.Exec(ctx, `UPDATE sessions SET last_used_at = GREATEST(last_used_at, $2) WHERE id = $1`,
				ss.ID, now.UTC()); err != nil {
				return nil, err
			}
			return &Issued{Session: *ss, RefreshToken: head, Replayed: true}, nil
		}
	}
	// Reuse: two clients hold this session. End it for both.
	if err := revokeTx(ctx, tx, ss.ID, ReasonReuseDetected, now); err != nil {
		return nil, err
	}
	s.log.WarnContext(ctx, "sessions: a retired refresh token was presented again; the session is revoked",
		"metric", "refresh_token_reuse", "session_id", ss.ID.String(), "user_id", ss.UserID.String(),
		"kind", string(ss.Kind), "retired_for", now.Sub(retiredAt).Round(time.Second).String())
	return nil, commitThen(ErrReused)
}

// walk opens a retired token's successor, and that one's while it was
// retired too, until it reaches the session's current token.
func (s *Store) walk(ctx context.Context, tx pgx.Tx, token string, sealed []byte, session uuid.UUID, current string, now time.Time) (string, bool) {
	for range maxWalk {
		next, err := open(token, sealed, session)
		if err != nil {
			return "", false
		}
		h := HashToken(next)
		if h == current {
			return next, true
		}
		var retiredAt time.Time
		err = tx.QueryRow(ctx, `SELECT retired_at, successor_sealed FROM session_retired_tokens
			WHERE token_hash = $1 AND session_id = $2`, h, session).Scan(&retiredAt, &sealed)
		if err != nil || len(sealed) == 0 || now.Sub(retiredAt) > s.grace {
			return "", false
		}
		token = next
	}
	return "", false
}

func revokeTx(ctx context.Context, tx pgx.Tx, id uuid.UUID, reason Reason, now time.Time) error {
	_, err := tx.Exec(ctx, `UPDATE sessions SET revoked_at = $2, revoked_reason = $3 WHERE id = $1 AND revoked_at IS NULL`,
		id, now.UTC(), string(reason))
	return err
}

// live is the condition for a session that still works.
const live = `s.revoked_at IS NULL AND s.expires_at > $2
	AND (s.grant_id IS NULL OR EXISTS (SELECT 1 FROM oauth_grants g WHERE g.id = s.grant_id
		AND g.revoked_at IS NULL AND (g.expires_at IS NULL OR g.expires_at > $2)))`

// List returns a person's live sessions, the most recently used first.
func (s *Store) List(ctx context.Context, user uuid.UUID) ([]Session, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+sessionColumns+`, s.refresh_token_hash FROM sessions s
		WHERE s.user_id = $1 AND `+live+` ORDER BY s.last_used_at DESC, s.created_at DESC, s.id LIMIT 500`,
		user, s.now().UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Session
	for rows.Next() {
		ss, _, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *ss)
	}
	return out, rows.Err()
}

// Get returns one of a person's live sessions, or ErrNotFound.
func (s *Store) Get(ctx context.Context, user, id uuid.UUID) (*Session, error) {
	ss, _, err := scanSession(s.pool.QueryRow(ctx, `SELECT `+sessionColumns+`, s.refresh_token_hash FROM sessions s
		WHERE s.user_id = $1 AND `+live+` AND s.id = $3`, user, s.now().UTC(), id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return ss, err
}

// Revoke ends one of a person's live sessions. ErrNotFound when it isn't
// theirs or has already ended.
func (s *Store) Revoke(ctx context.Context, user, id uuid.UUID, reason Reason) (*Session, error) {
	ss, _, err := scanSession(s.pool.QueryRow(ctx, `
		WITH s AS (
			UPDATE sessions s SET revoked_at = $2, revoked_reason = $4
			WHERE s.user_id = $1 AND s.id = $3 AND `+live+`
			RETURNING *
		)
		SELECT `+sessionColumns+`, s.refresh_token_hash FROM s`, user, s.now().UTC(), id, string(reason)))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return ss, err
}

// RevokeOthers ends every live session of a person's but keep, and says
// how many it ended.
func (s *Store) RevokeOthers(ctx context.Context, user, keep uuid.UUID) (int, error) {
	tag, err := s.pool.Exec(ctx, `UPDATE sessions s SET revoked_at = $2, revoked_reason = $4
		WHERE s.user_id = $1 AND s.id <> $3 AND `+live, user, s.now().UTC(), keep, string(ReasonRevokedOthers))
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

// RevokeToken ends the session a refresh token belongs to (signing out,
// RFC 7009 revocation). A retired token of the session ends it too. It
// returns the session, or nil when the token names none.
func (s *Store) RevokeToken(ctx context.Context, token string, reason Reason) (*Session, error) {
	if token == "" {
		return nil, nil
	}
	hash := HashToken(token)
	ss, _, err := scanSession(s.pool.QueryRow(ctx, `
		WITH s AS (
			UPDATE sessions s SET revoked_at = $2, revoked_reason = $3
			WHERE s.revoked_at IS NULL AND (s.refresh_token_hash = $1
				OR s.id = (SELECT session_id FROM session_retired_tokens WHERE token_hash = $1))
			RETURNING *
		)
		SELECT `+sessionColumns+`, s.refresh_token_hash FROM s`, hash, s.now().UTC(), string(reason)))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return ss, err
}

// Touch records that a session was used, at most every TouchEvery per
// session per process, in the background.
func (s *Store) Touch(id uuid.UUID) {
	if s == nil || id == uuid.Nil {
		return
	}
	now := s.now()
	s.touchMu.Lock()
	if s.touched == nil || len(s.touched) > 50_000 {
		s.touched = map[uuid.UUID]time.Time{}
	}
	if t, ok := s.touched[id]; ok && now.Sub(t) < TouchEvery {
		s.touchMu.Unlock()
		return
	}
	s.touched[id] = now
	s.touchMu.Unlock()
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := s.pool.Exec(ctx, `UPDATE sessions SET last_used_at = $2
			WHERE id = $1 AND revoked_at IS NULL AND last_used_at < $2`, id, now.UTC()); err != nil {
			s.log.Warn("sessions: could not record a session's use", "session_id", id.String(), "error", err)
		}
	}()
}

// Wait blocks until background writes have finished (shutdown, tests).
func (s *Store) Wait() {
	if s != nil {
		s.wg.Wait()
	}
}

// reap prunes, at most once a minute per process: seals past the grace
// window, retired hashes past RetiredKeep, and sessions that ended
// EndedKeep ago. Best effort; a failure is logged.
func (s *Store) reap(ctx context.Context) {
	now := s.now()
	s.reapMu.Lock()
	if now.Sub(s.lastReap) < time.Minute {
		s.reapMu.Unlock()
		return
	}
	s.lastReap = now
	s.reapMu.Unlock()
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	for _, q := range []struct {
		sql string
		at  time.Time
	}{
		{`UPDATE session_retired_tokens SET successor_sealed = NULL WHERE token_hash IN (
			SELECT token_hash FROM session_retired_tokens WHERE successor_sealed IS NOT NULL AND retired_at < $1 LIMIT 1000)`,
			now.Add(-s.grace)},
		{`DELETE FROM session_retired_tokens WHERE token_hash IN (
			SELECT token_hash FROM session_retired_tokens WHERE retired_at < $1 LIMIT 1000)`, now.Add(-RetiredKeep)},
		{`DELETE FROM sessions WHERE id IN (
			SELECT id FROM sessions WHERE expires_at < $1 OR revoked_at < $1 LIMIT 200)`, now.Add(-EndedKeep)},
	} {
		if _, err := s.pool.Exec(ctx, q.sql, q.at.UTC()); err != nil {
			s.log.Warn("sessions: reaping failed", "error", err)
			return
		}
	}
}

// sealKey derives the key a retired token's successor is sealed under
// from the retired token itself.
func sealKey(token string) []byte {
	mac := hmac.New(sha256.New, []byte(token))
	mac.Write([]byte("memax refresh successor/v1"))
	return mac.Sum(nil)
}

// seal encrypts next under a key only token opens, bound to the session.
func seal(token, next string, session uuid.UUID) ([]byte, error) {
	block, err := aes.NewCipher(sealKey(token))
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, []byte(next), session[:]), nil
}

// open reverses seal.
func open(token string, sealed []byte, session uuid.UUID) (string, error) {
	block, err := aes.NewCipher(sealKey(token))
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(sealed) < gcm.NonceSize() {
		return "", errors.New("sessions: sealed successor too short")
	}
	plain, err := gcm.Open(nil, sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():], session[:])
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

// clean bounds what a client says about where it is.
func clean(w Where) Where {
	return Where{IP: trim(w.IP, 64), City: trim(w.City, 100), UserAgent: trim(w.UserAgent, 512)}
}

// trim drops control characters and cuts s to at most n bytes on a rune
// boundary.
func trim(s string, n int) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, strings.TrimSpace(s))
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}
