package receiptchain_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/receiptchain"
)

func ptr[T any](v T) *T { return &v }

// The golden receipts: fixed values, shared with the SDK's verify.test.ts.
// If the encoding changes, both tests fail: bump receiptchain.Format and
// the magic instead of changing version 1.
var (
	space  = uuid.MustParse("0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5c")
	tenant = uuid.MustParse("0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5d")
	memory = uuid.MustParse("0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a70")
	person = uuid.MustParse("0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a71")
	agent  = uuid.MustParse("0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a72")
	salt   = []byte("0123456789abcdef")
	seed   = bytes.Repeat([]byte{7}, 32)
)

func golden() []receiptchain.Receipt {
	at := time.Date(2026, 10, 6, 9, 30, 0, 123456000, time.UTC)
	reason := "superseded by M-0219"
	return []receiptchain.Receipt{
		{ID: uuid.MustParse("0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a80"), Seq: 1, TenantID: tenant, SpaceID: space,
			ObjectKind: "memory", ObjectID: memory, ObjectRef: "M-0219", Action: "proposed", ActorKind: "agent",
			ActorID: &agent, Agent: ptr("codex"), Via: "mcp", SessionRef: ptr("cx-7f3a"),
			SourceKind: ptr("pr"), SourceRef: ptr("PR #212"),
			OccurredAt: at, RecordedAt: at.Add(time.Millisecond), StreamID: memory, StreamVersion: 1},
		{ID: uuid.MustParse("0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a81"), Seq: 2, TenantID: tenant, SpaceID: space,
			ObjectKind: "memory", ObjectID: memory, ObjectRef: "M-0219", Action: "kept", ActorKind: "person",
			ActorID: &person, Agent: ptr("codex"), Via: "web", Assurance: ptr("human_web"),
			OccurredAt: at.Add(time.Minute), RecordedAt: at.Add(time.Minute), StreamID: memory, StreamVersion: 2},
		{ID: uuid.MustParse("0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a82"), Seq: 3, TenantID: tenant, SpaceID: space,
			ObjectKind: "memory", ObjectID: memory, ObjectRef: "M-0219", Action: "edited", ActorKind: "person",
			ActorID: &person, Via: "web", Reason: &reason, ReasonSalt: salt,
			ReasonSHA256: hashOf(receiptchain.ReasonCommitment(salt, reason)),
			OccurredAt:   at.Add(2 * time.Minute), RecordedAt: at.Add(2 * time.Minute), StreamID: memory, StreamVersion: 3},
	}
}

func hashOf(h receiptchain.Hash) []byte { return h[:] }

func hexOf(h receiptchain.Hash) string { return hex.EncodeToString(h[:]) }

func signer(t *testing.T, seed []byte) *receiptchain.Ed25519Signer {
	t.Helper()
	s, err := receiptchain.NewEd25519Signer(seed)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func sign(t *testing.T, s receiptchain.Signer, c receiptchain.Checkpoint) receiptchain.Checkpoint {
	t.Helper()
	c.KeyID = s.KeyID()
	sig, err := s.Sign(context.Background(), c.Statement())
	if err != nil {
		t.Fatal(err)
	}
	c.Signature = sig
	return c
}

// The vectors packages/sdk/src/v2/verify.test.ts checks too.
const (
	goldenCanonical0 = "6d656d61782e726563656970742e7631000000100199a1b2c3d47e5f8a9b0c1d2e3f4a80000000080000000000000001" +
		"000000100199a1b2c3d47e5f8a9b0c1d2e3f4a5d000000100199a1b2c3d47e5f8a9b0c1d2e3f4a5c000000066d656d6f7279" +
		"000000100199a1b2c3d47e5f8a9b0c1d2e3f4a70000000064d2d303231390000000870726f706f736564000000056167656e74" +
		"000000100199a1b2c3d47e5f8a9b0c1d2e3f4a7200000005636f646578000000036d6370ffffffff0000000763782d37663361" +
		"0000000270720000000750522023323132ffffffff0000000800065d28a47ef8400000000800065d28a47efc28" +
		"000000100199a1b2c3d47e5f8a9b0c1d2e3f4a70000000080000000000000001"
	goldenLeaf0     = "57d7617baa4b718608bea22bea41a539279eb4f50fb4e759a659083889a14e74"
	goldenLeaf2     = "f002f2b4a242974230d7de1e6cad1a57f678830e70265b2048bde890d0da83de"
	goldenGenesis   = "104de6f72da79e28eadd427e21f6432e1974ead72ade8a49f55979c64d9b18bf"
	goldenChain     = "0a909173075b588caacd0bc31bccaa04f554102788e18fb3a3eb25aa91d57264"
	goldenRoot      = "417c52cb77a8b934c73d57955637de9dd8d6793de97ffffc28cd0f97390eb224"
	goldenKeyID     = "ed25519:fe812c12f3ab4ce6"
	goldenPublic    = "ea4a6c63e29c520abef5507b132ec5f9954776aebebe7b92421eea691446d22c"
	goldenSignature = "f56ce345bdb05dfb184b7bf88d9d95b0a35f5ecb221e7650a2cec666cfa250a838da6ec0c5d5667ea2167b3816852f46556e8eb4f7395025e71f88eeb5fb3101"
)

// Version 1 of the encoding, byte for byte: a change here is a new format,
// never an edit (and verify.test.ts must agree).
func TestGoldenVectors(t *testing.T) {
	rs := golden()
	c := receiptchain.Seal(space, tenant, 1, 1, receiptchain.Genesis(space), rs, rs[2].RecordedAt.Add(time.Second))
	s := signer(t, seed)
	c = sign(t, s, c)
	for name, got := range map[string][2]string{
		"canonical[0]": {hex.EncodeToString(receiptchain.Canonical(rs[0])), goldenCanonical0},
		"leaf[0]":      {hexOf(receiptchain.Leaf(rs[0])), goldenLeaf0},
		"leaf[2]":      {hexOf(receiptchain.Leaf(rs[2])), goldenLeaf2},
		"genesis":      {hexOf(receiptchain.Genesis(space)), goldenGenesis},
		"chain":        {hexOf(c.Chain), goldenChain},
		"merkle root":  {hexOf(c.MerkleRoot), goldenRoot},
		"key id":       {c.KeyID, goldenKeyID},
		"public key":   {hex.EncodeToString(s.Public()), goldenPublic},
		"signature":    {hex.EncodeToString(c.Signature), goldenSignature},
	} {
		if got[0] != got[1] {
			t.Errorf("%s = %s, want %s", name, got[0], got[1])
		}
	}
	if c.SealedAt.Format(time.RFC3339Nano) != "2026-10-06T09:32:01.123456Z" || c.Number != 1 || c.PositionTo != 3 {
		t.Errorf("checkpoint = %+v", c)
	}
}

// The encoding is a fixed layout: the magic, then length-prefixed fields,
// NULL distinct from "".
func TestCanonicalLayout(t *testing.T) {
	r := golden()[0]
	b := receiptchain.Canonical(r)
	if !bytes.HasPrefix(b, []byte("memax.receipt.v1")) {
		t.Fatalf("magic: %q", b[:16])
	}
	empty, null := r, r
	empty.Assurance = ptr("")
	null.Assurance = nil
	if bytes.Equal(receiptchain.Canonical(empty), receiptchain.Canonical(null)) {
		t.Error(`an empty assurance encodes like an absent one`)
	}
	// Moving bytes between adjacent fields changes the encoding (lengths
	// delimit them).
	a, c := r, r
	a.ObjectKind, a.ObjectRef = "memoryM", "-0219"
	c.ObjectKind, c.ObjectRef = "memory", "M-0219"
	if bytes.Equal(receiptchain.Canonical(a), receiptchain.Canonical(c)) {
		t.Error("field boundaries are ambiguous")
	}
	// Microseconds count; the reason itself doesn't, its commitment does.
	later := r
	later.RecordedAt = r.RecordedAt.Add(time.Microsecond)
	if receiptchain.Leaf(later) == receiptchain.Leaf(r) {
		t.Error("a microsecond doesn't change the leaf")
	}
	withReason := golden()[2]
	redacted := withReason
	redacted.Reason, redacted.ReasonSalt = nil, nil
	if receiptchain.Leaf(withReason) != receiptchain.Leaf(redacted) {
		t.Error("redacting a reason changed the sealed leaf")
	}
}

func TestMerkleRoot(t *testing.T) {
	h := func(s string) receiptchain.Hash { return sha256.Sum256([]byte(s)) }
	node := func(l, r receiptchain.Hash) receiptchain.Hash {
		return sha256.Sum256(append(append([]byte{1}, l[:]...), r[:]...))
	}
	a, b, c, d, e := h("a"), h("b"), h("c"), h("d"), h("e")
	cases := []struct {
		leaves []receiptchain.Hash
		want   receiptchain.Hash
	}{
		{nil, sha256.Sum256(nil)},
		{[]receiptchain.Hash{a}, a},
		{[]receiptchain.Hash{a, b}, node(a, b)},
		{[]receiptchain.Hash{a, b, c}, node(node(a, b), c)},
		{[]receiptchain.Hash{a, b, c, d}, node(node(a, b), node(c, d))},
		{[]receiptchain.Hash{a, b, c, d, e}, node(node(node(a, b), node(c, d)), e)},
	}
	for i, tc := range cases {
		if got := receiptchain.MerkleRoot(tc.leaves); got != tc.want {
			t.Errorf("case %d (%d leaves): %s, want %s", i, len(tc.leaves), hexOf(got), hexOf(tc.want))
		}
	}
}

// chainOf seals receipts into checkpoints of the given sizes.
func chainOf(t *testing.T, s receiptchain.Signer, rs []receiptchain.Receipt, sizes ...int) []receiptchain.Checkpoint {
	t.Helper()
	var out []receiptchain.Checkpoint
	prev, from := receiptchain.Genesis(space), int64(1)
	for i, n := range sizes {
		c := receiptchain.Seal(space, tenant, int64(i+1), from, prev, rs[from-1:from-1+int64(n)], time.Now())
		if s != nil {
			c = sign(t, s, c)
		}
		out = append(out, c)
		prev, from = c.Chain, c.PositionTo+1
	}
	return out
}

func verify(keys receiptchain.Keyring, cs []receiptchain.Checkpoint, rs []receiptchain.Receipt, forgotten ...uuid.UUID) receiptchain.Report {
	v := receiptchain.NewVerifier(space, keys, cs)
	for _, f := range forgotten {
		v.Forgotten(f)
	}
	for _, r := range rs {
		v.Add(r)
	}
	return v.Finish()
}

func kinds(r receiptchain.Report) []string {
	var out []string
	for _, p := range r.Problems {
		out = append(out, string(p.Kind))
	}
	return out
}

// Every way to change history is caught, and pointed at the checkpoint
// whose range changed.
func TestVerifierCatchesTampering(t *testing.T) {
	s := signer(t, seed)
	keys := receiptchain.Keyring{}
	keys.Add(s.Public())
	base := append(golden(), golden()...)
	for i := 3; i < 6; i++ {
		base[i].ID, base[i].Seq = uuid.New(), int64(i+1)
	}
	cs := chainOf(t, s, base, 2, 3, 1)
	if r := verify(keys, cs, base); !r.OK() || r.Signed != 3 || r.Receipts != 6 {
		t.Fatalf("the untouched chain: %+v", r)
	}
	clone := func() []receiptchain.Receipt {
		out := make([]receiptchain.Receipt, len(base))
		copy(out, base)
		return out
	}
	cases := []struct {
		name   string
		mutate func(rs []receiptchain.Receipt, cs []receiptchain.Checkpoint) ([]receiptchain.Receipt, []receiptchain.Checkpoint)
		want   []string
		at     int64 // the checkpoint the first problem names
	}{
		{"an action rewritten", func(rs []receiptchain.Receipt, cs []receiptchain.Checkpoint) ([]receiptchain.Receipt, []receiptchain.Checkpoint) {
			rs[3].Action = "rejected"
			return rs, cs
		}, []string{"merkle", "chain"}, 2},
		{"an actor swapped", func(rs []receiptchain.Receipt, cs []receiptchain.Checkpoint) ([]receiptchain.Receipt, []receiptchain.Checkpoint) {
			rs[0].ActorID = &person
			return rs, cs
		}, []string{"merkle", "chain"}, 1},
		{"a timestamp moved", func(rs []receiptchain.Receipt, cs []receiptchain.Checkpoint) ([]receiptchain.Receipt, []receiptchain.Checkpoint) {
			rs[5].OccurredAt = rs[5].OccurredAt.Add(-time.Hour)
			return rs, cs
		}, []string{"merkle", "chain"}, 3},
		{"two receipts swapped", func(rs []receiptchain.Receipt, cs []receiptchain.Checkpoint) ([]receiptchain.Receipt, []receiptchain.Checkpoint) {
			rs[2], rs[3] = rs[3], rs[2]
			return rs, cs
		}, []string{"range", "merkle", "chain"}, 2},
		{"a receipt deleted", func(rs []receiptchain.Receipt, cs []receiptchain.Checkpoint) ([]receiptchain.Receipt, []receiptchain.Checkpoint) {
			return slices.Delete(rs, 1, 2), cs
		}, []string{"range", "merkle", "chain", "range", "range", "merkle", "chain", "missing"}, 1},
		{"a receipt inserted", func(rs []receiptchain.Receipt, cs []receiptchain.Checkpoint) ([]receiptchain.Receipt, []receiptchain.Checkpoint) {
			extra := rs[0]
			extra.ID = uuid.New()
			return slices.Insert(rs, 1, extra), cs
		}, []string{"range", "merkle", "chain", "range", "range", "merkle", "chain", "range", "range", "merkle", "chain"}, 1},
		{"a reason rewritten", func(rs []receiptchain.Receipt, cs []receiptchain.Checkpoint) ([]receiptchain.Receipt, []receiptchain.Checkpoint) {
			rs[2].Reason = ptr("no reason at all")
			return rs, cs
		}, []string{"reason"}, 0},
		{"a reason and its commitment rewritten", func(rs []receiptchain.Receipt, cs []receiptchain.Checkpoint) ([]receiptchain.Receipt, []receiptchain.Checkpoint) {
			rs[2].Reason = ptr("no reason at all")
			rs[2].ReasonSHA256 = hashOf(receiptchain.ReasonCommitment(salt, "no reason at all"))
			return rs, cs
		}, []string{"merkle", "chain"}, 2},
		{"a reason removed without a forget", func(rs []receiptchain.Receipt, cs []receiptchain.Checkpoint) ([]receiptchain.Receipt, []receiptchain.Checkpoint) {
			rs[2].Reason, rs[2].ReasonSalt = nil, nil
			return rs, cs
		}, []string{"redaction"}, 0},
		{"a checkpoint's root rewritten", func(rs []receiptchain.Receipt, cs []receiptchain.Checkpoint) ([]receiptchain.Receipt, []receiptchain.Checkpoint) {
			cs[1].MerkleRoot[0] ^= 1
			return rs, cs
		}, []string{"signature", "merkle"}, 2},
		{"a checkpoint re-signed by another key", func(rs []receiptchain.Receipt, cs []receiptchain.Checkpoint) ([]receiptchain.Receipt, []receiptchain.Checkpoint) {
			cs[0] = sign(t, signer(t, bytes.Repeat([]byte{9}, 32)), cs[0])
			return rs, cs
		}, []string{"signature"}, 1},
		{"a checkpoint's signature stripped", func(rs []receiptchain.Receipt, cs []receiptchain.Checkpoint) ([]receiptchain.Receipt, []receiptchain.Checkpoint) {
			cs[2].Signature[0] ^= 1
			return rs, cs
		}, []string{"signature"}, 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cs := slices.Clone(cs)
			for i := range cs {
				cs[i].Signature = slices.Clone(cs[i].Signature)
			}
			rs, cs := tc.mutate(clone(), cs)
			r := verify(keys, cs, rs)
			if got := kinds(r); !slices.Equal(got, tc.want) {
				t.Fatalf("problems = %v, want %v\n%v", got, tc.want, r.Problems)
			}
			if r.Problems[0].Checkpoint != tc.at {
				t.Errorf("the first problem names checkpoint %d, want %d: %v", r.Problems[0].Checkpoint, tc.at, r.Problems[0])
			}
		})
	}
}

// Forget redacts a reason and its salt in the same transaction as the
// forgot receipt; the sealed leaf never had the reason, so the chain holds,
// and the verifier accepts the missing reason because the object was
// forgotten.
func TestRedactionKeepsTheChainValid(t *testing.T) {
	s := signer(t, seed)
	keys := receiptchain.Keyring{}
	keys.Add(s.Public())
	rs := golden()
	cs := chainOf(t, s, rs, 3)
	forgot := rs[2]
	forgot.ID, forgot.Seq, forgot.Action, forgot.StreamVersion = uuid.New(), 4, "forgot", 4
	forgot.Reason, forgot.ReasonSalt, forgot.ReasonSHA256 = nil, nil, nil
	rs = append(rs, forgot)
	rs[2].Reason, rs[2].ReasonSalt = nil, nil
	next := receiptchain.Seal(space, tenant, 2, 4, cs[0].Chain, rs[3:], time.Now())
	cs = append(cs, sign(t, s, next))
	if r := verify(keys, cs, rs); !r.OK() || r.Signed != 2 {
		t.Fatalf("after Forget: %v", r.Problems)
	}
	// A forget not sealed yet still counts.
	if r := verify(keys, cs[:1], rs[:3], memory); !r.OK() {
		t.Errorf("with an unsealed forget: %v", r.Problems)
	}
}

// Keys rotate: the keyring verifies checkpoints signed by a retired key
// and by the current one; without the retired key, its checkpoints are an
// unknown key.
func TestKeyRotation(t *testing.T) {
	old, current := signer(t, seed), signer(t, bytes.Repeat([]byte{8}, 32))
	rs := golden()
	cs := chainOf(t, old, rs, 2)
	next := sign(t, current, receiptchain.Seal(space, tenant, 2, 3, cs[0].Chain, rs[2:], time.Now()))
	cs = append(cs, next)
	both := receiptchain.Keyring{}
	both.Add(old.Public())
	both.Add(current.Public())
	if r := verify(both, cs, rs); !r.OK() || r.Signed != 2 {
		t.Fatalf("with both keys: %v", r.Problems)
	}
	onlyCurrent := receiptchain.Keyring{}
	onlyCurrent.Add(current.Public())
	r := verify(onlyCurrent, cs, rs)
	if len(r.Problems) != 1 || r.Problems[0].Kind != receiptchain.ProblemSignature || !strings.Contains(r.Problems[0].Detail, old.KeyID()) {
		t.Errorf("without the retired key: %v", r.Problems)
	}
	if old.KeyID() == current.KeyID() || !strings.HasPrefix(old.KeyID(), "ed25519:") {
		t.Errorf("key ids %q %q", old.KeyID(), current.KeyID())
	}
	// Unsigned checkpoints still chain, and say they're unsigned.
	unsigned := chainOf(t, nil, rs, 3)
	if r := verify(both, unsigned, rs); !r.OK() || r.Unsigned != 1 || r.Signed != 0 {
		t.Errorf("unsigned: %+v", r)
	}
}

func TestFromEnv(t *testing.T) {
	s := signer(t, seed)
	other := signer(t, bytes.Repeat([]byte{8}, 32))
	env := map[string]string{
		receiptchain.EnvSigningKey: hex.EncodeToString(seed),
		receiptchain.EnvVerifyKeys: " " + hex.EncodeToString(other.Public()) + ",",
	}
	got, keys, err := receiptchain.FromEnv(func(k string) string { return env[k] })
	if err != nil || got == nil || got.KeyID() != s.KeyID() || len(keys) != 2 || keys[other.KeyID()] == nil {
		t.Fatalf("FromEnv: %v %v %v", got, keys, err)
	}
	if !ed25519.PublicKey(keys[s.KeyID()]).Equal(s.Public()) {
		t.Error("the signer's public key isn't in the keyring")
	}
	if s, keys, err := receiptchain.FromEnv(func(string) string { return "" }); err != nil || s != nil || len(keys) != 0 {
		t.Errorf("unset: %v %v %v", s, keys, err)
	}
	if _, _, err := receiptchain.FromEnv(func(k string) string {
		if k == receiptchain.EnvSigningKey {
			return "not a key"
		}
		return ""
	}); err == nil {
		t.Error("a bad key was accepted")
	}
}
