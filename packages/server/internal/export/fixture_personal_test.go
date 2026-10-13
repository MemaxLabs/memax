package export_test

import (
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

// The personal fixture (personal-layer design §5.5): a Personal space
// linked to two machines, one unlinked since, each with its home files,
// and the receipts that did it. It is the SDK's second golden export
// (packages/sdk/src/v2/testdata/export-v1/personal), so a reader and the
// verifier are held to a space with home targets, their machines and the
// machines' receipts.

var (
	pxSpace    = id(0x1001)
	pxTenant   = id(0x1002)
	pxPerson   = id(0x1003)
	pxBrief    = id(0x1005)
	pxBriefV1  = id(0x1006)
	pxWork     = id(0x1010) // machine-1, work-mbp, linked
	pxHome     = id(0x1011) // machine-2, home-linux, unlinked since
	pxRules    = id(0x1020) // Claude Code's rules file on work-mbp
	pxCodex    = id(0x1021) // Codex's block on home-linux
	pxCompile1 = id(0x1030)
	pxCompile2 = id(0x1031)
	pxM1       = id(0x1101) // kept, imported from the person's ~/.claude/CLAUDE.md
	pxM2       = id(0x1102) // kept
	pxSource   = id(0x1201)
	pxExport   = id(0x140c) // the export's own receipt
)

func personalReceipts() []receiptchain.Receipt {
	type spec struct {
		id                     int
		kind, ref, action      string
		object                 uuid.UUID
		actor                  string
		version                int64
		minute                 int
		reason, via, assurance string
	}
	specs := []spec{
		{0x1401, "machine", "machine-1", "linked", pxWork, "person", 1, 0, "", "cli", ""},
		{0x1402, "brief", "B-0001", "revised", pxBrief, "person", 1, 0, ledger.FirstPersonalBriefReason, "cli", ""},
		{0x1403, "target", "…claude/rules/memax-personal.md", "configured", pxRules, "person", 1, 0, "", "cli", ""},
		{0x1404, "machine", "machine-2", "linked", pxHome, "person", 1, 3, "", "cli", ""},
		{0x1405, "target", "~/.codex/AGENTS.md", "configured", pxCodex, "person", 1, 3, "", "cli", ""},
		{0x1406, "memory", "M-0001", "kept", pxM1, "person", 1, 5, "", "web", "human_web"},
		{0x1407, "memory", "M-0002", "kept", pxM2, "person", 1, 6, "", "cli", "client_attested"},
		{0x1408, "compile", "C-0001", "compiled", pxCompile1, "memax", 1, 7, "", "system", ""},
		{0x1409, "compile", "C-0002", "compiled", pxCompile2, "memax", 1, 7, "", "system", ""},
		{0x140a, "machine", "machine-2", "unlinked", pxHome, "person", 2, 20, "", "web", ""},
		{0x140b, "target", "~/.codex/AGENTS.md", "stopped", pxCodex, "person", 2, 20, "", "web", ""},
		{0x140c, "space", "space", "exported", pxSpace, "person", 1, 30, "", "cli", ""},
	}
	out := make([]receiptchain.Receipt, len(specs))
	for i, s := range specs {
		r := receiptchain.Receipt{ID: id(s.id), Seq: int64(s.id - 0x1400), TenantID: pxTenant, SpaceID: pxSpace,
			ObjectKind: s.kind, ObjectID: s.object, ObjectRef: s.ref, Action: s.action, ActorKind: s.actor, Via: s.via,
			OccurredAt: at(s.minute), RecordedAt: at(s.minute).Add(time.Duration(i) * time.Millisecond),
			StreamID: s.object, StreamVersion: s.version}
		if s.actor == "person" {
			r.ActorID = ptr(pxPerson)
		}
		if s.assurance != "" {
			r.Assurance = ptr(s.assurance)
		}
		if s.reason != "" {
			sum := receiptchain.ReasonCommitment(fxSalt, s.reason)
			r.ReasonSHA256, r.Reason, r.ReasonSalt = sum[:], ptr(s.reason), fxSalt
		}
		out[i] = r
	}
	return out
}

func newPersonalFixture(t testing.TB) *fixture {
	t.Helper()
	f := &fixture{receipts: personalReceipts(), exported: pxExport}
	receipt := func(object uuid.UUID, action string) receiptchain.Receipt {
		for _, r := range f.receipts {
			if r.ObjectID == object && r.Action == action {
				return r
			}
		}
		t.Fatalf("no %s receipt for %s", action, object)
		return receiptchain.Receipt{}
	}
	stamps := func(object uuid.UUID) []ledger.ReceiptStamp {
		var out []ledger.ReceiptStamp
		for _, r := range f.receipts {
			if r.ObjectID == object {
				out = append(out, ledger.ReceiptStamp{ID: r.ID, Seq: r.Seq, Action: ledger.Action(r.Action), OccurredAt: r.OccurredAt})
			}
		}
		return out
	}
	last := f.receipts[len(f.receipts)-1]
	f.space = ledger.ExportSpaceInfo{
		Space: ledger.Space{ID: pxSpace, TenantID: pxTenant, Slug: "personal", Name: "Personal", Kind: policy.SpacePersonal,
			V2EnabledAt: ptr(at(-60))},
		AsOf: last.RecordedAt, Receipts: int64(len(f.receipts)),
	}

	kept := func(mid uuid.UUID, ref, statement string, minute int, sources []ledger.Source) ledger.ExportMemory {
		k := receipt(mid, "kept")
		return ledger.ExportMemory{
			Memory: ledger.Memory{ID: mid, Ref: ref, SpaceID: pxSpace, TenantID: pxTenant, Statement: statement,
				Section: ledger.SectionPreferences, Kind: ledger.KindFact, State: lifecycle.MarkKept, Lifecycle: lifecycle.Kept,
				Flags: lifecycle.Flags{}, Trust: policy.TrustPerson, Version: 1, Conditions: json.RawMessage(`[]`),
				Applies: json.RawMessage(`{}`), CreatedReceiptID: k.ID, LastReceiptID: k.ID, CreatedAt: at(minute),
				UpdatedAt: at(minute), Sources: sources},
			Versions: []ledger.MemoryVersion{{Version: 1, Statement: statement, ReceiptID: k.ID, CreatedAt: at(minute)}},
			Receipts: stamps(mid),
		}
	}
	f.memories = []ledger.ExportMemory{
		kept(pxM1, "M-0001", "Answer in British English, in short paragraphs.", 5, []ledger.Source{{ID: pxSource,
			Kind: ledger.SourceFile, Ref: "~/.claude/CLAUDE.md:3", Locator: json.RawMessage(`{"line": 3, "path": "~/.claude/CLAUDE.md"}`),
			Trust: policy.TrustPerson, CreatedAt: at(5)}}),
		kept(pxM2, "M-0002", "Search with rg, not grep.", 6, nil),
	}

	f.briefs = []ledger.Brief{{ID: pxBrief, VersionID: pxBriefV1, Ref: "B-0001", Version: 1, SpaceID: pxSpace, TenantID: pxTenant,
		Title: "Personal", Current: true,
		Sections: []ledger.BriefSection{
			{Key: "preferences", Heading: "Preferences", Items: []ledger.BriefItem{}},
			{Key: "conventions", Heading: "Conventions", Items: []ledger.BriefItem{}},
			{Key: "decisions", Heading: "Decisions", Items: []ledger.BriefItem{}},
			{Key: "open", Heading: "Open questions", Items: []ledger.BriefItem{}},
		},
		ReceiptID: receipt(pxBrief, "revised").ID, CreatedAt: at(0)}}

	f.machines = []ledger.Machine{
		{ID: pxWork, SpaceID: pxSpace, Label: "machine-1", Name: "work-mbp", Platform: "darwin", DeviceID: id(0x1050), Linked: true,
			LinkedAt: at(0), Version: 1, CreatedReceiptID: receipt(pxWork, "linked").ID, LastReceiptID: receipt(pxWork, "linked").ID,
			CreatedAt: at(0), UpdatedAt: at(0)},
		{ID: pxHome, SpaceID: pxSpace, Label: "machine-2", Name: "home-linux", Platform: "linux", DeviceID: id(0x1051),
			LinkedAt: at(3), UnlinkedAt: ptr(at(20)), Version: 2, CreatedReceiptID: receipt(pxHome, "linked").ID,
			LastReceiptID: receipt(pxHome, "unlinked").ID, CreatedAt: at(3), UpdatedAt: at(20)},
	}
	run := func(cid uuid.UUID, ref string, target uuid.UUID, path string) *ledger.CompileRun {
		return &ledger.CompileRun{ID: cid, Ref: ref, TargetID: target, SpaceID: pxSpace, Brief: "B-0001", BriefVersion: 1,
			Generation: 2, Status: ledger.CompileDelivered, InputSHA256: "aa", OutputSHA256: "bb", DriftSHA256: "cc", Bytes: 412,
			Lines: 12, Refs: []string{"M-0001", "M-0002"}, DroppedForBudget: []string{},
			Files: []ledger.CompiledOutput{{Path: path, SHA256: "bb", DriftSHA256: "cc", Bytes: 412, Lines: 12,
				Refs: []string{"M-0001", "M-0002"}, Cites: []string{}, DroppedForBudget: []string{}}},
			Warnings: []ledger.CompileWarning{}, EnqueuedAt: at(7), StartedAt: at(7), CompiledAt: at(7), DeliveredAt: ptr(at(8)),
			ReceiptID: receipt(cid, "compiled").ID}
	}
	home := func(tid uuid.UUID, kind ledger.TargetKind, path string, m ledger.Machine, owned bool, readBy string,
		state ledger.SyncState, version int, c *ledger.CompileRun, last uuid.UUID, updated int) ledger.Target {
		return ledger.Target{ID: tid, SpaceID: pxSpace, TenantID: pxTenant, Kind: kind, Root: ledger.RootHome, Path: path,
			Label: "~/" + path, Machine: &ledger.TargetMachine{ID: m.ID, Label: m.Label, Name: m.Name},
			Settings: ledger.TargetSettings{Include: ledger.IncludeKeptOnly, Stale: ledger.StaleMark,
				SizeBudget: ledger.DefaultHomeSizeBudget, UserOwned: owned, ReadBy: []string{readBy}},
			Delivery: ledger.DeliveryLocal, SyncState: state, Version: version, DirtyGen: 2, CompiledGen: 2, DirtyAt: at(6),
			LastCompile: c, Delivered: &ledger.Delivered{CompileID: ptr(c.ID), Compile: c.Ref, SHA256: "cc",
				Files: []ledger.DeliveredFile{{Path: path, SHA256: "cc"}}, At: ptr(at(8))},
			CreatedReceiptID: receipt(tid, "configured").ID, LastReceiptID: last, CreatedAt: c.EnqueuedAt.Add(-7 * 60e9),
			UpdatedAt: at(updated)}
	}
	f.targets = []ledger.Target{
		home(pxRules, ledger.TargetClaudeUser, ".claude/rules/memax-personal.md", f.machines[0], false, "claude-code",
			ledger.SyncInSync, 1, run(pxCompile1, "C-0001", pxRules, ".claude/rules/memax-personal.md"),
			receipt(pxRules, "configured").ID, 8),
		home(pxCodex, ledger.TargetCodexUser, ".codex/AGENTS.md", f.machines[1], true, "codex",
			ledger.SyncOff, 2, run(pxCompile2, "C-0002", pxCodex, ".codex/AGENTS.md"),
			receipt(pxCodex, "stopped").ID, 20),
	}
	f.agents = []ledger.ExportAgent{}
	f.tombstones = []ledger.Tombstone{}
	f.reads = ledger.ReadSummary{Total: 3,
		ByReader: []ledger.ReaderReads{{ReaderKind: "person", Agent: "claude-code", Reads: 3, Readers: 1}},
		Memories: []ledger.SubjectReads{},
		Compiles: []ledger.SubjectReads{{Ref: "C-0001", Reads: 3, Readers: 1, FirstDay: "2026-10-06", LastReadAt: at(25)}},
		Days:     []ledger.DayReads{{Day: "2026-10-06", Reads: 3}}}

	// The first nine receipts sealed in one checkpoint; three come after.
	signer, err := receiptchain.NewEd25519Signer(fxSeed)
	if err != nil {
		t.Fatal(err)
	}
	f.keys = []export.SigningKey{{KeyID: signer.KeyID(), Algorithm: "ed25519",
		PublicKey: base64.StdEncoding.EncodeToString(signer.Public())}}
	batch := f.receipts[:9]
	c := receiptchain.Seal(pxSpace, pxTenant, 1, 1, receiptchain.Genesis(pxSpace), batch, batch[8].RecordedAt.Add(time.Second))
	c.KeyID = signer.KeyID()
	c.Signature, _ = signer.Sign(context.Background(), c.Statement())
	cp := ledger.Checkpoint{ID: id(0x1800), SpaceID: pxSpace, TenantID: pxTenant, Number: c.Number,
		PositionFrom: c.PositionFrom, PositionTo: c.PositionTo, Receipts: 9, FirstReceiptID: c.FirstReceiptID,
		LastReceiptID: c.LastReceiptID, LastSeq: c.LastSeq, PrevSHA256: hex.EncodeToString(c.Prev[:]),
		ChainSHA256: hex.EncodeToString(c.Chain[:]), MerkleRoot: hex.EncodeToString(c.MerkleRoot[:]), Format: 1, Signed: true,
		KeyID: c.KeyID, Signature: base64.StdEncoding.EncodeToString(c.Signature), SealedAt: c.SealedAt}
	f.seal = ledger.ExportSeal{Checkpoints: []ledger.Checkpoint{cp}, Unsealed: 3, Head: &ledger.ChainHead{SpaceID: pxSpace,
		Position: 9, LastSeq: batch[8].Seq, LastReceiptID: ptr(batch[8].ID), Head: hex.EncodeToString(c.Chain[:]),
		Checkpoints: 1, SealedAt: ptr(cp.SealedAt)}}
	return f
}
