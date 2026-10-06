package ledger

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Row loaders for the Brief, targets, compile runs and observations. Like
// queries.go, every query runs inside a ledger transaction (memax_v2,
// scope set), and the explicit space filters are defence in depth.

// ---------------------------------------------------------------------
// Briefs
// ---------------------------------------------------------------------

const briefSelect = `
	SELECT b.id, v.id, v.seq, v.version, v.space_id, v.tenant_id, COALESCE(v.parent_version, 0),
	       v.title, COALESCE(v.summary, ''), v.structure, v.receipt_id, v.created_at, b.current_version
	  FROM v2.brief_versions v
	  JOIN v2.briefs b ON b.id = v.brief_id`

func scanBrief(row pgx.Row) (*Brief, error) {
	var b Brief
	var seq int64
	var current int
	var structure []byte
	if err := row.Scan(&b.ID, &b.VersionID, &seq, &b.Version, &b.SpaceID, &b.TenantID, &b.ParentVersion,
		&b.Title, &b.Summary, &structure, &b.ReceiptID, &b.CreatedAt, &current); err != nil {
		return nil, err
	}
	b.Ref = FormatRef(PrefixBrief, seq)
	b.Current = b.Version == current
	var st briefStructure
	if err := json.Unmarshal(structure, &st); err != nil {
		return nil, fmt.Errorf("ledger: Brief %s structure: %w", b.Ref, err)
	}
	b.Sections = st.Sections
	if b.Sections == nil {
		b.Sections = []BriefSection{}
	}
	b.Facts = len(briefRefs(b.Sections))
	return &b, nil
}

// briefStructure is brief_versions.structure.
type briefStructure struct {
	Sections []BriefSection `json:"sections"`
}

// briefRefs lists every memory a Brief places or cites, sorted.
func briefRefs(sections []BriefSection) []string {
	var refs []string
	for _, s := range sections {
		for _, it := range s.Items {
			for _, r := range append([]string{it.Ref}, it.Cites...) {
				if r != "" && !slices.Contains(refs, r) {
					refs = append(refs, r)
				}
			}
		}
	}
	slices.Sort(refs)
	return refs
}

// briefRow is the Brief's head, locked by a revision.
type briefRow struct {
	id            uuid.UUID
	version       int
	streamVersion int
	seq           int64
}

// lockBrief locks the space's Brief, or returns nil when it has none.
func lockBrief(ctx context.Context, tx pgx.Tx, spaceID uuid.UUID) (*briefRow, error) {
	var b briefRow
	err := tx.QueryRow(ctx, `
		SELECT b.id, b.current_version, b.stream_version, v.seq
		  FROM v2.briefs b
		  JOIN v2.brief_versions v ON v.brief_id = b.id AND v.version = b.current_version
		 WHERE b.space_id = $1
		   FOR UPDATE OF b`, spaceID).Scan(&b.id, &b.version, &b.streamVersion, &b.seq)
	if errNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("ledger: load Brief: %w", err)
	}
	return &b, nil
}

func loadBriefVersion(ctx context.Context, tx pgx.Tx, scope Scope, versionID uuid.UUID) (*Brief, error) {
	b, err := scanBrief(tx.QueryRow(ctx, briefSelect+` WHERE v.id = $1 AND v.space_id = ANY($2)`, versionID, scope.SpaceIDs()))
	if errNoRows(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("ledger: load Brief: %w", err)
	}
	return b, attachBriefReceipts(ctx, tx, scope, []*Brief{b})
}

func attachBriefReceipts(ctx context.Context, tx pgx.Tx, scope Scope, briefs []*Brief) error {
	ids := make([]uuid.UUID, 0, len(briefs))
	for _, b := range briefs {
		ids = append(ids, b.ReceiptID)
	}
	receipts, err := loadReceipts(ctx, tx, scope, ids)
	if err != nil {
		return err
	}
	for _, b := range briefs {
		for i := range receipts {
			if receipts[i].ID == b.ReceiptID {
				rc := receipts[i]
				b.Receipt = &rc
			}
		}
	}
	return nil
}

// ---------------------------------------------------------------------
// Targets
// ---------------------------------------------------------------------

const targetSelect = `
	SELECT t.id, t.tenant_id, t.space_id, t.kind, COALESCE(t.path, ''), t.settings, t.delivery, t.sync_state,
	       t.dirty_gen, t.dirty_at, t.compiled_gen, t.last_compile_id, t.delivered_compile_id,
	       COALESCE(t.delivered_sha256, ''), t.delivered_files, t.delivered_at, t.stream_version,
	       t.created_receipt_id, t.last_receipt_id, t.created_at, t.updated_at,
	       (SELECT count(*) FROM v2.target_observations o WHERE o.target_id = t.id AND o.status = 'open')
	  FROM v2.targets t`

func scanTarget(row pgx.Row) (*Target, error) {
	var t Target
	var settings, files []byte
	var deliveredID *uuid.UUID
	var deliveredSHA string
	var deliveredAt *time.Time
	if err := row.Scan(&t.ID, &t.TenantID, &t.SpaceID, &t.Kind, &t.Path, &settings, &t.Delivery, &t.SyncState,
		&t.DirtyGen, &t.DirtyAt, &t.CompiledGen, &t.lastCompileID, &deliveredID,
		&deliveredSHA, &files, &deliveredAt, &t.Version,
		&t.CreatedReceiptID, &t.LastReceiptID, &t.CreatedAt, &t.UpdatedAt, &t.OpenDrift); err != nil {
		return nil, err
	}
	t.Label = targetLabel(t.Kind, t.Path)
	if err := json.Unmarshal(settings, &t.Settings); err != nil {
		return nil, fmt.Errorf("ledger: target %s settings: %w", t.ID, err)
	}
	if deliveredSHA != "" {
		d := &Delivered{CompileID: deliveredID, SHA256: deliveredSHA, At: deliveredAt}
		if err := json.Unmarshal(files, &d.Files); err != nil {
			return nil, fmt.Errorf("ledger: target %s baseline: %w", t.ID, err)
		}
		if d.Files == nil {
			d.Files = []DeliveredFile{}
		}
		t.Delivered = d
	}
	return &t, nil
}

// loadTarget reads one target in scope; lock takes the row lock that
// serialises every command on it (and the compile coordinator's checks).
func loadTarget(ctx context.Context, tx pgx.Tx, scope Scope, id uuid.UUID, lock bool) (*Target, error) {
	q := targetSelect + ` WHERE t.id = $1 AND t.space_id = ANY($2)`
	if lock {
		q += ` FOR UPDATE OF t`
	}
	t, err := scanTarget(tx.QueryRow(ctx, q, id, scope.SpaceIDs()))
	if errNoRows(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("ledger: load target: %w", err)
	}
	return t, nil
}

// fillTargets loads the targets' latest runs and names their delivered
// runs.
func fillTargets(ctx context.Context, tx pgx.Tx, scope Scope, targets []*Target) error {
	var ids []uuid.UUID
	for _, t := range targets {
		if t.lastCompileID != nil {
			ids = append(ids, *t.lastCompileID)
		}
		if t.Delivered != nil && t.Delivered.CompileID != nil {
			ids = append(ids, *t.Delivered.CompileID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	rows, err := tx.Query(ctx, compileSelect+` WHERE r.id = ANY($1) AND r.space_id = ANY($2)`, ids, scope.SpaceIDs())
	if err != nil {
		return fmt.Errorf("ledger: load compile runs: %w", err)
	}
	runs, err := pgx.CollectRows(rows, scanCompileRun)
	if err != nil {
		return fmt.Errorf("ledger: load compile runs: %w", err)
	}
	byID := map[uuid.UUID]*CompileRun{}
	for i := range runs {
		byID[runs[i].ID] = &runs[i]
	}
	for _, t := range targets {
		if t.lastCompileID != nil {
			t.LastCompile = byID[*t.lastCompileID]
		}
		if t.Delivered != nil && t.Delivered.CompileID != nil {
			if r := byID[*t.Delivered.CompileID]; r != nil {
				t.Delivered.Compile = r.Ref
			}
		}
	}
	return nil
}

// targetLabel is what people see for a target.
func targetLabel(k TargetKind, path string) string {
	if k == TargetChatGPT {
		return "ChatGPT project"
	}
	return path
}

// targetRef is the receipts' object_ref for a target: its label, cut
// from the left to the 32 characters a receipt's ref holds.
func targetRef(t *Target) string {
	label := targetLabel(t.Kind, t.Path)
	if utf8.RuneCountInString(label) <= 32 {
		return label
	}
	r := []rune(label)
	return "…" + string(r[len(r)-31:])
}

// ---------------------------------------------------------------------
// Compile runs
// ---------------------------------------------------------------------

const compileSelect = `
	SELECT r.id, r.seq, r.target_id, r.space_id, v.seq, r.brief_version, r.generation, r.status,
	       r.input_sha256, COALESCE(r.output_sha256, ''), COALESCE(r.drift_sha256, ''), COALESCE(r.artifact_key, ''),
	       COALESCE(r.bytes, 0), COALESCE(r.lines, 0), r.refs, r.dropped_for_budget, r.files, r.warnings,
	       COALESCE(r.error, ''), r.enqueued_at, r.started_at, r.compiled_at, r.delivered_at, r.created_receipt_id
	  FROM v2.compile_runs r
	  JOIN v2.brief_versions v ON v.brief_id = r.brief_id AND v.version = r.brief_version`

func scanCompileRun(row pgx.CollectableRow) (CompileRun, error) {
	r, err := scanCompileRow(row)
	if err != nil {
		return CompileRun{}, err
	}
	return *r, nil
}

func scanCompileRow(row pgx.Row) (*CompileRun, error) {
	var r CompileRun
	var briefSeq int64
	var files, warnings []byte
	if err := row.Scan(&r.ID, &r.seq, &r.TargetID, &r.SpaceID, &briefSeq, &r.BriefVersion, &r.Generation, &r.Status,
		&r.InputSHA256, &r.OutputSHA256, &r.DriftSHA256, &r.ArtifactKey,
		&r.Bytes, &r.Lines, &r.Refs, &r.DroppedForBudget, &files, &warnings,
		&r.Error, &r.EnqueuedAt, &r.StartedAt, &r.CompiledAt, &r.DeliveredAt, &r.ReceiptID); err != nil {
		return nil, err
	}
	r.Ref = FormatRef(PrefixCompile, r.seq)
	r.Brief = FormatRef(PrefixBrief, briefSeq)
	if err := json.Unmarshal(files, &r.Files); err != nil {
		return nil, fmt.Errorf("ledger: compile %s outputs: %w", r.Ref, err)
	}
	if err := json.Unmarshal(warnings, &r.Warnings); err != nil {
		return nil, fmt.Errorf("ledger: compile %s warnings: %w", r.Ref, err)
	}
	r.Files = nonNilSlice(r.Files)
	r.Warnings = nonNilSlice(r.Warnings)
	r.Refs = nonNilSlice(r.Refs)
	r.DroppedForBudget = nonNilSlice(r.DroppedForBudget)
	return &r, nil
}

func loadCompileRun(ctx context.Context, tx pgx.Tx, scope Scope, id uuid.UUID) (*CompileRun, error) {
	r, err := scanCompileRow(tx.QueryRow(ctx, compileSelect+` WHERE r.id = $1 AND r.space_id = ANY($2)`, id, scope.SpaceIDs()))
	if errNoRows(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("ledger: load compile run: %w", err)
	}
	return r, nil
}

// findCompileRun resolves "C-0881" or a run id among one target's runs.
func findCompileRun(ctx context.Context, tx pgx.Tx, scope Scope, targetID uuid.UUID, ref string) (*CompileRun, error) {
	ref = strings.TrimSpace(ref)
	q := compileSelect + ` WHERE r.target_id = $1 AND r.space_id = ANY($2)`
	var arg any
	if id, err := uuid.Parse(ref); err == nil {
		q += ` AND r.id = $3`
		arg = id
	} else if p, n, ok := ParseRef(ref); ok && p == PrefixCompile {
		q += ` AND r.seq = $3`
		arg = n
	} else {
		return nil, invalid("compile", "use a compile ID like C-0881 or a run id")
	}
	r, err := scanCompileRow(tx.QueryRow(ctx, q, targetID, scope.SpaceIDs(), arg))
	if errNoRows(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("ledger: load compile run: %w", err)
	}
	return r, nil
}

// lastGoodRun is the target's latest run that didn't fail, or nil.
func lastGoodRun(ctx context.Context, tx pgx.Tx, scope Scope, targetID uuid.UUID) (*CompileRun, error) {
	r, err := scanCompileRow(tx.QueryRow(ctx, compileSelect+`
		 WHERE r.target_id = $1 AND r.space_id = ANY($2) AND r.status <> 'failed'
		 ORDER BY r.seq DESC LIMIT 1`, targetID, scope.SpaceIDs()))
	if errNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("ledger: load compile run: %w", err)
	}
	return r, nil
}

// nextStreamVersion is the next receipt version on an object's stream,
// for objects (compile runs) whose row doesn't count it. The caller holds
// the lock that serialises writers on the stream.
func nextStreamVersion(ctx context.Context, tx pgx.Tx, streamID uuid.UUID) (int, error) {
	var v int
	err := tx.QueryRow(ctx, `SELECT COALESCE(max(stream_version), 0) + 1 FROM v2.receipts WHERE stream_id = $1`, streamID).Scan(&v)
	if err != nil {
		return 0, fmt.Errorf("ledger: read stream: %w", err)
	}
	return v, nil
}

// ---------------------------------------------------------------------
// Observations
// ---------------------------------------------------------------------

const observationSelect = `
	SELECT o.id, o.target_id, o.space_id, o.path, o.observed_sha256, o.observer_kind, o.observer_id,
	       COALESCE(o.commit_sha, ''), o.artifact_key, o.bytes, o.base_compile_id, COALESCE(c.seq, 0),
	       COALESCE(o.base_sha256, ''), o.changeset, o.status, o.resolution, o.observed_at, o.resolved_at,
	       o.created_receipt_id, rc.actor_kind, rc.actor_id, rc.via
	  FROM v2.target_observations o
	  JOIN v2.receipts rc ON rc.id = o.created_receipt_id
	  LEFT JOIN v2.compile_runs c ON c.id = o.base_compile_id`

func scanObservation(row pgx.CollectableRow) (Observation, error) {
	var o Observation
	var baseSeq int64
	var changeset, resolution []byte
	var actorKind string
	if err := row.Scan(&o.ID, &o.TargetID, &o.SpaceID, &o.Path, &o.ObservedSHA, &o.ObserverKind, &o.ObserverID,
		&o.Commit, &o.ArtifactKey, &o.Bytes, &o.BaseCompileID, &baseSeq,
		&o.BaseSHA256, &changeset, &o.Status, &resolution, &o.ObservedAt, &o.ResolvedAt,
		&o.ReceiptID, &actorKind, &o.actorID, &o.via); err != nil {
		return Observation{}, err
	}
	if actorKind != "person" {
		o.actorID = nil
	}
	if baseSeq > 0 {
		o.BaseCompile = FormatRef(PrefixCompile, baseSeq)
	}
	if err := json.Unmarshal(changeset, &o.Changes); err != nil {
		return Observation{}, fmt.Errorf("ledger: observation %s changeset: %w", o.ID, err)
	}
	o.Changes.Changes = nonNilSlice(o.Changes.Changes)
	if len(resolution) > 0 {
		o.Resolution = &DriftResolution{}
		if err := json.Unmarshal(resolution, o.Resolution); err != nil {
			return Observation{}, fmt.Errorf("ledger: observation %s resolution: %w", o.ID, err)
		}
	}
	return o, nil
}

func loadObservations(ctx context.Context, tx pgx.Tx, scope Scope, where string, args ...any) ([]Observation, error) {
	args = append(args, scope.SpaceIDs())
	rows, err := tx.Query(ctx, observationSelect+` WHERE `+where+
		fmt.Sprintf(` AND o.space_id = ANY($%d) ORDER BY o.created_at, o.id`, len(args)), args...)
	if err != nil {
		return nil, fmt.Errorf("ledger: load observations: %w", err)
	}
	obs, err := pgx.CollectRows(rows, scanObservation)
	if err != nil {
		return nil, fmt.Errorf("ledger: load observations: %w", err)
	}
	return obs, nil
}

// ---------------------------------------------------------------------
// Manifests
// ---------------------------------------------------------------------

// ManifestSHA256 combines the hashes of a run's outputs (or a target's
// baseline files) into one: for a single output, its own hash, so a
// one-file target's drift hash is the compiler's drift_sha256 of that
// file; otherwise the sha256 of "<path or label> NUL <hash> LF" lines in
// path order (no outputs: the sha256 of nothing). A daemon acknowledging a
// delivery computes the same over what it wrote.
func ManifestSHA256(entries map[string]string) string {
	if len(entries) == 1 {
		for _, h := range entries {
			return h
		}
	}
	keys := make([]string, 0, len(entries))
	for k := range entries {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	sum := sha256.New()
	for _, k := range keys {
		sum.Write([]byte(k + "\x00" + entries[k] + "\n"))
	}
	return hex.EncodeToString(sum.Sum(nil))
}

// OutputManifests are a run's output and drift hashes (ManifestSHA256).
func OutputManifests(outputs []CompiledOutput) (output, drift string) {
	o, d := map[string]string{}, map[string]string{}
	for _, f := range outputs {
		key := f.Path
		if key == "" {
			key = f.Label
		}
		o[key], d[key] = f.SHA256, f.DriftSHA256
	}
	return ManifestSHA256(o), ManifestSHA256(d)
}

func baselineSHA(files []DeliveredFile) string {
	m := map[string]string{}
	for _, f := range files {
		m[f.Path] = f.SHA256
	}
	return ManifestSHA256(m)
}

func nonNilSlice[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}
