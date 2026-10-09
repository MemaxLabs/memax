package export_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/export"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/lifecycle"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
	"github.com/MemaxLabs/memax/packages/server/internal/receiptchain"
)

// The fixture: a small space with every kind of thing an export holds, and
// the receipts that made it, sealed in two signed checkpoints with five
// receipts after them. The same record is the SDK's golden export
// (packages/sdk/src/v2/testdata/export-v1, written by TestGoldenExport),
// so the Go writer and the SDK's reader and verifier can't drift apart.

func id(n int) uuid.UUID {
	return uuid.MustParse("0199a1b2-c3d4-7e5f-8a9b-" + hex.EncodeToString([]byte{0, 0, 0, 0, byte(n >> 8), byte(n)}))
}

func ptr[T any](v T) *T { return &v }

var (
	fxSpace    = id(1)
	fxTenant   = id(2)
	fxPerson   = id(3)
	fxCodex    = id(4)
	fxBrief    = id(5)
	fxTarget   = id(6)
	fxCompile  = id(7)
	fxM1       = id(0x101) // a kept decision, edited once, superseding M2
	fxM2       = id(0x102) // a decision M1 superseded
	fxM3       = id(0x103) // Codex's proposal
	fxM4       = id(0x104) // forgotten
	fxM5       = id(0x105) // a stale preference whose words span lines
	fxGate1    = id(0x201)
	fxGate2    = id(0x202)
	fxTomb4    = id(0x301)
	fxExport   = id(0x40e) // the export's own receipt
	fxBriefV1  = id(0x501)
	fxBriefV2  = id(0x502)
	fxSalt     = []byte("0123456789abcdef")
	fxSeed     = bytes.Repeat([]byte{9}, 32)
	fxStart    = time.Date(2026, 10, 6, 9, 30, 0, 123456000, time.UTC)
	fxSourceID = id(0x601)
	fxSource2  = id(0x602)
	fxSource3  = id(0x603)
	fxLink     = id(0x701)
)

func at(minutes int) time.Time { return fxStart.Add(time.Duration(minutes) * time.Minute) }

// fixture is everything ReadExport would hand the writer.
type fixture struct {
	space      ledger.ExportSpaceInfo
	briefs     []ledger.Brief
	gates      []ledger.Gate
	targets    []ledger.Target
	agents     []ledger.ExportAgent
	tombstones []ledger.Tombstone
	reads      ledger.ReadSummary
	memories   []ledger.ExportMemory
	receipts   []receiptchain.Receipt
	seal       ledger.ExportSeal
	keys       []export.SigningKey
}

func (f *fixture) feed(t testing.TB, sink ledger.ExportSink) {
	t.Helper()
	steps := []func() error{
		func() error { return sink.Space(f.space) },
		func() error { return sink.Briefs(f.briefs) },
		func() error { return sink.Gates(f.gates) },
		func() error { return sink.Targets(f.targets) },
		func() error { return sink.Agents(f.agents) },
		func() error { return sink.Tombstones(f.tombstones) },
		func() error { return sink.Reads(f.reads) },
		// Two batches each, as ReadExport sends large spaces.
		func() error { return sink.Memories(f.memories[:2]) },
		func() error { return sink.Memories(f.memories[2:]) },
		func() error { return sink.Receipts(f.receipts[:4]) },
		func() error { return sink.Receipts(f.receipts[4:]) },
		func() error { return sink.Seal(f.seal) },
	}
	for _, s := range steps {
		if err := s(); err != nil {
			t.Fatal(err)
		}
	}
}

// render writes the fixture's export into memory.
func (f *fixture) render(t testing.TB) map[string][]byte {
	t.Helper()
	sink := export.NewMapSink()
	w := export.NewWriter(sink, export.Options{Receipt: fxExport, Keys: f.keys})
	f.feed(t, w)
	if err := w.Finish(); err != nil {
		t.Fatal(err)
	}
	return sink.Files
}

// zip writes the fixture's export as an archive.
func (f *fixture) zip(t testing.TB) []byte {
	t.Helper()
	var buf bytes.Buffer
	z := export.NewZipSink(&buf)
	w := export.NewWriter(z, export.Options{Receipt: fxExport, Keys: f.keys, OnSpace: func(s ledger.ExportSpaceInfo) error {
		z.Start(s.Slug, s.AsOf)
		return nil
	}})
	f.feed(t, w)
	if err := w.Finish(); err != nil {
		t.Fatal(err)
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func newFixture(t testing.TB) *fixture {
	t.Helper()
	f := &fixture{}
	f.receipts = fixtureReceipts()
	stampsOf := func(object uuid.UUID) []ledger.ReceiptStamp {
		var out []ledger.ReceiptStamp
		for _, r := range f.receipts {
			if r.ObjectID == object {
				out = append(out, ledger.ReceiptStamp{ID: r.ID, Seq: r.Seq, Action: ledger.Action(r.Action), OccurredAt: r.OccurredAt})
			}
		}
		return out
	}
	byAction := func(object uuid.UUID, action string) receiptchain.Receipt {
		for _, r := range f.receipts {
			if r.ObjectID == object && r.Action == action {
				return r
			}
		}
		t.Fatalf("no %s receipt for %s", action, object)
		return receiptchain.Receipt{}
	}

	last := f.receipts[len(f.receipts)-1]
	f.space = ledger.ExportSpaceInfo{
		Space: ledger.Space{ID: fxSpace, TenantID: fxTenant, Slug: "memax-v2", Name: "memax-v2", Kind: policy.SpaceProject,
			Repository: "github.com/MemaxLabs/memax", V2EnabledAt: ptr(at(-60))},
		AsOf: last.RecordedAt, Receipts: int64(len(f.receipts)),
	}

	m1Edited := byAction(fxM1, "edited")
	f.memories = []ledger.ExportMemory{
		{
			Memory: ledger.Memory{ID: fxM1, Ref: "M-0001", SpaceID: fxSpace, TenantID: fxTenant,
				Statement: `Background jobs run on River, Postgres-backed. We don't use Temporal ("one queue" & <one> DB).`,
				Section:   ledger.SectionDecisions, Kind: ledger.KindDecision, State: lifecycle.MarkKept,
				Lifecycle: lifecycle.Kept, Flags: lifecycle.Flags{}, Trust: policy.TrustPerson, Version: 2,
				Conditions: json.RawMessage(`[{"kind": "file_exists", "path": "go.mod"}]`), Applies: json.RawMessage(`{}`),
				Decision: &ledger.DecisionFields{Why: "River keeps jobs in the same transaction as the write.", Status: ledger.DecisionInForce,
					Area: "infra", Options: []ledger.DecisionOption{{Label: "River", Detail: "Postgres-backed"}, {Label: "Temporal"}}},
				CreatedReceiptID: byAction(fxM1, "proposed").ID, LastReceiptID: m1Edited.ID,
				CreatedAt: at(0), UpdatedAt: m1Edited.OccurredAt,
				Sources: []ledger.Source{
					{ID: fxSourceID, Kind: ledger.SourcePR, Ref: "PR #212", URI: "https://github.com/MemaxLabs/memax/pull/212",
						Locator: json.RawMessage(`{}`), Trust: policy.TrustRepository, CreatedAt: at(0)},
					{ID: fxSource2, Kind: ledger.SourceFile, Ref: "go.mod:14", Locator: json.RawMessage(`{"line": 14, "path": "go.mod"}`),
						Trust: policy.TrustRepository, Quote: "github.com/riverqueue/river v0.20.0", ContentHash: "sha256:ab12", CreatedAt: at(0)},
				},
				Links: []ledger.Link{{ID: fxLink, Kind: ledger.LinkSupersedes, Direction: ledger.LinkOut, MemoryID: fxM2, Ref: "M-0002",
					ReceiptID: byAction(fxM2, "superseded").ID, CreatedAt: at(5)}},
			},
			Versions: []ledger.MemoryVersion{
				{Version: 1, Statement: "Background jobs run on River.", ReceiptID: byAction(fxM1, "proposed").ID, CreatedAt: at(0)},
				{Version: 2, Statement: `Background jobs run on River, Postgres-backed. We don't use Temporal ("one queue" & <one> DB).`,
					ReceiptID: m1Edited.ID, CreatedAt: m1Edited.OccurredAt},
			},
			Receipts: stampsOf(fxM1),
		},
		{
			Memory: ledger.Memory{ID: fxM2, Ref: "M-0002", SpaceID: fxSpace, TenantID: fxTenant,
				Statement: "Background jobs run on Temporal.", Section: ledger.SectionDecisions, Kind: ledger.KindDecision,
				State: lifecycle.MarkKept, Lifecycle: lifecycle.Kept, Flags: lifecycle.Flags{}, Trust: policy.TrustPerson, Version: 1,
				Conditions: json.RawMessage(`[]`), Applies: json.RawMessage(`{}`),
				Decision:         &ledger.DecisionFields{Status: ledger.DecisionSuperseded},
				CreatedReceiptID: byAction(fxM2, "kept").ID, LastReceiptID: byAction(fxM2, "superseded").ID,
				CreatedAt: at(1), UpdatedAt: at(5),
				Links: []ledger.Link{{ID: fxLink, Kind: ledger.LinkSupersedes, Direction: ledger.LinkIn, MemoryID: fxM1, Ref: "M-0001",
					ReceiptID: byAction(fxM2, "superseded").ID, CreatedAt: at(5)}},
			},
			Versions: []ledger.MemoryVersion{{Version: 1, Statement: "Background jobs run on Temporal.",
				ReceiptID: byAction(fxM2, "kept").ID, CreatedAt: at(1)}},
			Receipts: stampsOf(fxM2),
		},
		{
			Memory: ledger.Memory{ID: fxM3, Ref: "M-0003", SpaceID: fxSpace, TenantID: fxTenant,
				Statement: "pnpm workspaces only. Never run `npm install` at the root.", Section: ledger.SectionConventions,
				Kind: ledger.KindFact, State: lifecycle.MarkProposed, Lifecycle: lifecycle.Proposed, Flags: lifecycle.Flags{},
				Trust: policy.TrustAgentOwnWork, Version: 1, Conditions: json.RawMessage(`[]`),
				Applies:          json.RawMessage(`{"paths": ["packages/**"]}`),
				CreatedReceiptID: byAction(fxM3, "proposed").ID, LastReceiptID: byAction(fxM3, "proposed").ID,
				CreatedAt: at(3), UpdatedAt: at(3),
				Sources: []ledger.Source{{ID: fxSource3, Kind: ledger.SourceSession, Ref: "session cx-7f3a", Locator: json.RawMessage(`{}`),
					Trust: policy.TrustAgentOwnWork, CreatedAt: at(3)}},
			},
			Versions: []ledger.MemoryVersion{{Version: 1, Statement: "pnpm workspaces only. Never run `npm install` at the root.",
				ReceiptID: byAction(fxM3, "proposed").ID, CreatedAt: at(3)}},
			Receipts: stampsOf(fxM3),
		},
		{
			// Forgotten: the record keeps the row and its receipts, never the words.
			Memory: ledger.Memory{ID: fxM4, Ref: "M-0004", SpaceID: fxSpace, TenantID: fxTenant, Section: ledger.SectionPreferences,
				Kind: ledger.KindFact, State: lifecycle.MarkForgotten, Lifecycle: lifecycle.Forgotten, Flags: lifecycle.Flags{},
				Trust: policy.TrustPerson, Version: 1, Conditions: json.RawMessage(`[]`), Applies: json.RawMessage(`{}`),
				CreatedReceiptID: byAction(fxM4, "kept").ID, LastReceiptID: byAction(fxM4, "forgot").ID,
				CreatedAt: at(2), UpdatedAt: at(6)},
			Versions: []ledger.MemoryVersion{{Version: 1, ReceiptID: byAction(fxM4, "kept").ID, CreatedAt: at(2)}},
			Receipts: stampsOf(fxM4),
		},
		{
			Memory: ledger.Memory{ID: fxM5, Ref: "M-0005", SpaceID: fxSpace, TenantID: fxTenant,
				Statement: "Release steps:\n---\nTag first, then deploy. Café — naïve résumé, 日本語,   kept as is.",
				Section:   ledger.SectionPreferences, Kind: ledger.KindFact, State: lifecycle.MarkStale, Lifecycle: lifecycle.Kept,
				Flags: lifecycle.Flags{lifecycle.Stale}, Trust: policy.TrustPerson, Version: 1, StaleAfter: ptr(at(60 * 24)),
				Conditions: json.RawMessage(`[]`), Applies: json.RawMessage(`{}`),
				CreatedReceiptID: byAction(fxM5, "kept").ID, LastReceiptID: byAction(fxM5, "flagged").ID,
				CreatedAt: at(4), UpdatedAt: at(7)},
			Versions: []ledger.MemoryVersion{{Version: 1, Statement: "Release steps:\n---\nTag first, then deploy. Café — naïve résumé, 日本語,   kept as is.",
				ReceiptID: byAction(fxM5, "kept").ID, CreatedAt: at(4)}},
			Receipts: stampsOf(fxM5),
		},
	}

	f.briefs = []ledger.Brief{
		{ID: fxBrief, VersionID: fxBriefV1, Ref: "B-0001", Version: 1, SpaceID: fxSpace, TenantID: fxTenant, Title: "memax-v2",
			Sections: []ledger.BriefSection{{Key: "decisions", Heading: "Decisions", Items: []ledger.BriefItem{
				{Ref: "M-0002"}, {Forgotten: true, Cites: []string{"M-0004"}}}}},
			Facts: 2, ReceiptID: f.receipts[2].ID, CreatedAt: at(2)},
		{ID: fxBrief, VersionID: fxBriefV2, Ref: "B-0002", Version: 2, ParentVersion: 1, SpaceID: fxSpace, TenantID: fxTenant,
			Title: "memax-v2", Summary: "How memax-v2 is built and run.", Current: true,
			Sections: []ledger.BriefSection{
				{Key: "decisions", Heading: "Decisions", Items: []ledger.BriefItem{
					{Ref: "M-0001"}, {Text: "Everything async goes through one Postgres queue.", Cites: []string{"M-0001", "M-0002"}}}},
				{Key: "preferences", Heading: "Preferences", Items: []ledger.BriefItem{{Ref: "M-0005"}}},
				{Key: "open", Heading: "Open questions", Items: []ledger.BriefItem{}},
			},
			Facts: 3, ReceiptID: byAction(fxBrief, "revised").ID, CreatedAt: at(8)},
	}
	// B-0001's receipt is the first revised one.
	for _, r := range f.receipts {
		if r.ObjectID == fxBrief && r.StreamVersion == 1 {
			f.briefs[0].ReceiptID = r.ID
		}
		if r.ObjectID == fxBrief && r.StreamVersion == 2 {
			f.briefs[1].ReceiptID = r.ID
		}
	}

	f.gates = []ledger.Gate{
		{ID: fxGate1, Ref: "G-0001", SpaceID: fxSpace, TenantID: fxTenant, Question: "River or Temporal for background jobs?",
			Context: "Both are fine; River keeps one database.", Options: []ledger.DecisionOption{{Label: "Temporal"}, {Label: "River", Detail: "Postgres"}},
			Status: ledger.GateAnswered, ExpiresAt: at(60 * 24 * 7), AskedBy: fxCodex, Agent: "codex", SessionRef: "cx-7f3a",
			Answer: &ledger.GateAnswer{Option: 2, Label: "River", Memory: ledger.MemoryPointer{ID: fxM1, Ref: "M-0001"},
				AnsweredBy: fxPerson, AnsweredAt: at(0), Assurance: policy.AssuranceHumanWeb},
			Version: 2, CreatedReceiptID: byAction(fxGate1, "asked").ID, LastReceiptID: byAction(fxGate1, "answered").ID,
			CreatedAt: at(-1), UpdatedAt: at(0)},
		{ID: fxGate2, Ref: "G-0002", SpaceID: fxSpace, TenantID: fxTenant, Question: "Fly.io in iad or ams?",
			Options: []ledger.DecisionOption{{Label: "iad"}, {Label: "ams"}}, Status: ledger.GateWaiting, ExpiresAt: at(60 * 24 * 7),
			AskedBy: fxCodex, Agent: "codex", Version: 1, CreatedReceiptID: byAction(fxGate2, "asked").ID,
			LastReceiptID: byAction(fxGate2, "asked").ID, CreatedAt: at(9), UpdatedAt: at(9)},
	}

	run := &ledger.CompileRun{ID: fxCompile, Ref: "C-0001", TargetID: fxTarget, SpaceID: fxSpace, Brief: "B-0002", BriefVersion: 2,
		Generation: 3, Status: ledger.CompileDelivered, InputSHA256: "aa", OutputSHA256: "bb", DriftSHA256: "cc", Bytes: 812, Lines: 22,
		Refs: []string{"M-0001", "M-0005"}, DroppedForBudget: []string{},
		Files: []ledger.CompiledOutput{{Path: "AGENTS.md", SHA256: "bb", DriftSHA256: "cc", Bytes: 812, Lines: 22,
			Refs: []string{"M-0001", "M-0005"}, Cites: []string{"M-0002"}, DroppedForBudget: []string{}}},
		Warnings: []ledger.CompileWarning{}, EnqueuedAt: at(8), StartedAt: at(8), CompiledAt: at(8), DeliveredAt: ptr(at(9)),
		ReceiptID: byAction(fxCompile, "compiled").ID}
	f.targets = []ledger.Target{{ID: fxTarget, SpaceID: fxSpace, TenantID: fxTenant, Kind: ledger.TargetAgentsMD, Path: "AGENTS.md",
		Label: "AGENTS.md", Settings: ledger.TargetSettings{Include: ledger.IncludeKeptOnly, Stale: ledger.StaleMark,
			SizeBudget: ledger.DefaultSizeBudget, Scoped: ledger.ScopedInline},
		Delivery: ledger.DeliveryLocal, SyncState: ledger.SyncInSync, Version: 1, DirtyGen: 3, CompiledGen: 3, DirtyAt: at(8),
		LastCompile: run, Delivered: &ledger.Delivered{CompileID: ptr(fxCompile), Compile: "C-0001", SHA256: "cc",
			Files: []ledger.DeliveredFile{{Path: "AGENTS.md", SHA256: "cc"}}, At: ptr(at(9))},
		CreatedReceiptID: byAction(fxTarget, "configured").ID, LastReceiptID: byAction(fxTarget, "configured").ID,
		CreatedAt: at(-2), UpdatedAt: at(9)}}

	f.agents = []ledger.ExportAgent{{ID: fxCodex, Agent: "codex", DisplayName: "Codex", Surface: "cli", State: "active",
		Autonomy: policy.AutonomyPropose, PersonID: fxPerson, CreatedReceiptID: id(0x900), LastReceiptID: id(0x900), CreatedAt: at(-10)}}

	forgot := byAction(fxM4, "forgot")
	f.tombstones = []ledger.Tombstone{{ID: fxTomb4, OpID: fxTomb4, Ref: "M-0004", Kind: ledger.ObjectMemory, ObjectID: fxM4,
		SpaceID: fxSpace, TenantID: fxTenant, With: []string{}, Note: "this note is the person's own words and stays out",
		By: ledger.TombstoneActor{Kind: "person", ID: ptr(fxPerson)}, Via: policy.ViaWeb, ReceiptID: forgot.ID,
		ForgottenAt: forgot.OccurredAt, KeptAt: ptr(at(2)), ReadsBefore: 4,
		Gone: ledger.TombstoneGone{Versions: 1, Sources: 1, Embeddings: 1, Files: 1}, Agents: 1, Status: ledger.TombstoneDone,
		CompletedAt: ptr(forgot.OccurredAt.Add(150 * time.Millisecond))}}

	f.reads = ledger.ReadSummary{Total: 12,
		ByReader: []ledger.ReaderReads{{ReaderKind: "agent", Agent: "codex", Reads: 9, Readers: 1}, {ReaderKind: "person", Reads: 3, Readers: 1}},
		Memories: []ledger.SubjectReads{{Ref: "M-0001", Reads: 7, Readers: 2, FirstDay: "2026-10-06", LastReadAt: at(30)}},
		Compiles: []ledger.SubjectReads{{Ref: "C-0001", Reads: 5, Readers: 1, FirstDay: "2026-10-06", LastReadAt: at(40)}},
		Days:     []ledger.DayReads{{Day: "2026-10-06", Reads: 12}}}

	// Seal the first 13 receipts in two checkpoints; five come after.
	signer, err := receiptchain.NewEd25519Signer(fxSeed)
	if err != nil {
		t.Fatal(err)
	}
	f.keys = []export.SigningKey{{KeyID: signer.KeyID(), Algorithm: "ed25519",
		PublicKey: base64.StdEncoding.EncodeToString(signer.Public())}}
	prev := receiptchain.Genesis(fxSpace)
	var cps []ledger.Checkpoint
	from := int64(1)
	for i, n := range []int{6, 7} {
		batch := f.receipts[from-1 : int(from-1)+n]
		c := receiptchain.Seal(fxSpace, fxTenant, int64(i+1), from, prev, batch, batch[len(batch)-1].RecordedAt.Add(time.Second))
		c.KeyID = signer.KeyID()
		c.Signature, _ = signer.Sign(context.Background(), c.Statement())
		cps = append(cps, ledger.Checkpoint{ID: id(0x800 + i), SpaceID: fxSpace, TenantID: fxTenant, Number: c.Number,
			PositionFrom: c.PositionFrom, PositionTo: c.PositionTo, Receipts: n, FirstReceiptID: c.FirstReceiptID,
			LastReceiptID: c.LastReceiptID, LastSeq: c.LastSeq, PrevSHA256: hex.EncodeToString(c.Prev[:]),
			ChainSHA256: hex.EncodeToString(c.Chain[:]), MerkleRoot: hex.EncodeToString(c.MerkleRoot[:]), Format: 1, Signed: true,
			KeyID: c.KeyID, Signature: base64.StdEncoding.EncodeToString(c.Signature), SealedAt: c.SealedAt})
		prev, from = c.Chain, c.PositionTo+1
	}
	lastSealed := f.receipts[12]
	f.seal = ledger.ExportSeal{Checkpoints: cps, Unsealed: 5, Head: &ledger.ChainHead{SpaceID: fxSpace, Position: 13,
		LastSeq: lastSealed.Seq, LastReceiptID: ptr(lastSealed.ID), Head: hex.EncodeToString(prev[:]), Checkpoints: 2,
		SealedAt: ptr(cps[1].SealedAt)}}
	return f
}

// fixtureReceipts are the space's receipts in chain order. One is
// recorded out of seq order (a longer transaction committed later), and
// M-0004's reason was redacted by its Forget.
func fixtureReceipts() []receiptchain.Receipt {
	type spec struct {
		id                 int
		kind, ref, action  string
		object             uuid.UUID
		actor              string
		version            int64
		minute             int
		reason             string
		redacted           bool
		source             [2]string
		agent, via, assure string
	}
	specs := []spec{
		{0x401, "target", "AGENTS.md", "configured", fxTarget, "person", 1, -2, "", false, [2]string{}, "", "web", ""},
		{0x402, "gate", "G-0001", "asked", fxGate1, "agent", 1, -1, "", false, [2]string{}, "codex", "mcp", ""},
		{0x403, "brief", "B-0001", "revised", fxBrief, "person", 1, 2, "", false, [2]string{}, "", "web", ""},
		{0x404, "memory", "M-0001", "proposed", fxM1, "agent", 1, 0, "", false, [2]string{"pr", "PR #212"}, "codex", "mcp", ""},
		{0x405, "gate", "G-0001", "answered", fxGate1, "person", 2, 0, "", false, [2]string{}, "", "web", ""},
		{0x406, "memory", "M-0001", "kept", fxM1, "person", 2, 0, "", false, [2]string{}, "codex", "web", "human_web"},
		{0x407, "memory", "M-0002", "kept", fxM2, "person", 1, 1, "", false, [2]string{}, "", "cli", "client_attested"},
		{0x408, "memory", "M-0004", "kept", fxM4, "person", 1, 2, "kept for the release notes", true, [2]string{}, "", "web", "human_web"},
		{0x409, "memory", "M-0003", "proposed", fxM3, "agent", 1, 3, "", false, [2]string{"session", "cx-7f3a"}, "codex", "mcp", ""},
		{0x40a, "memory", "M-0005", "kept", fxM5, "person", 1, 4, "", false, [2]string{}, "", "web", "human_web"},
		{0x40b, "memory", "M-0002", "superseded", fxM2, "person", 2, 5, "replaced by M-0001", false, [2]string{"memory", "M-0001"}, "", "web", ""},
		{0x40c, "memory", "M-0001", "edited", fxM1, "person", 3, 5, "", false, [2]string{}, "", "web", ""},
		{0x40d, "memory", "M-0004", "forgot", fxM4, "person", 2, 6, "", false, [2]string{}, "", "web", ""},
		{0x40f, "memory", "M-0005", "flagged", fxM5, "memax", 2, 7, "", false, [2]string{}, "", "system", ""},
		{0x410, "brief", "B-0002", "revised", fxBrief, "person", 2, 8, "", false, [2]string{}, "", "web", ""},
		{0x411, "compile", "C-0001", "compiled", fxCompile, "memax", 1, 8, "", false, [2]string{}, "", "system", ""},
		{0x412, "gate", "G-0002", "asked", fxGate2, "agent", 1, 9, "", false, [2]string{}, "codex", "mcp", ""},
		{0x40e, "space", "space", "exported", fxSpace, "person", 1, 10, "", false, [2]string{}, "", "cli", ""},
	}
	out := make([]receiptchain.Receipt, len(specs))
	for i, s := range specs {
		r := receiptchain.Receipt{ID: id(s.id), Seq: int64(s.id - 0x400), TenantID: fxTenant, SpaceID: fxSpace,
			ObjectKind: s.kind, ObjectID: s.object, ObjectRef: s.ref, Action: s.action, ActorKind: s.actor, Via: s.via,
			OccurredAt: at(s.minute), RecordedAt: at(s.minute).Add(time.Duration(i) * time.Millisecond),
			StreamID: s.object, StreamVersion: s.version}
		switch s.actor {
		case "person":
			r.ActorID = ptr(fxPerson)
		case "agent":
			r.ActorID = ptr(fxCodex)
		}
		if s.agent != "" {
			r.Agent = ptr(s.agent)
		}
		if s.assure != "" {
			r.Assurance = ptr(s.assure)
		}
		if s.source[0] != "" {
			r.SourceKind, r.SourceRef = ptr(s.source[0]), ptr(s.source[1])
		}
		if s.kind == "agent" || s.actor == "agent" {
			r.SessionRef = ptr("cx-7f3a")
		}
		if s.reason != "" {
			sum := receiptchain.ReasonCommitment(fxSalt, s.reason)
			r.ReasonSHA256 = sum[:]
			if !s.redacted {
				r.Reason, r.ReasonSalt = ptr(s.reason), fxSalt
			}
		}
		out[i] = r
	}
	return out
}
