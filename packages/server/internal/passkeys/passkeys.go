// Package passkeys keeps people's passkeys (WebAuthn credentials) and runs
// the three ceremonies Memax uses them for (plan 25 §5.15; migration 051):
//
//   - Registration: a person on the web adds a passkey to their account.
//     Discoverable credentials only (residentKey required), user
//     verification required, attestation "none": Memax doesn't care which
//     authenticator it is, only that the person unlocked it. Excluded: the
//     person's passkeys already registered, so one authenticator isn't
//     added twice.
//   - Sign-in: a discoverable assertion (no user named first) signs the
//     person in on the web. The credential names its owner (the user handle
//     is the person's id).
//   - The re-check: before a decision only the person should make, the web
//     gets a challenge bound to the person, their session (the access
//     token's sid) and the SHA-256 of the exact request (method, target,
//     Idempotency-Key, If-Match, body), and retries that request with the
//     assertion. It verifies only for that person, that session and that
//     request, within ChallengeTTL, once.
//
// # Why go-webauthn
//
// WebAuthn verification is parsing attacker-supplied CBOR and COSE keys and
// checking a dozen steps of the Level 3 spec (client data type, challenge,
// origin and top origin, RP ID hash, the UP, UV, BE and BS flags, the
// signature over authenticator data and the client data hash, sign count).
// github.com/go-webauthn/webauthn (BSD-3-Clause, maintained, used by
// Gitea, Authelia and Ory) implements and fuzzes all of it; a hand-rolled
// CBOR/COSE parser in an authentication path is the riskier choice. Its
// transitive dependencies (CBOR, TPM and JWT parsing for attestation
// formats Memax never requests) cost binary size, not attack surface: with
// attestation "none" those paths never run.
//
// # Storage
//
// Credentials and challenges live in v2.passkeys and
// v2.passkey_challenges, under RLS keyed on app.person_id: every statement
// here runs as memax_v2 with the person set. Signing in finds a credential's
// owner through v2.passkey_owner, which answers only the person. A
// challenge is used once: the statement that verifies it marks it, inside
// the transaction that checks it was unused, so two assertions racing for
// one challenge can't both pass.
//
// # Configuration
//
// WEBAUTHN_RP_ID is the relying party: the registrable domain passkeys are
// bound to (memax.app in production; the staging host in staging, so
// staging's passkeys never show up on memax.app; localhost in development).
// WEBAUTHN_RP_ORIGINS lists the web app's origins (comma-separated), each
// on the RP ID or a subdomain of it, https except on localhost. Both
// default from APP_BASE_URL. With neither, passkeys are off: New returns
// nil, and every decision keeps today's human_web rule.
package passkeys

import (
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Defaults.
const (
	// ChallengeTTL is how long a challenge stays usable: long enough to
	// find a phone or a security key, short enough that an assertion is
	// fresh. The database refuses anything above 10 minutes.
	ChallengeTTL = 5 * time.Minute
	// EnrollWindow is how recent a sign-in must be to add a passkey without
	// an existing one (policy.DecidePasskey).
	EnrollWindow = 10 * time.Minute
	// MaxPasskeys is how many passkeys one person may have.
	MaxPasskeys = 20
	// MaxNameRunes bounds a passkey's name.
	MaxNameRunes = 64
	// Role is the Postgres role every statement here runs as.
	Role = "memax_v2"
)

// Config is the relying party.
type Config struct {
	// RPID is the domain passkeys are bound to.
	RPID string
	// RPName is what an authenticator shows ("Memax").
	RPName string
	// Origins are the web app's origins.
	Origins []string
	// ChallengeTTL defaults to ChallengeTTL.
	ChallengeTTL time.Duration
}

// ConfigFromEnv reads the relying party from the environment (getenv is
// os.Getenv). ok is false when passkeys aren't configured.
func ConfigFromEnv(getenv func(string) string) (cfg Config, ok bool, err error) {
	app := strings.TrimSpace(getenv("APP_BASE_URL"))
	cfg.RPID = strings.ToLower(strings.TrimSpace(getenv("WEBAUTHN_RP_ID")))
	cfg.RPName = strings.TrimSpace(getenv("WEBAUTHN_RP_NAME"))
	if raw := strings.TrimSpace(getenv("WEBAUTHN_RP_ORIGINS")); raw != "" {
		for _, o := range strings.Split(raw, ",") {
			if o = strings.TrimRight(strings.TrimSpace(o), "/"); o != "" {
				cfg.Origins = append(cfg.Origins, o)
			}
		}
	}
	if app != "" {
		u, err := url.Parse(app)
		if err != nil || u.Host == "" {
			return cfg, false, fmt.Errorf("passkeys: APP_BASE_URL %q is not a URL", app)
		}
		if cfg.RPID == "" {
			cfg.RPID = strings.ToLower(u.Hostname())
		}
		if len(cfg.Origins) == 0 {
			cfg.Origins = []string{u.Scheme + "://" + u.Host}
		}
	}
	if cfg.RPID == "" {
		return cfg, false, nil
	}
	if cfg.RPName == "" {
		cfg.RPName = "Memax"
	}
	return cfg, true, cfg.validate()
}

// validate checks every origin is on the RP ID, over https unless local.
func (c Config) validate() error {
	if c.RPID == "" {
		return errors.New("passkeys: no relying party ID")
	}
	if len(c.Origins) == 0 {
		return errors.New("passkeys: no origins; set WEBAUTHN_RP_ORIGINS or APP_BASE_URL")
	}
	for _, o := range c.Origins {
		u, err := url.Parse(o)
		if err != nil || u.Host == "" || u.Path != "" {
			return fmt.Errorf("passkeys: origin %q must be scheme://host[:port]", o)
		}
		host := strings.ToLower(u.Hostname())
		if host != c.RPID && !strings.HasSuffix(host, "."+c.RPID) {
			return fmt.Errorf("passkeys: origin %q is not on the relying party %q", o, c.RPID)
		}
		local := host == "localhost" || strings.HasSuffix(host, ".localhost")
		if u.Scheme != "https" && !(u.Scheme == "http" && local) {
			return fmt.Errorf("passkeys: origin %q must use https (http only on localhost)", o)
		}
	}
	return nil
}

// Service runs the ceremonies and keeps the credentials. A nil *Service
// means passkeys are off; callers check.
type Service struct {
	wa   *webauthn.WebAuthn
	cfg  Config
	pool *pgxpool.Pool
	now  func() time.Time
	log  *slog.Logger
}

// Option configures a Service.
type Option func(*Service)

// WithClock replaces time.Now (tests).
func WithClock(now func() time.Time) Option { return func(s *Service) { s.now = now } }

// WithLogger sets the logger.
func WithLogger(l *slog.Logger) Option { return func(s *Service) { s.log = l } }

// New returns the service for cfg.
func New(pool *pgxpool.Pool, cfg Config, opts ...Option) (*Service, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	if cfg.ChallengeTTL <= 0 || cfg.ChallengeTTL > 10*time.Minute {
		cfg.ChallengeTTL = ChallengeTTL
	}
	// The browser gets the TTL as its timeout; the challenge row's expires_at
	// is what Memax enforces (on its own clock), so go-webauthn doesn't.
	timeout := webauthn.TimeoutConfig{Timeout: cfg.ChallengeTTL, TimeoutUVD: cfg.ChallengeTTL}
	wa, err := webauthn.New(&webauthn.Config{
		RPID:          cfg.RPID,
		RPDisplayName: cfg.RPName,
		RPOrigins:     cfg.Origins,
		// Discoverable, user-verified passkeys; Memax doesn't ask which
		// authenticator made them.
		AttestationPreference: protocol.PreferNoAttestation,
		AuthenticatorSelection: protocol.AuthenticatorSelection{
			ResidentKey:        protocol.ResidentKeyRequirementRequired,
			RequireResidentKey: protocol.ResidentKeyRequired(),
			UserVerification:   protocol.VerificationRequired,
		},
		Timeouts: webauthn.TimeoutsConfig{Login: timeout, Registration: timeout},
		// Memax requests no extensions. Password managers sometimes return
		// outputs nobody asked for; they carry nothing Memax reads, so they
		// don't fail the ceremony.
		ExtensionsUnsolicitedOutputPolicy: protocol.UnsolicitedOutputPolicyIgnore,
	})
	if err != nil {
		return nil, fmt.Errorf("passkeys: %w", err)
	}
	s := &Service{wa: wa, cfg: cfg, pool: pool, now: time.Now, log: slog.Default()}
	for _, o := range opts {
		o(s)
	}
	return s, nil
}

// RPID is the relying party's ID.
func (s *Service) RPID() string { return s.cfg.RPID }

// Reason says why a passkey ceremony failed. It is safe to log and to show:
// it never carries the assertion.
type Reason string

// The reasons.
const (
	ReasonMalformed     Reason = "malformed"      // not a WebAuthn response
	ReasonUnknown       Reason = "unknown"        // no such challenge for this person
	ReasonExpired       Reason = "expired"        // the challenge's time ran out
	ReasonUsed          Reason = "used"           // the challenge was already used
	ReasonOtherSession  Reason = "other_session"  // a check from another session
	ReasonOtherRequest  Reason = "other_request"  // a check for another request
	ReasonNoCredential  Reason = "no_credential"  // the passkey isn't registered (or isn't theirs)
	ReasonInvalid       Reason = "invalid"        // the signature, origin, RP ID or flags don't verify
	ReasonNotVerified   Reason = "not_verified"   // the authenticator didn't verify the user (UV)
	ReasonCloned        Reason = "cloned"         // the sign count went backwards
	ReasonExists        Reason = "exists"         // that passkey is registered already
	ReasonLimit         Reason = "limit"          // MaxPasskeys reached
	ReasonNoPasskey     Reason = "no_passkey"     // the person has none to check with
	ReasonSessionEnded  Reason = "session_ended"  // the session was signed out
	ReasonSessionNeeded Reason = "session_needed" // the token names no session
)

// Error is a ceremony that failed.
type Error struct {
	Reason Reason
	err    error
}

func (e *Error) Error() string {
	if e.err != nil {
		return "passkeys: " + string(e.Reason) + ": " + e.err.Error()
	}
	return "passkeys: " + string(e.Reason)
}

func (e *Error) Unwrap() error { return e.err }

func fail(r Reason, err error) error { return &Error{Reason: r, err: err} }

// ReasonOf is err's Reason, or "" when err isn't a ceremony failure.
func ReasonOf(err error) Reason {
	var e *Error
	if errors.As(err, &e) {
		return e.Reason
	}
	return ""
}

// ErrNotFound: no such passkey of this person's.
var ErrNotFound = errors.New("passkeys: no such passkey")
