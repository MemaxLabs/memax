// Package passkeytest is a virtual WebAuthn authenticator for tests: the
// part of a platform authenticator (and the browser in front of it) that
// creates passkeys and signs assertions, on ES256 keys, with attestation
// "none". It speaks the JSON forms a browser's toJSON() produces, so tests
// drive internal/passkeys and the /v2 endpoints exactly as the web app does.
//
// Knobs let a test break what a real authenticator would never get wrong:
// skip user verification, sign for another origin or RP ID, or turn its
// sign count back (a cloned key).
package passkeytest

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/go-webauthn/webauthn/protocol/webauthncbor"
	"github.com/google/uuid"
)

// Authenticator holds passkeys for one relying party, as a person's
// device (or password manager) would.
type Authenticator struct {
	// Origin is where the "browser" says the ceremony ran.
	Origin string
	// AAGUID is the make it reports (iCloud Keychain's by default).
	AAGUID uuid.UUID
	// SkipUV leaves the user-verified flag off, as an authenticator that
	// only tested presence would.
	SkipUV bool
	// RPID, when set, signs for this RP ID instead of the options' one.
	RPID string
	// Counting makes the sign count go up with each assertion, as a
	// hardware key's does (synced passkeys report 0).
	Counting bool

	creds []*Credential
}

// Credential is one passkey the authenticator holds.
type Credential struct {
	ID         []byte
	RPID       string
	UserHandle []byte
	key        *ecdsa.PrivateKey
	// Count is its sign count.
	Count uint32
}

// New returns an authenticator for origin (scheme://host[:port]).
func New(origin string) *Authenticator {
	return &Authenticator{Origin: origin, AAGUID: uuid.MustParse("fbfc3007-154e-4ecc-8c0b-6e020557d7bd")}
}

// Credentials lists the passkeys it holds.
func (a *Authenticator) Credentials() []*Credential { return a.creds }

var b64 = base64.RawURLEncoding

// creation is the part of PublicKeyCredentialCreationOptionsJSON it reads.
type creation struct {
	RP struct {
		ID string `json:"id"`
	} `json:"rp"`
	User struct {
		ID string `json:"id"`
	} `json:"user"`
	Challenge          string `json:"challenge"`
	ExcludeCredentials []struct {
		ID string `json:"id"`
	} `json:"excludeCredentials"`
}

// request is the part of PublicKeyCredentialRequestOptionsJSON it reads.
type request struct {
	Challenge        string `json:"challenge"`
	RPID             string `json:"rpId"`
	AllowCredentials []struct {
		ID string `json:"id"`
	} `json:"allowCredentials"`
}

// ErrExcluded is navigator.credentials.create's InvalidStateError: the
// authenticator already holds one of the excluded credentials.
var ErrExcluded = errors.New("passkeytest: the authenticator holds an excluded credential")

// ErrNoCredential is navigator.credentials.get finding nothing to use.
var ErrNoCredential = errors.New("passkeytest: no passkey for this request")

// Create answers navigator.credentials.create: options is the
// PublicKeyCredentialCreationOptionsJSON, the result the
// RegistrationResponseJSON a browser's credential.toJSON() gives.
func (a *Authenticator) Create(options []byte) ([]byte, error) {
	var o creation
	if err := json.Unmarshal(options, &o); err != nil {
		return nil, err
	}
	for _, ex := range o.ExcludeCredentials {
		for _, c := range a.creds {
			if ex.ID == b64.EncodeToString(c.ID) {
				return nil, ErrExcluded
			}
		}
	}
	handle, err := b64.DecodeString(o.User.ID)
	if err != nil {
		return nil, fmt.Errorf("passkeytest: user.id: %w", err)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	id := make([]byte, 32)
	if _, err := rand.Read(id); err != nil {
		return nil, err
	}
	rpID := o.RP.ID
	if a.RPID != "" {
		rpID = a.RPID
	}
	c := &Credential{ID: id, RPID: o.RP.ID, UserHandle: handle, key: key}

	pub, err := coseKey(&key.PublicKey)
	if err != nil {
		return nil, err
	}
	auth := authData(rpID, a.flags()|0x40, 0)
	auth = append(auth, a.AAGUID[:]...)
	auth = binary.BigEndian.AppendUint16(auth, uint16(len(id)))
	auth = append(auth, id...)
	auth = append(auth, pub...)
	att, err := webauthncbor.Marshal(map[string]any{"fmt": "none", "attStmt": map[string]any{}, "authData": auth})
	if err != nil {
		return nil, err
	}
	client := a.clientData("webauthn.create", o.Challenge)
	a.creds = append(a.creds, c)
	return json.Marshal(map[string]any{
		"id": b64.EncodeToString(id), "rawId": b64.EncodeToString(id), "type": "public-key",
		"authenticatorAttachment": "platform", "clientExtensionResults": map[string]any{},
		"response": map[string]any{
			"clientDataJSON": b64.EncodeToString(client), "attestationObject": b64.EncodeToString(att),
			"transports": []string{"hybrid", "internal"},
		},
	})
}

// Get answers navigator.credentials.get: options is the
// PublicKeyCredentialRequestOptionsJSON, the result the
// AuthenticationResponseJSON. With allowCredentials it uses the first it
// holds; without (a discoverable sign-in) its newest for the RP ID.
func (a *Authenticator) Get(options []byte) ([]byte, error) {
	var o request
	if err := json.Unmarshal(options, &o); err != nil {
		return nil, err
	}
	var c *Credential
	for i := len(a.creds) - 1; i >= 0; i-- {
		cand := a.creds[i]
		if cand.RPID != o.RPID {
			continue
		}
		if len(o.AllowCredentials) == 0 {
			c = cand
			break
		}
		for _, allow := range o.AllowCredentials {
			if allow.ID == b64.EncodeToString(cand.ID) {
				c = cand
			}
		}
		if c != nil {
			break
		}
	}
	if c == nil {
		return nil, ErrNoCredential
	}
	return a.Sign(c, o.Challenge, o.RPID)
}

// Sign makes an assertion with c over challenge, as Get would.
func (a *Authenticator) Sign(c *Credential, challenge, rpID string) ([]byte, error) {
	if a.RPID != "" {
		rpID = a.RPID
	}
	if a.Counting {
		c.Count++
	}
	auth := authData(rpID, a.flags(), c.Count)
	client := a.clientData("webauthn.get", challenge)
	sum := sha256.Sum256(client)
	digest := sha256.Sum256(append(append([]byte{}, auth...), sum[:]...))
	sig, err := ecdsa.SignASN1(rand.Reader, c.key, digest[:])
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{
		"id": b64.EncodeToString(c.ID), "rawId": b64.EncodeToString(c.ID), "type": "public-key",
		"authenticatorAttachment": "platform", "clientExtensionResults": map[string]any{},
		"response": map[string]any{
			"clientDataJSON": b64.EncodeToString(client), "authenticatorData": b64.EncodeToString(auth),
			"signature": b64.EncodeToString(sig), "userHandle": b64.EncodeToString(c.UserHandle),
		},
	})
}

// flags: user present, user verified (unless SkipUV), backup eligible and
// backed up (a synced passkey).
func (a *Authenticator) flags() byte {
	f := byte(0x01 | 0x08 | 0x10)
	if !a.SkipUV {
		f |= 0x04
	}
	return f
}

func authData(rpID string, flags byte, count uint32) []byte {
	h := sha256.Sum256([]byte(rpID))
	out := append([]byte{}, h[:]...)
	out = append(out, flags)
	return binary.BigEndian.AppendUint32(out, count)
}

func (a *Authenticator) clientData(typ, challenge string) []byte {
	raw, _ := json.Marshal(map[string]any{"type": typ, "challenge": challenge, "origin": a.Origin, "crossOrigin": false})
	return raw
}

// coseKey is an ES256 public key as a COSE_Key.
func coseKey(pub *ecdsa.PublicKey) ([]byte, error) {
	raw, err := pub.Bytes() // uncompressed point: 0x04 ‖ X ‖ Y
	if err != nil {
		return nil, err
	}
	return webauthncbor.Marshal(map[int]any{1: 2, 3: -7, -1: 1, -2: raw[1:33], -3: raw[33:65]})
}

// Encode is an assertion as the X-Memax-Passkey header carries it.
func Encode(assertion []byte) string { return b64.EncodeToString(assertion) }
