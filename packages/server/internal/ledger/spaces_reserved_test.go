package ledger

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// A space's URL lives at the root of the web app, so every word the web
// app reserves (packages/web/src/lib/ui-gate.ts RESERVED_SPACE_SLUGS: its
// top-level routes, the public site included) must be refused here too,
// or a space could take /welcome or /unsubscribe from the page.
func TestReservedSlugsCoverTheWebRoutes(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile(filepath.Join("..", "..", "..", "web", "src", "lib", "ui-gate.ts"))
	if err != nil {
		t.Fatal(err)
	}
	block := regexp.MustCompile(`(?s)RESERVED_SPACE_SLUGS[^=]*= new Set\(\[(.*?)\]\)`).FindSubmatch(src)
	if block == nil {
		t.Fatal("RESERVED_SPACE_SLUGS isn't in ui-gate.ts any more; update this test")
	}
	var words, missing []string
	for _, m := range regexp.MustCompile(`"([a-z0-9-]+)"`).FindAllSubmatch(block[1], -1) {
		words = append(words, string(m[1]))
	}
	if len(words) < 40 {
		t.Fatalf("read %d words from RESERVED_SPACE_SLUGS, want the whole list", len(words))
	}
	for _, w := range words {
		// Shorter words can't be space slugs at all (spaceSlug).
		if spaceSlug.MatchString(w) && !reservedSlugs[w] {
			missing = append(missing, w)
		}
	}
	if len(missing) > 0 {
		t.Errorf("add to reservedSlugs (spaces.go): %s", strings.Join(missing, " "))
	}
}
