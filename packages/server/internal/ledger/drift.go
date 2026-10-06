package ledger

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger/lifecycle"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// Drift (HANDOFF rule 6, plan 25 §5.7): Memax never writes over a file
// whose hash isn't the last delivered one. A device (or, later, GitHub)
// reports a changed file with RecordObservation; the target is drifted
// until a person resolves it with ResolveDrift:
//
//   - pull: each change becomes a proposal, written by the person on the
//     device (or by the repository, external, when nobody is known). A
//     changed cited line proposes an edit of that memory; a new line
//     proposes a new memory; a removed cited line waits in the resolution
//     for a person to forget or exclude the memory, never an automatic
//     forget. The edited file becomes the baseline, so it stays as it is
//     until the next compile delivers over it.
//   - overwrite: the edited file becomes the baseline Memax may write
//     over, and the target recompiles and is delivered again.
//   - stop: the target is off.

// Owns reports whether path is (or, for scoped rules, is inside) the
// target's own file.
func (t *Target) Owns(path string) bool {
	switch t.Kind.role() {
	case "copy":
		return false
	case "scoped":
		return strings.HasPrefix(path, t.Path+"/")
	}
	return path == t.Path
}

// baseline is the drift hash Memax believes a file has on disk, or "".
func (t *Target) baseline(path string) (DeliveredFile, bool) {
	if t.Delivered == nil {
		return DeliveredFile{}, false
	}
	for _, f := range t.Delivered.Files {
		if f.Path == path {
			return f, true
		}
	}
	return DeliveredFile{}, false
}

// recordObservation records a hand edit, or nothing when the file matches.
func (w *writer) recordObservation(ctx context.Context, c *RecordObservation) (Result, error) {
	t, grant, sp, replay, err := w.openTarget(ctx, c.Target)
	if err != nil {
		return Result{}, err
	}
	if replay != nil {
		res, err := w.replayTarget(ctx, replay, t.ID)
		if err == nil && replay.objectID != nil {
			res.Observations, err = loadObservations(ctx, w.tx, w.meta.Scope, `o.id = $1`, *replay.objectID)
		}
		return res, err
	}
	dec := policy.Decide(w.policyActor(grant), policy.ActionReport, policy.Object{}, sp.policy())
	if dec.Effect == policy.EffectRefuse {
		return refused(dec), nil
	}
	if !t.Owns(c.Path) {
		return Result{}, invalid("path", "%s isn't a file %s writes", c.Path, t.Label)
	}
	unchanged := t.SyncState == SyncOff
	if base, ok := t.baseline(c.Path); ok && base.SHA256 == c.ObservedSHA256 {
		unchanged = true
	}
	if !unchanged {
		// Written but not yet acknowledged: the latest output, not a hand edit.
		last, err := lastGoodRun(ctx, w.tx, w.meta.Scope, t.ID)
		if err != nil {
			return Result{}, err
		}
		if last != nil && slices.ContainsFunc(last.Files, func(f CompiledOutput) bool {
			return f.Path == c.Path && f.DriftSHA256 == c.ObservedSHA256
		}) {
			unchanged = true
		}
	}
	if unchanged {
		if err := fillTargets(ctx, w.tx, w.meta.Scope, []*Target{t}); err != nil {
			return Result{}, err
		}
		return Result{Outcome: OutcomeApplied, Policy: dec, Target: t, Unchanged: true}, nil
	}

	rc := w.objectReceipt(sp, ObjectTarget, t.ID, targetRef(t), ActionObserved, t.Version+1, w.meta.Reason)
	src := c.Path
	if c.Commit != "" {
		src += " @ " + c.Commit[:min(len(c.Commit), 7)]
	}
	rc.Source = &ReceiptSource{Kind: string(SourceFile), Ref: truncateRunes(src, MaxSourceRefRunes)}
	if err := insertReceipt(ctx, w.tx, &rc); err != nil {
		return Result{}, err
	}
	id := newID()
	if err := w.closeObservations(ctx, t.ID, sp.ID, obsFilter{path: c.Path}, ObservationDismissed, "", rc.ID, nil, &id); err != nil {
		return Result{}, err
	}
	changeset, err := json.Marshal(c.Changes)
	if err != nil {
		return Result{}, err
	}
	if _, err := w.tx.Exec(ctx, `
		INSERT INTO v2.target_observations (id, tenant_id, space_id, target_id, path, observed_sha256, observer_kind,
		                                    observer_id, commit_sha, artifact_key, bytes, base_compile_id, base_sha256,
		                                    changeset, status, observed_at, created_receipt_id, last_receipt_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, 'open', $15, $16, $16)`,
		id, sp.TenantID, sp.ID, t.ID, c.Path, c.ObservedSHA256, c.ObserverKind, c.ObserverID, nullText(c.Commit),
		c.ArtifactKey, c.Bytes, c.BaseCompileID, nullText(c.BaseSHA256), changeset, w.meta.OccurredAt, rc.ID); err != nil {
		return Result{}, fmt.Errorf("ledger: write observation: %w", err)
	}
	if _, err := w.tx.Exec(ctx, `
		UPDATE v2.targets SET sync_state = 'drifted', stream_version = $2, last_receipt_id = $3, updated_at = now()
		 WHERE id = $1 AND space_id = $4`, t.ID, rc.StreamVersion, rc.ID, sp.ID); err != nil {
		return Result{}, fmt.Errorf("ledger: update target: %w", err)
	}
	res := Result{Outcome: OutcomeApplied, Policy: dec, Receipts: []Receipt{rc}}
	if err := w.record(ctx, res, id); err != nil {
		return Result{}, err
	}
	if res.Observations, err = loadObservations(ctx, w.tx, w.meta.Scope, `o.id = $1`, id); err != nil {
		return Result{}, err
	}
	if res.Target, err = w.target(ctx, t.ID); err != nil {
		return Result{}, err
	}
	return res, nil
}

// obsFilter picks a target's open observations: all, some by id, or one
// file's.
type obsFilter struct {
	ids  []uuid.UUID
	path string
}

// closeObservations resolves open observations with one receipt.
func (w *writer) closeObservations(ctx context.Context, targetID, spaceID uuid.UUID, f obsFilter, status ObservationStatus,
	mode DriftMode, receiptID uuid.UUID, outcomes map[uuid.UUID][]ChangeOutcome, supersededBy *uuid.UUID) error {
	open, err := loadObservations(ctx, w.tx, w.meta.Scope, `o.target_id = $1 AND o.status = 'open'`, targetID)
	if err != nil {
		return err
	}
	for _, o := range open {
		if (f.ids != nil && !slices.Contains(f.ids, o.ID)) || (f.path != "" && o.Path != f.path) {
			continue
		}
		resolution, err := json.Marshal(DriftResolution{Mode: mode, ReceiptID: receiptID, Changes: outcomes[o.ID], SupersededBy: supersededBy})
		if err != nil {
			return err
		}
		if _, err := w.tx.Exec(ctx, `
			UPDATE v2.target_observations
			   SET status = $2, resolution = $3, resolved_at = now(), last_receipt_id = $4
			 WHERE id = $1 AND space_id = $5`, o.ID, string(status), resolution, receiptID, spaceID); err != nil {
			return fmt.Errorf("ledger: resolve observation: %w", err)
		}
	}
	return nil
}

// resolveDrift pulls, overwrites or stops a target's open hand edits.
func (w *writer) resolveDrift(ctx context.Context, c *ResolveDrift) (Result, error) {
	t, grant, sp, replay, err := w.openTarget(ctx, c.Target)
	if err != nil {
		return Result{}, err
	}
	if replay != nil {
		return w.replayResolve(ctx, replay, t.ID)
	}
	action := policy.ActionConfigureTarget
	if c.Mode == DriftPull {
		action = policy.ActionPullDrift
	}
	dec := policy.Decide(w.policyActor(grant), action, policy.Object{}, sp.policy())
	if dec.Effect == policy.EffectRefuse {
		return refused(dec), nil
	}
	open, err := loadObservations(ctx, w.tx, w.meta.Scope, `o.target_id = $1 AND o.status = 'open'`, t.ID)
	if err != nil {
		return Result{}, err
	}
	var picked []Observation
	for _, o := range open {
		if c.Observation == uuid.Nil || o.ID == c.Observation {
			picked = append(picked, o)
		}
	}
	if len(picked) == 0 {
		return Result{}, &TargetStateError{Ref: t.Label, Message: "there is no hand edit to resolve"}
	}

	rcAction := map[DriftMode]Action{DriftPull: ActionPulled, DriftOverwrite: ActionOverwritten, DriftStop: ActionStopped}[c.Mode]
	rc := w.objectReceipt(sp, ObjectTarget, t.ID, targetRef(t), rcAction, t.Version+1, w.meta.Reason)
	if err := insertReceipt(ctx, w.tx, &rc); err != nil {
		return Result{}, err
	}
	receipts := []Receipt{rc}
	outcomes := map[uuid.UUID][]ChangeOutcome{}
	var files []DeliveredFile
	if t.Delivered != nil {
		files = slices.Clone(t.Delivered.Files)
	}
	var headings map[string]string
	if c.Mode == DriftPull {
		if headings, err = w.briefHeadings(ctx, sp.ID); err != nil {
			return Result{}, err
		}
	}
	ids := make([]uuid.UUID, 0, len(picked))
	for _, o := range picked {
		ids = append(ids, o.ID)
		if c.Mode == DriftPull {
			author, authorGrant := w.driftAuthor(o, grant)
			for _, ch := range o.Changes.Changes {
				out, rcs, err := author.pullChange(ctx, sp, authorGrant, o, ch, headings)
				if err != nil {
					return Result{}, err
				}
				outcomes[o.ID] = append(outcomes[o.ID], out)
				receipts = append(receipts, rcs...)
			}
		}
		if c.Mode != DriftStop {
			id := o.ID
			files = setBaseline(files, DeliveredFile{Path: o.Path, SHA256: o.ObservedSHA, Observation: &id})
		}
	}
	status := map[DriftMode]ObservationStatus{DriftPull: ObservationPulled, DriftOverwrite: ObservationOverwritten,
		DriftStop: ObservationStopped}[c.Mode]
	if err := w.closeObservations(ctx, t.ID, sp.ID, obsFilter{ids: ids}, status, c.Mode, rc.ID, outcomes, nil); err != nil {
		return Result{}, err
	}

	// The target leaves drifted once every file is resolved.
	remaining := len(open) - len(picked)
	delivered := t.Delivered
	if c.Mode != DriftStop {
		var compileID *uuid.UUID
		if delivered != nil && c.Mode == DriftPull {
			compileID = delivered.CompileID
		}
		// Overwrite forgets which run is on disk, so the latest run is
		// delivered again over the accepted baseline.
		delivered = &Delivered{CompileID: compileID, SHA256: baselineSHA(files), Files: files}
	}
	t.Delivered = delivered
	state := SyncOff
	switch {
	case c.Mode == DriftStop:
	case remaining > 0:
		state = SyncDrifted
	default:
		t.SyncState = SyncInSync // recompute from the counters and the baseline
		lastGood := uuid.Nil
		if g := t.lastGoodID(ctx, w); g != nil {
			lastGood = *g
		}
		state = settledState(t, lastGood)
	}
	var compileID any
	var sha, filesJSON any = nil, []byte("[]")
	if delivered != nil {
		if delivered.CompileID != nil {
			compileID = *delivered.CompileID
		}
		sha = delivered.SHA256
		b, err := json.Marshal(nonNilSlice(storedFiles(delivered.Files)))
		if err != nil {
			return Result{}, err
		}
		filesJSON = b
	}
	if _, err := w.tx.Exec(ctx, `
		UPDATE v2.targets
		   SET sync_state = $2, delivered_compile_id = $3, delivered_sha256 = $4, delivered_files = $5,
		       stream_version = $6, last_receipt_id = $7, updated_at = now()
		 WHERE id = $1 AND space_id = $8`,
		t.ID, string(state), compileID, sha, filesJSON, rc.StreamVersion, rc.ID, sp.ID); err != nil {
		return Result{}, fmt.Errorf("ledger: update target: %w", err)
	}
	if c.Mode == DriftOverwrite {
		if err := w.markDirty(ctx, sp.ID, t.ID); err != nil {
			return Result{}, err
		}
	}
	res := Result{Outcome: OutcomeApplied, Policy: dec, Receipts: receipts}
	if err := w.record(ctx, res, t.ID); err != nil {
		return Result{}, err
	}
	return w.loadResolution(ctx, res, t.ID)
}

func (w *writer) replayResolve(ctx context.Context, c *claimed, targetID uuid.UUID) (Result, error) {
	res, err := c.result(ctx, w)
	if err != nil {
		return Result{}, err
	}
	return w.loadResolution(ctx, res, targetID)
}

// loadResolution fills a resolution's result: the target, the
// observations it resolved and the proposals it wrote, found by the
// command's receipts.
func (w *writer) loadResolution(ctx context.Context, res Result, targetID uuid.UUID) (Result, error) {
	ids := make([]uuid.UUID, 0, len(res.Receipts))
	for _, rc := range res.Receipts {
		ids = append(ids, rc.ID)
	}
	var err error
	if res.Target, err = w.target(ctx, targetID); err != nil {
		return Result{}, err
	}
	if res.Observations, err = loadObservations(ctx, w.tx, w.meta.Scope, `o.target_id = $1 AND o.last_receipt_id = ANY($2)`, targetID, ids); err != nil {
		return Result{}, err
	}
	rows, err := w.tx.Query(ctx, memorySelect+` WHERE m.created_receipt_id = ANY($1) AND m.space_id = ANY($2) ORDER BY m.seq`,
		ids, w.meta.Scope.SpaceIDs())
	if err != nil {
		return Result{}, fmt.Errorf("ledger: load proposals: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		m, err := scanMemory(rows)
		if err != nil {
			return Result{}, fmt.Errorf("ledger: load proposals: %w", err)
		}
		res.Proposals = append(res.Proposals, *m)
	}
	if err := rows.Err(); err != nil {
		return Result{}, fmt.Errorf("ledger: load proposals: %w", err)
	}
	for i := range res.Proposals {
		if res.Proposals[i].Sources, err = loadSources(ctx, w.tx, res.Proposals[i].ID); err != nil {
			return Result{}, err
		}
	}
	return res, nil
}

func setBaseline(files []DeliveredFile, f DeliveredFile) []DeliveredFile {
	for i := range files {
		if files[i].Path == f.Path {
			files[i] = f
			return files
		}
	}
	files = append(files, f)
	slices.SortFunc(files, func(a, b DeliveredFile) int { return strings.Compare(a.Path, b.Path) })
	return files
}

// driftAuthor is the writer for a pull's proposals: the person who
// reported the edit from their device, or the repository (whose content is
// external) when no person is known. A device's person keeps the puller's
// role when they are the puller; anyone else proposes with a viewer's
// standing, the least that may propose (the puller authorises the pull).
func (w *writer) driftAuthor(o Observation, puller SpaceGrant) (*writer, SpaceGrant) {
	a := *w
	m := *w.meta
	m.Reason, m.SessionRef = "", ""
	grant := puller
	if o.ObserverKind == ObserverDevice && o.actorID != nil {
		m.Actor = Actor{Kind: policy.ActorPerson, ID: *o.actorID, Credential: policy.CredentialSession}
		m.Via = o.via
		if *o.actorID != w.meta.Actor.ID || w.meta.Actor.Kind != policy.ActorPerson {
			grant.Role, grant.CanForget = policy.RoleViewer, false
		}
	} else {
		m.Actor = Actor{Kind: policy.ActorRepository}
		m.Via = policy.ViaGitHub
		grant.Role, grant.CanForget = policy.RoleNone, false
	}
	a.meta = &m
	return &a, grant
}

// briefHeadings maps the current Brief's headings (lower case) to section
// keys, so a line added under "## Decisions" lands in decisions.
func (w *writer) briefHeadings(ctx context.Context, spaceID uuid.UUID) (map[string]string, error) {
	var structure []byte
	err := w.tx.QueryRow(ctx, `
		SELECT v.structure FROM v2.briefs b
		  JOIN v2.brief_versions v ON v.brief_id = b.id AND v.version = b.current_version
		 WHERE b.space_id = $1`, spaceID).Scan(&structure)
	keys := map[string]string{"what this is": "overview", "decisions": "decisions", "conventions": "conventions",
		"open": "open", "open question": "open", "open questions": "open", "preferences": "preferences"}
	if errNoRows(err) {
		return keys, nil
	}
	if err != nil {
		return nil, fmt.Errorf("ledger: load Brief: %w", err)
	}
	var st briefStructure
	if err := json.Unmarshal(structure, &st); err != nil {
		return nil, fmt.Errorf("ledger: Brief structure: %w", err)
	}
	for _, s := range st.Sections {
		keys[strings.ToLower(strings.TrimSpace(s.Heading))] = s.Key
	}
	return keys, nil
}

// sectionFor is the record section of a Brief section key.
func sectionFor(key string) Section {
	switch key {
	case "decisions":
		return SectionDecisions
	case "preferences":
		return SectionPreferences
	case "open", "open_question":
		return SectionOpenQuestion
	}
	return SectionConventions
}

// pullChange turns one change of a hand edit into a proposal (or a
// removal waiting for a person). w is the drift author's writer.
func (w *writer) pullChange(ctx context.Context, sp spaceRow, grant SpaceGrant, o Observation,
	ch DriftChange, headings map[string]string) (ChangeOutcome, []Receipt, error) {
	out := ChangeOutcome{Kind: ch.Kind, Ref: ch.Ref}
	text := ch.Text
	switch ch.Kind {
	case ChangeRemove:
		out.Line, out.Outcome = ch.OldLine, OutcomeChangeReview
		return out, nil, nil
	case ChangeEdit:
		out.Line, text = ch.NewLine, ch.NewText
	default:
		out.Line, out.Ref = ch.Line, ""
	}
	text = strings.TrimSpace(text)
	skip := func(reason string) (ChangeOutcome, []Receipt, error) {
		out.Outcome, out.Reason = OutcomeChangeSkipped, reason
		return out, nil, nil
	}
	switch {
	case text == "":
		return skip("empty")
	case !utf8.ValidString(text) || strings.ContainsRune(text, 0):
		return skip("invalid_text")
	case utf8.RuneCountInString(text) > MaxStatementRunes:
		return skip("too_long")
	}

	// A changed line citing one kept memory proposes an edit of it; any
	// other change (connective prose, a new line) proposes a new memory.
	var old *Memory
	if ch.Kind == ChangeEdit && len(ch.Refs) == 1 {
		if _, n, ok := ParseRef(ch.Ref); ok {
			var id uuid.UUID
			err := w.tx.QueryRow(ctx, `SELECT id FROM v2.memories WHERE space_id = $1 AND seq = $2`, sp.ID, n).Scan(&id)
			if err != nil && !errNoRows(err) {
				return ChangeOutcome{}, nil, fmt.Errorf("ledger: resolve %s: %w", ch.Ref, err)
			}
			if err == nil {
				m, err := loadMemory(ctx, w.tx, w.meta.Scope, id, false)
				if err != nil {
					return ChangeOutcome{}, nil, err
				}
				if m.Lifecycle == lifecycle.Kept {
					old = m
				}
			}
		}
	}
	section := SectionConventions
	if ch.Section != nil {
		section = sectionFor(headings[strings.ToLower(strings.TrimSpace(*ch.Section))])
	}
	kind := KindFact
	var applies json.RawMessage
	if old != nil {
		section, kind, applies = old.Section, old.Kind, old.Applies
	} else {
		if section == SectionDecisions {
			kind = KindDecision
		}
		if len(ch.Paths) > 0 {
			b, err := json.Marshal(map[string][]string{"paths": ch.Paths})
			if err != nil {
				return ChangeOutcome{}, nil, err
			}
			applies = b
		}
	}
	if len(applies) == 0 {
		applies = json.RawMessage("{}")
	}

	srcRef := fmt.Sprintf("%s:%d", o.Path, out.Line)
	locator := map[string]any{"path": o.Path, "line": out.Line, "observation": o.ID.String()}
	if o.BaseCompile != "" {
		locator["compile"] = o.BaseCompile
	}
	if o.Commit != "" {
		locator["commit"] = o.Commit
	}
	locJSON, err := json.Marshal(locator)
	if err != nil {
		return ChangeOutcome{}, nil, err
	}
	actorTrust := policy.ActorTrust(w.meta.Actor.Kind, w.meta.Via)
	srcs := resolveSources([]SourceInput{{Kind: SourceFile, Ref: truncateRunes(srcRef, MaxSourceRefRunes), URI: o.Path, Locator: locJSON}}, actorTrust)
	trust := policy.MinTrust(actorTrust, srcs[0].trust)

	dec := policy.Decide(w.policyActor(grant), policy.ActionPropose, policy.Object{
		Decision: kind == KindDecision, External: trust.External(), Secrets: findSecrets(text),
	}, sp.policy())
	if dec.Effect == policy.EffectRefuse {
		return skip(dec.Code)
	}
	// A pull never keeps: whatever the author could do, each line waits
	// in Review.
	state, err := lifecycle.Transition(lifecycle.State{}, lifecycle.VerbPropose)
	if err != nil {
		return ChangeOutcome{}, nil, err
	}
	id, rc, err := w.insertMemory(ctx, sp, grant, memoryRow{
		Statement: text, Section: section, Kind: kind, Trust: trust,
		Conditions: json.RawMessage("[]"), Applies: applies,
	}, state, &ReceiptSource{Kind: string(SourceFile), Ref: srcs[0].Ref})
	if err != nil {
		return ChangeOutcome{}, nil, err
	}
	if err := w.insertSources(ctx, sp.ID, id, rc.ID, srcs); err != nil {
		return ChangeOutcome{}, nil, err
	}
	if old != nil {
		if _, err := w.tx.Exec(ctx, `
			INSERT INTO v2.memory_links (id, space_id, kind, from_memory_id, to_memory_id, receipt_id)
			VALUES ($1, $2, 'supersedes', $3, $4, $5)`, newID(), sp.ID, id, old.ID, rc.ID); err != nil {
			return ChangeOutcome{}, nil, fmt.Errorf("ledger: link proposal: %w", err)
		}
	} else {
		out.Ref = ""
	}
	out.Outcome, out.Proposal = OutcomeChangeProposed, rc.ObjectRef
	return out, []Receipt{rc}, nil
}

func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}
