package longmemeval

import (
	"strings"
)

// ---------------------------------------------------------------------------
// Corpus construction — faithfully reproduces process_item_flat_index from
// LongMemEval's run_retrieval.py.
// ---------------------------------------------------------------------------

// Granularity controls the document granularity for corpus construction.
type Granularity string

const (
	SessionGranularity Granularity = "session"
	TurnGranularity    Granularity = "turn"
)

// CorpusDoc represents a single document in the retrieval corpus.
type CorpusDoc struct {
	ID        string // corpus_id — contains "answer" if this is an evidence document
	Text      string // document text content
	Timestamp string // session timestamp from haystack_dates
}

// CorpusMode controls which turns are included in corpus documents.
type CorpusMode string

const (
	// UserOnly indexes only user turns (LongMemEval standard).
	UserOnly CorpusMode = "user-only"
	// AllTurns indexes both user and assistant turns (matches how Hindsight,
	// agentmemory, Mastra, Backboard, and other top systems run the benchmark).
	AllTurns CorpusMode = "all-turns"
)

// BuildCorpus converts an entry's haystack sessions into retrieval corpus
// documents. With UserOnly mode, this exactly reproduces the Python
// process_item_flat_index logic. With AllTurns mode, assistant content is
// included, matching how most competing systems run the benchmark.
//
// Session granularity: concatenate turns per session into one doc.
// Turn granularity: each user turn is a separate document.
//
// Evidence labeling follows the original logic:
//   - Session with "answer" in its ID but no has_answer=true user turns
//     gets "answer" replaced with "noans" in its corpus ID.
//   - Turn-level: each user turn in an answer session is checked individually;
//     turns without has_answer=true get "answer" replaced with "noans".
//
// When mode is AllTurns, answer sessions keep their "answer" ID regardless of
// has_answer fields — the assistant turn IS the evidence.
func BuildCorpus(entry *Entry, granularity Granularity) []CorpusDoc {
	return BuildCorpusWithMode(entry, granularity, UserOnly)
}

// BuildCorpusWithMode is like BuildCorpus but allows specifying the corpus mode.
func BuildCorpusWithMode(entry *Entry, granularity Granularity, mode CorpusMode) []CorpusDoc {
	var docs []CorpusDoc

	for si, session := range entry.HaystackSessions {
		sessID := entry.HaystackSessionIDs[si]
		ts := entry.HaystackDates[si]

		switch granularity {
		case SessionGranularity:
			docs = append(docs, buildSessionDoc(session, sessID, ts, mode)...)
		case TurnGranularity:
			docs = append(docs, buildTurnDocs(session, sessID, ts)...)
		}
	}

	return docs
}

// buildSessionDoc implements session-level corpus construction.
// Matches: process_item_flat_index with granularity='session'.
func buildSessionDoc(session []Turn, sessID, ts string, mode CorpusMode) []CorpusDoc {
	var parts []string
	for _, turn := range session {
		if mode == AllTurns {
			// Include all turns with role prefix for clarity.
			parts = append(parts, turn.Role+": "+turn.Content)
		} else if turn.Role == "user" {
			parts = append(parts, turn.Content)
		}
	}
	text := strings.Join(parts, " ")

	id := sessID
	if mode == AllTurns {
		// With all turns indexed, answer sessions keep their "answer" ID —
		// the assistant turn IS the evidence, no reclassification needed.
	} else {
		// Standard mode: if this is an answer session but no user turns
		// actually have the answer, reclassify as non-answer.
		if strings.Contains(sessID, "answer") {
			hasAnyAnswer := false
			for _, turn := range session {
				if turn.Role == "user" && turn.HasAnswer != nil && *turn.HasAnswer {
					hasAnyAnswer = true
					break
				}
			}
			if !hasAnyAnswer {
				id = strings.Replace(sessID, "answer", "noans", 1)
			}
		}
	}

	return []CorpusDoc{{ID: id, Text: text, Timestamp: ts}}
}

// buildTurnDocs implements turn-level corpus construction.
// Matches: process_item_flat_index with granularity='turn'.
func buildTurnDocs(session []Turn, sessID, ts string) []CorpusDoc {
	var docs []CorpusDoc

	for ti, turn := range session {
		if turn.Role != "user" {
			continue
		}

		// Turn IDs use 1-indexed position within the full session (including
		// assistant turns), matching the Python: str(i_turn+1).
		turnIdx := ti + 1
		var id string

		if !strings.Contains(sessID, "answer") {
			// Non-answer session: straightforward turn ID.
			id = sessID + "_" + itoa(turnIdx)
		} else {
			// Answer session: check if this specific turn has the answer.
			if turn.HasAnswer != nil && *turn.HasAnswer {
				id = sessID + "_" + itoa(turnIdx)
			} else {
				id = strings.Replace(sessID, "answer", "noans", 1) + "_" + itoa(turnIdx)
			}
		}

		docs = append(docs, CorpusDoc{ID: id, Text: turn.Content, Timestamp: ts})
	}

	return docs
}

// CorrectDocs returns the set of corpus document IDs that contain evidence.
// A document is evidence if its ID contains "answer".
// This matches the Python: correct_docs = [id for id in corpus_ids if "answer" in id]
func CorrectDocs(docs []CorpusDoc) map[string]bool {
	correct := make(map[string]bool)
	for _, d := range docs {
		if strings.Contains(d.ID, "answer") {
			correct[d.ID] = true
		}
	}
	return correct
}

// CorrectDocsList returns CorrectDocs as a sorted slice.
func CorrectDocsList(docs []CorpusDoc) []string {
	seen := make(map[string]bool)
	var result []string
	for _, d := range docs {
		if strings.Contains(d.ID, "answer") && !seen[d.ID] {
			seen[d.ID] = true
			result = append(result, d.ID)
		}
	}
	return result
}

// itoa is a minimal int-to-string without importing strconv.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
