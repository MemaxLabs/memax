package store

import "testing"

func TestIsLeaderForClient(t *testing.T) {
	// River v0.32 writes leader_id as `<machine_id>_<timestamp>`,
	// but our heartbeat uses the bare `<machine_id>` as client_id.
	// The matcher must handle both exact equality and the underscore-
	// anchored prefix case. The anchor prevents two machines whose
	// IDs happen to share a prefix from being conflated.
	cases := []struct {
		name     string
		clientID string
		leaderID string
		want     bool
	}{
		{
			name:     "exact match",
			clientID: "6e826330b35568",
			leaderID: "6e826330b35568",
			want:     true,
		},
		{
			name:     "river prefix+timestamp format",
			clientID: "6e826330b35568",
			leaderID: "6e826330b35568_2026_04_23T05_03_01_837415",
			want:     true,
		},
		{
			name:     "different machine",
			clientID: "d893909c6ee718",
			leaderID: "6e826330b35568_2026_04_23T05_03_01_837415",
			want:     false,
		},
		{
			name:     "prefix-only collision must not match without underscore anchor",
			clientID: "abc",
			leaderID: "abcdef_2026_04_23T05_03_01_837415",
			want:     false,
		},
		{
			name:     "empty client",
			clientID: "",
			leaderID: "6e826330b35568_2026_04_23T05_03_01_837415",
			want:     false,
		},
		{
			name:     "empty leader",
			clientID: "6e826330b35568",
			leaderID: "",
			want:     false,
		},
		{
			name:     "leader exactly equals client + underscore (degenerate)",
			clientID: "machine",
			leaderID: "machine_",
			want:     true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := isLeaderForClient(tc.clientID, tc.leaderID)
			if got != tc.want {
				t.Errorf("isLeaderForClient(%q, %q) = %v, want %v",
					tc.clientID, tc.leaderID, got, tc.want)
			}
		})
	}
}
