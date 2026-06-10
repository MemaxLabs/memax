package locomo

import (
	"encoding/json"
	"fmt"
	"os"

	lme "github.com/MemaxLabs/memax/packages/server/eval/longmemeval"
)

// RetrievalResultEntry is one JSONL output row. It is intentionally shaped to
// support external LoCoMo answer/judge scripts without including third-party
// baseline scores.
type RetrievalResultEntry struct {
	SampleID        string              `json:"sample_id"`
	QuestionIndex   int                 `json:"question_index"`
	Question        string              `json:"question"`
	Answer          FlexString          `json:"answer"`
	Category        int                 `json:"category"`
	Evidence        []string            `json:"evidence"`
	CorpusMode      CorpusMode          `json:"corpus_mode"`
	RankedItems     []RankedItem        `json:"ranked_items"`
	Metrics         lme.MetricsAtK      `json:"metrics"`
	Hypothesis      string              `json:"hypothesis,omitempty"`
	QAMetrics       *QAMetrics          `json:"qa_metrics,omitempty"`
	QAResults       map[string]QAResult `json:"qa_results,omitempty"`
	SearchLatencyMs int64               `json:"search_latency_ms"`
	LatencyMs       int64               `json:"latency_ms"`
}

// RankedItem is a retrieved corpus document with dialog evidence IDs.
type RankedItem struct {
	CorpusID  string   `json:"corpus_id"`
	SourceIDs []string `json:"source_ids"`
	Text      string   `json:"text"`
	Timestamp string   `json:"timestamp"`
}

// QAResult is the answer generation and optional judge result at one cutoff.
type QAResult struct {
	Cutoff     int          `json:"cutoff"`
	Hypothesis string       `json:"hypothesis"`
	Metrics    QAMetrics    `json:"metrics"`
	Judge      *JudgeResult `json:"judge,omitempty"`
	LatencyMs  int64        `json:"latency_ms"`
}

// JudgeResult is an LLM-as-judge verdict for one generated answer.
type JudgeResult struct {
	Label  string  `json:"label"`
	Score  float64 `json:"score"`
	Reason string  `json:"reason,omitempty"`
	Raw    string  `json:"raw,omitempty"`
}

// WriteResults writes LoCoMo results as JSONL.
func WriteResults(path string, entries []RetrievalResultEntry, appendMode bool) error {
	f, err := openOutputFile(path, appendMode)
	if err != nil {
		return err
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetEscapeHTML(false)
	for _, entry := range entries {
		if err := enc.Encode(entry); err != nil {
			return fmt.Errorf("encode %s q%d: %w", entry.SampleID, entry.QuestionIndex+1, err)
		}
	}
	return nil
}

func openOutputFile(path string, appendMode bool) (*os.File, error) {
	if appendMode {
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return nil, fmt.Errorf("open %s for append: %w", path, err)
		}
		return f, nil
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("create %s: %w", path, err)
	}
	return f, nil
}
