package locomo

import (
	"fmt"
	"sort"
	"strings"
)

// CorpusMode controls which LoCoMo representation is indexed.
type CorpusMode string

const (
	// DialogMode indexes each raw dialogue turn. This matches LoCoMo's
	// published "dialog" RAG database.
	DialogMode CorpusMode = "dialog"
	// ObservationMode indexes generated per-speaker observations.
	ObservationMode CorpusMode = "observation"
	// SummaryMode indexes generated session summaries.
	SummaryMode CorpusMode = "summary"
)

// CorpusDoc is one retrievable unit in a LoCoMo sample.
type CorpusDoc struct {
	ID        string
	Text      string
	Timestamp string
	SourceIDs []string
}

// BuildCorpus converts a LoCoMo sample into benchmark corpus documents.
func BuildCorpus(sample *Sample, mode CorpusMode) []CorpusDoc {
	switch mode {
	case ObservationMode:
		if len(sample.Observation) > 0 {
			return buildObservationCorpus(sample)
		}
	case SummaryMode:
		if len(sample.SessionSummary) > 0 {
			return buildSummaryCorpus(sample)
		}
	}
	return buildDialogCorpus(sample)
}

func buildDialogCorpus(sample *Sample) []CorpusDoc {
	var docs []CorpusDoc
	for _, session := range sample.Conversation.Sessions {
		for _, dialog := range session.Dialogs {
			text := formatDialog(dialog)
			docs = append(docs, CorpusDoc{
				ID:        dialog.DialogID,
				Text:      text,
				Timestamp: session.DateTime,
				SourceIDs: []string{dialog.DialogID},
			})
		}
	}
	return docs
}

func buildObservationCorpus(sample *Sample) []CorpusDoc {
	sessionByNumber := sample.Conversation.sessionByNumber()
	var nums []int
	for n := range sample.Observation {
		nums = append(nums, n)
	}
	sort.Ints(nums)

	var docs []CorpusDoc
	for _, n := range nums {
		session := sessionByNumber[n]
		for i, observation := range sample.Observation[n] {
			id := fmt.Sprintf("S%d:obs:%03d", n, i+1)
			text := observation.Text
			if observation.Speaker != "" {
				text = observation.Speaker + ": " + text
			}
			docs = append(docs, CorpusDoc{
				ID:        id,
				Text:      text,
				Timestamp: session.DateTime,
				SourceIDs: observation.DialogIDs,
			})
		}
	}
	return docs
}

func buildSummaryCorpus(sample *Sample) []CorpusDoc {
	sessionByNumber := sample.Conversation.sessionByNumber()
	var nums []int
	for n := range sample.SessionSummary {
		nums = append(nums, n)
	}
	sort.Ints(nums)

	var docs []CorpusDoc
	for _, n := range nums {
		session := sessionByNumber[n]
		sourceIDs := make([]string, 0, len(session.Dialogs))
		for _, dialog := range session.Dialogs {
			sourceIDs = append(sourceIDs, dialog.DialogID)
		}
		docs = append(docs, CorpusDoc{
			ID:        fmt.Sprintf("S%d", n),
			Text:      sample.SessionSummary[n],
			Timestamp: session.DateTime,
			SourceIDs: sourceIDs,
		})
	}
	return docs
}

func (c Conversation) sessionByNumber() map[int]Session {
	out := make(map[int]Session, len(c.Sessions))
	for _, session := range c.Sessions {
		out[session.Number] = session
	}
	return out
}

func formatDialog(dialog DialogTurn) string {
	text := dialog.Speaker + ` said, "` + dialog.Text + `"`
	if dialog.BLIPCaption != "" {
		if dialog.Query != "" {
			text += " and shared an image for " + dialog.Query + ": " + dialog.BLIPCaption
		} else {
			text += " and shared " + dialog.BLIPCaption
		}
	}
	return text
}

// CorrectDocs returns the corpus documents that overlap with the QA evidence.
func CorrectDocs(docs []CorpusDoc, evidence []string) map[string]bool {
	evidenceSet := make(map[string]bool)
	for _, id := range SplitEvidenceIDs(evidence) {
		evidenceSet[id] = true
	}

	correct := make(map[string]bool)
	if len(evidenceSet) == 0 {
		return correct
	}
	for _, doc := range docs {
		for _, id := range doc.SourceIDs {
			if evidenceSet[id] {
				correct[doc.ID] = true
				break
			}
		}
	}
	return correct
}

// EvidenceRecallAtK returns the fraction of annotated evidence dialog IDs
// covered by the top-k retrieved corpus docs. This mirrors LoCoMo's RAG recall
// accounting over dialog IDs, while supporting observation and summary docs.
func EvidenceRecallAtK(rankedDocs []CorpusDoc, evidence []string, k int) float64 {
	evidenceIDs := SplitEvidenceIDs(evidence)
	if len(evidenceIDs) == 0 {
		return 0
	}

	evidenceSet := make(map[string]bool, len(evidenceIDs))
	for _, id := range evidenceIDs {
		evidenceSet[id] = true
	}

	n := k
	if n > len(rankedDocs) {
		n = len(rankedDocs)
	}
	found := make(map[string]bool)
	for i := 0; i < n; i++ {
		for _, id := range rankedDocs[i].SourceIDs {
			if evidenceSet[id] {
				found[id] = true
			}
		}
	}
	return float64(len(found)) / float64(len(evidenceSet))
}

func corpusIDs(docs []CorpusDoc) []string {
	ids := make([]string, len(docs))
	for i, doc := range docs {
		ids[i] = doc.ID
	}
	return ids
}

func corpusIndexByID(docs []CorpusDoc) map[string]int {
	out := make(map[string]int, len(docs))
	for i, doc := range docs {
		out[doc.ID] = i
	}
	return out
}

func sourceIDsString(ids []string) string {
	if len(ids) == 0 {
		return ""
	}
	return strings.Join(ids, ",")
}
