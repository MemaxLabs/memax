package handler

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// Pure-function helpers on the notifications handler.

func TestCompactMemoryPreview(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		max  int
		want string
	}{
		{"hello", 10, "hello"},
		{"hello world", 10, "hello worl…"},
		{"  lots   of   whitespace  ", 100, "lots of whitespace"},
		{"exactly-ten", 11, "exactly-ten"},
		{"", 5, ""},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			if got := compactMemoryPreview(tc.in, tc.max); got != tc.want {
				t.Errorf("compactMemoryPreview(%q, %d) = %q, want %q",
					tc.in, tc.max, got, tc.want)
			}
		})
	}
}

func TestFmtSscanInt(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in      string
		want    int
		wantErr bool
	}{
		{"42", 42, false},
		{"  100  ", 100, false},
		{"0", 0, false},
		{"", 0, false}, // empty string is a no-op, not an error
		{"not a number", 0, true},
		{"12a", 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			var n int
			_, err := fmtSscanInt(tc.in, &n)
			if tc.wantErr != (err != nil) {
				t.Errorf("err = %v, wantErr = %v", err, tc.wantErr)
			}
			if !tc.wantErr && n != tc.want {
				t.Errorf("n = %d, want %d", n, tc.want)
			}
		})
	}
}

func TestMapStoreErrToAPI(t *testing.T) {
	t.Parallel()
	// Every sentinel must map to a concrete status + code. Unrecognized
	// errors fall through with ok=false.
	cases := []struct {
		err        error
		wantStatus int
		wantCode   string
	}{
		{store.ErrHubInviteNotFound, http.StatusNotFound, "invite_not_found"},
		{store.ErrHubInviteExpired, http.StatusConflict, "invite_expired"},
		{store.ErrHubInviteUsed, http.StatusConflict, "invite_used"},
		{store.ErrHubInviteRevoked, http.StatusConflict, "invite_revoked"},
		{store.ErrHubInviteNotAddressable, http.StatusConflict, "invite_not_addressable"},
		{store.ErrHubInviteWrongInvitee, http.StatusForbidden, "invite_wrong_invitee"},
		{store.ErrHubAlreadyMember, http.StatusConflict, "already_member"},
		{store.ErrHubTransferNotFound, http.StatusNotFound, "transfer_not_found"},
		{store.ErrHubTransferExpired, http.StatusConflict, "transfer_expired"},
		{store.ErrHubTransferAccepted, http.StatusConflict, "transfer_accepted"},
		{store.ErrHubTransferCanceled, http.StatusConflict, "transfer_canceled"},
		{store.ErrHubTransferInvalid, http.StatusConflict, "transfer_invalid"},
		{store.ErrMemberCapExceeded, http.StatusForbidden, "member_cap_exceeded"},
		{store.ErrOwnershipCapExceeded, http.StatusForbidden, "ownership_cap_exceeded"},
	}
	for _, tc := range cases {
		t.Run(tc.wantCode, func(t *testing.T) {
			t.Parallel()
			code, _, status, ok := mapStoreErrToAPI(tc.err)
			if !ok {
				t.Fatalf("%v: expected recognized", tc.err)
			}
			if status != tc.wantStatus {
				t.Errorf("status = %d, want %d", status, tc.wantStatus)
			}
			if code != tc.wantCode {
				t.Errorf("code = %q, want %q", code, tc.wantCode)
			}
		})
	}

	// Unrecognized error → ok=false.
	if _, _, _, ok := mapStoreErrToAPI(errors.New("random error")); ok {
		t.Error("unrecognized error should return ok=false")
	}
}

func TestMapTopicMergeError(t *testing.T) {
	t.Parallel()
	cases := []struct {
		err        error
		wantStatus int
		wantCode   string
	}{
		{store.ErrTopicMergeEmptySources, http.StatusBadRequest, "merge_empty_sources"},
		{store.ErrTopicMergeTargetIsSource, http.StatusBadRequest, "merge_target_is_source"},
		{store.ErrTopicMergeTargetNotInHub, http.StatusConflict, "merge_target_missing"},
		{store.ErrTopicMergeSourceNotInHub, http.StatusConflict, "merge_source_missing"},
		{store.ErrTopicMergeCycle, http.StatusConflict, "merge_cycle"},
	}
	for _, tc := range cases {
		t.Run(tc.wantCode, func(t *testing.T) {
			t.Parallel()
			status, code, _, ok := mapTopicMergeError(tc.err)
			if !ok {
				t.Fatalf("%v unrecognized", tc.err)
			}
			if status != tc.wantStatus || code != tc.wantCode {
				t.Errorf("got (%d, %q), want (%d, %q)",
					status, code, tc.wantStatus, tc.wantCode)
			}
		})
	}
	if _, _, _, ok := mapTopicMergeError(errors.New("other")); ok {
		t.Error("random error should return ok=false")
	}
}

func TestMapTopicRestructureError(t *testing.T) {
	t.Parallel()
	cases := []struct {
		err        error
		wantStatus int
		wantCode   string
	}{
		{store.ErrTopicRestructureSelfParent, http.StatusBadRequest, "restructure_self_parent"},
		{store.ErrTopicRestructureChildNotInHub, http.StatusConflict, "restructure_child_missing"},
		{store.ErrTopicRestructureParentNotInHub, http.StatusConflict, "restructure_parent_missing"},
		{store.ErrTopicRestructureCycle, http.StatusConflict, "restructure_cycle"},
	}
	for _, tc := range cases {
		t.Run(tc.wantCode, func(t *testing.T) {
			t.Parallel()
			status, code, _, ok := mapTopicRestructureError(tc.err)
			if !ok {
				t.Fatalf("%v unrecognized", tc.err)
			}
			if status != tc.wantStatus || code != tc.wantCode {
				t.Errorf("got (%d, %q), want (%d, %q)",
					status, code, tc.wantStatus, tc.wantCode)
			}
		})
	}
	if _, _, _, ok := mapTopicRestructureError(errors.New("other")); ok {
		t.Error("random error should return ok=false")
	}
}

func TestBulkSeenAndBulkDismiss_RequireAuth(t *testing.T) {
	t.Parallel()
	h := NewNotificationsHandler(nil, nil)

	for _, name := range []string{"BulkSeen", "BulkDismiss"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			req, _ := http.NewRequest(http.MethodPost, "/v1/notifications/"+strings.ToLower(name), strings.NewReader(`{}`))
			rec := &dummyResponseWriter{}
			if name == "BulkSeen" {
				h.BulkSeen(rec, req)
			} else {
				h.BulkDismiss(rec, req)
			}
			if rec.status != http.StatusUnauthorized {
				t.Errorf("unauth %s: got %d, want 401", name, rec.status)
			}
		})
	}
}

type dummyResponseWriter struct {
	status int
	body   []byte
	hdrs   http.Header
}

func (d *dummyResponseWriter) Header() http.Header {
	if d.hdrs == nil {
		d.hdrs = http.Header{}
	}
	return d.hdrs
}
func (d *dummyResponseWriter) Write(b []byte) (int, error) {
	d.body = append(d.body, b...)
	return len(b), nil
}
func (d *dummyResponseWriter) WriteHeader(s int) { d.status = s }
