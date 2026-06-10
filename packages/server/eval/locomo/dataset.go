// Package locomo implements the LoCoMo benchmark for evaluating Memax's
// retrieval and answer synthesis pipelines on long-term conversational memory.
//
// Reference: https://github.com/snap-research/locomo
package locomo

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
)

// Sample is one LoCoMo conversation with annotated QA examples.
type Sample struct {
	SampleID       string            `json:"sample_id"`
	Conversation   Conversation      `json:"conversation"`
	Observation    ObservationSet    `json:"observation"`
	SessionSummary SessionSummarySet `json:"session_summary"`
	QA             []QuestionAnswer  `json:"qa"`
}

// Conversation contains speaker names plus chronological sessions.
type Conversation struct {
	SpeakerA string
	SpeakerB string
	Sessions []Session
}

// Session is a single dated dialogue session.
type Session struct {
	Number   int
	DateTime string
	Dialogs  []DialogTurn
}

// DialogTurn is one turn in a LoCoMo conversation.
type DialogTurn struct {
	Speaker     string   `json:"speaker"`
	DialogID    string   `json:"dia_id"`
	Text        string   `json:"text"`
	ImgURL      []string `json:"img_url,omitempty"`
	BLIPCaption string   `json:"blip_caption,omitempty"`
	Query       string   `json:"query,omitempty"`
}

// QuestionAnswer is an annotated LoCoMo QA item.
type QuestionAnswer struct {
	Question string     `json:"question"`
	Answer   FlexString `json:"answer"`
	Evidence []string   `json:"evidence"`
	Category int        `json:"category"`
}

// FlexString handles answers encoded as either JSON strings or numbers.
type FlexString string

func (f *FlexString) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*f = FlexString(s)
		return nil
	}
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
	if _, err := strconv.Atoi(s); err == nil {
		return []byte(s), nil
	}
	return json.Marshal(s)
}

// ObservationSet maps session numbers to generated observation facts.
type ObservationSet map[int][]ObservationDoc

// ObservationDoc is one generated observation and the dialog IDs supporting it.
type ObservationDoc struct {
	Speaker   string
	Text      string
	DialogIDs []string
}

// SessionSummarySet maps session numbers to generated session summaries.
type SessionSummarySet map[int]string

// UnmarshalJSON parses LoCoMo's dynamic conversation shape:
// speaker_a, speaker_b, session_1_date_time, session_1, ...
func (c *Conversation) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	_ = json.Unmarshal(raw["speaker_a"], &c.SpeakerA)
	_ = json.Unmarshal(raw["speaker_b"], &c.SpeakerB)

	var sessionNums []int
	for key := range raw {
		if strings.HasPrefix(key, "session_") && !strings.HasSuffix(key, "_date_time") {
			n, err := strconv.Atoi(strings.TrimPrefix(key, "session_"))
			if err == nil {
				sessionNums = append(sessionNums, n)
			}
		}
	}
	sort.Ints(sessionNums)

	c.Sessions = make([]Session, 0, len(sessionNums))
	for _, n := range sessionNums {
		var dialogs []DialogTurn
		if err := json.Unmarshal(raw[fmt.Sprintf("session_%d", n)], &dialogs); err != nil {
			return fmt.Errorf("parse session_%d: %w", n, err)
		}
		var dateTime string
		_ = json.Unmarshal(raw[fmt.Sprintf("session_%d_date_time", n)], &dateTime)
		c.Sessions = append(c.Sessions, Session{
			Number:   n,
			DateTime: dateTime,
			Dialogs:  dialogs,
		})
	}
	return nil
}

// UnmarshalJSON parses observation.session_N_observation objects. Each
// observation is encoded as {"Speaker": [["fact", "D1:3"], ...]}.
func (o *ObservationSet) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*o = nil
		return nil
	}
	var raw map[string]map[string][][]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	out := make(ObservationSet)
	for key, bySpeaker := range raw {
		n, ok := parseSessionNumber(key, "session_", "_observation")
		if !ok {
			continue
		}
		for speaker, rows := range bySpeaker {
			for _, row := range rows {
				if len(row) < 2 {
					continue
				}
				var text string
				if err := json.Unmarshal(row[0], &text); err != nil {
					return fmt.Errorf("parse %s observation text: %w", key, err)
				}
				ids, err := parseDialogIDsJSON(row[1])
				if err != nil {
					return fmt.Errorf("parse %s observation evidence: %w", key, err)
				}
				out[n] = append(out[n], ObservationDoc{
					Speaker:   speaker,
					Text:      text,
					DialogIDs: ids,
				})
			}
		}
	}
	*o = out
	return nil
}

// UnmarshalJSON parses session_summary.session_N_summary strings.
func (s *SessionSummarySet) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*s = nil
		return nil
	}
	var raw map[string]string
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	out := make(SessionSummarySet)
	for key, summary := range raw {
		n, ok := parseSessionNumber(key, "session_", "_summary")
		if ok {
			out[n] = summary
		}
	}
	*s = out
	return nil
}

// LoadDataset reads a LoCoMo JSON file.
func LoadDataset(path string) ([]Sample, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read dataset %s: %w", path, err)
	}
	var samples []Sample
	if err := json.Unmarshal(data, &samples); err != nil {
		return nil, fmt.Errorf("parse dataset %s: %w", path, err)
	}
	for i, sample := range samples {
		if sample.SampleID == "" {
			return nil, fmt.Errorf("sample %d: missing sample_id", i)
		}
		if len(sample.Conversation.Sessions) == 0 {
			return nil, fmt.Errorf("sample %s: no conversation sessions", sample.SampleID)
		}
		if len(sample.QA) == 0 {
			return nil, fmt.Errorf("sample %s: no qa annotations", sample.SampleID)
		}
	}
	return samples, nil
}

func parseSessionNumber(key, prefix, suffix string) (int, bool) {
	if !strings.HasPrefix(key, prefix) || !strings.HasSuffix(key, suffix) {
		return 0, false
	}
	raw := strings.TrimSuffix(strings.TrimPrefix(key, prefix), suffix)
	n, err := strconv.Atoi(raw)
	return n, err == nil
}

func parseDialogIDsJSON(raw json.RawMessage) ([]string, error) {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return SplitEvidenceIDs([]string{s}), nil
	}
	var list []string
	if err := json.Unmarshal(raw, &list); err == nil {
		return SplitEvidenceIDs(list), nil
	}
	return nil, fmt.Errorf("expected string or string list, got %s", string(raw))
}

// SplitEvidenceIDs normalizes LoCoMo evidence cells. Some entries contain
// multiple dialog IDs in one string separated by semicolons or commas.
func SplitEvidenceIDs(evidence []string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, item := range evidence {
		item = strings.NewReplacer("(", "", ")", "").Replace(item)
		for _, part := range strings.FieldsFunc(item, func(r rune) bool {
			return r == ';' || r == ','
		}) {
			id := strings.TrimSpace(part)
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// CategoryName returns a stable label for LoCoMo's numeric QA categories.
func CategoryName(category int) string {
	switch category {
	case 1:
		return "multi-hop"
	case 2:
		return "temporal"
	case 3:
		return "open-domain"
	case 4:
		return "single-hop"
	case 5:
		return "adversarial"
	default:
		return fmt.Sprintf("category-%d", category)
	}
}
