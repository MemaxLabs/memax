package passkeys

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// The wire forms of the options, as WebAuthn Level 3's JSON types define
// them (PublicKeyCredentialCreationOptionsJSON and
// PublicKeyCredentialRequestOptionsJSON): binary values are base64url, so
// a browser hands them to PublicKeyCredential.parseCreationOptionsFromJSON
// and parseRequestOptionsFromJSON as they are.

// RelyingParty is the rp entity.
type RelyingParty struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// UserEntity is the user entity: id is the user handle (the person's id).
type UserEntity struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
}

// CredentialParameter is one algorithm Memax accepts.
type CredentialParameter struct {
	Type string `json:"type"`
	Alg  int    `json:"alg"`
}

// Descriptor names a credential.
type Descriptor struct {
	Type       string   `json:"type"`
	ID         string   `json:"id"`
	Transports []string `json:"transports,omitempty"`
}

// Selection is the authenticator selection: discoverable and verified.
type Selection struct {
	ResidentKey        string `json:"residentKey"`
	RequireResidentKey bool   `json:"requireResidentKey"`
	UserVerification   string `json:"userVerification"`
}

// CreationOptions is what navigator.credentials.create takes.
type CreationOptions struct {
	RP                     RelyingParty          `json:"rp"`
	User                   UserEntity            `json:"user"`
	Challenge              string                `json:"challenge"`
	PubKeyCredParams       []CredentialParameter `json:"pubKeyCredParams"`
	Timeout                int                   `json:"timeout"`
	ExcludeCredentials     []Descriptor          `json:"excludeCredentials"`
	AuthenticatorSelection Selection             `json:"authenticatorSelection"`
	Attestation            string                `json:"attestation"`
}

// RequestOptions is what navigator.credentials.get takes.
type RequestOptions struct {
	Challenge        string       `json:"challenge"`
	Timeout          int          `json:"timeout"`
	RPID             string       `json:"rpId"`
	AllowCredentials []Descriptor `json:"allowCredentials"`
	UserVerification string       `json:"userVerification"`
}

// The purposes a challenge is issued for.
const (
	purposeRegister = "register"
	purposeSignIn   = "sign_in"
	purposeCheck    = "check"
)

// Binding is what a re-check is bound to: the person, the session they ask
// from, and the request it confirms (Action: the lowercase hex SHA-256 the
// API computes over the request, see v2api's actionHash).
type Binding struct {
	Person  uuid.UUID
	Session uuid.UUID
	Action  string
}

var hex64 = regexp.MustCompile(`^[0-9a-f]{64}$`)

func (b Binding) validate() error {
	switch {
	case b.Person == uuid.Nil:
		return fail(ReasonUnknown, errors.New("no person"))
	case b.Session == uuid.Nil:
		return fail(ReasonSessionNeeded, nil)
	case !hex64.MatchString(b.Action):
		return fail(ReasonOtherRequest, errors.New("the request isn't named"))
	}
	return nil
}

// user is a person as go-webauthn sees them.
type user struct {
	id          uuid.UUID
	name        string
	displayName string
	creds       []webauthn.Credential
}

func (u *user) WebAuthnID() []byte                         { return u.id[:] }
func (u *user) WebAuthnName() string                       { return u.name }
func (u *user) WebAuthnDisplayName() string                { return u.displayName }
func (u *user) WebAuthnCredentials() []webauthn.Credential { return u.creds }

func newUser(id uuid.UUID, recs []*record) *user {
	u := &user{id: id, name: id.String(), displayName: id.String()}
	for _, r := range recs {
		u.creds = append(u.creds, r.credential())
	}
	return u
}

func descriptor(d protocol.CredentialDescriptor) Descriptor {
	out := Descriptor{Type: string(d.Type), ID: d.CredentialID.String()}
	for _, t := range d.Transport {
		out.Transports = append(out.Transports, string(t))
	}
	return out
}

func descriptors(ds []protocol.CredentialDescriptor) []Descriptor {
	out := make([]Descriptor, 0, len(ds))
	for _, d := range ds {
		out = append(out, descriptor(d))
	}
	return out
}

func requestOptions(a *protocol.CredentialAssertion) *RequestOptions {
	o := a.Response
	return &RequestOptions{
		Challenge: o.Challenge.String(), Timeout: o.Timeout, RPID: o.RelyingPartyID,
		AllowCredentials: descriptors(o.AllowedCredentials), UserVerification: string(o.UserVerification),
	}
}

// saveChallenge records a challenge and clears out a few long-expired ones
// the transaction can see.
func (s *Service) saveChallenge(ctx context.Context, tx pgx.Tx, purpose string, person, session uuid.UUID, action string, sd *webauthn.SessionData) (time.Time, error) {
	now := s.now()
	expires := now.Add(s.cfg.ChallengeTTL)
	raw, err := json.Marshal(sd)
	if err != nil {
		return time.Time{}, fmt.Errorf("passkeys: challenge: %w", err)
	}
	var personArg, sessionArg, actionArg any
	if person != uuid.Nil {
		personArg = person
	}
	if session != uuid.Nil {
		sessionArg = session
	}
	if action != "" {
		actionArg = action
	}
	if _, err := tx.Exec(ctx, `SELECT v2.clear_passkey_challenges($1, false)`, now.Add(-time.Hour)); err != nil {
		return time.Time{}, fmt.Errorf("passkeys: clear challenges: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO v2.passkey_challenges (challenge, purpose, person_id, session_id, action_sha256, ceremony, created_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		sd.Challenge, purpose, personArg, sessionArg, actionArg, raw, now, expires); err != nil {
		return time.Time{}, fmt.Errorf("passkeys: save challenge: %w", err)
	}
	return expires, nil
}

// challengeRow is a stored challenge.
type challengeRow struct {
	purpose   string
	person    *uuid.UUID
	session   *uuid.UUID
	action    *string
	ceremony  []byte
	expiresAt time.Time
	usedAt    *time.Time
}

// takeChallenge locks a challenge the transaction can see (RLS: the
// person's own, or a sign-in one), and refuses one that is unknown, of
// another purpose, expired or used.
func (s *Service) takeChallenge(ctx context.Context, tx pgx.Tx, challenge, purpose string) (*challengeRow, *webauthn.SessionData, error) {
	var c challengeRow
	err := tx.QueryRow(ctx, `
		SELECT purpose, person_id, session_id, action_sha256, ceremony, expires_at, used_at
		  FROM v2.passkey_challenges WHERE challenge = $1 FOR UPDATE`, challenge).
		Scan(&c.purpose, &c.person, &c.session, &c.action, &c.ceremony, &c.expiresAt, &c.usedAt)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, nil, fail(ReasonUnknown, nil)
	case err != nil:
		return nil, nil, fmt.Errorf("passkeys: read challenge: %w", err)
	case c.purpose != purpose:
		return nil, nil, fail(ReasonUnknown, fmt.Errorf("a %s challenge, not %s", c.purpose, purpose))
	case c.usedAt != nil:
		return nil, nil, fail(ReasonUsed, nil)
	case !s.now().Before(c.expiresAt):
		return nil, nil, fail(ReasonExpired, nil)
	}
	var sd webauthn.SessionData
	if err := json.Unmarshal(c.ceremony, &sd); err != nil {
		return nil, nil, fmt.Errorf("passkeys: read challenge: %w", err)
	}
	return &c, &sd, nil
}

func useChallenge(ctx context.Context, tx pgx.Tx, challenge string, at time.Time) error {
	tag, err := tx.Exec(ctx, `UPDATE v2.passkey_challenges SET used_at = $2 WHERE challenge = $1 AND used_at IS NULL`, challenge, at)
	if err != nil {
		return fmt.Errorf("passkeys: use challenge: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return fail(ReasonUsed, nil)
	}
	return nil
}

// verifyFailure names what go-webauthn refused.
func verifyFailure(err error) error {
	var pe *protocol.Error
	if errors.As(err, &pe) {
		return fail(ReasonInvalid, fmt.Errorf("%s: %s", pe.Type, pe.Details))
	}
	return fail(ReasonInvalid, err)
}

// ---------------------------------------------------------------------
// Registration
// ---------------------------------------------------------------------

// BeginRegistration starts adding a passkey to the person's account from
// session: the options for navigator.credentials.create, and when they
// expire. Whether they may (a fresh sign-in, or their passkey) is the
// caller's to decide (policy.DecidePasskey).
func (s *Service) BeginRegistration(ctx context.Context, person, session uuid.UUID) (*CreationOptions, time.Time, error) {
	if session == uuid.Nil {
		return nil, time.Time{}, fail(ReasonSessionNeeded, nil)
	}
	name, display, err := s.personNames(ctx, person)
	if err != nil {
		return nil, time.Time{}, err
	}
	var out *CreationOptions
	var expires time.Time
	err = s.inPerson(ctx, person, func(tx pgx.Tx) error {
		recs, err := loadRecords(ctx, tx, person)
		if err != nil {
			return err
		}
		if len(recs) >= MaxPasskeys {
			return fail(ReasonLimit, nil)
		}
		u := newUser(person, recs)
		u.name, u.displayName = name, display
		creation, sd, err := s.wa.BeginRegistration(u, webauthn.WithExclusions(webauthn.Credentials(u.creds).CredentialDescriptors()))
		if err != nil {
			return fmt.Errorf("passkeys: begin registration: %w", err)
		}
		if expires, err = s.saveChallenge(ctx, tx, purposeRegister, person, session, "", sd); err != nil {
			return err
		}
		o := creation.Response
		out = &CreationOptions{
			RP:        RelyingParty{ID: o.RelyingParty.ID, Name: o.RelyingParty.Name},
			User:      UserEntity{ID: protocol.URLEncodedBase64(u.WebAuthnID()).String(), Name: name, DisplayName: display},
			Challenge: o.Challenge.String(), Timeout: o.Timeout,
			ExcludeCredentials: descriptors(o.CredentialExcludeList),
			AuthenticatorSelection: Selection{
				ResidentKey: string(o.AuthenticatorSelection.ResidentKey), RequireResidentKey: true,
				UserVerification: string(o.AuthenticatorSelection.UserVerification),
			},
			Attestation: string(o.Attestation),
		}
		for _, p := range o.Parameters {
			out.PubKeyCredParams = append(out.PubKeyCredParams, CredentialParameter{Type: string(p.Type), Alg: int(p.Algorithm)})
		}
		return nil
	})
	return out, expires, err
}

// FinishRegistration verifies the authenticator's answer to a registration
// begun from the same session and stores the passkey under name (a
// default from its provider when empty).
func (s *Service) FinishRegistration(ctx context.Context, person, session uuid.UUID, response []byte, name string) (*Passkey, error) {
	parsed, err := protocol.ParseCredentialCreationResponseBytes(response)
	if err != nil {
		return nil, fail(ReasonMalformed, err)
	}
	var out *Passkey
	err = s.inPerson(ctx, person, func(tx pgx.Tx) error {
		c, sd, err := s.takeChallenge(ctx, tx, parsed.Response.CollectedClientData.Challenge, purposeRegister)
		if err != nil {
			return err
		}
		if c.person == nil || *c.person != person {
			return fail(ReasonUnknown, nil)
		}
		if c.session == nil || *c.session != session {
			return fail(ReasonOtherSession, nil)
		}
		if !parsed.Response.AttestationObject.AuthData.Flags.UserVerified() {
			return fail(ReasonNotVerified, nil)
		}
		recs, err := loadRecords(ctx, tx, person)
		if err != nil {
			return err
		}
		if len(recs) >= MaxPasskeys {
			return fail(ReasonLimit, nil)
		}
		cred, err := s.wa.CreateCredential(newUser(person, recs), *sd, parsed)
		if err != nil {
			return verifyFailure(err)
		}
		now := s.now()
		if err := useChallenge(ctx, tx, sd.Challenge, now); err != nil {
			return err
		}
		var aaguid *uuid.UUID
		if id, err := uuid.FromBytes(cred.Authenticator.AAGUID); err == nil && id != uuid.Nil {
			aaguid = &id
		}
		if clean, ok := CleanName(name); ok {
			name = clean
		} else {
			name = "Passkey"
			if aaguid != nil && ProviderName(*aaguid) != "" {
				name = ProviderName(*aaguid)
			}
		}
		transports := make([]string, 0, len(cred.Transport))
		for _, t := range cred.Transport {
			transports = append(transports, string(t))
		}
		r, err := scanRecord(tx.QueryRow(ctx, `
			INSERT INTO v2.passkeys (id, person_id, credential_id, public_key, sign_count, transports, backup_eligible,
			                         backed_up, aaguid, attestation_format, name, created_at, created_session_id)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
			RETURNING `+recordColumns,
			uuid.Must(uuid.NewV7()), person, cred.ID, cred.PublicKey, int64(cred.Authenticator.SignCount), transports,
			cred.Flags.BackupEligible, cred.Flags.BackupState, aaguid, nonEmpty(cred.AttestationFormat, "none"), name, now, session))
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return fail(ReasonExists, nil)
		}
		if err != nil {
			return fmt.Errorf("passkeys: store: %w", err)
		}
		out = &r.Passkey
		return nil
	})
	return out, err
}

func nonEmpty(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// personNames are the person's email (the passkey's account name, as an
// authenticator lists it) and display name. They are read as the login
// role, like the rest of identity.
func (s *Service) personNames(ctx context.Context, person uuid.UUID) (string, string, error) {
	var email, display string
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(email, ''), COALESCE(NULLIF(display_name, ''), NULLIF(name, ''), email, '')
		  FROM public.users WHERE id = $1`, person).Scan(&email, &display)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", fail(ReasonUnknown, errors.New("no such person"))
	}
	if err != nil {
		return "", "", fmt.Errorf("passkeys: read person: %w", err)
	}
	if email == "" {
		email = person.String()
	}
	if display == "" {
		display = email
	}
	return email, display, nil
}

// ---------------------------------------------------------------------
// Sign-in
// ---------------------------------------------------------------------

// BeginSignIn starts a sign-in with any passkey: the options for
// navigator.credentials.get, naming no one, and when they expire.
func (s *Service) BeginSignIn(ctx context.Context) (*RequestOptions, time.Time, error) {
	var out *RequestOptions
	var expires time.Time
	err := s.inPerson(ctx, uuid.Nil, func(tx pgx.Tx) error {
		a, sd, err := s.wa.BeginDiscoverableLogin(webauthn.WithUserVerification(protocol.VerificationRequired))
		if err != nil {
			return fmt.Errorf("passkeys: begin sign-in: %w", err)
		}
		if expires, err = s.saveChallenge(ctx, tx, purposeSignIn, uuid.Nil, uuid.Nil, "", sd); err != nil {
			return err
		}
		out = requestOptions(a)
		return nil
	})
	return out, expires, err
}

// FinishSignIn verifies a sign-in assertion and returns whose passkey
// signed in. The caller starts the web session.
func (s *Service) FinishSignIn(ctx context.Context, assertion []byte) (uuid.UUID, *Passkey, error) {
	parsed, err := protocol.ParseCredentialRequestResponseBytes(assertion)
	if err != nil {
		return uuid.Nil, nil, fail(ReasonMalformed, err)
	}
	var owner *uuid.UUID
	if err := s.inPerson(ctx, uuid.Nil, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT v2.passkey_owner($1)`, parsed.RawID).Scan(&owner)
	}); err != nil {
		return uuid.Nil, nil, err
	}
	if owner == nil {
		return uuid.Nil, nil, fail(ReasonNoCredential, nil)
	}
	person := *owner
	if !bytes.Equal(parsed.Response.UserHandle, person[:]) {
		return uuid.Nil, nil, fail(ReasonNoCredential, errors.New("the user handle isn't the credential's owner"))
	}
	var used *Passkey
	err = s.inPerson(ctx, person, func(tx pgx.Tx) error {
		_, sd, err := s.takeChallenge(ctx, tx, parsed.Response.CollectedClientData.Challenge, purposeSignIn)
		if err != nil {
			return err
		}
		used, err = s.verifyAssertion(ctx, tx, person, sd, parsed, true)
		return err
	})
	if err != nil {
		return uuid.Nil, nil, err
	}
	return person, used, nil
}

// verifyAssertion checks an assertion against the person's passkeys,
// uses its challenge, and records the passkey's use.
func (s *Service) verifyAssertion(ctx context.Context, tx pgx.Tx, person uuid.UUID, sd *webauthn.SessionData, parsed *protocol.ParsedCredentialAssertionData, discoverable bool) (*Passkey, error) {
	recs, err := loadRecords(ctx, tx, person)
	if err != nil {
		return nil, err
	}
	var rec *record
	for _, r := range recs {
		if bytes.Equal(r.credentialID, parsed.RawID) {
			rec = r
		}
	}
	if rec == nil {
		return nil, fail(ReasonNoCredential, nil)
	}
	if !parsed.Response.AuthenticatorData.Flags.UserVerified() {
		return nil, fail(ReasonNotVerified, nil)
	}
	u := newUser(person, recs)
	var cred *webauthn.Credential
	if discoverable {
		_, cred, err = s.wa.ValidatePasskeyLogin(func(_, _ []byte) (webauthn.User, error) { return u, nil }, *sd, parsed)
	} else {
		cred, err = s.wa.ValidateLogin(u, *sd, parsed)
	}
	if err != nil {
		return nil, verifyFailure(err)
	}
	if cred.Authenticator.CloneWarning {
		s.log.WarnContext(ctx, "passkeys: a passkey's sign count went backwards; refused it as a possible copy",
			"person_id", person.String(), "passkey_id", rec.ID.String())
		return nil, fail(ReasonCloned, nil)
	}
	now := s.now()
	if err := useChallenge(ctx, tx, sd.Challenge, now); err != nil {
		return nil, err
	}
	r, err := scanRecord(tx.QueryRow(ctx, `
		UPDATE v2.passkeys SET sign_count = $3, backed_up = $4, last_used_at = $5
		 WHERE id = $1 AND person_id = $2 RETURNING `+recordColumns,
		rec.ID, person, int64(cred.Authenticator.SignCount), cred.Flags.BackupState && rec.BackupEligible, now))
	if err != nil {
		return nil, fmt.Errorf("passkeys: record use: %w", err)
	}
	return &r.Passkey, nil
}

// ---------------------------------------------------------------------
// The re-check
// ---------------------------------------------------------------------

// BeginCheck issues a challenge for one request (b): the options for
// navigator.credentials.get, listing the person's passkeys, and when they
// expire.
func (s *Service) BeginCheck(ctx context.Context, b Binding) (*RequestOptions, time.Time, error) {
	if err := b.validate(); err != nil {
		return nil, time.Time{}, err
	}
	var out *RequestOptions
	var expires time.Time
	err := s.inPerson(ctx, b.Person, func(tx pgx.Tx) error {
		recs, err := loadRecords(ctx, tx, b.Person)
		if err != nil {
			return err
		}
		if len(recs) == 0 {
			return fail(ReasonNoPasskey, nil)
		}
		a, sd, err := s.wa.BeginLogin(newUser(b.Person, recs), webauthn.WithUserVerification(protocol.VerificationRequired))
		if err != nil {
			return fmt.Errorf("passkeys: begin check: %w", err)
		}
		if expires, err = s.saveChallenge(ctx, tx, purposeCheck, b.Person, b.Session, b.Action, sd); err != nil {
			return err
		}
		out = requestOptions(a)
		return nil
	})
	return out, expires, err
}

// VerifyCheck verifies a re-check: the assertion answers a challenge
// issued to this person, from this session, for this request, unexpired
// and unused, signed by one of their passkeys with the user verified. The
// challenge is used up whatever the request then does.
func (s *Service) VerifyCheck(ctx context.Context, b Binding, assertion []byte) (*Passkey, error) {
	if err := b.validate(); err != nil {
		return nil, err
	}
	parsed, err := protocol.ParseCredentialRequestResponseBytes(assertion)
	if err != nil {
		return nil, fail(ReasonMalformed, err)
	}
	var used *Passkey
	err = s.inPerson(ctx, b.Person, func(tx pgx.Tx) error {
		c, sd, err := s.takeChallenge(ctx, tx, parsed.Response.CollectedClientData.Challenge, purposeCheck)
		if err != nil {
			return err
		}
		switch {
		case c.person == nil || *c.person != b.Person:
			return fail(ReasonUnknown, nil)
		case c.session == nil || *c.session != b.Session:
			return fail(ReasonOtherSession, nil)
		case c.action == nil || *c.action != b.Action:
			return fail(ReasonOtherRequest, nil)
		}
		used, err = s.verifyAssertion(ctx, tx, b.Person, sd, parsed, false)
		return err
	})
	return used, err
}
