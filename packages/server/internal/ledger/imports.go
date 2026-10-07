package ledger

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
	"github.com/MemaxLabs/memax/packages/server/internal/textsig"
)

// Imports (plan 25 §7.3, epic 2.1).
//
// `npx memax-cli init` reads what agents already know from their files,
// splits it into statements on the person's machine (secrets and hidden
// characters never leave it) and uploads the rest in one request. Import
// writes the batch in four steps:
//
//  1. It opens the import (v2.imports), keyed by the request's
//     idempotency key, so a retry resumes the same import. Policy decides
//     first whether the actor may write in the space at all.
//  2. It folds the statements of the batch that repeat each other, by the
//     judge's stage 0 (internal/textsig: the same words, or a near-verbatim
//     repeat), into one proposal that cites every file saying it.
//  3. It writes each statement as its own command (ImportStatement): its
//     own transaction, receipt and idempotency key, through policy like
//     any write. Arriving through the import surface (via import), it is
//     always a proposal (policy.CodeImport), whoever sends it. A statement
//     the space already has (the same words, or a near-verbatim repeat, in
//     any state but forgotten) is skipped, so running init again proposes
//     only what is new.
//  4. It closes the import and queues its conflict check (judge_import,
//     internal/judge) in the same transaction. Each proposal is also judged
//     on its own (judge_proposal), as every proposal is.
//
// Nothing an import writes is kept: a person keeps it, one by one, in bulk
// (memories:keep), or by settling a conflict.

// ImportLocation is where a file an import read lives.
type ImportLocation string

// The locations.
const (
	// ImportRepository: in the repository init ran in, shared with
	// everyone who clones it.
	ImportRepository ImportLocation = "repository"
	// ImportHome: on the person's machine only (their home directory, or a
	// file their tools keep out of git).
	ImportHome ImportLocation = "home"
	// ImportV1: a space's own V1 memory, imported by its switch to V2
	// (switch.go). Clients don't send it.
	ImportV1 ImportLocation = "v1"
)

// Valid reports whether l is a known location.
func (l ImportLocation) Valid() bool { return l == ImportRepository || l == ImportHome }

// ImportOutcome is what became of one statement an import sent.
type ImportOutcome string

// The outcomes.
const (
	ImportProposed ImportOutcome = "proposed" // it is a new proposal
	ImportFolded   ImportOutcome = "folded"   // it repeats another statement of the batch, whose proposal cites it too
	ImportExisting ImportOutcome = "existing" // the space already has it
	ImportRefused  ImportOutcome = "refused"  // policy refused it (a credential the CLI missed, say)
)

// ImportOutcomes lists every outcome.
var ImportOutcomes = []ImportOutcome{ImportProposed, ImportFolded, ImportExisting, ImportRefused}

// ImportSkipReason is why the CLI kept a statement on the machine.
type ImportSkipReason string

// The reasons.
const (
	SkipSecret  ImportSkipReason = "secret"   // it holds a credential
	SkipTooLong ImportSkipReason = "too_long" // it is longer than a statement can be
	SkipLimit   ImportSkipReason = "limit"    // the file has more statements than one import takes
)

// ImportSkipReasons lists every reason.
var ImportSkipReasons = []ImportSkipReason{SkipSecret, SkipTooLong, SkipLimit}

// ImportFile is one file an import read: where it is and what came of it,
// never what it says.
type ImportFile struct {
	// Path is what people see: "CLAUDE.md", "~/.codex/memories/notes.md".
	Path string `json:"path"`
	// Kind is the client's name for the file's role ("claude_md",
	// "cursor_rule", "claude_memory", …), a short identifier.
	Kind     string         `json:"kind"`
	Agent    string         `json:"agent,omitempty"`
	Location ImportLocation `json:"location"`
	// Trust is the class the file's statements were sent at.
	Trust      policy.Trust `json:"trust,omitempty"`
	Statements int          `json:"statements"`
	// Skipped counts the statements the CLI kept on the machine.
	Skipped int `json:"skipped"`
	// HiddenCharacters counts the hidden characters the CLI removed.
	HiddenCharacters int `json:"hidden_characters"`
}

// ImportSkip is a statement the CLI didn't send, and why: a ref and the
// rule that matched, never its words.
type ImportSkip struct {
	Ref    string           `json:"ref"`
	Reason ImportSkipReason `json:"reason"`
	// Detail names the rule ("GitHub token"), never the matched text.
	Detail string `json:"detail,omitempty"`
}

// ImportItem is one statement of an import.
type ImportItem struct {
	// Key is unique within the request; the item's results carry it back.
	Key string
	// Ref is where it came from, "CLAUDE.md:12". Defaults to its first
	// source's ref.
	Ref      string
	Location ImportLocation
	// HiddenCharacters counts the hidden characters the CLI removed from it.
	HiddenCharacters int
	// NewMemory is the statement. Its SpaceID is the request's.
	NewMemory
}

// ImportRequest is one upload.
type ImportRequest struct {
	// Meta's IdempotencyKey is the request's: a retry with the same key
	// resumes the same import. Via is always import.
	Meta
	SpaceID uuid.UUID
	// Client names the uploader ("memax-cli 0.3.0").
	Client  string
	Files   []ImportFile
	Skipped []ImportSkip
	Items   []ImportItem
}

// The limits of one import.
const (
	MaxImportItems   = 500
	MaxImportFiles   = 200
	MaxImportSkipped = 1000
	MaxImportClient  = 100
	MaxImportPath    = 500
	MaxImportDetail  = 100
)

var (
	importKeyPattern  = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,64}$`)
	importKindPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)
)

// Import is an upload's summary.
type Import struct {
	ID         uuid.UUID        `json:"id"`
	SpaceID    uuid.UUID        `json:"space_id"`
	TenantID   uuid.UUID        `json:"tenant_id"`
	ActorKind  policy.ActorKind `json:"actor_kind"`
	ActorID    *uuid.UUID       `json:"actor_id,omitempty"`
	Agent      string           `json:"agent,omitempty"`
	Client     string           `json:"client,omitempty"`
	Files      []ImportFile     `json:"files"`
	Skipped    []ImportSkip     `json:"skipped"`
	Counts     ImportCounts     `json:"counts"`
	UploadedAt *time.Time       `json:"uploaded_at,omitempty"`
	Check      ImportCheck      `json:"check"`
	// Origin is init (memax init's upload) or v1 (a space's own V1
	// memories, offered for bulk keep when it switched to V2).
	Origin    string    `json:"origin"`
	CreatedAt time.Time `json:"created_at"`
}

// ImportCounts counts an import's statements by outcome, and its conflicts.
type ImportCounts struct {
	Items     int `json:"items"`
	Proposed  int `json:"proposed"`
	Folded    int `json:"folded"`
	Existing  int `json:"existing"`
	Refused   int `json:"refused"`
	Conflicts int `json:"conflicts"`
}

// ImportCheck is how far the import's conflict check got.
type ImportCheck struct {
	// State is pending, checked, no_model (only the judge's stage 0 ran:
	// no model is configured), failed (the model gave no answer) or
	// skipped (fewer than two proposals to compare).
	State     string     `json:"state"`
	CheckedAt *time.Time `json:"checked_at,omitempty"`
	Tier      string     `json:"tier,omitempty"`
	Model     string     `json:"model,omitempty"`
}

// The check states.
const (
	CheckPending = "pending"
	CheckChecked = "checked"
	CheckNoModel = "no_model"
	CheckFailed  = "failed"
	CheckSkipped = "skipped"
)

// ImportCheckStates lists every state.
var ImportCheckStates = []string{CheckPending, CheckChecked, CheckNoModel, CheckFailed, CheckSkipped}

// ImportResult is what Import did.
type ImportResult struct {
	// Refused is set when policy refused the whole import; nothing else is.
	Refused *policy.Decision
	Import  *Import
	// Items has one result per item, in request order.
	Items []ImportItemResult
	// Replayed: the key had opened this import before; nothing was written
	// twice.
	Replayed bool
}

// ImportItemResult is what became of one item.
type ImportItemResult struct {
	Key      string        `json:"key"`
	Position int           `json:"position"`
	Ref      string        `json:"ref"`
	Outcome  ImportOutcome `json:"outcome"`
	// Memory is the proposal it became (or, folded, the one it folded
	// into), or the memory the space already had.
	Memory *MemoryPointer `json:"memory,omitempty"`
	// Lifecycle is that memory's, for an existing one.
	Lifecycle string `json:"lifecycle,omitempty"`
	// FoldedInto is the key of the item it folded into.
	FoldedInto string `json:"folded_into,omitempty"`
	// Policy says why it was refused.
	Policy *policy.Decision `json:"policy,omitempty"`
}

func (r *ImportRequest) validate() error {
	if r.SpaceID == uuid.Nil {
		return invalid("space_id", "say which space to import into")
	}
	if err := checkText("client", r.Client, MaxImportClient, false); err != nil {
		return err
	}
	switch {
	case len(r.Items) > MaxImportItems:
		return invalid("items", "send at most %d statements in one import; split the rest into another", MaxImportItems)
	case len(r.Files) > MaxImportFiles:
		return invalid("files", "describe at most %d files in one import", MaxImportFiles)
	case len(r.Skipped) > MaxImportSkipped:
		return invalid("skipped", "list at most %d skipped statements", MaxImportSkipped)
	}
	for i := range r.Files {
		f := &r.Files[i]
		f.Path = strings.TrimSpace(f.Path)
		if err := checkText("files.path", f.Path, MaxImportPath, true); err != nil {
			return err
		}
		if !importKindPattern.MatchString(f.Kind) {
			return invalid("files.kind", "use a short lowercase name such as claude_md")
		}
		if err := checkText("files.agent", f.Agent, MaxAgentSlug, false); err != nil {
			return err
		}
		if !f.Location.Valid() {
			return invalid("files.location", "use repository or home")
		}
		if f.Trust != "" && !f.Trust.Valid() {
			return invalid("files.trust", "use person, agent_own_work, repository or external")
		}
		if f.Statements < 0 || f.Skipped < 0 || f.HiddenCharacters < 0 {
			return invalid("files", "counts can't be negative")
		}
	}
	for i := range r.Skipped {
		s := &r.Skipped[i]
		s.Ref = strings.TrimSpace(s.Ref)
		if err := checkText("skipped.ref", s.Ref, MaxSourceRefRunes, true); err != nil {
			return err
		}
		if !slices.Contains(ImportSkipReasons, s.Reason) {
			return invalid("skipped.reason", "use secret, too_long or limit")
		}
		if err := checkText("skipped.detail", s.Detail, MaxImportDetail, false); err != nil {
			return err
		}
	}
	keys := map[string]bool{}
	for i := range r.Items {
		it := &r.Items[i]
		if !importKeyPattern.MatchString(it.Key) {
			return invalid("items.key", "use 1 to 64 letters, digits, dots, colons, underscores or hyphens")
		}
		if keys[it.Key] {
			return invalid("items.key", "%s appears twice; every item needs its own key", it.Key)
		}
		keys[it.Key] = true
		if !it.Location.Valid() {
			return invalid("items.location", "use repository or home")
		}
		if it.HiddenCharacters < 0 {
			return invalid("items.hidden_characters", "can't be negative")
		}
		it.SpaceID = r.SpaceID
		if err := it.NewMemory.validate(); err != nil {
			return err
		}
		it.Ref = strings.TrimSpace(it.Ref)
		if it.Ref == "" && len(it.Sources) > 0 {
			it.Ref = it.Sources[0].Ref
		}
		if err := checkText("items.ref", it.Ref, MaxSourceRefRunes, true); err != nil {
			return err
		}
	}
	return nil
}

// hash fingerprints what the request asks for, so a key reused for a
// different upload is refused.
func (r *ImportRequest) hash() ([]byte, error) {
	b, err := json.Marshal(struct {
		Space   uuid.UUID
		Client  string
		Files   []ImportFile
		Skipped []ImportSkip
		Items   []ImportItem
	}{r.SpaceID, r.Client, r.Files, r.Skipped, r.Items})
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(b)
	return sum[:], nil
}

// capRepositoryTrust holds a repository file's statements to the
// repository class, whatever the client says: a file anyone with push
// access can change is no one person's word. Lines the client found only
// on another branch arrive external, which this keeps.
func (it *ImportItem) capRepositoryTrust() {
	if it.Location != ImportRepository {
		return
	}
	for i := range it.Sources {
		s := &it.Sources[i]
		if s.Kind != SourceFile && s.Kind != SourceImport {
			continue
		}
		t := s.Trust
		if t == "" {
			t = s.Kind.defaultTrust(policy.TrustRepository)
		}
		s.Trust = policy.MinTrust(t, policy.TrustRepository)
	}
}

// Import writes an upload (see the package note above). A refusal of the
// whole import is ImportResult.Refused; a refused statement is that item's
// outcome. Errors are for a malformed request, a reused key and the
// database; a retry with the same key resumes where it stopped.
func (l *Ledger) Import(ctx context.Context, req ImportRequest) (*ImportResult, error) {
	if l == nil {
		return nil, ErrDisabled
	}
	req.Via = policy.ViaImport
	meta := req.Meta
	if err := validateMeta(&meta, l.now()); err != nil {
		return nil, err
	}
	if err := req.validate(); err != nil {
		return nil, err
	}
	for i := range req.Items {
		req.Items[i].capRepositoryTrust()
	}
	return l.importItems(ctx, meta, req, importOriginInit)
}

// importOrigin is where an import came from: memax init's upload, or a
// space's V1 memories at the switch (switch.go).
type importOrigin string

const (
	importOriginInit importOrigin = "init"
	importOriginV1   importOrigin = "v1"
)

// importItems is Import once the request is valid: open the import, write
// each statement (or group of repeats) as its own command, then close it
// and queue its conflict check.
func (l *Ledger) importItems(ctx context.Context, meta Meta, req ImportRequest, origin importOrigin) (*ImportResult, error) {
	if _, ok := meta.Scope.Grant(req.SpaceID); !ok {
		return nil, ErrNotFound
	}
	meta.Via = policy.ViaImport
	if err := validateMeta(&meta, l.now()); err != nil {
		return nil, err
	}
	for i := range req.Items {
		req.Items[i].SpaceID = req.SpaceID
		if err := req.Items[i].NewMemory.validate(); err != nil {
			return nil, err
		}
	}
	meta.Scope = meta.Scope.Narrow(req.SpaceID)
	hash, err := req.hash()
	if err != nil {
		return nil, err
	}
	imp, replayed, refusal, err := l.openImport(ctx, meta, &req, hash, origin)
	if err != nil {
		return nil, err
	}
	if refusal != nil {
		return &ImportResult{Refused: refusal}, nil
	}

	results := make([]ImportItemResult, len(req.Items))
	var refused []importItemRow
	prefix := itemKeyPrefix(meta.IdempotencyKey)
	for _, g := range foldImport(req.Items) {
		lead := req.Items[g[0]]
		cmd := &ImportStatement{Meta: meta, Import: imp.ID, NewMemory: mergeGroup(req.Items, g)}
		cmd.IdempotencyKey = prefix + lead.Key
		for _, p := range g {
			it := req.Items[p]
			cmd.Items = append(cmd.Items, importItemRow{Position: p, Key: it.Key, Ref: it.Ref, Location: it.Location,
				HiddenCharacters: it.HiddenCharacters})
		}
		res, err := l.Apply(ctx, cmd)
		if err != nil {
			return nil, err
		}
		for i, p := range g {
			r := ImportItemResult{Key: req.Items[p].Key, Position: p, Ref: req.Items[p].Ref}
			switch {
			case res.Outcome == OutcomeRefused:
				d := res.Policy
				r.Outcome, r.Policy = ImportRefused, &d
				row := cmd.Items[i]
				row.Outcome, row.Code = ImportRefused, d.Code
				refused = append(refused, row)
			case len(res.Receipts) == 0:
				r.Outcome = ImportExisting
				r.Memory = &MemoryPointer{ID: res.Memory.ID, Ref: res.Memory.Ref}
				r.Lifecycle = string(res.Memory.Lifecycle)
			case i == 0:
				r.Outcome = ImportProposed
				r.Memory = &MemoryPointer{ID: res.Memory.ID, Ref: res.Memory.Ref}
			default:
				r.Outcome, r.FoldedInto = ImportFolded, lead.Key
				r.Memory = &MemoryPointer{ID: res.Memory.ID, Ref: res.Memory.Ref}
			}
			results[p] = r
		}
	}
	if err := l.closeImport(ctx, meta, imp, refused); err != nil {
		return nil, err
	}
	summary, err := l.importSummary(ctx, meta.Scope, imp.SpaceID, imp.ID)
	if err != nil {
		return nil, err
	}
	return &ImportResult{Import: summary, Items: results, Replayed: replayed}, nil
}

// itemKeyPrefix derives the items' idempotency keys from the request's,
// short enough for any request key.
func itemKeyPrefix(key string) string {
	sum := sha256.Sum256([]byte(key))
	return "import:" + hex.EncodeToString(sum[:12]) + ":"
}

// foldImport groups the items that repeat each other, as the judge's
// stage 0 decides a repeat: the same normalised words, or a near-verbatim
// repeat (textsig.NearDuplicate, which refuses any difference in numbers,
// negations and content words). Groups keep request order, and an item
// joins the first group whose first item it repeats. Only items from the
// same kind of location fold, and a group cites at most MaxSources files.
func foldImport(items []ImportItem) [][]int {
	var groups [][]int
	sources := []int{}
	byHash := map[string]int{}
	byBand := map[int64][]int{}
	for i, it := range items {
		hash := textsig.ContentSHA256(it.Statement)
		bands := textsig.BandsOf(it.Statement)
		fits := func(g int) bool {
			lead := items[groups[g][0]]
			return lead.Location == it.Location && sources[g]+max(1, len(it.Sources)) <= MaxSources
		}
		target := -1
		if g, ok := byHash[hash]; ok && fits(g) {
			target = g
		}
		if target < 0 {
			var cands []int
			for _, b := range bands {
				cands = append(cands, byBand[b]...)
			}
			slices.Sort(cands)
			for _, g := range slices.Compact(cands) {
				if fits(g) && textsig.NearDuplicate(it.Statement, items[groups[g][0]].Statement) {
					target = g
					break
				}
			}
		}
		if target >= 0 {
			groups[target] = append(groups[target], i)
			sources[target] += max(1, len(it.Sources))
			continue
		}
		groups = append(groups, []int{i})
		sources = append(sources, max(1, len(it.Sources)))
		g := len(groups) - 1
		if _, ok := byHash[hash]; !ok {
			byHash[hash] = g
		}
		for _, b := range bands {
			byBand[b] = append(byBand[b], g)
		}
	}
	return groups
}

// mergeGroup is one proposal for a group of repeats: the first item's
// words, section and kind, citing every item's sources once. It applies
// wherever any of them does: unscoped if any is, else every path named.
func mergeGroup(items []ImportItem, g []int) NewMemory {
	nm := items[g[0]].NewMemory
	if len(g) == 1 {
		return nm
	}
	nm.Sources = nil
	seen := map[string]bool{}
	var paths []string
	unscoped := false
	for _, p := range g {
		it := items[p]
		for _, s := range it.Sources {
			k := string(s.Kind) + "\x00" + s.Ref
			if !seen[k] && len(nm.Sources) < MaxSources {
				seen[k] = true
				nm.Sources = append(nm.Sources, s)
			}
		}
		ps := scopePaths(it.Applies)
		if len(ps) == 0 {
			unscoped = true
		}
		paths = append(paths, ps...)
	}
	switch {
	case unscoped:
		nm.Applies = json.RawMessage(`{}`)
	default:
		slices.Sort(paths)
		b, _ := json.Marshal(map[string][]string{"paths": slices.Compact(paths)})
		nm.Applies = b
	}
	return nm
}

// scopePaths reads {"paths": [...]} from a memory's scope.
func scopePaths(raw json.RawMessage) []string {
	var s struct {
		Paths []string `json:"paths"`
	}
	if len(bytes.TrimSpace(raw)) == 0 || json.Unmarshal(raw, &s) != nil {
		return nil
	}
	return s.Paths
}

// openImport opens the import for the request's key, or finds the one it
// opened before (replayed). Policy first decides whether the actor may
// write in the space at all.
func (l *Ledger) openImport(ctx context.Context, meta Meta, req *ImportRequest, hash []byte, origin importOrigin) (*Import, bool, *policy.Decision, error) {
	tx, _, err := l.begin(ctx, meta.Scope, pgx.ReadWrite)
	if err != nil {
		return nil, false, nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	sp, err := loadSpace(ctx, tx, req.SpaceID)
	if err != nil {
		return nil, false, nil, err
	}
	grant, _ := meta.Scope.Grant(sp.ID)
	if d := policy.Decide(toPolicyActor(meta.Actor, meta.Via, grant), policy.ActionPropose, policy.Object{}, sp.policy()); d.Effect == policy.EffectRefuse {
		return nil, false, &d, nil
	}
	files, err := json.Marshal(nonNilSlice(req.Files))
	if err != nil {
		return nil, false, nil, err
	}
	skipped, err := json.Marshal(nonNilSlice(req.Skipped))
	if err != nil {
		return nil, false, nil, err
	}
	var actorID *uuid.UUID
	if meta.Actor.ID != uuid.Nil {
		id := meta.Actor.ID
		actorID = &id
	}
	imp := &Import{ID: newID(), SpaceID: sp.ID, TenantID: sp.TenantID}
	tag, err := tx.Exec(ctx, `
		INSERT INTO v2.imports (id, tenant_id, space_id, actor_kind, actor_id, agent, idempotency_key, request_sha256,
		                        client, files, skipped, items_total, origin)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		ON CONFLICT ON CONSTRAINT imports_key DO NOTHING`,
		imp.ID, sp.TenantID, sp.ID, string(meta.Actor.Kind), actorID, nullText(meta.Actor.Agent),
		meta.IdempotencyKey, hash, nullText(req.Client), files, skipped, len(req.Items), string(origin))
	if err != nil {
		return nil, false, nil, fmt.Errorf("ledger: open import: %w", err)
	}
	replayed := tag.RowsAffected() == 0
	if replayed {
		var stored []byte
		if err := tx.QueryRow(ctx, `
			SELECT id, request_sha256 FROM v2.imports
			 WHERE space_id = $1 AND actor_kind = $2 AND actor_id IS NOT DISTINCT FROM $3 AND idempotency_key = $4`,
			sp.ID, string(meta.Actor.Kind), actorID, meta.IdempotencyKey).Scan(&imp.ID, &stored); err != nil {
			return nil, false, nil, fmt.Errorf("ledger: open import: %w", err)
		}
		if !bytes.Equal(stored, hash) {
			return nil, false, nil, fmt.Errorf("%w: key %q opened another import; send a new key for a new import",
				ErrIdempotencyKeyReused, meta.IdempotencyKey)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, false, nil, mapDBError(fmt.Errorf("ledger: open import: %w", err))
	}
	return imp, replayed, nil, nil
}

// closeImport records the refused items, marks the import uploaded and
// queues its conflict check, in one transaction.
func (l *Ledger) closeImport(ctx context.Context, meta Meta, imp *Import, refused []importItemRow) error {
	tx, loginRole, err := l.begin(ctx, meta.Scope, pgx.ReadWrite)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := insertImportItems(ctx, tx, imp.ID, imp.SpaceID, refused); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE v2.imports SET uploaded_at = COALESCE(uploaded_at, now()) WHERE id = $1 AND space_id = $2`,
		imp.ID, imp.SpaceID); err != nil {
		return fmt.Errorf("ledger: close import: %w", err)
	}
	w := &writer{tx: tx, meta: &meta, inserter: l.inserter, loginRole: loginRole}
	w.jobs = append(w.jobs, river.InsertManyParams{Args: JudgeImportArgs{ImportID: imp.ID, SpaceID: imp.SpaceID}})
	if err := w.flush(ctx); err != nil {
		return mapDBError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return mapDBError(fmt.Errorf("ledger: close import: %w", err))
	}
	return nil
}

// ---------------------------------------------------------------------
// One statement
// ---------------------------------------------------------------------

// CommandImportStatement names ImportStatement.
const CommandImportStatement CommandName = "import_statement"

// importItemRow is one v2.import_items row.
type importItemRow struct {
	Position         int
	Key              string
	Ref              string
	Location         ImportLocation
	HiddenCharacters int
	Outcome          ImportOutcome
	MemoryID         *uuid.UUID
	FoldedInto       *int
	Code             string
}

// ImportStatement writes one statement of an import (or one group of
// repeats) as a proposal, or finds it already in the space. Import runs
// it; it isn't a request on its own.
type ImportStatement struct {
	Meta
	NewMemory
	Import uuid.UUID
	// Items are the import's items this statement stands for, the first
	// one first.
	Items []importItemRow
}

// Name implements Command.
func (*ImportStatement) Name() CommandName { return CommandImportStatement }

func (c *ImportStatement) validate() error {
	if c.Import == uuid.Nil || len(c.Items) == 0 {
		return invalid("import", "say which import and which of its items")
	}
	return c.NewMemory.validate()
}

// importStatement applies ImportStatement.
func (w *writer) importStatement(ctx context.Context, c *ImportStatement) (Result, error) {
	grant, ok := w.meta.Scope.Grant(c.SpaceID)
	if !ok {
		return Result{}, ErrNotFound
	}
	sp, err := loadSpace(ctx, w.tx, c.SpaceID)
	if err != nil {
		return Result{}, err
	}
	if replay, err := w.claim(ctx, sp.ID); err != nil || replay != nil {
		return deref(replay), err
	}
	var open bool
	if err := w.tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM v2.imports WHERE id = $1 AND space_id = $2)`,
		c.Import, sp.ID).Scan(&open); err != nil {
		return Result{}, fmt.Errorf("ledger: import: %w", err)
	}
	if !open {
		return Result{}, ErrNotFound
	}
	rows := slices.Clone(c.Items)
	existing, err := w.alreadyRecorded(ctx, sp.ID, c.Statement)
	if err != nil {
		return Result{}, err
	}
	if existing != nil {
		for i := range rows {
			rows[i].Outcome, rows[i].MemoryID = ImportExisting, &existing.ID
		}
		if err := insertImportItems(ctx, w.tx, c.Import, sp.ID, rows); err != nil {
			return Result{}, err
		}
		return w.finish(ctx, Result{Outcome: OutcomeApplied, Policy: policy.Decision{Effect: policy.EffectApply}}, existing.ID)
	}
	res, err := w.writeMemory(ctx, sp, grant, c.NewMemory, true)
	if err != nil || res.Outcome == OutcomeRefused {
		return res, err
	}
	lead := rows[0].Position
	for i := range rows {
		rows[i].Outcome, rows[i].MemoryID = ImportProposed, &res.Memory.ID
		if i > 0 {
			rows[i].Outcome, rows[i].FoldedInto = ImportFolded, &lead
		}
	}
	if err := insertImportItems(ctx, w.tx, c.Import, sp.ID, rows); err != nil {
		return Result{}, err
	}
	return res, nil
}

// alreadyRecorded finds a memory of the space that the statement repeats,
// by the judge's stage 0: the same normalised words first, then a
// near-verbatim repeat, preferring what is kept, then proposed, faded,
// merged and rejected. Forgotten memories never match: their words are
// gone.
func (w *writer) alreadyRecorded(ctx context.Context, spaceID uuid.UUID, statement string) (*Memory, error) {
	hash, bands := signature(statement)
	rows, err := w.tx.Query(ctx, memorySelect+`
		 WHERE m.space_id = $1 AND m.lifecycle <> 'forgotten'
		   AND (m.content_sha256 = $2 OR m.minhash_bands && $3::bigint[])
		 ORDER BY (m.content_sha256 = $2) DESC,
		          CASE m.lifecycle WHEN 'kept' THEN 0 WHEN 'proposed' THEN 1 WHEN 'faded' THEN 2 WHEN 'merged' THEN 3 ELSE 4 END,
		          m.seq
		 LIMIT 50`, spaceID, hash, bands)
	if err != nil {
		return nil, fmt.Errorf("ledger: find repeats: %w", err)
	}
	cands, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (*Memory, error) { return scanMemory(r) })
	if err != nil {
		return nil, fmt.Errorf("ledger: find repeats: %w", err)
	}
	for _, m := range cands {
		if textsig.ContentSHA256(m.Statement) == hash || textsig.NearDuplicate(statement, m.Statement) {
			return m, nil
		}
	}
	return nil, nil
}

func insertImportItems(ctx context.Context, tx pgx.Tx, importID, spaceID uuid.UUID, rows []importItemRow) error {
	if len(rows) == 0 {
		return nil
	}
	b := &pgx.Batch{}
	for _, r := range rows {
		b.Queue(`
			INSERT INTO v2.import_items (import_id, position, space_id, item_key, ref, location, outcome, memory_id,
			                             folded_into, code, hidden_characters)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
			ON CONFLICT (import_id, position) DO NOTHING`,
			importID, r.Position, spaceID, r.Key, r.Ref, string(r.Location), string(r.Outcome), r.MemoryID,
			r.FoldedInto, nullText(r.Code), r.HiddenCharacters)
	}
	if err := tx.SendBatch(ctx, b).Close(); err != nil {
		return fmt.Errorf("ledger: import items: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------
// The conflict check job
// ---------------------------------------------------------------------

// JudgeImportArgs is the River job that checks an import for
// disagreements among its proposals (internal/judge). It runs once per
// import, on the judge's queue.
type JudgeImportArgs struct {
	ImportID uuid.UUID `json:"import_id"`
	SpaceID  uuid.UUID `json:"space_id"`
}

// Kind implements river.JobArgs.
func (JudgeImportArgs) Kind() string { return "judge_import" }

// InsertOpts implements river.JobArgsWithInsertOpts.
func (JudgeImportArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: QueueJudge, MaxAttempts: 3, UniqueOpts: river.UniqueOpts{ByArgs: true}}
}
