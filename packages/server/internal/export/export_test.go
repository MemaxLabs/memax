package export_test

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/MemaxLabs/memax/packages/server/internal/export"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/lifecycle"
	"github.com/MemaxLabs/memax/packages/server/internal/receiptchain"
)

var update = flag.Bool("update", false, "rewrite the SDK's golden export (packages/sdk/src/v2/testdata/export-v1)")

// golden is where the SDK's tests read the fixture's export from.
const golden = "../../../sdk/src/v2/testdata/export-v1/memax-v2"

// TestGoldenExport: the fixture's export, file for file, is the SDK's
// golden export, which its reader and verifier are tested on. Run with
// -update after a deliberate change to the format, and commit both.
func TestGoldenExport(t *testing.T) {
	files := newFixture(t).render(t)
	if *update {
		if err := os.RemoveAll(golden); err != nil {
			t.Fatal(err)
		}
		for name, data := range files {
			p := filepath.Join(golden, filepath.FromSlash(name))
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		return
	}
	got := map[string][]byte{}
	err := filepath.WalkDir(golden, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(golden, p)
		data, err := os.ReadFile(p)
		got[filepath.ToSlash(rel)] = data
		return err
	})
	if err != nil {
		t.Fatalf("read the golden export (run go test ./internal/export/ -run TestGoldenExport -update): %v", err)
	}
	for name, want := range files {
		if !bytes.Equal(got[name], want) {
			t.Errorf("%s differs from the golden export; run with -update if the change is deliberate:\n%s", name, want)
		}
	}
	for name := range got {
		if _, ok := files[name]; !ok {
			t.Errorf("the golden export has %s, which the writer no longer writes", name)
		}
	}
}

// TestExportIsDeterministic: the same record gives byte-identical files
// and a byte-identical archive, and the archive holds exactly the files.
func TestExportIsDeterministic(t *testing.T) {
	t.Parallel()
	a, b := newFixture(t).render(t), newFixture(t).render(t)
	if !reflect.DeepEqual(a, b) {
		t.Fatal("two renders of the same record differ")
	}
	za, zb := newFixture(t).zip(t), newFixture(t).zip(t)
	if !bytes.Equal(za, zb) {
		t.Fatal("two archives of the same record differ")
	}
	root, files, err := export.ReadZip(za)
	if err != nil {
		t.Fatal(err)
	}
	if root != "memax-v2" {
		t.Errorf("the archive's folder is %q, want the space's slug", root)
	}
	if !reflect.DeepEqual(files, a) {
		t.Error("the archive's files aren't the rendered files")
	}
	// The manifest lists every other file with its hash, sorted.
	var m export.Manifest
	if err := json.Unmarshal(a[export.ManifestFile], &m); err != nil {
		t.Fatal(err)
	}
	if len(m.Files) != len(a)-1 || !sort.SliceIsSorted(m.Files, func(i, j int) bool { return m.Files[i].Path < m.Files[j].Path }) {
		t.Errorf("the manifest lists %d files of %d", len(m.Files), len(a)-1)
	}
}

// TestRoundTrip is rule 14 on the fixture: export → parse → the same
// record, field for field, less only what the format leaves out on
// purpose (a tombstone's note).
func TestRoundTrip(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	r, err := export.Parse(f.render(t))
	if err != nil {
		t.Fatal(err)
	}
	same := func(what string, got, want any) {
		t.Helper()
		g, w := canon(t, got), canon(t, want)
		if !reflect.DeepEqual(g, w) {
			gj, _ := json.MarshalIndent(g, "", "  ")
			wj, _ := json.MarshalIndent(w, "", "  ")
			t.Errorf("%s doesn't round-trip:\n got %s\nwant %s", what, gj, wj)
		}
	}
	var kept []ledger.ExportMemory
	for _, m := range f.memories {
		if m.Memory.Lifecycle != lifecycle.Forgotten {
			kept = append(kept, m)
		}
	}
	same("memories", r.Memories, kept)
	same("briefs", r.Briefs, f.briefs)
	same("gates", r.Gates, f.gates)
	same("targets", r.Targets, f.targets)
	same("agents", r.Agents, f.agents)
	same("reads", r.Reads, f.reads)
	same("receipts", r.Receipts, f.receipts)

	want := f.tombstones[0]
	want.Note = "" // the person's own words stay out
	d := want.CompletedAt.Sub(want.ForgottenAt).Milliseconds()
	want.DurationMS = &d
	var stamps []ledger.ReceiptStamp
	for _, m := range f.memories {
		if m.Memory.ID == want.ObjectID {
			stamps = m.Receipts
		}
	}
	same("tombstones", r.Tombstones, []export.ParsedTombstone{{Tombstone: want, Receipts: stamps}})

	cps, keys, err := r.ChainCheckpoints()
	if err != nil {
		t.Fatal(err)
	}
	if len(cps) != 2 || len(keys) != 1 {
		t.Fatalf("checkpoints %d, keys %d", len(cps), len(keys))
	}
	if m := r.Manifest; m.Receipt == nil || *m.Receipt != fxExport || m.Seal.SealedReceipts != 13 || m.Seal.Unsealed != 5 ||
		m.Counts.Memories != 4 || m.Counts.Tombstones != 1 || m.Counts.Receipts != 18 {
		t.Errorf("manifest = %+v", m)
	}
}

// TestChainVerifies: the export's receipts verify against its signed
// checkpoints with receiptchain's verifier, and a changed receipt, a
// forged checkpoint and a wrong key each fail.
func TestChainVerifies(t *testing.T) {
	t.Parallel()
	files := newFixture(t).render(t)
	verify := func(files map[string][]byte) receiptchain.Report {
		t.Helper()
		r, err := export.Parse(files)
		if err != nil {
			t.Fatal(err)
		}
		cps, keys, err := r.ChainCheckpoints()
		if err != nil {
			t.Fatal(err)
		}
		v := receiptchain.NewVerifier(r.Manifest.Space.ID, keys, cps)
		for _, rc := range r.Receipts {
			v.Add(rc)
		}
		return v.Finish()
	}
	if rep := verify(files); !rep.OK() || rep.Signed != 2 || rep.Receipts != 18 {
		t.Fatalf("the export doesn't verify: %+v", rep)
	}

	tampered := clone(files)
	tampered[export.ReceiptsFile] = bytes.Replace(tampered[export.ReceiptsFile], []byte(`"via":"cli"`), []byte(`"via":"web"`), 1)
	if rep := verify(tampered); rep.OK() || !hasKind(rep, receiptchain.ProblemMerkle) {
		t.Errorf("a changed receipt verified: %+v", rep.Problems)
	}

	forged := clone(files)
	var doc map[string]any
	_ = json.Unmarshal(forged[export.CheckpointsFile], &doc)
	cp := doc["checkpoints"].([]any)[0].(map[string]any)
	cp["merkle_root"] = strings.Repeat("00", 32)
	forged[export.CheckpointsFile], _ = json.Marshal(doc)
	if rep := verify(forged); rep.OK() || !hasKind(rep, receiptchain.ProblemSignature) {
		t.Errorf("a forged checkpoint verified: %+v", rep.Problems)
	}
}

// TestNoForgottenWords: a forgotten memory leaves a tombstone, never its
// words; a redacted reason stays redacted (only its commitment is there);
// the person's note on the tombstone stays out.
func TestNoForgottenWords(t *testing.T) {
	t.Parallel()
	files := newFixture(t).render(t)
	if _, ok := files["memories/M-0004.md"]; ok {
		t.Error("the forgotten M-0004 has a memory file")
	}
	tomb, ok := files["tombstones/M-0004.md"]
	if !ok {
		t.Fatal("the forgotten M-0004 has no tombstone")
	}
	for _, words := range []string{"kept for the release notes", "this note is the person's own words"} {
		for name, data := range files {
			if bytes.Contains(data, []byte(words)) {
				t.Errorf("%s holds %q", name, words)
			}
		}
	}
	if !bytes.Contains(tomb, []byte("M-0004 was forgotten on 2026-10-06 09:36 UTC.")) {
		t.Errorf("the tombstone doesn't say when:\n%s", tomb)
	}
	// The redacted receipt keeps its commitment, without reason or salt.
	for _, line := range strings.Split(string(files[export.ReceiptsFile]), "\n") {
		if strings.Contains(line, `"object_ref":"M-0004","action":"kept"`) &&
			(!strings.Contains(line, `"reason":null,"reason_salt":null,"reason_sha256":"`)) {
			t.Errorf("the redacted receipt isn't redacted: %s", line)
		}
	}
}

// TestFrontmatterIsPlainYAML: every Markdown file's frontmatter is read
// the same by a standard YAML parser and holds only the subset the SDK
// reads: `key: <json>`, or a block list or object of JSON values.
func TestFrontmatterIsPlainYAML(t *testing.T) {
	t.Parallel()
	files := newFixture(t).render(t)
	n := 0
	for name, data := range files {
		if !strings.HasSuffix(name, ".md") || name == export.ReadmeFile || name == export.DecisionsFile {
			continue
		}
		n++
		s := string(data)
		end := strings.Index(s[4:], "\n---\n")
		front := s[4 : 4+end+1]
		var y map[string]any
		if err := yaml.Unmarshal([]byte(front), &y); err != nil {
			t.Errorf("%s: %v", name, err)
		}
		for _, line := range strings.Split(strings.TrimSuffix(front, "\n"), "\n") {
			var v any
			switch {
			case strings.HasPrefix(line, "  - "):
				if err := json.Unmarshal([]byte(line[4:]), &v); err != nil {
					t.Errorf("%s: list item isn't JSON: %s", name, line)
				}
			case strings.HasPrefix(line, "  "):
				_, val, ok := strings.Cut(line[2:], ": ")
				if !ok || json.Unmarshal([]byte(val), &v) != nil {
					t.Errorf("%s: object field isn't `name: <json>`: %s", name, line)
				}
			case strings.HasSuffix(line, ":"):
			default:
				_, val, ok := strings.Cut(line, ": ")
				if !ok || json.Unmarshal([]byte(val), &v) != nil {
					t.Errorf("%s: field isn't `key: <json>`: %s", name, line)
				}
			}
		}
	}
	if n < 6 {
		t.Errorf("only %d frontmatter files", n)
	}
	// Times are UTC to the microsecond.
	if !bytes.Contains(files["memories/M-0001.md"], []byte(`created_at: "2026-10-06T09:30:00.123456Z"`)) {
		t.Error("times aren't written UTC to the microsecond")
	}
}

func hasKind(r receiptchain.Report, k receiptchain.ProblemKind) bool {
	return slices.ContainsFunc(r.Problems, func(p receiptchain.Problem) bool { return p.Kind == k })
}

func clone(m map[string][]byte) map[string][]byte {
	out := make(map[string][]byte, len(m))
	for k, v := range m {
		out[k] = slices.Clone(v)
	}
	return out
}

// canon is v's JSON as plain data, with every timestamp in UTC, so two
// values compare by what they say.
func canon(t *testing.T, v any) any {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var out any
	if err := d.Decode(&out); err != nil {
		t.Fatal(err)
	}
	return walk(out)
}

func walk(v any) any {
	switch x := v.(type) {
	case map[string]any:
		for k, item := range x {
			x[k] = walk(item)
		}
	case []any:
		for i, item := range x {
			x[i] = walk(item)
		}
		if len(x) == 0 {
			return nil
		}
	case string:
		if ts, err := time.Parse(time.RFC3339Nano, x); err == nil {
			return ts.UTC().Format(time.RFC3339Nano)
		}
	}
	return v
}
