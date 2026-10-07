package v2dream

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/judge"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/lifecycle"
	"github.com/MemaxLabs/memax/packages/server/internal/secrets"
	"github.com/MemaxLabs/memax/packages/server/internal/textsig"
	"github.com/MemaxLabs/memax/packages/server/internal/v2recall"
)

// Limits on what one run asks the model.
const (
	notesPerBatch     = 8
	candidatesPerNote = 5
	maxCandidates     = 30
	factsPerBatch     = 4
	maxFactRunes      = 240
	maxConflictChecks = 20
	conflictCands     = 6
	maxDedupeChecks   = 20
	maxStale          = 50
	maxBriefNew       = 20
)

// runState is one run in progress.
type runState struct {
	e          *Engine
	snap       *ledger.DreamSnapshot
	scope      ledger.Scope
	model      *countingModel
	actions    []ledger.PlannedAction
	timings    map[string]int64
	considered map[string]int
	// proposed are the content hashes of facts this run proposes.
	proposed []string
}

func (r *runState) add(a ledger.PlannedAction) { r.actions = append(r.actions, a) }

// search finds the kept memories nearest a text.
func (r *runState) search(ctx context.Context, text string, limit int) []v2recall.Hit {
	if r.e.searcher == nil || strings.TrimSpace(text) == "" {
		return nil
	}
	res, err := r.e.searcher.Search(ctx, r.scope, v2recall.Query{Text: truncate(text, 600), Limit: limit,
		Filter: v2recall.Filter{Spaces: []uuid.UUID{r.snap.SpaceID}, Lifecycle: "kept"}})
	if err != nil {
		r.e.log.WarnContext(ctx, "dream: candidate search failed", "error", err.Error())
		return nil
	}
	return res.Hits
}

// ---------------------------------------------------------------------
// Fold: notes into what was kept, or into proposals
// ---------------------------------------------------------------------

func (r *runState) fold(ctx context.Context) error {
	r.considered["notes"] = len(r.snap.Notes)
	if len(r.snap.Notes) == 0 || r.model == nil {
		return nil
	}
	for start := 0; start < len(r.snap.Notes); start += notesPerBatch {
		if r.model.spent() {
			r.considered["budget_spent"]++
			return nil
		}
		batch := r.snap.Notes[start:min(start+notesPerBatch, len(r.snap.Notes))]
		if err := r.foldBatch(ctx, batch); err != nil {
			return err
		}
	}
	return nil
}

func (r *runState) foldBatch(ctx context.Context, batch []ledger.DreamNote) error {
	// Candidates: the kept memories nearest each note.
	var cands []v2recall.Hit
	for _, n := range batch {
		for _, h := range r.search(ctx, n.Title+"\n"+n.Body, candidatesPerNote) {
			if len(cands) < maxCandidates && !slices.ContainsFunc(cands, func(x v2recall.Hit) bool { return x.ID == h.ID }) {
				cands = append(cands, h)
			}
		}
	}
	var b strings.Builder
	b.WriteString("<memories>\n")
	for _, c := range cands {
		writeTag(&b, "memory", [][2]string{{"id", c.Ref}, {"kind", string(c.Kind)}, {"section", string(c.Section)}}, c.Statement)
	}
	b.WriteString("</memories>\n<notes>\n")
	byShort := map[string]ledger.DreamNote{}
	for i, n := range batch {
		id := fmt.Sprintf("n%d", i+1)
		byShort[id] = n
		by := n.Agent
		if by == "" {
			by = n.AuthorKind
		}
		text := strings.TrimSpace(n.Title + "\n" + n.Body)
		writeTag(&b, "note", [][2]string{{"id", id}, {"by", by}}, text)
	}
	b.WriteString("</notes>\n")
	fmt.Fprintf(&b, "\nAt most %d facts.\n", factsPerBatch)
	var ans foldAnswer
	if _, err := r.e.structured(ctx, r.model, "fold", foldSystem, b.String(), foldSchema, &ans); err != nil {
		return err
	}
	saw := make([]uuid.UUID, len(cands))
	for i, c := range cands {
		saw[i] = c.ID
	}
	notesOf := func(ids []string) []uuid.UUID {
		var out []uuid.UUID
		for _, s := range ids {
			if n, ok := byShort[strings.ToLower(strings.TrimSpace(s))]; ok && !slices.Contains(out, n.ID) {
				out = append(out, n.ID)
			}
		}
		return out
	}
	for _, f := range ans.Folds {
		ref := strings.ToUpper(strings.TrimSpace(f.Memory))
		i := slices.IndexFunc(cands, func(c v2recall.Hit) bool { return c.Ref == ref })
		notes := notesOf(f.Notes)
		if i < 0 || len(notes) == 0 || f.Confidence < r.e.cfg.FoldBar {
			r.considered["fold_dropped"]++
			continue
		}
		r.add(ledger.PlannedAction{Kind: ledger.DreamFold, Memory: cands[i].ID, Version: cands[i].Version, Notes: notes, Saw: saw})
	}
	facts := 0
	for _, f := range ans.Facts {
		statement := oneLine(f.Statement)
		notes := notesOf(f.Notes)
		hash := textsig.ContentSHA256(statement)
		switch {
		case statement == "" || utf8.RuneCountInString(statement) > maxFactRunes || len(notes) == 0:
			r.considered["fact_dropped"]++
			continue
		case f.Confidence < r.e.cfg.FactBar || facts >= factsPerBatch:
			r.considered["fact_dropped"]++
			continue
		case len(secrets.DetectCredentials(statement)) > 0:
			r.considered["fact_secret"]++
			continue
		case slices.Contains(r.proposed, hash) || slices.ContainsFunc(cands, func(c v2recall.Hit) bool {
			return textsig.ContentSHA256(c.Statement) == hash || textsig.NearDuplicate(c.Statement, statement)
		}):
			r.considered["fact_repeat"]++
			continue
		}
		section := ledger.Section(f.Section)
		kind := ledger.Kind(f.Kind)
		if !section.Valid() {
			section = ledger.SectionConventions
		}
		if kind != ledger.KindDecision {
			kind = ledger.KindFact
		}
		facts++
		r.proposed = append(r.proposed, hash)
		r.add(ledger.PlannedAction{Kind: ledger.DreamPropose, Notes: notes, Saw: saw,
			New: &ledger.DreamFact{Statement: statement, Section: section, Kind: kind}})
	}
	return nil
}

// ---------------------------------------------------------------------
// Dedupe: proposals that repeat one already waiting
// ---------------------------------------------------------------------

func (r *runState) dedupe(ctx context.Context) error {
	props := r.snap.Proposals
	r.considered["proposals"] = len(props)
	if len(props) < 2 {
		return nil
	}
	folded := map[uuid.UUID]bool{}
	into := map[uuid.UUID]bool{}
	fresh := func(m ledger.DreamMemory) bool { return r.snap.Since == nil || m.CreatedAt.After(*r.snap.Since) }
	acted := func(a, b ledger.DreamMemory) bool {
		return r.snap.Acted[ledger.DreamActed{Kind: ledger.DreamDedupe, Memory: a.ID, Version: a.Version, Related: b.ID}]
	}
	var classifier *judge.Classifier
	if r.model != nil {
		classifier = judge.NewClassifier(r.model, r.e.cfg.judgeConfig())
	}
	checks := 0
	// Newest first: a repeat folds into the oldest proposal it repeats.
	for i := len(props) - 1; i >= 1; i-- {
		p := props[i]
		if !fresh(p) || folded[p.ID] || into[p.ID] {
			continue
		}
		older := props[:i]
		// Stage 0: the same words, or near-verbatim (no model).
		j := slices.IndexFunc(older, func(o ledger.DreamMemory) bool {
			return !folded[o.ID] && !acted(p, o) && (textsig.ContentSHA256(o.Statement) == textsig.ContentSHA256(p.Statement) ||
				textsig.NearDuplicate(o.Statement, p.Statement))
		})
		if j >= 0 {
			r.foldDuplicate(p, older[j], folded, into)
			continue
		}
		if classifier == nil || checks >= maxDedupeChecks || r.model.spent() {
			continue
		}
		var cands []judge.Candidate
		var pick []ledger.DreamMemory
		for _, o := range older {
			if folded[o.ID] || acted(p, o) {
				continue
			}
			if shared, _ := textsig.Overlap(p.Statement, o.Statement); shared >= 2 && len(cands) < 5 {
				cands = append(cands, judge.Candidate{Ref: o.Ref, Statement: o.Statement, Kind: string(o.Kind), Section: string(o.Section)})
				pick = append(pick, o)
			}
		}
		if len(cands) == 0 {
			continue
		}
		checks++
		cls, err := classifier.Classify(withPurpose(ctx, "dedupe"), judge.Proposal{Ref: p.Ref, Statement: p.Statement,
			Kind: string(p.Kind), Section: string(p.Section)}, cands)
		if err != nil {
			return err
		}
		best, bestConf := -1, 0.0
		for k, pr := range cls.Pairs {
			if pr.Relation == ledger.RelationDuplicate && pr.Confidence >= r.e.cfg.DuplicateBar && pr.Confidence > bestConf {
				best, bestConf = k, pr.Confidence
			}
		}
		if best >= 0 {
			r.foldDuplicate(p, pick[best], folded, into)
		}
	}
	return nil
}

func (r *runState) foldDuplicate(p, into ledger.DreamMemory, folded, targets map[uuid.UUID]bool) {
	folded[p.ID], targets[into.ID] = true, true
	r.add(ledger.PlannedAction{Kind: ledger.DreamDedupe, Memory: p.ID, Version: p.Version, Related: into.ID, RelatedVersion: into.Version})
}

// ---------------------------------------------------------------------
// Conflicts: what changed against what is in force
// ---------------------------------------------------------------------

func (r *runState) conflicts(ctx context.Context) error {
	var items []ledger.DreamMemory
	for _, m := range append(slices.Clone(r.snap.Changed), r.snap.Unjudged...) {
		if !slices.ContainsFunc(items, func(x ledger.DreamMemory) bool { return x.ID == m.ID }) {
			items = append(items, m)
		}
	}
	r.considered["conflict_checks"] = min(len(items), maxConflictChecks)
	if r.model == nil || len(items) == 0 {
		return nil
	}
	classifier := judge.NewClassifier(r.model, r.e.cfg.judgeConfig())
	decisions := make([]ledger.JudgeCandidate, 0, len(r.snap.Decisions))
	for _, d := range r.snap.Decisions {
		decisions = append(decisions, ledger.JudgeCandidate{ID: d.ID, Ref: d.Ref, Statement: d.Statement, Kind: d.Kind,
			Section: d.Section, Lifecycle: d.Lifecycle, Area: textsig.NormalizeKey(d.Area), InForce: d.InForce})
	}
	flagged := map[uuid.UUID]bool{}
	for i, x := range items {
		if i >= maxConflictChecks || r.model.spent() {
			break
		}
		type cand struct {
			id      uuid.UUID
			version int
			inForce bool
		}
		var cands []judge.Candidate
		var meta []cand
		addCand := func(id uuid.UUID, ref, statement string, kind ledger.Kind, section ledger.Section, area string, inForce bool, version int) {
			if id == x.ID || len(cands) >= conflictCands || slices.ContainsFunc(meta, func(c cand) bool { return c.id == id }) {
				return
			}
			cands = append(cands, judge.Candidate{Ref: ref, Statement: statement, Kind: string(kind), Section: string(section),
				Area: area, InForce: inForce, Keyed: inForce})
			meta = append(meta, cand{id, version, inForce})
		}
		for _, t := range ledger.Touches(x.Statement, x.Area, decisions) {
			d := t.Decision
			for _, full := range r.snap.Decisions {
				if full.ID == d.ID {
					addCand(d.ID, d.Ref, d.Statement, d.Kind, d.Section, d.Area, true, full.Version)
				}
			}
		}
		if x.Lifecycle == lifecycle.Kept {
			for _, h := range r.search(ctx, x.Statement, conflictCands+1) {
				addCand(h.ID, h.Ref, h.Statement, h.Kind, h.Section, "", false, h.Version)
			}
		}
		if len(cands) == 0 {
			continue
		}
		cls, err := classifier.Classify(withPurpose(ctx, "conflict"), judge.Proposal{Ref: x.Ref, Statement: x.Statement,
			Kind: string(x.Kind), Section: string(x.Section), Area: x.Area}, cands)
		if err != nil {
			return err
		}
		best, bestConf := -1, 0.0
		for k, pr := range cls.Pairs {
			c := meta[k]
			if flagged[c.id] || r.snap.Acted[ledger.DreamActed{Kind: ledger.DreamConflict, Memory: x.ID, Version: x.Version, Related: c.id}] {
				continue
			}
			var hit bool
			switch {
			case c.inForce:
				// A decision in force: a contradiction the strong tier confirmed.
				hit = pr.Relation == ledger.RelationContradicts && !pr.Unconfirmed && !pr.ExplicitChange &&
					pr.Confidence >= r.e.cfg.ConflictBar
			case x.Lifecycle == lifecycle.Kept:
				// Two kept facts that can't both stand: one contradicts or
				// replaces the other's value, and both are in force.
				hit = (pr.Relation == ledger.RelationContradicts || pr.Relation == ledger.RelationUpdates) &&
					pr.Confidence >= r.e.cfg.FactConflictBar
			}
			if hit && pr.Confidence > bestConf {
				best, bestConf = k, pr.Confidence
			}
		}
		if best < 0 {
			continue
		}
		c := meta[best]
		flagged[x.ID] = true
		saw := []uuid.UUID{x.ID}
		for _, m := range meta {
			saw = append(saw, m.id)
		}
		r.add(ledger.PlannedAction{Kind: ledger.DreamConflict, Memory: x.ID, Version: x.Version, Related: c.id,
			RelatedVersion: c.version, Saw: saw})
	}
	return nil
}

// ---------------------------------------------------------------------
// Stale and fade: no model
// ---------------------------------------------------------------------

func (r *runState) stale(context.Context) error {
	r.considered["stale_due"] = len(r.snap.StaleDue)
	for i, m := range r.snap.StaleDue {
		if i >= maxStale {
			break
		}
		r.add(ledger.PlannedAction{Kind: ledger.DreamStale, Memory: m.ID, Version: m.Version, StaleAfter: m.StaleAfter})
	}
	return nil
}

func (r *runState) fade(ctx context.Context) error {
	cands, err := r.e.ledger.FadeCandidates(ctx, r.scope, r.snap.SpaceID, r.e.cfg.FadeAfter, r.e.cfg.MaxFades)
	if err != nil {
		return err
	}
	r.considered["fade_candidates"] = len(cands)
	for _, c := range cands {
		unread := c.Unread
		r.add(ledger.PlannedAction{Kind: ledger.DreamFade, Memory: c.ID, Version: c.Version, Unread: &unread})
	}
	return nil
}

// ---------------------------------------------------------------------
// The Brief: small, cited changes
// ---------------------------------------------------------------------

func (r *runState) brief(ctx context.Context) error {
	b := r.snap.Brief
	if b == nil || r.model == nil || r.model.spent() {
		return nil
	}
	// Only what was kept since the last edition is new to the Brief; what
	// a person left out before, they left out.
	var fresh []ledger.DreamMemory
	faded := map[uuid.UUID]bool{}
	for _, a := range r.actions {
		if a.Kind == ledger.DreamFade {
			faded[a.Memory] = true
		}
	}
	for _, m := range r.snap.Unplaced {
		if (r.snap.Since == nil || m.UpdatedAt.After(*r.snap.Since)) && !faded[m.ID] && len(fresh) < maxBriefNew {
			fresh = append(fresh, m)
		}
	}
	r.considered["brief_new"] = len(fresh)
	if len(fresh) == 0 {
		return nil
	}
	shown := map[string]bool{}
	var saw []uuid.UUID
	var p strings.Builder
	p.WriteString("<brief>\n")
	for _, s := range b.Sections {
		fmt.Fprintf(&p, "<section key=\"%s\" heading=\"%s\">\n", attr(s.Key), attr(s.Heading))
		for i, it := range s.Items {
			id := ledger.BriefItemID(s.Key, i, it)
			if it.Ref != "" {
				m, ok := r.snap.Placed[it.Ref]
				if !ok {
					continue
				}
				shown[it.Ref] = true
				saw = append(saw, m.ID)
				writeTag(&p, "memory", [][2]string{{"id", it.Ref}}, m.Statement)
				continue
			}
			if it.Forgotten {
				continue
			}
			writeTag(&p, "prose", [][2]string{{"id", id}, {"cites", strings.Join(it.Cites, ",")}}, it.Text)
		}
		p.WriteString("</section>\n")
	}
	p.WriteString("</brief>\n<unplaced>\n")
	for _, m := range fresh {
		shown[m.Ref] = true
		saw = append(saw, m.ID)
		writeTag(&p, "memory", [][2]string{{"id", m.Ref}, {"section", string(m.Section)}}, m.Statement)
	}
	p.WriteString("</unplaced>\n")
	for ref, m := range r.snap.Placed {
		if !shown[ref] && m.Lifecycle == lifecycle.Kept {
			shown[ref] = true
		}
	}
	var ans briefAnswer
	if _, err := r.e.structured(ctx, r.model, "brief", fmt.Sprintf(briefSystem, r.e.cfg.BriefMaxOps), p.String(), briefSchema, &ans); err != nil {
		return err
	}
	// Each change is checked on its own against what came before it: one
	// that fails (an uncited line, a memory not shown) is dropped.
	kept := func(ref string) bool { return shown[ref] }
	var ops []ledger.BriefOp
	for _, o := range ans.Ops {
		if len(ops) >= r.e.cfg.BriefMaxOps {
			break
		}
		op := ledger.BriefOp{Op: o.Op, Item: strings.TrimSpace(o.Item), Section: strings.TrimSpace(o.Section),
			After: strings.TrimSpace(o.After), Text: oneLine(o.Text), Cites: o.Cites}
		if op.Op == ledger.BriefOpPlace || op.Op == ledger.BriefOpMove {
			op.Text, op.Cites = "", nil
		}
		if len(secrets.DetectCredentials(op.Text)) > 0 || utf8.RuneCountInString(op.Text) > maxFactRunes {
			r.considered["brief_dropped"]++
			continue
		}
		if _, err := ledger.ApplyBriefOps(b.Sections, append(slices.Clone(ops), op), kept); err != nil {
			r.considered["brief_dropped"]++
			continue
		}
		ops = append(ops, op)
	}
	if len(ops) == 0 {
		return nil
	}
	r.add(ledger.PlannedAction{Kind: ledger.DreamBrief, Brief: &ledger.BriefDelta{BaseVersion: b.Version, Ops: ops}, Saw: saw})
	return nil
}

// oneLine trims a model's text to one line.
func oneLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	return s
}

func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}
