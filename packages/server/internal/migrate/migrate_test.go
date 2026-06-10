package migrate

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// TestMigrationSequence enforces migration file hygiene so a bad rebase or a
// hand-picked version number (e.g. 075 instead of 002 after a squash) is
// caught in CI instead of in production.
//
// Rules:
//  1. Every `NNN_name.up.sql` has a matching `NNN_name.down.sql`.
//  2. Versions are zero-padded 3-digit integers.
//  3. Versions form a contiguous sequence with no gaps and no duplicates.
func TestMigrationSequence(t *testing.T) {
	dir := findMigrationsDir(t)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read migrations dir %s: %v", dir, err)
	}

	pattern := regexp.MustCompile(`^(\d{3})_([a-z0-9_]+)\.(up|down)\.sql$`)
	ups := map[int]string{}
	downs := map[int]string{}
	var unknown []string

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		m := pattern.FindStringSubmatch(name)
		if m == nil {
			unknown = append(unknown, name)
			continue
		}
		version, _ := strconv.Atoi(m[1])
		slug := m[2]
		switch m[3] {
		case "up":
			if existing, ok := ups[version]; ok {
				t.Errorf("duplicate up migration for version %03d: %s and %s", version, existing, name)
			}
			ups[version] = slug
		case "down":
			if existing, ok := downs[version]; ok {
				t.Errorf("duplicate down migration for version %03d: %s and %s", version, existing, name)
			}
			downs[version] = slug
		}
	}

	if len(unknown) > 0 {
		t.Errorf("migration files must match `NNN_slug.(up|down).sql` with a 3-digit zero-padded version and a lowercase snake_case slug; offenders: %s", strings.Join(unknown, ", "))
	}

	for version, slug := range ups {
		downSlug, ok := downs[version]
		if !ok {
			t.Errorf("version %03d has up migration (%s) but no matching down migration", version, slug)
			continue
		}
		if downSlug != slug {
			t.Errorf("version %03d slug mismatch: up=%s down=%s (up and down must share the same slug)", version, slug, downSlug)
		}
	}
	for version, slug := range downs {
		if _, ok := ups[version]; !ok {
			t.Errorf("version %03d has down migration (%s) but no matching up migration", version, slug)
		}
	}

	if len(ups) == 0 {
		t.Fatal("no migrations found")
	}

	versions := make([]int, 0, len(ups))
	for v := range ups {
		versions = append(versions, v)
	}
	sort.Ints(versions)

	if versions[0] != 1 {
		t.Fatalf("migration sequence must start at 001, but the lowest version present is %03d. If 001_baseline_v1 was deleted or renamed, restore it — the baseline is the anchor of the sequence.", versions[0])
	}
	for i, v := range versions {
		want := 1 + i
		if v != want {
			t.Errorf("migration numbering has a gap or duplicate: expected %03d after %03d but found %03d. Migrations must be sequential starting from 001 with no gaps. If you're adding a new migration after a squash, use `pnpm --filter @memaxlabs/server migrate:new <slug>` to pick the correct next number.", want, want-1, v)
			return
		}
	}
}

// findMigrationsDir locates packages/server/migrations relative to the test
// binary's working directory. Tests run from the package directory
// (internal/migrate), so we walk up to the server root.
func findMigrationsDir(t *testing.T) string {
	t.Helper()
	start, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	dir := start
	for i := 0; i < 6; i++ {
		candidate := filepath.Join(dir, "migrations")
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatalf("could not locate migrations dir from %s", start)
	return ""
}
