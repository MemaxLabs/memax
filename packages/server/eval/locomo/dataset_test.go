package locomo

import (
	"encoding/json"
	"os"
	"testing"
)

func TestSampleUnmarshalDynamicSessions(t *testing.T) {
	raw := []byte(`[{
		"sample_id": "sample_1",
		"conversation": {
			"speaker_a": "Caroline",
			"speaker_b": "Melanie",
			"session_2_date_time": "2:00 pm on 9 May, 2023",
			"session_2": [{"speaker":"Melanie","dia_id":"D2:1","text":"I went camping.","img_url":["https://example.com/image.jpg"],"blip_caption":"a tent","query":"camping"}],
			"session_1_date_time": "1:56 pm on 8 May, 2023",
			"session_1": [{"speaker":"Caroline","dia_id":"D1:1","text":"I joined a support group."}]
		},
		"observation": {
			"session_1_observation": {
				"Caroline": [["Caroline joined a support group.", "D1:1"]]
			}
		},
		"session_summary": {
			"session_1_summary": "Caroline discussed a support group."
		},
		"qa": [{"question":"What did Caroline join?","answer":"support group","evidence":["D1:1"],"category":4}]
	}]`)

	var samples []Sample
	if err := json.Unmarshal(raw, &samples); err != nil {
		t.Fatal(err)
	}
	if len(samples) != 1 {
		t.Fatalf("expected one sample, got %d", len(samples))
	}
	sample := samples[0]
	if sample.Conversation.SpeakerA != "Caroline" || sample.Conversation.SpeakerB != "Melanie" {
		t.Fatalf("unexpected speakers: %#v", sample.Conversation)
	}
	if got := sample.Conversation.Sessions[0].Number; got != 1 {
		t.Fatalf("sessions not sorted; first number=%d", got)
	}
	if got := sample.Observation[1][0].DialogIDs[0]; got != "D1:1" {
		t.Fatalf("observation evidence = %q", got)
	}
	if got := sample.SessionSummary[1]; got == "" {
		t.Fatal("expected session summary")
	}
}

func TestSplitEvidenceIDs(t *testing.T) {
	got := SplitEvidenceIDs([]string{"D8:6; D9:17", "(D1:1)", "D2:2, D2:3", "D1:1"})
	want := []string{"D8:6", "D9:17", "D1:1", "D2:2", "D2:3"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestOfficialDatasetLoads(t *testing.T) {
	path := "data/locomo10.json"
	if _, err := os.Stat(path); err != nil {
		t.Skip("official LoCoMo dataset not downloaded")
	}
	samples, err := LoadDataset(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(samples) != 10 {
		t.Fatalf("samples = %d, want 10", len(samples))
	}
}
