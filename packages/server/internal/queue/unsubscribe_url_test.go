package queue

import "testing"

func TestBuildUnsubscribeURL(t *testing.T) {
	cases := []struct {
		name   string
		base   string
		token  string
		want   string
		expErr bool
	}{
		{
			name:  "bare origin",
			base:  "https://memax.app",
			token: "abc",
			want:  "https://memax.app/v1/unsubscribe?token=abc",
		},
		{
			name:  "origin with trailing slash",
			base:  "https://memax.app/",
			token: "abc",
			want:  "https://memax.app/v1/unsubscribe?token=abc",
		},
		{
			name:  "already suffixed",
			base:  "https://memax.app/v1/unsubscribe",
			token: "abc",
			want:  "https://memax.app/v1/unsubscribe?token=abc",
		},
		{
			// Operator footgun: base ends with /v1/unsubscribe/ (trailing
			// slash). Must collapse to the canonical path, not append again.
			name:  "already suffixed with trailing slash",
			base:  "https://memax.app/v1/unsubscribe/",
			token: "abc",
			want:  "https://memax.app/v1/unsubscribe?token=abc",
		},
		{
			name:  "existing query param preserved",
			base:  "https://memax.app/v1/unsubscribe?ref=email",
			token: "abc",
			want:  "https://memax.app/v1/unsubscribe?ref=email&token=abc",
		},
		{
			name:  "fragment discarded",
			base:  "https://memax.app/v1/unsubscribe#foo",
			token: "abc",
			want:  "https://memax.app/v1/unsubscribe?token=abc",
		},
		{
			name:   "empty base rejected",
			base:   "",
			token:  "abc",
			expErr: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := buildUnsubscribeURL(c.base, c.token)
			if c.expErr {
				if err == nil {
					t.Fatalf("expected error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}
