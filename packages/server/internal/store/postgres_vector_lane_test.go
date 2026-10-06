package store

import "testing"

func TestHnswEfSearchFor(t *testing.T) {
	tests := []struct {
		limit int
		want  int
	}{
		{limit: 0, want: hnswDefaultEfSearch},
		{limit: 10, want: hnswDefaultEfSearch},
		{limit: 40, want: 40},
		{limit: 60, want: 60},
		{limit: 120, want: 120},
		{limit: 5000, want: hnswMaxEfSearch},
	}
	for _, tt := range tests {
		if got := hnswEfSearchFor(tt.limit); got != tt.want {
			t.Errorf("hnswEfSearchFor(%d) = %d, want %d", tt.limit, got, tt.want)
		}
	}
}

func TestVectorLaneSettings(t *testing.T) {
	got := vectorLaneSettings(90, true)
	want := []string{
		"SET LOCAL hnsw.ef_search = 90",
		"SET LOCAL hnsw.iterative_scan = strict_order",
	}
	if len(got) != len(want) {
		t.Fatalf("vectorLaneSettings(90, true) = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("statement %d = %q, want %q", i, got[i], want[i])
		}
	}
	if got := vectorLaneSettings(90, false); len(got) != 1 {
		t.Errorf("without iterative scan, want only the ef_search statement, got %q", got)
	}
}

func TestPgvectorSupportsIterativeScan(t *testing.T) {
	tests := []struct {
		version string
		want    bool
	}{
		{"0.8.0", true},
		{"0.8.7", true},
		{"0.9.1", true},
		{"1.0.0", true},
		{"0.7.4", false},
		{"0.5", false},
		{"", false},
		{"garbage", false},
	}
	for _, tt := range tests {
		if got := pgvectorSupportsIterativeScan(tt.version); got != tt.want {
			t.Errorf("pgvectorSupportsIterativeScan(%q) = %v, want %v", tt.version, got, tt.want)
		}
	}
}
