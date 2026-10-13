package trust_test

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/MemaxLabs/memax/packages/server/internal/trust"
)

var update = flag.Bool("update", false, "rewrite the web app's cloud posture (packages/web/src/lib/v2/cloud-posture.json)")

// cloudPosture is the file the public Security page (memax.app/security)
// reads: Memax cloud's posture as the server's defaults set it, with every
// processor's key set. The page is static and anyone's to read, so it
// can't ask GET /v2/security (people only); this golden keeps it saying
// what the server's own defaults say. Settings › Security reads the
// running server's configuration.
var cloudPosture = filepath.Join("..", "..", "..", "web", "src", "lib", "v2", "cloud-posture.json")

// The environment Memax cloud runs the defaults with: model calls through
// OpenRouter, V2 embeddings on Voyage, and the morning email on Resend.
// The tiers, hosts, floors, residency and backup window are the code's
// defaults (anthropic.DefaultProviders, DefaultResidency and the rest).
var cloudEnv = map[string]string{
	"ANTHROPIC_API_KEY":  "set",
	"ANTHROPIC_BASE_URL": "https://openrouter.ai/api",
	"VOYAGE_API_KEY":     "set",
	"RESEND_API_KEY":     "set",
}

// The public Security page says what the server's defaults say. A change
// to a tier, a host list, a floor, the residency or the backup window
// fails here until the page's file is rewritten with -update and reviewed
// with the page (and the docs site's Security page, which web's
// public-posture test holds to the same names).
func TestCloudPostureMatchesThePublicPage(t *testing.T) {
	t.Parallel()
	got, err := json.MarshalIndent(trust.FromEnv(env(cloudEnv)), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	if *update {
		if err := os.WriteFile(cloudPosture, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(cloudPosture)
	if err != nil {
		t.Fatalf("read the public page's posture (run go test ./internal/trust/ -run TestCloudPosture -update): %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("the server's defaults changed what the public Security page must say; run with -update if the change is deliberate, then check the page.\ngot:\n%s", got)
	}
}
