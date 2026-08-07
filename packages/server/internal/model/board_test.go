package model

import "testing"

// The team kinds' whole claim is about people, so the owner rule is
// part of their definition, not a nicety: distinct owners for the two
// kinds that say "these two members differ", one owner for the kind
// that says "this member is who you ask".
func TestSatisfiesOwnerRule(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		rule   BoardOwnerRule
		owners []string
		want   bool
	}{
		{"any rule ignores composition", BoardOwnersAny, []string{"u1", "u1"}, true},
		{"any rule tolerates empty", BoardOwnersAny, nil, true},
		{"distinct accepts two members", BoardOwnersDistinct, []string{"u1", "u2"}, true},
		{"distinct accepts three members", BoardOwnersDistinct, []string{"u1", "u2", "u3"}, true},
		{"distinct rejects one member twice", BoardOwnersDistinct, []string{"u1", "u1"}, false},
		{"distinct rejects a single quote", BoardOwnersDistinct, []string{"u1"}, false},
		{"distinct rejects empty", BoardOwnersDistinct, nil, false},
		{"same accepts one member's memories", BoardOwnersSame, []string{"u1", "u1", "u1"}, true},
		{"same rejects a mixed set", BoardOwnersSame, []string{"u1", "u2"}, false},
		{"same rejects empty", BoardOwnersSame, nil, false},
		{"unattributable owner never satisfies distinct", BoardOwnersDistinct, []string{"u1", ""}, false},
		{"unattributable owner never satisfies same", BoardOwnersSame, []string{"", ""}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := SatisfiesOwnerRule(tc.rule, tc.owners); got != tc.want {
				t.Fatalf("SatisfiesOwnerRule(%v, %v) = %v, want %v",
					tc.rule, tc.owners, got, tc.want)
			}
		})
	}
}

func TestLaneBOwnerRuleByKind(t *testing.T) {
	t.Parallel()
	tests := []struct {
		kind string
		want BoardOwnerRule
	}{
		{BoardKindConsensusGap, BoardOwnersDistinct},
		{BoardKindTeamEcho, BoardOwnersDistinct},
		{BoardKindWhoKnows, BoardOwnersSame},
		// Personal kinds carry no owner constraint — on a shared hub an
		// echo is still allowed to be one person's.
		{BoardKindEcho, BoardOwnersAny},
		{BoardKindThread, BoardOwnersAny},
		{BoardKindPattern, BoardOwnersAny},
		{BoardKindNextUp, BoardOwnersAny},
		{"unknown", BoardOwnersAny},
	}
	for _, tc := range tests {
		t.Run(tc.kind, func(t *testing.T) {
			if got := LaneBOwnerRule(tc.kind); got != tc.want {
				t.Fatalf("LaneBOwnerRule(%q) = %v, want %v", tc.kind, got, tc.want)
			}
		})
	}
}

func TestTeamKindsHaveCitationFloors(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{BoardKindConsensusGap, BoardKindTeamEcho, BoardKindWhoKnows} {
		floor, ok := LaneBCitationFloor(kind)
		if !ok {
			t.Fatalf("%s must be a known Lane B kind", kind)
		}
		if floor < 2 {
			t.Fatalf("%s floor = %d, want at least 2 (a team claim needs two receipts)", kind, floor)
		}
		if !IsTeamWowKind(kind) {
			t.Fatalf("%s must be recognised as a team kind", kind)
		}
	}
}

func TestWowKindsForHub(t *testing.T) {
	t.Parallel()
	if got := WowKindsForHub(HubTypeTeam); len(got) != len(TeamWowKinds) {
		t.Fatalf("team pool = %v, want the six team lenses", got)
	}
	for _, hubType := range []string{"personal", "", "something-new"} {
		got := WowKindsForHub(hubType)
		if len(got) != len(WowKinds) {
			t.Fatalf("WowKindsForHub(%q) = %v, want the personal three", hubType, got)
		}
		for _, kind := range got {
			if IsTeamWowKind(kind) {
				t.Fatalf("team kind %q leaked into the %q pool", kind, hubType)
			}
		}
	}
}
