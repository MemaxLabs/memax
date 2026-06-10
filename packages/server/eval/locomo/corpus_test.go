package locomo

import "testing"

func TestBuildCorpusModesAndCorrectDocs(t *testing.T) {
	sample := sampleFixture()

	dialogDocs := BuildCorpus(&sample, DialogMode)
	if len(dialogDocs) != 3 {
		t.Fatalf("dialog docs = %d, want 3", len(dialogDocs))
	}
	if dialogDocs[0].ID != "D1:1" || dialogDocs[0].SourceIDs[0] != "D1:1" {
		t.Fatalf("unexpected dialog doc: %#v", dialogDocs[0])
	}

	observationDocs := BuildCorpus(&sample, ObservationMode)
	if len(observationDocs) != 1 {
		t.Fatalf("observation docs = %d, want 1", len(observationDocs))
	}
	if !CorrectDocs(observationDocs, []string{"D1:1"})[observationDocs[0].ID] {
		t.Fatalf("observation doc should match D1:1 evidence")
	}

	summaryDocs := BuildCorpus(&sample, SummaryMode)
	if len(summaryDocs) != 2 {
		t.Fatalf("summary docs = %d, want 2", len(summaryDocs))
	}
	if !CorrectDocs(summaryDocs, []string{"D2:1"})["S2"] {
		t.Fatalf("session summary should match session evidence")
	}
}

func TestEvidenceRecallAtK(t *testing.T) {
	ranked := []CorpusDoc{
		{ID: "a", SourceIDs: []string{"D1:1"}},
		{ID: "b", SourceIDs: []string{"D2:1", "D2:2"}},
	}
	evidence := []string{"D1:1", "D2:2"}
	if got := EvidenceRecallAtK(ranked, evidence, 1); got != 0.5 {
		t.Fatalf("recall@1 = %.2f, want 0.5", got)
	}
	if got := EvidenceRecallAtK(ranked, evidence, 2); got != 1 {
		t.Fatalf("recall@2 = %.2f, want 1.0", got)
	}
}

func sampleFixture() Sample {
	return Sample{
		SampleID: "sample_1",
		Conversation: Conversation{
			SpeakerA: "Caroline",
			SpeakerB: "Melanie",
			Sessions: []Session{
				{
					Number:   1,
					DateTime: "1:56 pm on 8 May, 2023",
					Dialogs: []DialogTurn{
						{Speaker: "Caroline", DialogID: "D1:1", Text: "I joined a support group."},
						{Speaker: "Melanie", DialogID: "D1:2", Text: "That is great."},
					},
				},
				{
					Number:   2,
					DateTime: "2:00 pm on 9 May, 2023",
					Dialogs: []DialogTurn{
						{Speaker: "Melanie", DialogID: "D2:1", Text: "I went camping."},
					},
				},
			},
		},
		Observation: ObservationSet{
			1: []ObservationDoc{{Speaker: "Caroline", Text: "Caroline joined a support group.", DialogIDs: []string{"D1:1"}}},
		},
		SessionSummary: SessionSummarySet{
			1: "Caroline discussed a support group.",
			2: "Melanie discussed camping.",
		},
	}
}
