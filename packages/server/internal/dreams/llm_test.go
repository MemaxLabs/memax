package dreams

import "testing"

func TestHubDistinctID(t *testing.T) {
	tests := []struct {
		name  string
		hubID string
		want  string
	}{
		{name: "prefixes raw hub id", hubID: "11111111-1111-1111-1111-111111111111", want: "hub:11111111-1111-1111-1111-111111111111"},
		{name: "does not double prefix", hubID: "hub:11111111-1111-1111-1111-111111111111", want: "hub:11111111-1111-1111-1111-111111111111"},
		{name: "trims whitespace", hubID: " 11111111-1111-1111-1111-111111111111 ", want: "hub:11111111-1111-1111-1111-111111111111"},
		{name: "empty stays empty", hubID: " ", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hubDistinctID(tt.hubID); got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got)
			}
		})
	}
}
