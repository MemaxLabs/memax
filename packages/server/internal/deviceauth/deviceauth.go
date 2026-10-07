// Package deviceauth is the device authorization grant (RFC 8628) that
// signs the memax CLI in where no browser can open on its machine: over
// SSH, in a container, on a headless box (plan 25 §5.15, §7.3 step 2).
//
// The CLI asks for a code and shows the person a short user code and
// memax.app/device; the person, signed in on the web app, checks that the
// code matches the terminal and confirms it; the CLI, polling all the
// while, collects a person's CLI session once. The protocol endpoints
// (POST /oauth/device_authorization, the device_code grant of POST
// /oauth/token) live with the OAuth server in internal/handler; the web's
// confirmation (/v2/device-authorizations:lookup, :approve, :deny) lives
// in internal/handler/v2api. Both go through this Store.
//
// Security choices (THREAT_MODEL-style notes live beside each):
//
//   - Neither code is stored: the device code (256 random bits) as its
//     SHA-256, the user code (about 30 bits) as an HMAC keyed by the
//     server's secret, so a copy of the table can't be turned back into
//     live codes.
//   - A user code is 4 consonants and 4 digits ("WQRT-4821", RFC 8628
//     §6.1's base-20 alphabet: no vowels, so no words, and nothing to
//     confuse with a digit), lives 10 minutes, and is decided once. A
//     device code yields one session.
//   - New codes are limited per address (here, across machines, and in
//     the route's per-IP limiter); polling too fast is slow_down, which
//     adds 5 s to that code's interval for good.
//   - The session it issues is the CLI's (surface cli), so nothing done
//     with it is ever human_web. Only a person on the web app confirms a
//     code (policy.DecideDevice), so neither an agent nor a CLI token can
//     mint more sessions from a code it reads.
package deviceauth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// GrantType is the device_code grant's grant_type at the token endpoint.
const GrantType = "urn:ietf:params:oauth:grant-type:device_code"

// ClientCLI is the one client that may use the grant: the memax CLI, a
// public client. A code signs a person in to the CLI, never an agent.
const ClientCLI = "memax-cli"

const (
	// Lifetime is how long a code lives (the CliAuth board: "Codes expire
	// after 10 minutes").
	Lifetime = 10 * time.Minute
	// DefaultInterval is how long, in seconds, the CLI waits between polls.
	DefaultInterval = 5
	// SlowDownStep is what slow_down adds to a code's interval (RFC 8628 §3.5).
	SlowDownStep = 5
	// MaxInterval caps a code's interval.
	MaxInterval = 60
	// PerIPWindow and PerIPMax limit new codes from one address, across
	// API machines (the route's in-memory limiter counts per machine).
	PerIPWindow = time.Hour
	PerIPMax    = 20
	// reapAfter is how long an expired request is kept, so a late poll
	// still hears expired_token rather than an unknown code.
	reapAfter = 24 * time.Hour
)

// The user code's alphabet: RFC 8628 §6.1's base-20 consonants, then digits.
const (
	userCodeLetters = "BCDFGHJKLMNPQRSTVWXZ"
	userCodeDigits  = "0123456789"
)

// Status is what happened to a request, as stored.
type Status string

const (
	StatusPending  Status = "pending"
	StatusApproved Status = "approved"
	StatusDenied   Status = "denied"
	StatusConsumed Status = "consumed"
)

// State is what the confirmation page shows: the status, with expiry read
// from the clock.
type State string

const (
	StatePending  State = "pending"
	StateApproved State = "approved"
	// StateSignedIn: confirmed, and the CLI collected its session.
	StateSignedIn State = "signed_in"
	StateDenied   State = "denied"
	StateExpired  State = "expired"
)

// Errors. The token endpoint maps the poll errors onto RFC 8628 §3.5's codes.
var (
	// ErrUnknownClient: a client_id other than the memax CLI.
	ErrUnknownClient = errors.New("deviceauth: only the memax CLI signs in with a device code")
	// ErrRateLimited: too many codes from this address lately.
	ErrRateLimited = errors.New("deviceauth: too many codes requested from this address")
	// ErrNotFound: no request by that code the viewer may see.
	ErrNotFound = errors.New("deviceauth: no such code")
	// ErrPending (authorization_pending): nobody has confirmed it yet.
	ErrPending = errors.New("deviceauth: waiting for the person to confirm the code")
	// ErrSlowDown (slow_down): polled before its interval passed.
	ErrSlowDown = errors.New("deviceauth: polling too fast")
	// ErrExpired (expired_token): the code's 10 minutes are up.
	ErrExpired = errors.New("deviceauth: the code expired")
	// ErrDenied (access_denied): the person said it doesn't match.
	ErrDenied = errors.New("deviceauth: the person declined the code")
	// ErrUsed (invalid_grant): the code already signed a CLI in, or there
	// is no such device code.
	ErrUsed = errors.New("deviceauth: the device code was already used, or isn't one")
)

// DecidedError: the code was already decided the other way.
type DecidedError struct{ State State }

func (e *DecidedError) Error() string {
	return fmt.Sprintf("deviceauth: the code is already %s", e.State)
}

// Start is what the CLI says about itself when it asks for a code, plus
// where the request came from. Everything but ClientID is optional and is
// shown to the person as what the device says.
type Start struct {
	ClientID      string
	ClientVersion string
	DeviceName    string
	DeviceOS      string
	Space         string
	IP            string
	UserAgent     string
}

// Request is one device authorization.
type Request struct {
	ID            uuid.UUID
	ClientID      string
	ClientVersion string
	DeviceName    string
	DeviceOS      string
	Space         string
	IP            string
	Status        Status
	UserID        *uuid.UUID
	Interval      int
	CreatedAt     time.Time
	ExpiresAt     time.Time
	DecidedAt     *time.Time
	ConsumedAt    *time.Time
	// UserCode is the code in its display form ("WQRT-4821"), when the
	// caller knows it (the store never does: it keeps an HMAC).
	UserCode string
}

// StateAt is the request's state at now.
func (r *Request) StateAt(now time.Time) State {
	switch r.Status {
	case StatusConsumed:
		return StateSignedIn
	case StatusDenied:
		return StateDenied
	}
	if !now.Before(r.ExpiresAt) {
		return StateExpired
	}
	if r.Status == StatusApproved {
		return StateApproved
	}
	return StatePending
}

// Issued is a new request with its codes, returned once to the CLI.
type Issued struct {
	DeviceCode string
	// UserCode in its display form, "WQRT-4821".
	UserCode string
	Request  Request
}

// Store keeps device authorizations in Postgres (migration 045).
type Store struct {
	pool   *pgxpool.Pool
	pepper []byte
	now    func() time.Time
}

// New returns a Store; pepper keys the user codes' HMAC (the server's
// JWT secret). A nil pool returns nil: no database, no device sign-in.
func New(pool *pgxpool.Pool, pepper []byte) *Store {
	if pool == nil || len(pepper) == 0 {
		return nil
	}
	return &Store{pool: pool, pepper: pepper, now: time.Now}
}

// WithClock replaces time.Now (tests).
func (s *Store) WithClock(now func() time.Time) *Store {
	c := *s
	c.now = now
	return &c
}

// Now is the store's clock.
func (s *Store) Now() time.Time { return s.now() }

// Create issues a new request for a client.
func (s *Store) Create(ctx context.Context, st Start) (*Issued, error) {
	if st.ClientID != ClientCLI {
		return nil, ErrUnknownClient
	}
	now := s.now()
	st = clean(st)
	if st.IP != "" {
		var n int
		if err := s.pool.QueryRow(ctx,
			`SELECT count(*) FROM device_authorizations WHERE request_ip = $1 AND created_at > $2`,
			st.IP, now.Add(-PerIPWindow)).Scan(&n); err != nil {
			return nil, fmt.Errorf("deviceauth: count recent: %w", err)
		}
		if n >= PerIPMax {
			return nil, ErrRateLimited
		}
	}
	// Best effort: requests a day past expiry go.
	_, _ = s.pool.Exec(ctx, `DELETE FROM device_authorizations WHERE id IN (
		SELECT id FROM device_authorizations WHERE expires_at < $1 LIMIT 100)`, now.Add(-reapAfter))

	for range 5 {
		deviceCode, err := newDeviceCode()
		if err != nil {
			return nil, err
		}
		userCode, err := newUserCode()
		if err != nil {
			return nil, err
		}
		userHash := s.userCodeHash(userCode)
		// A waiting request whose code ran out frees its code.
		if _, err := s.pool.Exec(ctx, `DELETE FROM device_authorizations
			WHERE user_code_hash = $1 AND status = 'pending' AND expires_at <= $2`, userHash, now); err != nil {
			return nil, fmt.Errorf("deviceauth: free code: %w", err)
		}
		req := Request{
			ClientID: st.ClientID, ClientVersion: st.ClientVersion, DeviceName: st.DeviceName,
			DeviceOS: st.DeviceOS, Space: st.Space, IP: st.IP, Status: StatusPending,
			Interval: DefaultInterval, CreatedAt: now, ExpiresAt: now.Add(Lifetime),
		}
		err = s.pool.QueryRow(ctx, `INSERT INTO device_authorizations
			(device_code_hash, user_code_hash, client_id, client_version, device_name, device_os,
			 space_slug, request_ip, user_agent, interval_seconds, created_at, expires_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
			ON CONFLICT (user_code_hash) WHERE status = 'pending' DO NOTHING
			RETURNING id`,
			hashDeviceCode(deviceCode), userHash, st.ClientID, st.ClientVersion, st.DeviceName, st.DeviceOS,
			st.Space, st.IP, st.UserAgent, req.Interval, now, req.ExpiresAt).Scan(&req.ID)
		if errors.Is(err, pgx.ErrNoRows) {
			continue // another waiting request has this user code: draw again
		}
		if err != nil {
			return nil, fmt.Errorf("deviceauth: insert: %w", err)
		}
		req.UserCode = FormatUserCode(userCode)
		return &Issued{DeviceCode: deviceCode, UserCode: req.UserCode, Request: req}, nil
	}
	return nil, errors.New("deviceauth: could not draw a free user code")
}

// Poll is the CLI asking whether its code was confirmed. It returns the
// person to issue a session for exactly once, after which the code is
// used. Errors are ErrPending, ErrSlowDown (the code's interval grew),
// ErrExpired, ErrDenied, ErrUsed and ErrUnknownClient.
func (s *Store) Poll(ctx context.Context, clientID, deviceCode string) (uuid.UUID, error) {
	c, err := s.Collect(ctx, clientID, deviceCode)
	if err != nil {
		return uuid.Nil, err
	}
	return c.User, nil
}

// Collected is a confirmed code, collected: the person to issue a session
// for, and what the device said about itself (for the sessions list).
type Collected struct {
	User          uuid.UUID
	ClientVersion string
	DeviceName    string
	DeviceOS      string
}

// Collect is Poll, with what the device said about itself.
func (s *Store) Collect(ctx context.Context, clientID, deviceCode string) (*Collected, error) {
	if clientID != ClientCLI {
		return nil, ErrUnknownClient
	}
	if deviceCode == "" {
		return nil, ErrUsed
	}
	now := s.now()
	var user Collected
	// waiting is ErrPending or ErrSlowDown: the poll's bookkeeping commits
	// with that answer (BeginFunc rolls back when the function errs).
	var waiting error
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var (
			id       uuid.UUID
			client   string
			status   Status
			userID   *uuid.UUID
			interval int
			polled   *time.Time
			expires  time.Time
		)
		err := tx.QueryRow(ctx, `SELECT id, client_id, status, user_id, interval_seconds, last_polled_at, expires_at,
				client_version, device_name, device_os
			FROM device_authorizations WHERE device_code_hash = $1 FOR UPDATE`, hashDeviceCode(deviceCode)).
			Scan(&id, &client, &status, &userID, &interval, &polled, &expires,
				&user.ClientVersion, &user.DeviceName, &user.DeviceOS)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrUsed
		}
		if err != nil {
			return err
		}
		if client != clientID || status == StatusConsumed {
			return ErrUsed
		}
		if status == StatusDenied {
			return ErrDenied
		}
		if !now.Before(expires) {
			return ErrExpired
		}
		if status == StatusApproved && userID != nil {
			if _, err := tx.Exec(ctx, `UPDATE device_authorizations
				SET status = 'consumed', consumed_at = $2, last_polled_at = $2 WHERE id = $1`, id, now); err != nil {
				return err
			}
			user.User = *userID
			return nil
		}
		// Still waiting. Too soon after the last poll is slow_down, which
		// keeps the longer interval for the rest of the code's life.
		tooSoon := polled != nil && now.Sub(*polled) < time.Duration(interval)*time.Second
		if tooSoon {
			interval = min(interval+SlowDownStep, MaxInterval)
		}
		if _, err := tx.Exec(ctx, `UPDATE device_authorizations
			SET last_polled_at = $2, interval_seconds = $3 WHERE id = $1`, id, now, interval); err != nil {
			return err
		}
		waiting = ErrPending
		if tooSoon {
			waiting = ErrSlowDown
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if waiting != nil {
		return nil, waiting
	}
	return &user, nil
}

// Find is the request a user code names, as the person confirming it may
// see it: a waiting code, or one the viewer decided. Anything else is
// ErrNotFound, so the page never confirms that someone else's code exists.
func (s *Store) Find(ctx context.Context, userCode string, viewer uuid.UUID) (*Request, error) {
	norm, ok := NormalizeUserCode(userCode)
	if !ok {
		return nil, ErrNotFound
	}
	req, err := s.latest(ctx, s.pool, norm)
	if err != nil {
		return nil, err
	}
	if req.Status != StatusPending && (req.UserID == nil || *req.UserID != viewer) {
		return nil, ErrNotFound
	}
	return req, nil
}

// Approve confirms a waiting code for the viewer: the CLI's next poll
// collects a session for them. Approving a code the viewer already
// approved answers it as it is (a retry).
func (s *Store) Approve(ctx context.Context, userCode string, viewer uuid.UUID) (*Request, error) {
	return s.decide(ctx, userCode, viewer, StatusApproved)
}

// Deny declines a waiting code: the CLI hears access_denied.
func (s *Store) Deny(ctx context.Context, userCode string, viewer uuid.UUID) (*Request, error) {
	return s.decide(ctx, userCode, viewer, StatusDenied)
}

func (s *Store) decide(ctx context.Context, userCode string, viewer uuid.UUID, to Status) (*Request, error) {
	norm, ok := NormalizeUserCode(userCode)
	if !ok {
		return nil, ErrNotFound
	}
	now := s.now()
	var out *Request
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		req, err := s.latest(ctx, tx, norm)
		if err != nil {
			return err
		}
		if req.Status != StatusPending {
			if req.UserID == nil || *req.UserID != viewer {
				return ErrNotFound
			}
			state := req.StateAt(now)
			same := req.Status == to || (to == StatusApproved && req.Status == StatusConsumed)
			if !same {
				return &DecidedError{State: state}
			}
			out = req
			return nil
		}
		if !now.Before(req.ExpiresAt) {
			return &DecidedError{State: StateExpired}
		}
		tag, err := tx.Exec(ctx, `UPDATE device_authorizations
			SET status = $2, user_id = $3, decided_at = $4 WHERE id = $1 AND status = 'pending'`,
			req.ID, string(to), viewer, now)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return &DecidedError{State: req.StateAt(now)}
		}
		req.Status, req.UserID, req.DecidedAt = to, &viewer, &now
		out = req
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// latest is the newest request with the code, locked when q is a transaction.
func (s *Store) latest(ctx context.Context, q querier, norm string) (*Request, error) {
	lock := ""
	if _, ok := q.(pgx.Tx); ok {
		lock = " FOR UPDATE"
	}
	var r Request
	var status string
	err := q.QueryRow(ctx, `SELECT id, client_id, client_version, device_name, device_os, space_slug, request_ip,
			status, user_id, interval_seconds, created_at, expires_at, decided_at, consumed_at
		FROM device_authorizations WHERE user_code_hash = $1
		ORDER BY created_at DESC LIMIT 1`+lock, s.userCodeHash(norm)).
		Scan(&r.ID, &r.ClientID, &r.ClientVersion, &r.DeviceName, &r.DeviceOS, &r.Space, &r.IP,
			&status, &r.UserID, &r.Interval, &r.CreatedAt, &r.ExpiresAt, &r.DecidedAt, &r.ConsumedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("deviceauth: find: %w", err)
	}
	r.Status = Status(status)
	r.UserCode = FormatUserCode(norm)
	return &r, nil
}

// userCodeHash keys the code's hash with the server's secret.
func (s *Store) userCodeHash(norm string) string {
	mac := hmac.New(sha256.New, s.pepper)
	mac.Write([]byte("memax-device-user-code:" + norm))
	return hex.EncodeToString(mac.Sum(nil))
}

func hashDeviceCode(code string) string {
	sum := sha256.Sum256([]byte(code))
	return hex.EncodeToString(sum[:])
}

func newDeviceCode() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("deviceauth: random: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// newUserCode draws 4 consonants and 4 digits, normalised (no dash).
func newUserCode() (string, error) {
	var b strings.Builder
	for i, alphabet := range []string{userCodeLetters, userCodeLetters, userCodeLetters, userCodeLetters,
		userCodeDigits, userCodeDigits, userCodeDigits, userCodeDigits} {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		if err != nil {
			return "", fmt.Errorf("deviceauth: random %d: %w", i, err)
		}
		b.WriteByte(alphabet[n.Int64()])
	}
	return b.String(), nil
}

// NormalizeUserCode reads what a person typed: case, spaces and dashes
// don't matter, and among the digits an O reads as 0 and an I or L as 1.
// It returns the code without its dash, and whether it can be a code.
func NormalizeUserCode(s string) (string, bool) {
	var out []byte
	for _, r := range strings.ToUpper(s) {
		if r == '-' || r == '–' || r == '—' || unicode.IsSpace(r) {
			continue
		}
		if r > unicode.MaxASCII || len(out) >= 8 {
			return "", false
		}
		c := byte(r)
		if len(out) >= 4 {
			switch c {
			case 'O':
				c = '0'
			case 'I', 'L':
				c = '1'
			}
			if !strings.ContainsRune(userCodeDigits, rune(c)) {
				return "", false
			}
		} else if !strings.ContainsRune(userCodeLetters, rune(c)) {
			return "", false
		}
		out = append(out, c)
	}
	if len(out) != 8 {
		return "", false
	}
	return string(out), true
}

// FormatUserCode is a normalised code as people read it: "WQRT-4821".
func FormatUserCode(norm string) string {
	if len(norm) != 8 {
		return norm
	}
	return norm[:4] + "-" + norm[4:]
}

var (
	versionRE = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z.+-]{0,31}$`)
	slugRE    = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,48}[a-z0-9])?$`)
)

// clean keeps what the device says about itself short and printable, and
// drops what isn't shaped like what it claims to be.
func clean(st Start) Start {
	if !versionRE.MatchString(st.ClientVersion) {
		st.ClientVersion = ""
	}
	st.DeviceName = printable(st.DeviceName, 64)
	switch strings.ToLower(strings.TrimSpace(st.DeviceOS)) {
	case "darwin", "macos", "mac":
		st.DeviceOS = "macOS"
	case "linux":
		st.DeviceOS = "Linux"
	case "win32", "windows":
		st.DeviceOS = "Windows"
	case "freebsd":
		st.DeviceOS = "FreeBSD"
	case "":
		st.DeviceOS = ""
	default:
		st.DeviceOS = "other"
	}
	if !slugRE.MatchString(st.Space) {
		st.Space = ""
	}
	st.UserAgent = printable(st.UserAgent, 256)
	st.IP = printable(st.IP, 64)
	return st
}

// printable trims s, drops control and format characters (bidi controls
// among them, so a name can't reorder the page), and caps it at max runes.
func printable(s string, max int) string {
	var b strings.Builder
	n := 0
	for _, r := range strings.TrimSpace(s) {
		if n >= max {
			break
		}
		if r == utf8.RuneError || unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			continue
		}
		b.WriteRune(r)
		n++
	}
	return strings.TrimSpace(b.String())
}
