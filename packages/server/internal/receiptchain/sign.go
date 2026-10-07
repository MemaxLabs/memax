package receiptchain

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// Signer signs checkpoint statements. The worker's sealer holds one; a nil
// Signer leaves checkpoints unsigned (still chained and Merkle-rooted, and
// marked unsigned).
//
// Ed25519Signer reads its key from the environment, for development and
// staging. Production's signer belongs in a KMS (AWS KMS and Google Cloud
// KMS both sign Ed25519 without exporting the key): implement Signer over
// the KMS client, keep KeyID stable per key version, and nothing else
// changes. A signature is a network call then, made inside the seal's
// transaction while it holds that space's head, so only that space's next
// seal waits on it.
type Signer interface {
	// KeyID names the key, so verifiers can pick its public key and keys
	// can rotate. It is part of the signed statement.
	KeyID() string
	Sign(ctx context.Context, msg []byte) ([]byte, error)
}

// Ed25519Signer signs with an in-process Ed25519 key.
type Ed25519Signer struct {
	key ed25519.PrivateKey
	id  string
}

// NewEd25519Signer takes a 32-byte seed or a 64-byte private key.
func NewEd25519Signer(key []byte) (*Ed25519Signer, error) {
	var priv ed25519.PrivateKey
	switch len(key) {
	case ed25519.SeedSize:
		priv = ed25519.NewKeyFromSeed(key)
	case ed25519.PrivateKeySize:
		priv = ed25519.PrivateKey(append([]byte(nil), key...))
		if !bytes.Equal(ed25519.NewKeyFromSeed(priv.Seed()), priv) {
			return nil, errors.New("receiptchain: the Ed25519 private key's public half doesn't match its seed")
		}
	default:
		return nil, fmt.Errorf("receiptchain: an Ed25519 key is a 32-byte seed or a 64-byte private key, not %d bytes", len(key))
	}
	return &Ed25519Signer{key: priv, id: KeyID(priv.Public().(ed25519.PublicKey))}, nil
}

// KeyID implements Signer.
func (s *Ed25519Signer) KeyID() string { return s.id }

// Sign implements Signer.
func (s *Ed25519Signer) Sign(_ context.Context, msg []byte) ([]byte, error) {
	return ed25519.Sign(s.key, msg), nil
}

// Public is the key's public half.
func (s *Ed25519Signer) Public() ed25519.PublicKey { return s.key.Public().(ed25519.PublicKey) }

// KeyID is a public key's id: "ed25519:" and the first 16 hex digits of
// its SHA-256. Derived, so an id can't be claimed for another key.
func KeyID(pub ed25519.PublicKey) string {
	sum := sha256.Sum256(pub)
	return "ed25519:" + hex.EncodeToString(sum[:8])
}

// Keyring holds the public keys checkpoints may be signed with, by id: the
// current signing key and the retired ones (rotation keeps verifying old
// checkpoints).
type Keyring map[string]ed25519.PublicKey

// Add adds a public key under its id.
func (k Keyring) Add(pub ed25519.PublicKey) string {
	id := KeyID(pub)
	k[id] = append(ed25519.PublicKey(nil), pub...)
	return id
}

// ErrUnknownKey: a checkpoint names a key the keyring doesn't hold.
var ErrUnknownKey = errors.New("receiptchain: checkpoint signed by an unknown key")

// ErrBadSignature: the signature doesn't verify.
var ErrBadSignature = errors.New("receiptchain: checkpoint signature doesn't verify")

// ErrUnsigned: the checkpoint has no signature.
var ErrUnsigned = errors.New("receiptchain: checkpoint is unsigned")

// Verify checks a checkpoint's signature.
func (k Keyring) Verify(c Checkpoint) error {
	if c.KeyID == "" || len(c.Signature) == 0 {
		return ErrUnsigned
	}
	pub, ok := k[c.KeyID]
	if !ok {
		return fmt.Errorf("%w: %s", ErrUnknownKey, c.KeyID)
	}
	if !ed25519.Verify(pub, c.Statement(), c.Signature) {
		return ErrBadSignature
	}
	return nil
}

// The environment.
const (
	// EnvSigningKey is the signing key (base64 or hex of a 32-byte
	// Ed25519 seed or 64-byte private key). Worker only.
	EnvSigningKey = "RECEIPT_SIGNING_KEY"
	// EnvVerifyKeys are more public keys to verify with, comma-separated
	// (base64 or hex, 32 bytes each): retired signing keys after a
	// rotation, and on the API (which never signs) the current one too.
	EnvVerifyKeys = "RECEIPT_VERIFY_KEYS"
)

// FromEnv reads the signer (nil when RECEIPT_SIGNING_KEY is unset) and the
// keyring: the signer's public key plus RECEIPT_VERIFY_KEYS.
func FromEnv(getenv func(string) string) (*Ed25519Signer, Keyring, error) {
	keys := Keyring{}
	var signer *Ed25519Signer
	if raw := strings.TrimSpace(getenv(EnvSigningKey)); raw != "" {
		key, err := decodeKey(raw)
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", EnvSigningKey, err)
		}
		if signer, err = NewEd25519Signer(key); err != nil {
			return nil, nil, fmt.Errorf("%s: %w", EnvSigningKey, err)
		}
		keys.Add(signer.Public())
	}
	for _, part := range strings.Split(getenv(EnvVerifyKeys), ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		pub, err := decodeKey(part)
		if err != nil || len(pub) != ed25519.PublicKeySize {
			return nil, nil, fmt.Errorf("%s: each key is base64 or hex of a 32-byte Ed25519 public key", EnvVerifyKeys)
		}
		keys.Add(pub)
	}
	return signer, keys, nil
}

func decodeKey(s string) ([]byte, error) {
	if b, err := hex.DecodeString(s); err == nil {
		return b, nil
	}
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if b, err := enc.DecodeString(s); err == nil {
			return b, nil
		}
	}
	return nil, errors.New("not base64 or hex")
}
