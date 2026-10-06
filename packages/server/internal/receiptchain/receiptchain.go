// Package receiptchain is the tamper evidence of the V2 record's receipts
// (plan 25 §5.3 "Hash chain, kept off the write path"): a stable, versioned
// byte encoding of a receipt, the per-space hash chain over it, RFC 6962
// Merkle roots, signed checkpoints, and a verifier. It has no database and
// no I/O: internal/ledger feeds it receipts in chain order, and the SDK's
// verifyReceiptChain (packages/sdk/src/v2/verify.ts) implements the same
// encoding, so an export can be checked anywhere. Golden vectors in both
// test suites keep the two byte-for-byte equal.
//
// # The canonical form, version 1
//
// A receipt is encoded as the magic "memax.receipt.v1" followed by 21
// fields in this order, each as a 4-byte big-endian length and its bytes;
// an absent value (SQL NULL) is the length 0xFFFFFFFF and no bytes:
//
//	 1 id              16 bytes (uuid)
//	 2 seq             8 bytes, big-endian two's complement
//	 3 tenant_id       16
//	 4 space_id        16
//	 5 object_kind     UTF-8
//	 6 object_id       16
//	 7 object_ref      UTF-8
//	 8 action          UTF-8
//	 9 actor_kind      UTF-8
//	10 actor_id        16, or absent
//	11 agent           UTF-8, or absent
//	12 via             UTF-8
//	13 assurance       UTF-8, or absent
//	14 session_ref     UTF-8, or absent
//	15 source kind     UTF-8, or absent
//	16 source ref      UTF-8, or absent
//	17 reason_sha256   32, or absent
//	18 occurred_at     8: microseconds since the Unix epoch, UTC
//	19 recorded_at     8: the same
//	20 stream_id       16
//	21 stream_version  8
//
// Length-prefixed binary, not JSON (RFC 8785): there is nothing to escape
// or normalise (Go's encoding/json and ECMAScript's JSON.stringify differ
// on U+2028, U+2029 and HTML characters, which JCS would need a custom
// serializer for), no number formatting, and NULL is distinct from "".
// Timestamps are microseconds because Postgres stores microseconds.
//
// The reason itself is never encoded, only its salted commitment
// (reason_sha256 = SHA-256(salt ‖ utf8(reason)), written by the database
// with the receipt, migration 038). Forget redacts the reason and its salt
// and keeps the commitment, so a redaction never changes a sealed leaf, and
// the forgot receipt that authorises it is sealed like any other. txid is
// not encoded either: it orders the chain, but a logical restore assigns
// new ones.
//
// # The chain and checkpoints
//
//	leaf_i  = SHA-256(0x00 ‖ canonical(receipt_i))
//	h_0     = SHA-256("memax.receipts.chain.v1" ‖ 0x00 ‖ space_id)
//	h_i     = SHA-256(h_(i-1) ‖ leaf_i)
//	root    = RFC 6962 Merkle tree hash over a checkpoint's leaves
//	          (internal nodes SHA-256(0x01 ‖ left ‖ right))
//
// A checkpoint covers positions [from, to] of the space's chain (counting
// from 1), with h_(from-1), h_to and the root. Its statement, the bytes an
// Ed25519 key signs, is the magic "memax.checkpoint.v1" followed by the
// length-prefixed space_id, tenant_id, number, from, to, first receipt id,
// last receipt id, last seq, h_(from-1), h_to, root, sealed_at
// (microseconds) and key_id (absent when unsigned).
package receiptchain

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"time"

	"github.com/google/uuid"
)

// Format is the canonical encoding's version.
const Format = 1

const (
	receiptMagic    = "memax.receipt.v1"
	checkpointMagic = "memax.checkpoint.v1"
	chainMagic      = "memax.receipts.chain.v1"
	absent          = 0xFFFFFFFF
)

// Hash is a SHA-256 digest.
type Hash [32]byte

// Receipt is a receipt as the database holds it, with SQL NULLs as nil.
type Receipt struct {
	ID         uuid.UUID
	Seq        int64
	TenantID   uuid.UUID
	SpaceID    uuid.UUID
	ObjectKind string
	ObjectID   uuid.UUID
	ObjectRef  string
	Action     string
	ActorKind  string
	ActorID    *uuid.UUID
	Agent      *string
	Via        string
	Assurance  *string
	SessionRef *string
	SourceKind *string
	SourceRef  *string
	// Reason and ReasonSalt are never encoded: the verifier checks them
	// against ReasonSHA256, which is. All three are nil without a reason;
	// after Forget, only ReasonSHA256 is left.
	Reason        *string
	ReasonSalt    []byte
	ReasonSHA256  []byte
	OccurredAt    time.Time
	RecordedAt    time.Time
	StreamID      uuid.UUID
	StreamVersion int64
}

// encoder writes length-prefixed fields.
type encoder struct{ b bytes.Buffer }

func (e *encoder) bytes(p []byte) {
	var n [4]byte
	binary.BigEndian.PutUint32(n[:], uint32(len(p)))
	e.b.Write(n[:])
	e.b.Write(p)
}

func (e *encoder) none() {
	var n [4]byte
	binary.BigEndian.PutUint32(n[:], absent)
	e.b.Write(n[:])
}

func (e *encoder) str(s string) { e.bytes([]byte(s)) }

func (e *encoder) optStr(s *string) {
	if s == nil {
		e.none()
		return
	}
	e.str(*s)
}

func (e *encoder) id(u uuid.UUID) { e.bytes(u[:]) }

func (e *encoder) optID(u *uuid.UUID) {
	if u == nil {
		e.none()
		return
	}
	e.id(*u)
}

func (e *encoder) optBytes(p []byte) {
	if p == nil {
		e.none()
		return
	}
	e.bytes(p)
}

func (e *encoder) int(n int64) {
	var p [8]byte
	binary.BigEndian.PutUint64(p[:], uint64(n))
	e.bytes(p[:])
}

func (e *encoder) time(t time.Time) { e.int(t.UnixMicro()) }

// Canonical is the receipt's canonical form, version 1.
func Canonical(r Receipt) []byte {
	var e encoder
	e.b.WriteString(receiptMagic)
	e.id(r.ID)
	e.int(r.Seq)
	e.id(r.TenantID)
	e.id(r.SpaceID)
	e.str(r.ObjectKind)
	e.id(r.ObjectID)
	e.str(r.ObjectRef)
	e.str(r.Action)
	e.str(r.ActorKind)
	e.optID(r.ActorID)
	e.optStr(r.Agent)
	e.str(r.Via)
	e.optStr(r.Assurance)
	e.optStr(r.SessionRef)
	e.optStr(r.SourceKind)
	e.optStr(r.SourceRef)
	e.optBytes(r.ReasonSHA256)
	e.time(r.OccurredAt)
	e.time(r.RecordedAt)
	e.id(r.StreamID)
	e.int(r.StreamVersion)
	return e.b.Bytes()
}

// Leaf is the receipt's leaf hash: SHA-256(0x00 ‖ canonical).
func Leaf(r Receipt) Hash {
	h := sha256.New()
	h.Write([]byte{0})
	h.Write(Canonical(r))
	return Hash(h.Sum(nil))
}

// Genesis is a space's chain hash before its first receipt.
func Genesis(space uuid.UUID) Hash {
	h := sha256.New()
	h.Write([]byte(chainMagic))
	h.Write([]byte{0})
	h.Write(space[:])
	return Hash(h.Sum(nil))
}

// Next chains one leaf onto prev.
func Next(prev, leaf Hash) Hash {
	h := sha256.New()
	h.Write(prev[:])
	h.Write(leaf[:])
	return Hash(h.Sum(nil))
}

// MerkleRoot is the RFC 6962 Merkle tree hash over leaf hashes.
func MerkleRoot(leaves []Hash) Hash {
	switch n := len(leaves); n {
	case 0:
		return sha256.Sum256(nil)
	case 1:
		return leaves[0]
	default:
		k := 1
		for k*2 < n {
			k *= 2
		}
		l, r := MerkleRoot(leaves[:k]), MerkleRoot(leaves[k:])
		h := sha256.New()
		h.Write([]byte{1})
		h.Write(l[:])
		h.Write(r[:])
		return Hash(h.Sum(nil))
	}
}

// ReasonCommitment is SHA-256(salt ‖ utf8(reason)), as the database
// computes it (migration 038).
func ReasonCommitment(salt []byte, reason string) Hash {
	h := sha256.New()
	h.Write(salt)
	h.Write([]byte(reason))
	return Hash(h.Sum(nil))
}

// Checkpoint is one signed checkpoint of a space's chain.
type Checkpoint struct {
	SpaceID        uuid.UUID
	TenantID       uuid.UUID
	Number         int64
	PositionFrom   int64
	PositionTo     int64
	FirstReceiptID uuid.UUID
	LastReceiptID  uuid.UUID
	LastSeq        int64
	Prev           Hash
	Chain          Hash
	MerkleRoot     Hash
	SealedAt       time.Time
	// KeyID and Signature are empty for an unsigned checkpoint.
	KeyID     string
	Signature []byte
}

// Receipts is how many receipts the checkpoint covers.
func (c Checkpoint) Receipts() int64 { return c.PositionTo - c.PositionFrom + 1 }

// Statement is the bytes the checkpoint's signature is over.
func (c Checkpoint) Statement() []byte {
	var e encoder
	e.b.WriteString(checkpointMagic)
	e.id(c.SpaceID)
	e.id(c.TenantID)
	e.int(c.Number)
	e.int(c.PositionFrom)
	e.int(c.PositionTo)
	e.id(c.FirstReceiptID)
	e.id(c.LastReceiptID)
	e.int(c.LastSeq)
	e.bytes(c.Prev[:])
	e.bytes(c.Chain[:])
	e.bytes(c.MerkleRoot[:])
	e.time(c.SealedAt)
	if c.KeyID == "" {
		e.none()
	} else {
		e.str(c.KeyID)
	}
	return e.b.Bytes()
}

// Seal chains receipts (in chain order) onto prev at position from, and
// returns the checkpoint for them, unsigned.
func Seal(space, tenant uuid.UUID, number, from int64, prev Hash, receipts []Receipt, at time.Time) Checkpoint {
	leaves := make([]Hash, len(receipts))
	h := prev
	for i, r := range receipts {
		leaves[i] = Leaf(r)
		h = Next(h, leaves[i])
	}
	c := Checkpoint{SpaceID: space, TenantID: tenant, Number: number, PositionFrom: from,
		PositionTo: from + int64(len(receipts)) - 1, Prev: prev, Chain: h, MerkleRoot: MerkleRoot(leaves),
		SealedAt: at.UTC().Truncate(time.Microsecond)}
	if len(receipts) > 0 {
		c.FirstReceiptID, c.LastReceiptID = receipts[0].ID, receipts[len(receipts)-1].ID
		c.LastSeq = receipts[len(receipts)-1].Seq
	}
	return c
}
