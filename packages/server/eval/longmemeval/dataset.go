// Package longmemeval implements the LongMemEval benchmark (ICLR 2025) for
// evaluating Memax's retrieval pipeline against standardized long-term memory
// tasks.
//
// Reference: https://github.com/xiaowu0162/LongMemEval
//
// The benchmark contains 500 questions across 6 question types, each with a
// haystack of timestamped chat sessions. Evidence sessions are buried among
// filler sessions to test retrieval precision.
package longmemeval

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// ---------------------------------------------------------------------------
// Dataset types — mirrors the LongMemEval JSON schema exactly.
// ---------------------------------------------------------------------------

// Entry represents a single benchmark question with its associated haystack.
type Entry struct {
	QuestionID        string       `json:"question_id"`
	QuestionType      string       `json:"question_type"`
	Question          string       `json:"question"`
	Answer            FlexString   `json:"answer"` // string or number in the dataset
	QuestionDate      string       `json:"question_date"`
	HaystackDates     []string     `json:"haystack_dates"`
	HaystackSessionIDs []string    `json:"haystack_session_ids"`
	HaystackSessions  [][]Turn     `json:"haystack_sessions"`
	AnswerSessionIDs  []string     `json:"answer_session_ids"`
}

// FlexString handles JSON fields that can be either a string or a number.
// LongMemEval's "answer" field is typically a string but can be an integer
// for temporal reasoning questions (e.g., "how many days" → 3).
type FlexString string

func (f *FlexString) UnmarshalJSON(data []byte) error {
	// Try string first.
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*f = FlexString(s)
		return nil
	}
	// Try number — convert to string.
	var n json.Number
	if err := json.Unmarshal(data, &n); err == nil {
		*f = FlexString(n.String())
		return nil
	}
	return fmt.Errorf("answer field: expected string or number, got %s", string(data))
}

func (f FlexString) String() string {
	return string(f)
}

func (f FlexString) MarshalJSON() ([]byte, error) {
	s := string(f)
	// If it looks like a pure integer, marshal as number to match original format.
	if _, err := strconv.Atoi(s); err == nil {
		return []byte(s), nil
	}
	return json.Marshal(s)
}

// Turn represents a single conversational turn within a session.
type Turn struct {
	Role      string `json:"role"`
	Content   string `json:"content"`
	HasAnswer *bool  `json:"has_answer,omitempty"` // only present on user turns in evidence sessions
}

// IsAbstention returns true if this is an abstention question (unanswerable).
func (e *Entry) IsAbstention() bool {
	return strings.Contains(e.QuestionID, "_abs")
}

// HasAnswerTurns returns true if any user turn in the haystack is marked as
// containing the answer. Questions without any has_answer=true turns are
// excluded from retrieval metrics (matching the Python evaluation).
func (e *Entry) HasAnswerTurns() bool {
	for _, session := range e.HaystackSessions {
		for _, turn := range session {
			if turn.Role == "user" && turn.HasAnswer != nil && *turn.HasAnswer {
				return true
			}
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Dataset loading
// ---------------------------------------------------------------------------

// LoadDataset reads a LongMemEval JSON file and returns the parsed entries.
// Supports all three dataset sizes: oracle, s_cleaned, m_cleaned.
func LoadDataset(path string) ([]Entry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read dataset %s: %w", path, err)
	}

	var entries []Entry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("parse dataset %s: %w", path, err)
	}

	// Validate basic structure.
	for i, e := range entries {
		if e.QuestionID == "" {
			return nil, fmt.Errorf("entry %d: missing question_id", i)
		}
		if len(e.HaystackSessions) != len(e.HaystackSessionIDs) {
			return nil, fmt.Errorf("entry %s: session count (%d) != session ID count (%d)",
				e.QuestionID, len(e.HaystackSessions), len(e.HaystackSessionIDs))
		}
		if len(e.HaystackSessions) != len(e.HaystackDates) {
			return nil, fmt.Errorf("entry %s: session count (%d) != date count (%d)",
				e.QuestionID, len(e.HaystackSessions), len(e.HaystackDates))
		}
	}

	return entries, nil
}

// AllQuestionTypes is the canonical list of question types in LongMemEval.
var AllQuestionTypes = []string{
	"single-session-user",
	"single-session-preference",
	"single-session-assistant",
	"multi-session",
	"temporal-reasoning",
	"knowledge-update",
}
