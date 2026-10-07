package secrets

import (
	"encoding/json"
	"os"
	"testing"
)

// TestSharedCredentialCorpus holds the refusal patterns to the corpus
// memax init's local scan is tested against too
// (packages/cli/test/init/secrets.test.ts), so nothing the server refuses
// is ever sent to it.
func TestSharedCredentialCorpus(t *testing.T) {
	raw, err := os.ReadFile("testdata/credentials.json")
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		Cases []struct {
			Text   string `json:"text"`
			Secret bool   `json:"secret"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &corpus); err != nil {
		t.Fatal(err)
	}
	if len(corpus.Cases) < 10 {
		t.Fatalf("%d cases", len(corpus.Cases))
	}
	for _, c := range corpus.Cases {
		if got := len(DetectCredentials(c.Text)) > 0; got != c.Secret {
			t.Errorf("DetectCredentials(%q) found %v, want %v", c.Text, got, c.Secret)
		}
	}
}
