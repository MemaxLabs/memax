package textsig

import (
	"slices"
	"testing"
)

func TestNormalizeAndHash(t *testing.T) {
	t.Parallel()
	same := [][2]string{
		{"Use pnpm workspaces only.", "use pnpm   workspaces only"},
		{"River, not Temporal.", "River, not Temporal"},
		{"Don’t import from V1.", "Don't import from V1"},
		{"Run​ tests first", "Run tests first"},
	}
	for _, c := range same {
		if ContentSHA256(c[0]) != ContentSHA256(c[1]) {
			t.Errorf("hash(%q) != hash(%q): %q vs %q", c[0], c[1], Normalize(c[0]), Normalize(c[1]))
		}
	}
	differ := [][2]string{
		{"Use pnpm 9.", "Use pnpm 10."},
		{"Run tests before commit.", "Run tests before push."},
		{"River, not Temporal.", "Temporal, not River."},
	}
	for _, c := range differ {
		if ContentSHA256(c[0]) == ContentSHA256(c[1]) {
			t.Errorf("hash(%q) == hash(%q)", c[0], c[1])
		}
	}
	if got := ContentSHA256("x"); len(got) != 64 {
		t.Errorf("hash length %d", len(got))
	}
}

func TestNearDuplicate(t *testing.T) {
	t.Parallel()
	cases := []struct {
		a, b string
		want bool
	}{
		{"Use pnpm workspaces only; never npm or yarn.", "Use pnpm workspaces only, never npm or yarn", true},
		{"API errors are RFC 9457 problem+json.", "API errors are RFC 9457 problem+json", true},
		{"We deploy the v2 API to Fly.io in iad and ams.", "Deploy the v2 API to Fly.io in iad and ams.", true},
		// Numbers, dates and qualifiers (Graphiti's rule).
		{"Use pnpm 9 for every package in the monorepo, with workspaces.", "Use pnpm 10 for every package in the monorepo, with workspaces.", false},
		{"Run the full test suite before every commit to the main branch of the repository.", "Run the full test suite before every push to the main branch of the repository.", false},
		{"Freeze V1 code after Oct 6, 2026; bug fixes only.", "Freeze V1 code after Oct 9, 2026; bug fixes only.", false},
		{"River, not Temporal, runs background jobs.", "Temporal, not River, runs background jobs.", false},
		{"Keep secrets out of receipts.", "Never keep secrets out of receipts.", false},
		{"Deploy to Railway.", "Deploy to Fly.io.", false},
	}
	for _, c := range cases {
		if got := NearDuplicate(c.a, c.b); got != c.want {
			t.Errorf("NearDuplicate(%q, %q) = %v (jaccard %.2f, salient %v), want %v",
				c.a, c.b, got, Jaccard(c.a, c.b), SameSalient(c.a, c.b), c.want)
		}
	}
}

func TestBandsFindNearDuplicates(t *testing.T) {
	t.Parallel()
	a := "Use pnpm workspaces only; never npm or yarn in this repository."
	b := "Use pnpm workspaces only, never npm or yarn in this repository"
	c := "Background jobs run on River with Postgres, not on Temporal."
	ba, bb, bc := BandsOf(a), BandsOf(b), BandsOf(c)
	if len(ba) != Bands {
		t.Fatalf("%d bands", len(ba))
	}
	if !shares(ba, bb) {
		t.Errorf("near-verbatim statements share no band (jaccard %.2f)", Jaccard(a, b))
	}
	if shares(ba, bc) {
		t.Errorf("unrelated statements share a band")
	}
	// Deterministic: the stored keys must be reproducible forever.
	if !slices.Equal(BandsOf(a), ba) {
		t.Error("bands are not deterministic")
	}
	if MinHash("")[0] != ^uint64(0) {
		t.Error("an empty statement has shingles")
	}
}

func shares(a, b []int64) bool {
	for _, x := range a {
		if slices.Contains(b, x) {
			return true
		}
	}
	return false
}

func TestWordsAndNumbers(t *testing.T) {
	t.Parallel()
	got := Words("Use @base-ui/react 1.8 and v4.1; don't use node 18.")
	want := []string{"use", "@base-ui/react", "1.8", "and", "v4.1", "don't", "use", "node", "18"}
	if !slices.Equal(got, want) {
		t.Errorf("Words = %q, want %q", got, want)
	}
	if n := Numbers("Postgres 18 with pgvector 0.8.6 on port 5432"); !slices.Equal(n, []string{"18", "0.8.6", "5432"}) {
		t.Errorf("Numbers = %q", n)
	}
}

func TestOverlapAndMentions(t *testing.T) {
	t.Parallel()
	shared, coeff := Overlap("Deploy the v2 API to Railway for preview environments.", "The v2 API deploys to Fly.io.")
	if shared < 3 || coeff < 0.4 {
		t.Errorf("Overlap = %d, %.2f; the two deploy statements should overlap", shared, coeff)
	}
	if shared, _ := Overlap("Use pnpm for installs.", "River, not Temporal, for jobs."); shared > 1 {
		t.Errorf("unrelated statements share %d words", shared)
	}
	if !Mentions("Our deploy target is Fly.io now.", "deploy-target") {
		t.Error("area not mentioned")
	}
	if Mentions("Deploy on Fly.io.", "deploy target") {
		t.Error("half an area counts as mentioned")
	}
	if NormalizeKey(" Deploy-Target ") != "deploy target" || NormalizeKey("deploy_target") != "deploy target" {
		t.Errorf("NormalizeKey = %q", NormalizeKey(" Deploy-Target "))
	}
	if Stem("deploys") != "deploy" || Stem("deployed") != "deploy" || Stem("sources") != "source" || Stem("v4.1") != "v4.1" {
		t.Errorf("Stem: %q %q %q", Stem("deploys"), Stem("deployed"), Stem("sources"))
	}
}
