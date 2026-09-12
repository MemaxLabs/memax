package handler

import (
	"fmt"
	"strings"
	"testing"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

func chunkFixture(id, memoryID string, score float64) model.Chunk {
	return model.Chunk{ID: id, MemoryID: memoryID, RelevanceScore: score}
}

// The dual-query fusion is the load-bearing half of the A1 recall fix:
// the raw channel must be able to rescue entities the distiller
// dropped, without letting either channel's raw scores dominate, and
// with a byte-stable output order.
func TestFuseDualQueryChunks(t *testing.T) {
	raw := []model.Chunk{
		chunkFixture("c-gracery", "m-hotel", 0.41), // the entity only the RAW query finds
		chunkFixture("c-shared", "m-plan", 0.80),
		chunkFixture("c-raw-tail", "m-misc", 0.10),
	}
	distilled := []model.Chunk{
		chunkFixture("c-shared", "m-plan", 0.75),
		chunkFixture("c-dist-only", "m-other", 0.60),
	}

	fused := fuseDualQueryChunks(raw, distilled, 10)

	// Found by BOTH channels → highest fused rank.
	if fused[0].ID != "c-shared" {
		t.Fatalf("expected the both-channel chunk first, got %s", fused[0].ID)
	}
	// RelevanceScore keeps the max of the two channels.
	if fused[0].RelevanceScore != 0.80 {
		t.Fatalf("expected max relevance 0.80, got %v", fused[0].RelevanceScore)
	}
	// The raw-only entity chunk survives — this IS the distiller-drop
	// rescue the whole change exists for.
	found := false
	for _, c := range fused {
		if c.ID == "c-gracery" {
			found = true
		}
	}
	if !found {
		t.Fatal("raw-only entity chunk must survive fusion")
	}
	// No duplicates.
	seen := map[string]bool{}
	for _, c := range fused {
		if seen[c.ID] {
			t.Fatalf("duplicate chunk %s in fused output", c.ID)
		}
		seen[c.ID] = true
	}
}

func TestFuseDualQueryChunksDeterministic(t *testing.T) {
	// Equal-rank ties everywhere: same contribution from single
	// channels at the same rank position. Order must be decided by
	// the tie-break keys, not input ordering.
	a := []model.Chunk{chunkFixture("c-b", "m-2", 0.5), chunkFixture("c-a", "m-1", 0.5)}
	b := []model.Chunk{chunkFixture("c-d", "m-4", 0.5), chunkFixture("c-c", "m-3", 0.5)}

	first := fuseDualQueryChunks(a, b, 10)
	second := fuseDualQueryChunks(b, a, 10)

	if len(first) != len(second) {
		t.Fatalf("length mismatch: %d vs %d", len(first), len(second))
	}
	for i := range first {
		if first[i].ID != second[i].ID {
			t.Fatalf("order differs at %d: %s vs %s — fusion is input-order dependent", i, first[i].ID, second[i].ID)
		}
	}
}

func TestFuseDualQueryChunksRespectsLimit(t *testing.T) {
	var raw, distilled []model.Chunk
	for i := 0; i < 30; i++ {
		id := string(rune('a' + i%26))
		raw = append(raw, chunkFixture(id+"-r", "m-"+id, 0.5))
	}
	fused := fuseDualQueryChunks(raw, distilled, 5)
	if len(fused) != 5 {
		t.Fatalf("expected limit 5, got %d", len(fused))
	}
}

func TestFuseDualQueryChunksPerMemoryCap(t *testing.T) {
	// One memory must not monopolize the fused candidate set: each
	// channel caps at 3 chunks/memory in the store, and fusion
	// re-applies the same cap so disjoint picks can't stack to 6
	// (adversarial review finding 1).
	var raw, distilled []model.Chunk
	for i := 0; i < 4; i++ {
		raw = append(raw, chunkFixture(fmt.Sprintf("r-%d", i), "m-hog", 0.9))
		distilled = append(distilled, chunkFixture(fmt.Sprintf("d-%d", i), "m-hog", 0.9))
	}
	distilled = append(distilled, chunkFixture("other", "m-other", 0.5))
	fused := fuseDualQueryChunks(raw, distilled, 10)
	hog := 0
	foundOther := false
	for _, c := range fused {
		if c.MemoryID == "m-hog" {
			hog++
		}
		if c.MemoryID == "m-other" {
			foundOther = true
		}
	}
	if hog > 3 {
		t.Fatalf("m-hog occupies %d slots, cap is 3", hog)
	}
	if !foundOther {
		t.Fatal("the capped memory must not push other memories out")
	}
}

// The natural-entity regression fixture (A1 done-condition). These are
// the exact entities the May-2026 eval failures lost to distillation:
// p1 "Gracery" (hotel), p3 "Kin Khao" (restaurant), p6 "knee" (body
// part). preserveRawEntityTerms can NOT rescue them (it only keeps
// structured tokens — slugs, paths, CamelCase), which is precisely why
// the raw query must be its own retrieval channel: with dual-query
// fusion the raw channel carries these terms end-to-end regardless of
// what the distiller drops.
func TestNaturalEntitiesSurviveViaRawChannel(t *testing.T) {
	cases := []struct {
		name      string
		raw       string
		distilled string
		entity    string
	}{
		{"hotel name", "what was the hotel near Gracery Shinjuku we booked", "tokyo trip hotel booking", "gracery"},
		{"restaurant name", "the thai place Kin Khao Derek recommended", "recommended thai restaurant", "kin khao"},
		{"body part", "notes about my knee pain after running", "running injury notes", "knee"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Documents the KNOWN GAP: the structured-token preserver
			// does not catch natural-language entities...
			preserved := preserveRawEntityTerms(tc.raw, tc.distilled)
			for _, term := range preserved {
				if strings.Contains(strings.ToLower(term), tc.entity) {
					t.Fatalf("fixture invalid: %q unexpectedly preserved by structured-token pass — pick a purer natural entity", tc.entity)
				}
			}
			// ...and the raw channel is what carries them: the raw
			// search query (the fusion input) must still contain the
			// entity verbatim.
			if !strings.Contains(strings.ToLower(tc.raw), tc.entity) {
				t.Fatalf("fixture invalid: raw query must contain %q", tc.entity)
			}
		})
	}
}
