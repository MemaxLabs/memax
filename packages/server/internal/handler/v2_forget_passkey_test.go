package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

type fakeHolders map[uuid.UUID]bool

func (f fakeHolders) Has(_ context.Context, person uuid.UUID) (bool, error) {
	if person == uuid.Nil {
		return false, errors.New("down")
	}
	return f[person], nil
}

// TestV1RefusesPasskeyHolders: V1's space delete, data wipe and provider
// link can't ask for a passkey, so a person who has one is sent to the web
// app, and everyone else goes on as before.
func TestV1RefusesPasskeyHolders(t *testing.T) {
	t.Parallel()
	holder, other := uuid.New(), uuid.New()
	holders := fakeHolders{holder: true}
	cases := []struct {
		name    string
		p       PasskeyHolders
		person  uuid.UUID
		refused bool
		status  int
	}{
		{"passkeys off", nil, holder, false, 0},
		{"a person without a passkey", holders, other, false, 0},
		{"a person with one", holders, holder, true, http.StatusForbidden},
		{"the lookup failed", holders, uuid.Nil, true, http.StatusServiceUnavailable},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		got := refusedForPasskey(rec, httptest.NewRequest("DELETE", "/v1/account/data", nil), c.p, c.person, "Forget your record in Settings › Account")
		if got != c.refused || (c.refused && rec.Code != c.status) {
			t.Errorf("%s: refused=%v status=%d", c.name, got, rec.Code)
		}
		if c.status == http.StatusForbidden && !strings.Contains(rec.Body.String(), `"needs_passkey"`) {
			t.Errorf("%s: %s", c.name, rec.Body)
		}
	}
}
