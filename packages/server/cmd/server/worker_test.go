package main

import "testing"

func TestEmbeddedWorkerEnabled(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  bool
	}{
		{"", false},
		{"0", false},
		{"false", false},
		{"no", false},
		{"1", true},
		{"true", true},
		{"TRUE", true},
	} {
		t.Setenv("MEMAX_EMBEDDED_WORKER", tc.value)
		if got := embeddedWorkerEnabled(); got != tc.want {
			t.Errorf("MEMAX_EMBEDDED_WORKER=%q: got %v, want %v", tc.value, got, tc.want)
		}
	}
}
