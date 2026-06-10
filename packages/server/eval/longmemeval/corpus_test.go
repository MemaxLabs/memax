package longmemeval

import (
	"testing"
)

func boolPtr(b bool) *bool { return &b }

// TestBuildCorpusSession verifies session-level corpus construction matches
// the Python process_item_flat_index with granularity='session'.
func TestBuildCorpusSession(t *testing.T) {
	entry := &Entry{
		QuestionID:   "test_q1",
		QuestionType: "single-session-user",
		Question:     "What color is the sky?",
		Answer:       "blue",
		HaystackDates: []string{
			"2023/04/10 (Mon) 17:50",
			"2023/04/10 (Mon) 18:00",
		},
		HaystackSessionIDs: []string{
			"answer_abc123_1",
			"doc_xyz456_2",
		},
		HaystackSessions: [][]Turn{
			// Answer session — user turn 1 has answer, turn 3 does not.
			{
				{Role: "user", Content: "The sky is blue.", HasAnswer: boolPtr(true)},
				{Role: "assistant", Content: "Indeed!"},
				{Role: "user", Content: "What about clouds?", HasAnswer: boolPtr(false)},
			},
			// Filler session.
			{
				{Role: "user", Content: "Tell me a joke."},
				{Role: "assistant", Content: "Why did the chicken..."},
			},
		},
	}

	docs := BuildCorpus(entry, SessionGranularity)
	if len(docs) != 2 {
		t.Fatalf("expected 2 docs, got %d", len(docs))
	}

	// First doc: answer session — has at least one has_answer=true turn, so
	// ID should keep "answer".
	if docs[0].ID != "answer_abc123_1" {
		t.Errorf("doc[0].ID = %q, want %q", docs[0].ID, "answer_abc123_1")
	}
	// Text should concatenate user-side turns.
	wantText := "The sky is blue. What about clouds?"
	if docs[0].Text != wantText {
		t.Errorf("doc[0].Text = %q, want %q", docs[0].Text, wantText)
	}

	// Second doc: filler session.
	if docs[1].ID != "doc_xyz456_2" {
		t.Errorf("doc[1].ID = %q, want %q", docs[1].ID, "doc_xyz456_2")
	}
}

// TestBuildCorpusSessionNoAnswerReclassify verifies that an "answer" session
// with no has_answer=true user turns is reclassified to "noans".
func TestBuildCorpusSessionNoAnswerReclassify(t *testing.T) {
	entry := &Entry{
		QuestionID: "test_q2",
		HaystackDates: []string{
			"2023/04/10 (Mon) 17:50",
		},
		HaystackSessionIDs: []string{
			"answer_abc123_1",
		},
		HaystackSessions: [][]Turn{
			{
				{Role: "user", Content: "Hello there.", HasAnswer: boolPtr(false)},
				{Role: "assistant", Content: "Hi!"},
				{Role: "user", Content: "How are you?", HasAnswer: boolPtr(false)},
			},
		},
	}

	docs := BuildCorpus(entry, SessionGranularity)
	if len(docs) != 1 {
		t.Fatalf("expected 1 doc, got %d", len(docs))
	}
	// Should be reclassified to "noans".
	if docs[0].ID != "noans_abc123_1" {
		t.Errorf("doc[0].ID = %q, want %q", docs[0].ID, "noans_abc123_1")
	}
}

// TestBuildCorpusTurn verifies turn-level corpus construction.
func TestBuildCorpusTurn(t *testing.T) {
	entry := &Entry{
		QuestionID: "test_q3",
		HaystackDates: []string{
			"2023/04/10 (Mon) 17:50",
			"2023/04/10 (Mon) 18:00",
		},
		HaystackSessionIDs: []string{
			"answer_abc123_1",
			"doc_xyz456_2",
		},
		HaystackSessions: [][]Turn{
			// Answer session: 3 turns (user, assistant, user).
			// Turn indices are 1-indexed over the full session list.
			{
				{Role: "user", Content: "The sky is blue.", HasAnswer: boolPtr(true)},
				{Role: "assistant", Content: "Indeed!"},
				{Role: "user", Content: "What about clouds?", HasAnswer: boolPtr(false)},
			},
			// Filler session.
			{
				{Role: "user", Content: "Tell me a joke."},
				{Role: "assistant", Content: "Why did the chicken..."},
			},
		},
	}

	docs := BuildCorpus(entry, TurnGranularity)

	// Expected: 3 user turns across 2 sessions.
	if len(docs) != 3 {
		t.Fatalf("expected 3 docs, got %d", len(docs))
	}

	// Turn 0 (index=1): has_answer=true → keeps "answer".
	if docs[0].ID != "answer_abc123_1_1" {
		t.Errorf("doc[0].ID = %q, want %q", docs[0].ID, "answer_abc123_1_1")
	}

	// Turn 2 (index=3): has_answer=false → "answer" → "noans".
	if docs[1].ID != "noans_abc123_1_3" {
		t.Errorf("doc[1].ID = %q, want %q", docs[1].ID, "noans_abc123_1_3")
	}

	// Filler session turn (index=1): no "answer" in session ID.
	if docs[2].ID != "doc_xyz456_2_1" {
		t.Errorf("doc[2].ID = %q, want %q", docs[2].ID, "doc_xyz456_2_1")
	}
}

// TestCorrectDocs verifies evidence identification.
func TestCorrectDocs(t *testing.T) {
	docs := []CorpusDoc{
		{ID: "answer_a"},
		{ID: "doc_b"},
		{ID: "noans_c"},
		{ID: "answer_d"},
	}
	correct := CorrectDocs(docs)
	if len(correct) != 2 {
		t.Fatalf("expected 2 correct docs, got %d", len(correct))
	}
	if !correct["answer_a"] || !correct["answer_d"] {
		t.Error("wrong correct docs:", correct)
	}
}
