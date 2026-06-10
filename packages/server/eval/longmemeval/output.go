package longmemeval

import (
	"encoding/json"
	"fmt"
	"os"
)

// ---------------------------------------------------------------------------
// Output types — match LongMemEval's expected JSONL formats.
// ---------------------------------------------------------------------------

// RetrievalResultEntry is the per-question retrieval output, matching the
// JSONL format produced by run_retrieval.py.
type RetrievalResultEntry struct {
	QuestionID        string       `json:"question_id"`
	QuestionType      string       `json:"question_type"`
	Question          string       `json:"question"`
	Answer            FlexString   `json:"answer"`
	QuestionDate      string       `json:"question_date"`
	HaystackDates     []string     `json:"haystack_dates"`
	HaystackSessions  [][]Turn     `json:"haystack_sessions"`
	HaystackSessionIDs []string    `json:"haystack_session_ids"`
	AnswerSessionIDs  []string     `json:"answer_session_ids"`
	RetrievalResults  *RetrievalResults `json:"retrieval_results"`
}

// RetrievalResults holds the retrieval output for a single question.
type RetrievalResults struct {
	Query       string              `json:"query"`
	RankedItems []RankedItem        `json:"ranked_items"`
	Metrics     map[string]MetricsAtK `json:"metrics"` // "session" and/or "turn"
}

// RankedItem is a single retrieved document with its corpus ID and text.
type RankedItem struct {
	CorpusID  string `json:"corpus_id"`
	Text      string `json:"text"`
	Timestamp string `json:"timestamp"`
}

// QAHypothesis is the per-question QA output, matching the JSONL format
// expected by evaluate_qa.py.
type QAHypothesis struct {
	QuestionID string `json:"question_id"`
	Hypothesis string `json:"hypothesis"`
}

// ---------------------------------------------------------------------------
// File writers
// ---------------------------------------------------------------------------

// WriteRetrievalLog writes retrieval results as JSONL.
func WriteRetrievalLog(path string, entries []RetrievalResultEntry, appendMode bool) error {
	f, err := openOutputFile(path, appendMode)
	if err != nil {
		return err
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetEscapeHTML(false)
	for _, e := range entries {
		if err := enc.Encode(e); err != nil {
			return fmt.Errorf("encode entry %s: %w", e.QuestionID, err)
		}
	}
	return nil
}

// WriteQAHypotheses writes QA hypotheses as JSONL.
func WriteQAHypotheses(path string, hypotheses []QAHypothesis, appendMode bool) error {
	f, err := openOutputFile(path, appendMode)
	if err != nil {
		return err
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetEscapeHTML(false)
	for _, h := range hypotheses {
		if err := enc.Encode(h); err != nil {
			return fmt.Errorf("encode hypothesis %s: %w", h.QuestionID, err)
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

// ---------------------------------------------------------------------------
// Report printing — matches print_retrieval_metrics.py output format.
// ---------------------------------------------------------------------------

// PrintRetrievalReport prints aggregated retrieval metrics.
func PrintRetrievalReport(entries []RetrievalResultEntry) {
	// Exclude abstention questions.
	var filtered []RetrievalResultEntry
	for _, e := range entries {
		if !isAbstention(e.QuestionID) {
			filtered = append(filtered, e)
		}
	}

	fmt.Printf("\n=== LongMemEval Retrieval Results (%d questions, %d abstention excluded) ===\n\n",
		len(filtered), len(entries)-len(filtered))

	printGranularityMetrics("Session", filtered, "session")
	printGranularityMetrics("Turn", filtered, "turn")
}

func printGranularityMetrics(label string, entries []RetrievalResultEntry, granKey string) {
	metricNames := []string{"recall_any@5", "recall_all@5", "ndcg_any@5", "recall_any@10", "recall_all@10", "ndcg_any@10"}

	// Check if any entries have this granularity.
	hasData := false
	for _, e := range entries {
		if e.RetrievalResults != nil {
			if _, ok := e.RetrievalResults.Metrics[granKey]; ok {
				hasData = true
				break
			}
		}
	}
	if !hasData {
		return
	}

	fmt.Printf("%s-level metrics:\n", label)
	for _, name := range metricNames {
		sum := 0.0
		count := 0
		for _, e := range entries {
			if e.RetrievalResults == nil {
				continue
			}
			m, ok := e.RetrievalResults.Metrics[granKey]
			if !ok {
				continue
			}
			if v, ok := m[name]; ok {
				sum += v
				count++
			}
		}
		if count > 0 {
			fmt.Printf("\t%s = %.4f\n", name, sum/float64(count))
		}
	}
	fmt.Println()
}

// PrintQAReport prints QA accuracy metrics by question type.
// This mirrors print_qa_metrics.py output format.
func PrintQAReport(results []qaEvalResult) {
	typeAcc := make(map[string][]int)
	for _, qt := range AllQuestionTypes {
		typeAcc[qt] = nil
	}

	var allAcc []int
	var abstentionAcc []int

	for _, r := range results {
		label := 0
		if r.Correct {
			label = 1
		}
		typeAcc[r.QuestionType] = append(typeAcc[r.QuestionType], label)
		allAcc = append(allAcc, label)
		if isAbstention(r.QuestionID) {
			abstentionAcc = append(abstentionAcc, label)
		}
	}

	fmt.Println("\n=== LongMemEval QA Results ===")
	fmt.Println()
	fmt.Println("Evaluation results by task:")
	var taskAccs []float64
	for _, qt := range AllQuestionTypes {
		accs := typeAcc[qt]
		if len(accs) == 0 {
			continue
		}
		mean := meanInt(accs)
		fmt.Printf("\t%s: %.4f (%d)\n", qt, mean, len(accs))
		taskAccs = append(taskAccs, mean)
	}

	fmt.Printf("\nTask-averaged Accuracy: %.4f\n", meanFloat(taskAccs))
	fmt.Printf("Overall Accuracy: %.4f\n", meanInt(allAcc))
	if len(abstentionAcc) > 0 {
		fmt.Printf("Abstention Accuracy: %.4f (%d)\n", meanInt(abstentionAcc), len(abstentionAcc))
	}
}

type qaEvalResult struct {
	QuestionID   string
	QuestionType string
	Correct      bool
}

func isAbstention(questionID string) bool {
	for i := 0; i < len(questionID)-3; i++ {
		if questionID[i:i+4] == "_abs" {
			return true
		}
	}
	return false
}

func meanInt(vals []int) float64 {
	if len(vals) == 0 {
		return 0
	}
	sum := 0
	for _, v := range vals {
		sum += v
	}
	return float64(sum) / float64(len(vals))
}

func meanFloat(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range vals {
		sum += v
	}
	return sum / float64(len(vals))
}
