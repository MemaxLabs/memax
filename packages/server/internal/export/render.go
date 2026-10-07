package export

import (
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/lifecycle"
)

// memoryFile is memories/M-0219.md: the memory's record in frontmatter,
// and the statement in force as the body.
func memoryFile(m ledger.ExportMemory) []byte {
	mem := m.Memory
	var f frontmatter
	f.field("format", MemoryFormat)
	f.field("ref", mem.Ref)
	f.field("id", mem.ID)
	f.field("state", string(mem.State))
	f.field("lifecycle", string(mem.Lifecycle))
	f.field("flags", mem.Flags.Strings())
	f.field("section", string(mem.Section))
	f.field("kind", string(mem.Kind))
	f.field("trust", string(mem.Trust))
	f.field("version", mem.Version)
	f.field("stale_after", optTime(mem.StaleAfter))
	f.field("valid_from", optTime(mem.ValidFrom))
	f.field("valid_to", optTime(mem.ValidTo))
	f.field("created_at", stamp(mem.CreatedAt))
	f.field("updated_at", stamp(mem.UpdatedAt))
	f.field("created_receipt", mem.CreatedReceiptID)
	f.field("last_receipt", mem.LastReceiptID)
	f.object("decision", decisionObj(mem.Decision))
	f.field("conditions", jsonOrEmpty(mem.Conditions, "[]"))
	f.field("scope", jsonOrEmpty(mem.Applies, "{}"))
	sources := make([]any, len(mem.Sources))
	for i, s := range mem.Sources {
		sources[i] = obj{{"id", s.ID}, {"kind", string(s.Kind)}, {"ref", s.Ref}, {"uri", optString(s.URI)},
			{"locator", jsonOrEmpty(s.Locator, "{}")}, {"trust", string(s.Trust)}, {"external", s.External},
			{"quote", optString(s.Quote)}, {"content_hash", optString(s.ContentHash)}, {"created_at", stamp(s.CreatedAt)}}
	}
	f.list("sources", sources)
	links := make([]any, len(mem.Links))
	for i, l := range mem.Links {
		links[i] = obj{{"id", l.ID}, {"kind", string(l.Kind)}, {"direction", l.Direction}, {"ref", l.Ref},
			{"memory_id", l.MemoryID}, {"receipt", l.ReceiptID}, {"created_at", stamp(l.CreatedAt)}}
	}
	f.list("links", links)
	versions := make([]any, len(m.Versions))
	for i, v := range m.Versions {
		versions[i] = obj{{"version", v.Version}, {"receipt", v.ReceiptID}, {"created_at", stamp(v.CreatedAt)},
			{"statement", v.Statement}}
	}
	f.list("versions", versions)
	f.list("receipts", stamps(m.Receipts))
	return f.document(mem.Statement)
}

func stamps(rs []ledger.ReceiptStamp) []any {
	out := make([]any, len(rs))
	for i, r := range rs {
		out[i] = obj{{"seq", r.Seq}, {"id", r.ID}, {"action", string(r.Action)}, {"occurred_at", stamp(r.OccurredAt)}}
	}
	return out
}

func decisionObj(d *ledger.DecisionFields) obj {
	if d == nil {
		return nil
	}
	options := make([]any, len(d.Options))
	for i, o := range d.Options {
		options[i] = obj{{"label", o.Label}, {"detail", optString(o.Detail)}}
	}
	return obj{{"status", optString(d.Status)}, {"why", optString(d.Why)}, {"options", options},
		{"consequences", optString(d.Consequences)}, {"area", optString(d.Area)}}
}

// jsonOrEmpty is stored JSON, or def when there is none.
func jsonOrEmpty(raw []byte, def string) any {
	if v := rawValue(raw); v != nil {
		return v
	}
	return rawValue([]byte(def))
}

// tombstoneFile is tombstones/M-0201.md (or space-<id>.md): that it was
// forgotten, when, by whom and what went with it. Never words: not the
// statement, not the sources, not the person's note.
func tombstoneFile(t ledger.Tombstone, receipts []ledger.ReceiptStamp) []byte {
	var f frontmatter
	f.field("format", TombstoneFormat)
	f.field("ref", t.Ref)
	f.field("id", t.ObjectID)
	f.field("kind", t.Kind)
	if t.ID == uuid.Nil {
		f.field("tombstone", nil)
		f.field("op", nil)
	} else {
		f.field("tombstone", t.ID)
		f.field("op", t.OpID)
	}
	if t.ForgottenAt.IsZero() {
		f.field("forgotten_at", nil)
	} else {
		f.field("forgotten_at", stamp(t.ForgottenAt))
	}
	var by any
	if t.By.Kind != "" {
		var id any
		if t.By.ID != nil {
			id = *t.By.ID
		}
		by = obj{{"kind", t.By.Kind}, {"id", id}}
	}
	f.field("by", by)
	var requested any
	if t.RequestedBy != nil {
		requested = obj{{"connection_id", t.RequestedBy.ConnectionID}}
	}
	f.field("requested_by", requested)
	f.field("via", optString(string(t.Via)))
	if t.ReceiptID == uuid.Nil {
		f.field("receipt", nil)
	} else {
		f.field("receipt", t.ReceiptID)
	}
	f.field("carried", optString(t.Carried))
	f.field("primary", optString(t.Primary))
	f.field("with", nonNil(t.With))
	f.field("kept_at", optTime(t.KeptAt))
	f.field("reads_before", t.ReadsBefore)
	f.object("gone", obj{{"versions", t.Gone.Versions}, {"sources", t.Gone.Sources}, {"embeddings", t.Gone.Embeddings},
		{"verdicts", t.Gone.Verdicts}, {"model_verdicts", t.Gone.ModelVerdicts}, {"gates", t.Gone.Gates},
		{"files", t.Gone.Files}, {"memories", t.Gone.Memories}})
	f.field("agents_told", t.Agents)
	f.field("status", optString(t.Status))
	f.field("completed_at", optTime(t.CompletedAt))
	f.field("reapplied_at", optTime(t.ReappliedAt))
	f.list("receipts", stamps(receipts))
	what := t.Ref + " was forgotten"
	if t.Kind == ledger.ObjectSpace {
		what = "Everything in this space was forgotten"
	}
	if !t.ForgottenAt.IsZero() {
		what += " on " + stampOf(t.ForgottenAt)
	}
	return f.document(what + ". Its words are gone from Memax: this file keeps only that it existed, and when and how it was forgotten.\n")
}

// briefFile is brief/B-0007.md: the version's structure in frontmatter, and
// the Brief as it reads (its memories' statements as they are now) in the
// body.
func briefFile(b ledger.Brief, statements map[string]string) []byte {
	var f frontmatter
	f.field("format", BriefFormat)
	f.field("ref", b.Ref)
	f.field("version", b.Version)
	f.field("current", b.Current)
	f.field("brief_id", b.ID)
	f.field("version_id", b.VersionID)
	var parent any
	if b.ParentVersion > 0 {
		parent = b.ParentVersion
	}
	f.field("parent_version", parent)
	f.field("title", b.Title)
	f.field("summary", optString(b.Summary))
	f.field("facts", b.Facts)
	f.field("receipt", b.ReceiptID)
	f.field("created_at", stamp(b.CreatedAt))
	sections := make([]any, len(b.Sections))
	for i, s := range b.Sections {
		items := make([]any, len(s.Items))
		for j, it := range s.Items {
			o := obj{}
			if it.Ref != "" {
				o = append(o, kv{"ref", it.Ref})
			}
			if it.Text != "" {
				o = append(o, kv{"text", it.Text})
			}
			if len(it.Cites) > 0 {
				o = append(o, kv{"cites", it.Cites})
			}
			if it.Forgotten {
				o = append(o, kv{"forgotten", true})
			}
			items[j] = o
		}
		sections[i] = obj{{"key", s.Key}, {"heading", s.Heading}, {"items", items}}
	}
	f.list("sections", sections)

	var body strings.Builder
	title := b.Title
	if title == "" {
		title = "Brief"
	}
	fmt.Fprintf(&body, "# %s\n", title)
	if b.Summary != "" {
		fmt.Fprintf(&body, "\n%s\n", b.Summary)
	}
	for _, s := range b.Sections {
		fmt.Fprintf(&body, "\n## %s\n\n", s.Heading)
		if len(s.Items) == 0 {
			body.WriteString("Nothing here yet.\n")
		}
		for _, it := range s.Items {
			switch {
			case it.Ref != "":
				words, ok := statements[it.Ref]
				if !ok {
					words = "(not in this export)"
				}
				fmt.Fprintf(&body, "- %s [%s]\n", indentItem(words), it.Ref)
			case it.Forgotten:
				fmt.Fprintf(&body, "- (words forgotten) [%s]\n", strings.Join(it.Cites, ", "))
			default:
				fmt.Fprintf(&body, "- %s [%s]\n", indentItem(it.Text), strings.Join(it.Cites, ", "))
			}
		}
	}
	return f.document(body.String())
}

// indentItem keeps a multi-line statement inside its list item.
func indentItem(s string) string { return strings.ReplaceAll(s, "\n", "\n  ") }

var sectionHeadings = map[ledger.Section]string{
	ledger.SectionDecisions:    "Decisions",
	ledger.SectionConventions:  "Conventions",
	ledger.SectionPreferences:  "Preferences",
	ledger.SectionOpenQuestion: "Open questions",
}

// stateWords is how a memory's displayed state reads in the index files.
var stateWords = map[lifecycle.Mark]string{
	lifecycle.MarkKept:      "kept",
	lifecycle.MarkProposed:  "waiting on you",
	lifecycle.MarkMerged:    "merged",
	lifecycle.MarkStale:     "stale",
	lifecycle.MarkConflict:  "in conflict",
	lifecycle.MarkFaded:     "faded",
	lifecycle.MarkForgotten: "forgotten",
	lifecycle.MarkRejected:  "rejected",
}

func memoryLink(ref string) string { return fmt.Sprintf("[%s](%s%s.md)", ref, MemoriesDir, ref) }

// decisionsFile is decisions.md: every decision by where it stands, and
// every decision gate.
func decisionsFile(sp ledger.ExportSpaceInfo, ms []summary, gates []ledger.Gate) []byte {
	groups := []struct {
		title string
		match func(summary) bool
	}{
		{"In force", func(s summary) bool {
			return s.lifecycle == lifecycle.Kept && (s.decision == "" || s.decision == ledger.DecisionInForce)
		}},
		{"Open", func(s summary) bool { return s.lifecycle == lifecycle.Kept && s.decision == ledger.DecisionOpen }},
		{"Superseded", func(s summary) bool { return s.lifecycle == lifecycle.Kept && s.decision == ledger.DecisionSuperseded }},
		{"Waiting on you", func(s summary) bool { return s.lifecycle == lifecycle.Proposed }},
		{"No longer in force", func(s summary) bool {
			return s.lifecycle != lifecycle.Kept && s.lifecycle != lifecycle.Proposed
		}},
	}
	var decisions []summary
	for _, s := range ms {
		if s.kind == ledger.KindDecision {
			decisions = append(decisions, s)
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# Decisions in %s\n\n", sp.Name)
	fmt.Fprintf(&b, "%s and %s. Each decision's file in `memories/` has why it was decided, the options weighed and its receipts.\n",
		plural(len(decisions), "decision", "decisions"), plural(len(gates), "decision gate", "decision gates"))
	for _, g := range groups {
		var in []summary
		for _, s := range decisions {
			if g.match(s) {
				in = append(in, s)
			}
		}
		if len(in) == 0 {
			continue
		}
		fmt.Fprintf(&b, "\n## %s\n\n", g.title)
		for _, s := range in {
			fmt.Fprintf(&b, "- %s %s · %s\n", memoryLink(s.ref), s.line, stateWords[s.state])
		}
	}
	if len(gates) > 0 {
		b.WriteString("\n## Decision gates\n\nQuestions agents asked a person to decide.\n\n")
		for _, g := range gates {
			question := firstLine(g.Question, 160)
			if question == "" {
				question = "(its words were forgotten with the decision it became)"
			}
			var status string
			switch {
			case g.Answer != nil && g.Answer.Memory.Ref != "":
				status = fmt.Sprintf("answered with option %d, kept as %s", g.Answer.Option, memoryLink(g.Answer.Memory.Ref))
			case g.Answer != nil:
				status = fmt.Sprintf("answered with option %d", g.Answer.Option)
			case g.Withdrawn != nil:
				status = "withdrawn " + stampOf(g.Withdrawn.At)
			default:
				status = "waiting until " + stampOf(g.ExpiresAt)
			}
			fmt.Fprintf(&b, "- %s · %s: %s\n", g.Ref, status, question)
		}
	}
	return []byte(b.String())
}

// readmeFile is README.md: what the folder is, how to verify it, and every
// memory by section.
func readmeFile(sp ledger.ExportSpaceInfo, c Counts, ms []summary, briefs []ledger.Brief, seal ledger.ExportSeal) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", sp.Name)
	asOf := "with no receipts yet"
	if !sp.AsOf.IsZero() {
		asOf = "as of " + stampOf(sp.AsOf)
	}
	fmt.Fprintf(&b, "The Memax record of the %s space `%s`, %s: %s, %s, %s and %s.\n\n",
		sp.Kind, sp.Slug, asOf, plural(c.Memories, "memory", "memories"), plural(c.Tombstones, "tombstone", "tombstones"),
		plural(c.BriefVersions, "Brief version", "Brief versions"), plural(int(c.Receipts), "receipt", "receipts"))
	b.WriteString(`Every memory is a Markdown file in ` + "`memories/`" + `: its statement is the body, and its record
(state, section, trust, sources with their ` + "`file:line`" + `, versions, decision fields,
conditions, links and the receipts that touched it) is the YAML frontmatter. A forgotten
memory leaves only a tombstone in ` + "`tombstones/`" + `, without its words.

## Files

- ` + "`memories/`" + `: one file per memory
- ` + "`tombstones/`" + `: one file per forgotten memory, and one per Forget of the whole space
- ` + "`brief/`" + `: every version of the Brief as it reads; ` + "`current: true`" + ` marks the one in force
- ` + "`decisions.md`" + `: every decision by where it stands, and the decision gates
- ` + "`gates.json`" + `, ` + "`targets.json`" + `, ` + "`agents.json`" + `, ` + "`reads.json`" + `: decision gates, compile targets with their latest compile, agents, and read counts
- ` + "`receipts.jsonl`" + `: every receipt in chain order, one per line, with the fields its hash covers
- ` + "`checkpoints.json`" + `: the signed checkpoints of the receipt chain, and the keys they are signed with
- ` + "`export.json`" + `: the format, the space, and the SHA-256 of every other file

## Verify it

    memax verify-export <this folder>

recomputes the receipt chain from the first receipt, checks it against every signed
checkpoint, checks every file against ` + "`export.json`" + `, and reads the memories back against
their receipts.
`)
	sealed := "No receipt was sealed yet when this was exported."
	if h := seal.Head; h != nil && h.Position > 0 {
		sealed = fmt.Sprintf("Receipts 1 to %d are sealed in %s.", h.Position, plural(len(seal.Checkpoints), "checkpoint", "checkpoints"))
	}
	if seal.Unsealed > 0 {
		sealed += fmt.Sprintf(" %s came after the last seal: they chain on, but no checkpoint covers them yet.",
			plural(int(seal.Unsealed), "receipt", "receipts"))
	}
	fmt.Fprintf(&b, "\n%s\n", sealed)
	for _, br := range briefs {
		if br.Current {
			fmt.Fprintf(&b, "\nThe Brief in force is [%s](%s%s.md).\n", br.Ref, BriefDir, br.Ref)
		}
	}
	b.WriteString("\n## Memories\n")
	if len(ms) == 0 {
		b.WriteString("\nNone yet.\n")
	}
	for _, sec := range ledger.Sections {
		var in []summary
		for _, s := range ms {
			if s.section == sec {
				in = append(in, s)
			}
		}
		if len(in) == 0 {
			continue
		}
		fmt.Fprintf(&b, "\n### %s (%d)\n\n", sectionHeadings[sec], len(in))
		for _, s := range in {
			fmt.Fprintf(&b, "- %s %s · %s\n", memoryLink(s.ref), s.line, stateWords[s.state])
		}
	}
	// Sections the ledger doesn't know (none today) still appear.
	var other []summary
	for _, s := range ms {
		if !slices.Contains(ledger.Sections, s.section) {
			other = append(other, s)
		}
	}
	if len(other) > 0 {
		fmt.Fprintf(&b, "\n### Other (%d)\n\n", len(other))
		for _, s := range other {
			fmt.Fprintf(&b, "- %s %s · %s\n", memoryLink(s.ref), s.line, stateWords[s.state])
		}
	}
	return []byte(b.String())
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}
