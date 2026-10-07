// Package export writes a space's whole record in the Memax export format
// (plan 25 §7.2, rule 14; Phase 2 epic 2.4): Markdown with frontmatter for
// people and any tool, the receipts in their canonical form and the signed
// checkpoints, so `memax verify-export` can check nothing was altered.
//
// # The format, memax.export.v1
//
// One folder per space, named by its slug:
//
//	README.md            what this is and how to verify it, and every memory by section
//	export.json          the manifest: format, space, as_of, counts, seal, and every file's SHA-256
//	memories/M-0219.md   one file per memory that isn't forgotten: frontmatter + its statement
//	tombstones/M-0201.md one per forgotten memory (and space-<id>.md per Forget of the whole space): no words
//	brief/B-0007.md      every version of the Brief, as it reads; `current: true` on the one in force
//	decisions.md         every decision by status, and the decision gates
//	gates.json           every gate (G-), as /v2 serves it
//	targets.json         every compile target with its latest compile (C-)
//	agents.json          the agent connections that work in the space or wrote to it
//	reads.json           reads as counts (never what was read for, never words)
//	receipts.jsonl       every receipt in chain order, one JSON object per line, its canonical fields
//	checkpoints.json     every signed checkpoint, the seal status, and the public keys
//
// The same record always gives the same bytes: everything is sorted, times
// are UTC to the microsecond, nothing depends on the clock or the reader
// (a gate's status is the stored one, with expires_at; reads are all-time
// counts; the nightly verifier's and object storage's bookkeeping is left
// out). Each export is itself a receipt (exported), so two exports differ
// by that receipt; a retry with the same idempotency key writes none and
// gives the same bytes.
//
// What the export never holds: a forgotten memory's words (its statement,
// sources, decision fields; Forget purged them, and the tombstone keeps
// IDs, times and counts only, not even the person's note), a redacted
// receipt reason (only its SHA-256 commitment, which is what the chain
// needs), the judge's working notes, idempotency records and query text.
//
// The writer streams: it receives the record from ledger.ReadExport part
// by part and writes each file as soon as it can, keeping only short
// summaries for the index files it writes last.
package export

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/lifecycle"
	"github.com/MemaxLabs/memax/packages/server/internal/receiptchain"
)

// The format and the per-file format names. A change that a v1 reader
// can't read is a new version; v1 stays readable.
const (
	Format            = "memax.export.v1"
	MemoryFormat      = "memax.memory.v1"
	TombstoneFormat   = "memax.tombstone.v1"
	BriefFormat       = "memax.brief.v1"
	GatesFormat       = "memax.gates.v1"
	TargetsFormat     = "memax.targets.v1"
	AgentsFormat      = "memax.agents.v1"
	ReadsFormat       = "memax.reads.v1"
	CheckpointsFormat = "memax.checkpoints.v1"
)

// The fixed file names.
const (
	ManifestFile    = "export.json"
	ReadmeFile      = "README.md"
	DecisionsFile   = "decisions.md"
	GatesFile       = "gates.json"
	TargetsFile     = "targets.json"
	AgentsFile      = "agents.json"
	ReadsFile       = "reads.json"
	ReceiptsFile    = "receipts.jsonl"
	CheckpointsFile = "checkpoints.json"
	MemoriesDir     = "memories/"
	TombstonesDir   = "tombstones/"
	BriefDir        = "brief/"
)

// Sink receives the export's files one at a time, in order.
type Sink interface {
	// Create starts the next file, ending the one before.
	Create(path string) (io.Writer, error)
}

// SigningKey is a public key checkpoints may be signed with.
type SigningKey struct {
	KeyID     string `json:"key_id"`
	Algorithm string `json:"algorithm"`
	PublicKey string `json:"public_key"`
}

// Options configure a Writer.
type Options struct {
	// Receipt is the export's own `exported` receipt.
	Receipt uuid.UUID
	// Keys are the public keys embedded in checkpoints.json, sorted by id.
	Keys []SigningKey
	// OnSpace is called with the space before the first file: the handler
	// names the download and dates the archive's entries from it.
	OnSpace func(ledger.ExportSpaceInfo) error
}

// ManifestEntry is one file in export.json.
type ManifestEntry struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
}

// Writer turns the record ledger.ReadExport hands over into the export's
// files. It implements ledger.ExportSink; call Finish after ReadExport.
type Writer struct {
	sink Sink
	opts Options

	space ledger.ExportSpaceInfo
	files []ManifestEntry
	cur   *countingHash
	path  string

	briefs     []ledger.Brief
	briefRefs  map[string]bool
	statements map[string]string
	gates      []ledger.Gate
	tombs      map[uuid.UUID]ledger.Tombstone
	tombsUsed  map[uuid.UUID]bool
	summaries  []summary
	counts     Counts

	receipts    int64
	receiptsOut io.Writer
	seal        ledger.ExportSeal
	finished    bool
}

// Counts are how many of each thing the export holds.
type Counts struct {
	Memories      int   `json:"memories"`
	Tombstones    int   `json:"tombstones"`
	BriefVersions int   `json:"brief_versions"`
	Gates         int   `json:"gates"`
	Targets       int   `json:"targets"`
	Agents        int   `json:"agents"`
	Receipts      int64 `json:"receipts"`
	Checkpoints   int   `json:"checkpoints"`
}

// summary is what the index files need of one memory.
type summary struct {
	ref       string
	section   ledger.Section
	kind      ledger.Kind
	state     lifecycle.Mark
	lifecycle lifecycle.Lifecycle
	decision  string
	line      string
}

// NewWriter writes an export to sink.
func NewWriter(sink Sink, o Options) *Writer {
	keys := slices.Clone(o.Keys)
	slices.SortFunc(keys, func(a, b SigningKey) int { return strings.Compare(a.KeyID, b.KeyID) })
	o.Keys = keys
	return &Writer{sink: sink, opts: o, briefRefs: map[string]bool{}, statements: map[string]string{},
		tombs: map[uuid.UUID]ledger.Tombstone{}, tombsUsed: map[uuid.UUID]bool{}}
}

// countingHash hashes and counts what passes through to w.
type countingHash struct {
	w io.Writer
	h hash.Hash
	n int64
}

func (c *countingHash) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.h.Write(p[:n])
	c.n += int64(n)
	return n, err
}

func (w *Writer) begin(path string) (io.Writer, error) {
	w.end()
	out, err := w.sink.Create(path)
	if err != nil {
		return nil, fmt.Errorf("export: %s: %w", path, err)
	}
	w.cur, w.path = &countingHash{w: out, h: sha256.New()}, path
	return w.cur, nil
}

func (w *Writer) end() {
	if w.cur == nil {
		return
	}
	w.files = append(w.files, ManifestEntry{Path: w.path, SHA256: hex.EncodeToString(w.cur.h.Sum(nil)), Bytes: w.cur.n})
	w.cur, w.path = nil, ""
}

func (w *Writer) write(path string, data []byte) error {
	out, err := w.begin(path)
	if err != nil {
		return err
	}
	if _, err := out.Write(data); err != nil {
		return fmt.Errorf("export: %s: %w", path, err)
	}
	w.end()
	return nil
}

// writeJSON writes v indented, with <, > and & as themselves.
func (w *Writer) writeJSON(path string, v any) error {
	return w.write(path, indentJSON(v))
}

func indentJSON(v any) []byte {
	var b strings.Builder
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	e.SetIndent("", "  ")
	if err := e.Encode(v); err != nil {
		panic(fmt.Sprintf("export: encode %T: %v", v, err))
	}
	return []byte(b.String())
}

// Space implements ledger.ExportSink.
func (w *Writer) Space(info ledger.ExportSpaceInfo) error {
	w.space = info
	if w.opts.OnSpace != nil {
		return w.opts.OnSpace(info)
	}
	return nil
}

// Briefs implements ledger.ExportSink. The files are written at Finish,
// once the memories they place are read.
func (w *Writer) Briefs(bs []ledger.Brief) error {
	w.briefs = bs
	for _, b := range bs {
		for _, s := range b.Sections {
			for _, it := range s.Items {
				if it.Ref != "" {
					w.briefRefs[it.Ref] = true
				}
			}
		}
	}
	w.counts.BriefVersions = len(bs)
	return nil
}

// Gates implements ledger.ExportSink.
func (w *Writer) Gates(gs []ledger.Gate) error {
	w.gates = gs
	w.counts.Gates = len(gs)
	return w.writeJSON(GatesFile, struct {
		Format string        `json:"format"`
		Note   string        `json:"note"`
		Gates  []ledger.Gate `json:"gates"`
	}{GatesFormat, "A waiting gate keeps its stored status; it has expired once expires_at is past.", nonNil(gs)})
}

// Targets implements ledger.ExportSink.
func (w *Writer) Targets(ts []ledger.Target) error {
	w.counts.Targets = len(ts)
	return w.writeJSON(TargetsFile, struct {
		Format  string          `json:"format"`
		Targets []ledger.Target `json:"targets"`
	}{TargetsFormat, nonNil(ts)})
}

// Agents implements ledger.ExportSink.
func (w *Writer) Agents(as []ledger.ExportAgent) error {
	w.counts.Agents = len(as)
	return w.writeJSON(AgentsFile, struct {
		Format string               `json:"format"`
		Agents []ledger.ExportAgent `json:"agents"`
	}{AgentsFormat, nonNil(as)})
}

// Tombstones implements ledger.ExportSink: a whole space's Forget is
// written now, a memory's with the memory.
func (w *Writer) Tombstones(ts []ledger.Tombstone) error {
	for _, t := range ts {
		if t.Kind != ledger.ObjectMemory {
			if err := w.write(TombstonesDir+"space-"+t.ID.String()+".md", tombstoneFile(t, nil)); err != nil {
				return err
			}
			w.counts.Tombstones++
			continue
		}
		// The latest tombstone of an object is its own (a re-applied Forget
		// keeps the first).
		if prev, ok := w.tombs[t.ObjectID]; !ok || !t.ForgottenAt.Before(prev.ForgottenAt) {
			w.tombs[t.ObjectID] = t
		}
	}
	return nil
}

// Reads implements ledger.ExportSink.
func (w *Writer) Reads(r ledger.ReadSummary) error {
	return w.writeJSON(ReadsFile, struct {
		Format string `json:"format"`
		Note   string `json:"note"`
		ledger.ReadSummary
	}{ReadsFormat, "Reads by agents and people, counted per day. Counts only: never what was read for, never words. " +
		"A compiled file's load counts once for the compile (C-), not for each memory in it.", r})
}

// Memories implements ledger.ExportSink.
func (w *Writer) Memories(batch []ledger.ExportMemory) error {
	for _, m := range batch {
		mem := m.Memory
		if mem.Lifecycle == lifecycle.Forgotten {
			t, ok := w.tombs[mem.ID]
			var tp *ledger.Tombstone
			if ok {
				tp = &t
				w.tombsUsed[mem.ID] = true
			} else {
				tp = &ledger.Tombstone{Ref: mem.Ref, Kind: ledger.ObjectMemory, ObjectID: mem.ID, SpaceID: mem.SpaceID,
					TenantID: mem.TenantID, With: []string{}}
			}
			if err := w.write(TombstonesDir+mem.Ref+".md", tombstoneFile(*tp, m.Receipts)); err != nil {
				return err
			}
			w.counts.Tombstones++
			continue
		}
		if err := w.write(MemoriesDir+mem.Ref+".md", memoryFile(m)); err != nil {
			return err
		}
		w.counts.Memories++
		if w.briefRefs[mem.Ref] {
			w.statements[mem.Ref] = mem.Statement
		}
		dec := ""
		if mem.Decision != nil {
			dec = mem.Decision.Status
		}
		w.summaries = append(w.summaries, summary{ref: mem.Ref, section: mem.Section, kind: mem.Kind, state: mem.State,
			lifecycle: mem.Lifecycle, decision: dec, line: firstLine(mem.Statement, 120)})
	}
	return nil
}

// Receipts implements ledger.ExportSink: receipts.jsonl, written as the
// batches arrive.
func (w *Writer) Receipts(batch []receiptchain.Receipt) error {
	if w.receiptsOut == nil {
		out, err := w.begin(ReceiptsFile)
		if err != nil {
			return err
		}
		w.receiptsOut = out
	}
	for _, r := range batch {
		if _, err := w.receiptsOut.Write(ReceiptLine(r)); err != nil {
			return fmt.Errorf("export: %s: %w", ReceiptsFile, err)
		}
		w.receipts++
	}
	return nil
}

// Seal implements ledger.ExportSink: it ends receipts.jsonl and writes
// checkpoints.json.
func (w *Writer) Seal(s ledger.ExportSeal) error {
	if w.receiptsOut == nil {
		if err := w.write(ReceiptsFile, nil); err != nil {
			return err
		}
	}
	w.end()
	w.receiptsOut = nil
	w.seal = s
	w.counts.Receipts = w.receipts
	w.counts.Checkpoints = len(s.Checkpoints)
	return w.writeJSON(CheckpointsFile, checkpointsDoc(w.space.ID, s, w.opts.Keys))
}

// Finish writes the files that need the whole record (the Brief, the
// index files) and, last, the manifest.
func (w *Writer) Finish() error {
	if w.finished {
		return nil
	}
	w.finished = true
	w.end()
	// A memory's tombstone whose memory row is gone (never, unless a
	// space was retired midway) still says it was forgotten.
	var left []ledger.Tombstone
	for id, t := range w.tombs {
		if !w.tombsUsed[id] {
			left = append(left, t)
		}
	}
	slices.SortFunc(left, func(a, b ledger.Tombstone) int { return strings.Compare(a.Ref, b.Ref) })
	for _, t := range left {
		if err := w.write(TombstonesDir+t.Ref+".md", tombstoneFile(t, nil)); err != nil {
			return err
		}
		w.counts.Tombstones++
	}
	for _, b := range w.briefs {
		if err := w.write(BriefDir+b.Ref+".md", briefFile(b, w.statements)); err != nil {
			return err
		}
	}
	if err := w.write(DecisionsFile, decisionsFile(w.space, w.summaries, w.gates)); err != nil {
		return err
	}
	if err := w.write(ReadmeFile, readmeFile(w.space, w.counts, w.summaries, w.briefs, w.seal)); err != nil {
		return err
	}
	return w.writeJSON(ManifestFile, w.manifest())
}

// Manifest is export.json.
type Manifest struct {
	Format  string           `json:"format"`
	Space   ManifestSpace    `json:"space"`
	AsOf    *string          `json:"as_of"`
	Receipt *uuid.UUID       `json:"receipt"`
	Counts  Counts           `json:"counts"`
	Seal    ManifestSeal     `json:"seal"`
	Files   []ManifestEntry  `json:"files"`
	Formats []ManifestFormat `json:"formats"`
}

// ManifestSpace is the exported space.
type ManifestSpace struct {
	ID          uuid.UUID `json:"id"`
	TenantID    uuid.UUID `json:"tenant_id"`
	Slug        string    `json:"slug"`
	Name        string    `json:"name"`
	Kind        string    `json:"kind"`
	Repository  *string   `json:"repository"`
	V2EnabledAt *string   `json:"v2_enabled_at"`
}

// ManifestSeal is how far the receipts are sealed: receipts 1 to
// SealedReceipts (by chain position) are in checkpoints; Unsealed came
// after.
type ManifestSeal struct {
	SealedReceipts         int64      `json:"sealed_receipts"`
	SealedThroughSeq       *int64     `json:"sealed_through_seq"`
	SealedThroughReceiptID *uuid.UUID `json:"sealed_through_receipt_id"`
	Unsealed               int64      `json:"unsealed"`
}

// ManifestFormat names the format of a kind of file, for tools that look.
type ManifestFormat struct {
	Path   string `json:"path"`
	Format string `json:"format"`
}

func (w *Writer) manifest() Manifest {
	files := slices.Clone(w.files)
	slices.SortFunc(files, func(a, b ManifestEntry) int { return strings.Compare(a.Path, b.Path) })
	sp := w.space
	m := Manifest{Format: Format, Counts: w.counts, Files: files,
		Space: ManifestSpace{ID: sp.ID, TenantID: sp.TenantID, Slug: sp.Slug, Name: sp.Name, Kind: string(sp.Kind)},
		Formats: []ManifestFormat{
			{MemoriesDir + "*.md", MemoryFormat}, {TombstonesDir + "*.md", TombstoneFormat}, {BriefDir + "*.md", BriefFormat},
			{GatesFile, GatesFormat}, {TargetsFile, TargetsFormat}, {AgentsFile, AgentsFormat}, {ReadsFile, ReadsFormat},
			{ReceiptsFile, "memax.receipt.v1 (receiptchain format 1, as JSON lines)"}, {CheckpointsFile, CheckpointsFormat},
		}}
	if sp.Repository != "" {
		repo := sp.Repository
		m.Space.Repository = &repo
	}
	if sp.V2EnabledAt != nil {
		at := stamp(*sp.V2EnabledAt)
		m.Space.V2EnabledAt = &at
	}
	if !sp.AsOf.IsZero() {
		at := stamp(sp.AsOf)
		m.AsOf = &at
	}
	if w.opts.Receipt != uuid.Nil {
		id := w.opts.Receipt
		m.Receipt = &id
	}
	m.Seal = ManifestSeal{Unsealed: w.seal.Unsealed}
	if h := w.seal.Head; h != nil && h.Position > 0 {
		seq := h.LastSeq
		m.Seal.SealedReceipts, m.Seal.SealedThroughSeq, m.Seal.SealedThroughReceiptID = h.Position, &seq, h.LastReceiptID
	}
	return m
}

// Checkpoints is checkpoints.json.
type Checkpoints struct {
	Format      string           `json:"format"`
	SpaceID     uuid.UUID        `json:"space_id"`
	Seal        CheckpointSeal   `json:"seal"`
	Keys        []SigningKey     `json:"keys"`
	Checkpoints []CheckpointJSON `json:"checkpoints"`
}

// CheckpointSeal is the chain head as of the export.
type CheckpointSeal struct {
	SealedReceipts         int64      `json:"sealed_receipts"`
	SealedThroughSeq       *int64     `json:"sealed_through_seq,omitempty"`
	SealedThroughReceiptID *uuid.UUID `json:"sealed_through_receipt_id,omitempty"`
	HeadSHA256             string     `json:"head_sha256,omitempty"`
	Checkpoints            int64      `json:"checkpoints"`
	SealedAt               *string    `json:"sealed_at,omitempty"`
	Unsealed               int64      `json:"unsealed"`
}

// CheckpointJSON is one checkpoint as /v2 serves it, without when its
// copy reached object storage (bookkeeping, not the chain).
type CheckpointJSON struct {
	ID             uuid.UUID `json:"id"`
	SpaceID        uuid.UUID `json:"space_id"`
	TenantID       uuid.UUID `json:"tenant_id"`
	Number         int64     `json:"number"`
	PositionFrom   int64     `json:"position_from"`
	PositionTo     int64     `json:"position_to"`
	Receipts       int       `json:"receipts"`
	FirstReceiptID uuid.UUID `json:"first_receipt_id"`
	LastReceiptID  uuid.UUID `json:"last_receipt_id"`
	LastSeq        int64     `json:"last_seq"`
	PrevSHA256     string    `json:"prev_sha256"`
	ChainSHA256    string    `json:"chain_sha256"`
	MerkleRoot     string    `json:"merkle_root"`
	Format         int       `json:"format"`
	Signed         bool      `json:"signed"`
	KeyID          string    `json:"key_id,omitempty"`
	Signature      string    `json:"signature,omitempty"`
	SealedAt       string    `json:"sealed_at"`
}

func checkpointsDoc(space uuid.UUID, s ledger.ExportSeal, keys []SigningKey) Checkpoints {
	out := Checkpoints{Format: CheckpointsFormat, SpaceID: space, Keys: nonNil(keys),
		Checkpoints: make([]CheckpointJSON, len(s.Checkpoints)), Seal: CheckpointSeal{Unsealed: s.Unsealed}}
	for i, c := range s.Checkpoints {
		out.Checkpoints[i] = CheckpointJSON{ID: c.ID, SpaceID: c.SpaceID, TenantID: c.TenantID, Number: c.Number,
			PositionFrom: c.PositionFrom, PositionTo: c.PositionTo, Receipts: c.Receipts, FirstReceiptID: c.FirstReceiptID,
			LastReceiptID: c.LastReceiptID, LastSeq: c.LastSeq, PrevSHA256: c.PrevSHA256, ChainSHA256: c.ChainSHA256,
			MerkleRoot: c.MerkleRoot, Format: c.Format, Signed: c.Signed, KeyID: c.KeyID, Signature: c.Signature,
			SealedAt: stamp(c.SealedAt)}
	}
	if h := s.Head; h != nil {
		out.Seal.SealedReceipts, out.Seal.Checkpoints, out.Seal.HeadSHA256 = h.Position, h.Checkpoints, h.Head
		if h.SealedAt != nil {
			at := stamp(*h.SealedAt)
			out.Seal.SealedAt = &at
		}
		if h.Position > 0 {
			seq := h.LastSeq
			out.Seal.SealedThroughSeq, out.Seal.SealedThroughReceiptID = &seq, h.LastReceiptID
		}
	}
	return out
}

// receiptLine is one line of receipts.jsonl: the receipt's canonical
// fields (receiptchain format 1) in their order, with the reason and its
// salt while they exist. Absent values are null, never left out.
type receiptLine struct {
	ID            uuid.UUID      `json:"id"`
	Seq           int64          `json:"seq"`
	TenantID      uuid.UUID      `json:"tenant_id"`
	SpaceID       uuid.UUID      `json:"space_id"`
	ObjectKind    string         `json:"object_kind"`
	ObjectID      uuid.UUID      `json:"object_id"`
	ObjectRef     string         `json:"object_ref"`
	Action        string         `json:"action"`
	ActorKind     string         `json:"actor_kind"`
	ActorID       *uuid.UUID     `json:"actor_id"`
	Agent         *string        `json:"agent"`
	Via           string         `json:"via"`
	Assurance     *string        `json:"assurance"`
	SessionRef    *string        `json:"session_ref"`
	Source        *receiptSource `json:"source"`
	Reason        *string        `json:"reason"`
	ReasonSalt    *string        `json:"reason_salt"`
	ReasonSHA256  *string        `json:"reason_sha256"`
	OccurredAt    string         `json:"occurred_at"`
	RecordedAt    string         `json:"recorded_at"`
	StreamID      uuid.UUID      `json:"stream_id"`
	StreamVersion int64          `json:"stream_version"`
}

type receiptSource struct {
	Kind *string `json:"kind"`
	Ref  *string `json:"ref"`
}

// ReceiptLine is one receipt as receipts.jsonl writes it, with its newline.
func ReceiptLine(r receiptchain.Receipt) []byte {
	l := receiptLine{ID: r.ID, Seq: r.Seq, TenantID: r.TenantID, SpaceID: r.SpaceID, ObjectKind: r.ObjectKind,
		ObjectID: r.ObjectID, ObjectRef: r.ObjectRef, Action: r.Action, ActorKind: r.ActorKind, ActorID: r.ActorID,
		Agent: r.Agent, Via: r.Via, Assurance: r.Assurance, SessionRef: r.SessionRef, Reason: r.Reason,
		OccurredAt: stamp(r.OccurredAt), RecordedAt: stamp(r.RecordedAt), StreamID: r.StreamID, StreamVersion: r.StreamVersion}
	if r.SourceKind != nil || r.SourceRef != nil {
		l.Source = &receiptSource{Kind: r.SourceKind, Ref: r.SourceRef}
	}
	if r.ReasonSalt != nil {
		s := hex.EncodeToString(r.ReasonSalt)
		l.ReasonSalt = &s
	}
	if r.ReasonSHA256 != nil {
		s := hex.EncodeToString(r.ReasonSHA256)
		l.ReasonSHA256 = &s
	}
	var b strings.Builder
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	if err := e.Encode(l); err != nil {
		panic(fmt.Sprintf("export: encode receipt %s: %v", r.ID, err))
	}
	return []byte(b.String())
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// firstLine is the statement's first line, cut to n characters.
func firstLine(s string, n int) string {
	line, _, _ := strings.Cut(s, "\n")
	line = strings.TrimSpace(line)
	if utf8.RuneCountInString(line) <= n {
		return line
	}
	r := []rune(line)
	return strings.TrimSpace(string(r[:n-1])) + "…"
}

// stampOf formats a time for prose: "2026-10-07 14:02 UTC".
func stampOf(t time.Time) string { return t.UTC().Format("2006-01-02 15:04 UTC") }
