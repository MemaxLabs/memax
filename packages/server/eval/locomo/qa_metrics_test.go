package locomo

import "testing"

func TestEvaluateAnswer(t *testing.T) {
	got := EvaluateAnswer("support group", "the support group", 4)
	if got.F1 != 1 {
		t.Fatalf("F1 = %.2f, want 1.0", got.F1)
	}
	if got.BLEU1 <= 0 {
		t.Fatalf("BLEU1 = %.2f, want positive", got.BLEU1)
	}
}

func TestEvaluateAnswerMultiAnswer(t *testing.T) {
	got := EvaluateAnswer("pottery, camping", "camping, pottery, painting", 1)
	if got.F1 <= 0.6 || got.F1 >= 1.0 {
		t.Fatalf("multi-answer F1 = %.2f, want partial credit", got.F1)
	}
}
