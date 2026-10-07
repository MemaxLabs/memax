package export

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.yaml.in/yaml/v3"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/lifecycle"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
	"github.com/MemaxLabs/memax/packages/server/internal/receiptchain"
)

// Parse reads an export back (rule 14's round trip): the files, by path
// below the space's folder, become the record they were written from. The
// frontmatter is read with a standard YAML parser, as any tool would.
// Parse checks the format, not the hashes or the chain: that is
// verification (the SDK's verifyExport, `memax verify-export`).
func Parse(files map[string][]byte) (*Record, error) {
	r := &Record{}
	if err := decodeJSON(files, ManifestFile, &r.Manifest); err != nil {
		return nil, err
	}
	if r.Manifest.Format != Format {
		return nil, fmt.Errorf("export: %s is format %q; this reads %s", ManifestFile, r.Manifest.Format, Format)
	}
	var gates struct {
		Gates []ledger.Gate `json:"gates"`
	}
	var targets struct {
		Targets []ledger.Target `json:"targets"`
	}
	var agents struct {
		Agents []ledger.ExportAgent `json:"agents"`
	}
	for name, v := range map[string]any{GatesFile: &gates, TargetsFile: &targets, AgentsFile: &agents,
		ReadsFile: &r.Reads, CheckpointsFile: &r.Checkpoints} {
		if err := decodeJSON(files, name, v); err != nil {
			return nil, err
		}
	}
	r.Gates, r.Targets, r.Agents = gates.Gates, targets.Targets, agents.Agents
	var err error
	if r.Receipts, err = ParseReceipts(files[ReceiptsFile]); err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		switch {
		case strings.HasPrefix(p, MemoriesDir):
			m, err := parseMemory(p, files[p])
			if err != nil {
				return nil, err
			}
			r.Memories = append(r.Memories, *m)
		case strings.HasPrefix(p, TombstonesDir):
			t, err := parseTombstone(p, files[p])
			if err != nil {
				return nil, err
			}
			r.Tombstones = append(r.Tombstones, *t)
		case strings.HasPrefix(p, BriefDir):
			b, err := parseBrief(p, files[p])
			if err != nil {
				return nil, err
			}
			r.Briefs = append(r.Briefs, *b)
		}
	}
	// The space is the manifest's; the files don't repeat it.
	sp := r.Manifest.Space
	for i := range r.Memories {
		r.Memories[i].Memory.SpaceID, r.Memories[i].Memory.TenantID = sp.ID, sp.TenantID
	}
	for i := range r.Tombstones {
		r.Tombstones[i].SpaceID, r.Tombstones[i].TenantID = sp.ID, sp.TenantID
	}
	for i := range r.Briefs {
		r.Briefs[i].SpaceID, r.Briefs[i].TenantID = sp.ID, sp.TenantID
	}
	sort.Slice(r.Memories, func(i, j int) bool { return refLess(r.Memories[i].Memory.Ref, r.Memories[j].Memory.Ref) })
	sort.Slice(r.Briefs, func(i, j int) bool { return r.Briefs[i].Version < r.Briefs[j].Version })
	return r, nil
}

// Record is a space's record as an export holds it.
type Record struct {
	Manifest Manifest
	Memories []ledger.ExportMemory
	// Tombstones are the forgotten memories and spaces, each with the
	// receipts about it.
	Tombstones  []ParsedTombstone
	Briefs      []ledger.Brief
	Gates       []ledger.Gate
	Targets     []ledger.Target
	Agents      []ledger.ExportAgent
	Reads       ledger.ReadSummary
	Receipts    []receiptchain.Receipt
	Checkpoints Checkpoints
}

// ParsedTombstone is a tombstone file read back.
type ParsedTombstone struct {
	ledger.Tombstone
	Receipts []ledger.ReceiptStamp
}

// ChainCheckpoints are the checkpoints as receiptchain verifies them, and
// the keys checkpoints.json embeds.
func (r *Record) ChainCheckpoints() ([]receiptchain.Checkpoint, receiptchain.Keyring, error) {
	out := make([]receiptchain.Checkpoint, len(r.Checkpoints.Checkpoints))
	for i, c := range r.Checkpoints.Checkpoints {
		cp := receiptchain.Checkpoint{SpaceID: c.SpaceID, TenantID: c.TenantID, Number: c.Number,
			PositionFrom: c.PositionFrom, PositionTo: c.PositionTo, FirstReceiptID: c.FirstReceiptID,
			LastReceiptID: c.LastReceiptID, LastSeq: c.LastSeq, KeyID: c.KeyID}
		for _, h := range []struct {
			dst *receiptchain.Hash
			src string
		}{{&cp.Prev, c.PrevSHA256}, {&cp.Chain, c.ChainSHA256}, {&cp.MerkleRoot, c.MerkleRoot}} {
			b, err := hex.DecodeString(h.src)
			if err != nil || len(b) != len(h.dst) {
				return nil, nil, fmt.Errorf("export: checkpoint %d: %q isn't a SHA-256", c.Number, h.src)
			}
			copy(h.dst[:], b)
		}
		if c.Signature != "" {
			sig, err := base64.StdEncoding.DecodeString(c.Signature)
			if err != nil {
				return nil, nil, fmt.Errorf("export: checkpoint %d: its signature isn't base64", c.Number)
			}
			cp.Signature = sig
		}
		at, err := time.Parse(time.RFC3339Nano, c.SealedAt)
		if err != nil {
			return nil, nil, fmt.Errorf("export: checkpoint %d: sealed_at: %w", c.Number, err)
		}
		cp.SealedAt = at.UTC()
		out[i] = cp
	}
	keys := receiptchain.Keyring{}
	for _, k := range r.Checkpoints.Keys {
		pub, err := base64.StdEncoding.DecodeString(k.PublicKey)
		if err != nil {
			return nil, nil, fmt.Errorf("export: key %s isn't base64", k.KeyID)
		}
		if id := keys.Add(pub); id != k.KeyID {
			return nil, nil, fmt.Errorf("export: key %s is named %s, but its id is %s", k.PublicKey, k.KeyID, id)
		}
	}
	return out, keys, nil
}

func decodeJSON(files map[string][]byte, name string, v any) error {
	raw, ok := files[name]
	if !ok {
		return fmt.Errorf("export: %s is missing", name)
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	if err := d.Decode(v); err != nil {
		return fmt.Errorf("export: %s: %w", name, err)
	}
	return nil
}

// ParseReceipts reads receipts.jsonl.
func ParseReceipts(raw []byte) ([]receiptchain.Receipt, error) {
	var out []receiptchain.Receipt
	for i, line := range strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n") {
		if line == "" && len(raw) == 0 {
			break
		}
		var l receiptLine
		d := json.NewDecoder(strings.NewReader(line))
		d.DisallowUnknownFields()
		if err := d.Decode(&l); err != nil {
			return nil, fmt.Errorf("export: %s line %d: %w", ReceiptsFile, i+1, err)
		}
		r := receiptchain.Receipt{ID: l.ID, Seq: l.Seq, TenantID: l.TenantID, SpaceID: l.SpaceID, ObjectKind: l.ObjectKind,
			ObjectID: l.ObjectID, ObjectRef: l.ObjectRef, Action: l.Action, ActorKind: l.ActorKind, ActorID: l.ActorID,
			Agent: l.Agent, Via: l.Via, Assurance: l.Assurance, SessionRef: l.SessionRef, Reason: l.Reason,
			StreamID: l.StreamID, StreamVersion: l.StreamVersion}
		if l.Source != nil {
			r.SourceKind, r.SourceRef = l.Source.Kind, l.Source.Ref
		}
		var err error
		if r.ReasonSalt, err = optHex(l.ReasonSalt); err != nil {
			return nil, fmt.Errorf("export: %s line %d: reason_salt: %w", ReceiptsFile, i+1, err)
		}
		if r.ReasonSHA256, err = optHex(l.ReasonSHA256); err != nil {
			return nil, fmt.Errorf("export: %s line %d: reason_sha256: %w", ReceiptsFile, i+1, err)
		}
		if r.OccurredAt, err = time.Parse(time.RFC3339Nano, l.OccurredAt); err != nil {
			return nil, fmt.Errorf("export: %s line %d: occurred_at: %w", ReceiptsFile, i+1, err)
		}
		if r.RecordedAt, err = time.Parse(time.RFC3339Nano, l.RecordedAt); err != nil {
			return nil, fmt.Errorf("export: %s line %d: recorded_at: %w", ReceiptsFile, i+1, err)
		}
		r.OccurredAt, r.RecordedAt = r.OccurredAt.UTC(), r.RecordedAt.UTC()
		out = append(out, r)
	}
	return out, nil
}

func optHex(s *string) ([]byte, error) {
	if s == nil {
		return nil, nil
	}
	return hex.DecodeString(*s)
}

// readFront reads a file's frontmatter with a YAML parser into v (through
// JSON, so v's json tags apply), and returns the body.
func readFront(path string, raw []byte, format string, v any) (string, error) {
	front, body, err := splitDocument(raw)
	if err != nil {
		return "", fmt.Errorf("export: %s: %w", path, err)
	}
	var data map[string]any
	if err := yaml.Unmarshal([]byte(front), &data); err != nil {
		return "", fmt.Errorf("export: %s: frontmatter: %w", path, err)
	}
	if data["format"] != format {
		return "", fmt.Errorf("export: %s: format %v, want %s", path, data["format"], format)
	}
	js, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("export: %s: frontmatter: %w", path, err)
	}
	d := json.NewDecoder(bytes.NewReader(js))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return "", fmt.Errorf("export: %s: frontmatter: %w", path, err)
	}
	return body, nil
}

type memoryFront struct {
	Format         string              `json:"format"`
	Ref            string              `json:"ref"`
	ID             uuid.UUID           `json:"id"`
	State          lifecycle.Mark      `json:"state"`
	Lifecycle      lifecycle.Lifecycle `json:"lifecycle"`
	Flags          []string            `json:"flags"`
	Section        ledger.Section      `json:"section"`
	Kind           ledger.Kind         `json:"kind"`
	Trust          policy.Trust        `json:"trust"`
	Version        int                 `json:"version"`
	StaleAfter     *time.Time          `json:"stale_after"`
	ValidFrom      *time.Time          `json:"valid_from"`
	ValidTo        *time.Time          `json:"valid_to"`
	CreatedAt      time.Time           `json:"created_at"`
	UpdatedAt      time.Time           `json:"updated_at"`
	CreatedReceipt uuid.UUID           `json:"created_receipt"`
	LastReceipt    uuid.UUID           `json:"last_receipt"`
	Decision       *struct {
		Status  *string `json:"status"`
		Why     *string `json:"why"`
		Options []struct {
			Label  string  `json:"label"`
			Detail *string `json:"detail"`
		} `json:"options"`
		Consequences *string `json:"consequences"`
		Area         *string `json:"area"`
	} `json:"decision"`
	Conditions json.RawMessage `json:"conditions"`
	Scope      json.RawMessage `json:"scope"`
	Sources    []struct {
		ID          uuid.UUID         `json:"id"`
		Kind        ledger.SourceKind `json:"kind"`
		Ref         string            `json:"ref"`
		URI         *string           `json:"uri"`
		Locator     json.RawMessage   `json:"locator"`
		Trust       policy.Trust      `json:"trust"`
		External    bool              `json:"external"`
		Quote       *string           `json:"quote"`
		ContentHash *string           `json:"content_hash"`
		CreatedAt   time.Time         `json:"created_at"`
	} `json:"sources"`
	Links []struct {
		ID        uuid.UUID       `json:"id"`
		Kind      ledger.LinkKind `json:"kind"`
		Direction string          `json:"direction"`
		Ref       string          `json:"ref"`
		MemoryID  uuid.UUID       `json:"memory_id"`
		Receipt   uuid.UUID       `json:"receipt"`
		CreatedAt time.Time       `json:"created_at"`
	} `json:"links"`
	Versions []struct {
		Version   int       `json:"version"`
		Receipt   uuid.UUID `json:"receipt"`
		CreatedAt time.Time `json:"created_at"`
		Statement string    `json:"statement"`
	} `json:"versions"`
	Receipts []stampFront `json:"receipts"`
}

type stampFront struct {
	Seq        int64         `json:"seq"`
	ID         uuid.UUID     `json:"id"`
	Action     ledger.Action `json:"action"`
	OccurredAt time.Time     `json:"occurred_at"`
}

func (s stampFront) stamp() ledger.ReceiptStamp {
	return ledger.ReceiptStamp{ID: s.ID, Seq: s.Seq, Action: s.Action, OccurredAt: s.OccurredAt.UTC()}
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func utc(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

func parseMemory(path string, raw []byte) (*ledger.ExportMemory, error) {
	var f memoryFront
	body, err := readFront(path, raw, MemoryFormat, &f)
	if err != nil {
		return nil, err
	}
	if want := MemoriesDir + f.Ref + ".md"; path != want {
		return nil, fmt.Errorf("export: %s holds %s; its file is %s", path, f.Ref, want)
	}
	flags, err := lifecycle.ParseFlags(f.Flags)
	if err != nil {
		return nil, fmt.Errorf("export: %s: flags: %w", path, err)
	}
	m := ledger.Memory{ID: f.ID, Ref: f.Ref, Section: f.Section, Kind: f.Kind, State: f.State, Lifecycle: f.Lifecycle,
		Flags: flags, Trust: f.Trust, Version: f.Version, StaleAfter: utc(f.StaleAfter), ValidFrom: utc(f.ValidFrom),
		ValidTo: utc(f.ValidTo), Conditions: compactJSON(f.Conditions), Applies: compactJSON(f.Scope),
		CreatedReceiptID: f.CreatedReceipt, LastReceiptID: f.LastReceipt, CreatedAt: f.CreatedAt.UTC(), UpdatedAt: f.UpdatedAt.UTC()}
	if d := f.Decision; d != nil {
		m.Decision = &ledger.DecisionFields{Why: deref(d.Why), Consequences: deref(d.Consequences), Area: deref(d.Area),
			Status: deref(d.Status)}
		for _, o := range d.Options {
			m.Decision.Options = append(m.Decision.Options, ledger.DecisionOption{Label: o.Label, Detail: deref(o.Detail)})
		}
	}
	for _, s := range f.Sources {
		m.Sources = append(m.Sources, ledger.Source{ID: s.ID, Kind: s.Kind, Ref: s.Ref, URI: deref(s.URI),
			Locator: compactJSON(s.Locator), External: s.External, Trust: s.Trust, Quote: deref(s.Quote),
			ContentHash: deref(s.ContentHash), CreatedAt: s.CreatedAt.UTC()})
	}
	for _, l := range f.Links {
		m.Links = append(m.Links, ledger.Link{ID: l.ID, Kind: l.Kind, Direction: l.Direction, MemoryID: l.MemoryID,
			Ref: l.Ref, ReceiptID: l.Receipt, CreatedAt: l.CreatedAt.UTC()})
	}
	out := &ledger.ExportMemory{}
	for _, v := range f.Versions {
		out.Versions = append(out.Versions, ledger.MemoryVersion{Version: v.Version, Statement: v.Statement,
			ReceiptID: v.Receipt, CreatedAt: v.CreatedAt.UTC()})
		if v.Version == f.Version {
			m.Statement = v.Statement
		}
	}
	if body != m.Statement+"\n" {
		return nil, fmt.Errorf("export: %s: the body isn't the statement of version %d in its frontmatter", path, f.Version)
	}
	for _, s := range f.Receipts {
		out.Receipts = append(out.Receipts, s.stamp())
	}
	out.Memory = m
	return out, nil
}

type tombstoneFront struct {
	Format      string     `json:"format"`
	Ref         string     `json:"ref"`
	ID          uuid.UUID  `json:"id"`
	Kind        string     `json:"kind"`
	Tombstone   *uuid.UUID `json:"tombstone"`
	Op          *uuid.UUID `json:"op"`
	ForgottenAt *time.Time `json:"forgotten_at"`
	By          *struct {
		Kind string     `json:"kind"`
		ID   *uuid.UUID `json:"id"`
	} `json:"by"`
	RequestedBy *struct {
		ConnectionID uuid.UUID `json:"connection_id"`
	} `json:"requested_by"`
	Via         *string              `json:"via"`
	Receipt     *uuid.UUID           `json:"receipt"`
	Carried     *string              `json:"carried"`
	Primary     *string              `json:"primary"`
	With        []string             `json:"with"`
	KeptAt      *time.Time           `json:"kept_at"`
	ReadsBefore int                  `json:"reads_before"`
	Gone        ledger.TombstoneGone `json:"gone"`
	AgentsTold  int                  `json:"agents_told"`
	Status      *string              `json:"status"`
	CompletedAt *time.Time           `json:"completed_at"`
	ReappliedAt *time.Time           `json:"reapplied_at"`
	Receipts    []stampFront         `json:"receipts"`
}

func parseTombstone(path string, raw []byte) (*ParsedTombstone, error) {
	var f tombstoneFront
	if _, err := readFront(path, raw, TombstoneFormat, &f); err != nil {
		return nil, err
	}
	t := ledger.Tombstone{Ref: f.Ref, Kind: f.Kind, ObjectID: f.ID, Carried: deref(f.Carried), Primary: deref(f.Primary),
		With: f.With, KeptAt: utc(f.KeptAt), ReadsBefore: f.ReadsBefore, Gone: f.Gone, Agents: f.AgentsTold,
		Status: deref(f.Status), CompletedAt: utc(f.CompletedAt), ReappliedAt: utc(f.ReappliedAt), Via: policy.Via(deref(f.Via))}
	if f.Tombstone != nil {
		t.ID = *f.Tombstone
	}
	if f.Op != nil {
		t.OpID = *f.Op
	}
	if f.ForgottenAt != nil {
		t.ForgottenAt = f.ForgottenAt.UTC()
	}
	if f.By != nil {
		t.By = ledger.TombstoneActor{Kind: f.By.Kind, ID: f.By.ID}
	}
	if f.RequestedBy != nil {
		t.RequestedBy = &ledger.TombstoneAgent{ConnectionID: f.RequestedBy.ConnectionID}
	}
	if f.Receipt != nil {
		t.ReceiptID = *f.Receipt
	}
	if t.CompletedAt != nil {
		d := t.CompletedAt.Sub(t.ForgottenAt).Milliseconds()
		t.DurationMS = &d
	}
	out := &ParsedTombstone{Tombstone: t}
	for _, s := range f.Receipts {
		out.Receipts = append(out.Receipts, s.stamp())
	}
	return out, nil
}

type briefFront struct {
	Format        string    `json:"format"`
	Ref           string    `json:"ref"`
	Version       int       `json:"version"`
	Current       bool      `json:"current"`
	BriefID       uuid.UUID `json:"brief_id"`
	VersionID     uuid.UUID `json:"version_id"`
	ParentVersion *int      `json:"parent_version"`
	Title         string    `json:"title"`
	Summary       *string   `json:"summary"`
	Facts         int       `json:"facts"`
	Receipt       uuid.UUID `json:"receipt"`
	CreatedAt     time.Time `json:"created_at"`
	Sections      []struct {
		Key     string             `json:"key"`
		Heading string             `json:"heading"`
		Items   []ledger.BriefItem `json:"items"`
	} `json:"sections"`
}

func parseBrief(path string, raw []byte) (*ledger.Brief, error) {
	var f briefFront
	if _, err := readFront(path, raw, BriefFormat, &f); err != nil {
		return nil, err
	}
	b := &ledger.Brief{ID: f.BriefID, VersionID: f.VersionID, Ref: f.Ref, Version: f.Version, Title: f.Title,
		Summary: deref(f.Summary), Facts: f.Facts, Current: f.Current, ReceiptID: f.Receipt, CreatedAt: f.CreatedAt.UTC(),
		Sections: []ledger.BriefSection{}}
	if f.ParentVersion != nil {
		b.ParentVersion = *f.ParentVersion
	}
	for _, s := range f.Sections {
		items := s.Items
		if items == nil {
			items = []ledger.BriefItem{}
		}
		b.Sections = append(b.Sections, ledger.BriefSection{Key: s.Key, Heading: s.Heading, Items: items})
	}
	return b, nil
}

// compactJSON is raw without insignificant whitespace, with object keys
// sorted (as Postgres's jsonb keeps them sorted its own way, compare
// values, not bytes).
func compactJSON(raw json.RawMessage) json.RawMessage {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return raw
	}
	out, err := json.Marshal(v)
	if err != nil {
		return raw
	}
	return out
}

// refLess orders display IDs by number.
func refLess(a, b string) bool {
	_, x, okA := ledger.ParseRef(a)
	_, y, okB := ledger.ParseRef(b)
	if okA && okB && x != y {
		return x < y
	}
	return a < b
}
