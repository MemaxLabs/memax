package deviceauth_test

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MemaxLabs/memax/packages/server/internal/deviceauth"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
)

var pepper = []byte("deviceauth-test-secret-0123456789")

// clock is a settable time, safe across the store's goroutines.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func setup(t *testing.T) (*deviceauth.Store, *pgxpool.Pool, *clock) {
	t.Helper()
	_, pool := testdb.Acquire(t)
	c := &clock{t: time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)}
	return deviceauth.New(pool, pepper).WithClock(c.now), pool, c
}

func person(t *testing.T, pool *pgxpool.Pool, name string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if _, err := pool.Exec(context.Background(), `INSERT INTO users (id, email, name) VALUES ($1, $2, $3)`,
		id, id.String()[:8]+"@"+name+".test", name); err != nil {
		t.Fatal(err)
	}
	return id
}

func start(t *testing.T, s *deviceauth.Store, ip string) *deviceauth.Issued {
	t.Helper()
	got, err := s.Create(context.Background(), deviceauth.Start{
		ClientID: deviceauth.ClientCLI, ClientVersion: "2.0.0", DeviceName: "ziyang-mbp",
		DeviceOS: "darwin", Space: "memax-v2", IP: ip, UserAgent: "memax-cli/2.0.0",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	return got
}

func TestUserCodes(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"WQRT-4821":  "WQRT4821",
		"wqrt 4821":  "WQRT4821",
		" wqrt4821 ": "WQRT4821",
		"WQRT–4821":  "WQRT4821", // an en dash
		"WQRT-O82I":  "WQRT0821", // O and I among the digits
		"wqrt-482l":  "WQRT4821", // l among the digits
		"BCDF-0000":  "BCDF0000",
		"AQRT-4821":  "", // a vowel
		"WQRT-48210": "",
		"WQR-4821":   "",
		"WQRT-48A1":  "",
		"0QRT-4821":  "",
		"WQRT-48⁢1":  "", // an invisible character
		"":           "",
	} {
		got, ok := deviceauth.NormalizeUserCode(in)
		if (want == "") == ok || got != want {
			t.Errorf("NormalizeUserCode(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
	if got := deviceauth.FormatUserCode("WQRT4821"); got != "WQRT-4821" {
		t.Errorf("FormatUserCode = %q", got)
	}
}

// TestCodesAreStoredHashed: neither code appears in the table, and both
// look as the board draws them: 4 consonants and 4 digits.
func TestCodesAreStoredHashed(t *testing.T) {
	t.Parallel()
	s, pool, _ := setup(t)
	codeRE := regexp.MustCompile(`^[BCDFGHJKLMNPQRSTVWXZ]{4}-[0-9]{4}$`)
	seen := map[string]bool{}
	for range 20 {
		got := start(t, s, "")
		if !codeRE.MatchString(got.UserCode) {
			t.Fatalf("user code %q", got.UserCode)
		}
		if len(got.DeviceCode) < 43 || seen[got.DeviceCode] {
			t.Fatalf("device code %q", got.DeviceCode)
		}
		seen[got.DeviceCode] = true
		norm, _ := deviceauth.NormalizeUserCode(got.UserCode)
		var leaked int
		if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM device_authorizations
			WHERE strpos(device_code_hash || user_code_hash || client_id || device_name, $1) > 0
			   OR strpos(device_code_hash || user_code_hash, $2) > 0
			   OR strpos(device_code_hash || user_code_hash, $3) > 0`,
			got.DeviceCode, norm, got.UserCode).Scan(&leaked); err != nil {
			t.Fatal(err)
		}
		if leaked != 0 {
			t.Fatalf("a code is stored as it is")
		}
	}
	var hashes []string
	rows, _ := pool.Query(context.Background(), `SELECT device_code_hash, user_code_hash FROM device_authorizations`)
	for rows.Next() {
		var d, u string
		_ = rows.Scan(&d, &u)
		hashes = append(hashes, d, u)
	}
	rows.Close()
	for _, h := range hashes {
		if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(h) {
			t.Errorf("stored %q, want a hex SHA-256", h)
		}
	}
}

// TestPollFollowsRFC8628: authorization_pending until a person confirms,
// slow_down (and a longer interval) when polled too fast, then one
// session, then invalid_grant.
func TestPollFollowsRFC8628(t *testing.T) {
	t.Parallel()
	s, pool, c := setup(t)
	ctx := context.Background()
	zz := person(t, pool, "zz")
	issued := start(t, s, "203.0.113.4")
	if issued.Request.Interval != deviceauth.DefaultInterval || !issued.Request.ExpiresAt.Equal(c.now().Add(10*time.Minute)) {
		t.Fatalf("interval %d, expires %s", issued.Request.Interval, issued.Request.ExpiresAt)
	}
	poll := func() error {
		_, err := s.Poll(ctx, deviceauth.ClientCLI, issued.DeviceCode)
		return err
	}
	if err := poll(); !errors.Is(err, deviceauth.ErrPending) {
		t.Fatalf("first poll: %v", err)
	}
	c.add(2 * time.Second)
	if err := poll(); !errors.Is(err, deviceauth.ErrSlowDown) {
		t.Fatalf("a poll 2 s later: %v, want slow_down", err)
	}
	// The interval is now 10 s for good: 6 s later is still too soon, and
	// makes it 15.
	c.add(6 * time.Second)
	if err := poll(); !errors.Is(err, deviceauth.ErrSlowDown) {
		t.Fatalf("a poll 6 s later: %v, want slow_down", err)
	}
	c.add(16 * time.Second)
	if err := poll(); !errors.Is(err, deviceauth.ErrPending) {
		t.Fatalf("a poll after the longer interval: %v", err)
	}
	found, err := s.Find(ctx, issued.UserCode, zz)
	if err != nil || found.StateAt(c.now()) != deviceauth.StatePending || found.DeviceOS != "macOS" ||
		found.IP != "203.0.113.4" || found.Space != "memax-v2" || found.ClientVersion != "2.0.0" {
		t.Fatalf("find: %+v, %v", found, err)
	}
	if _, err := s.Approve(ctx, strings.ToLower(issued.UserCode), zz); err != nil {
		t.Fatalf("approve: %v", err)
	}
	user, err := s.Poll(ctx, deviceauth.ClientCLI, issued.DeviceCode)
	if err != nil || user != zz {
		t.Fatalf("poll after approval: %s, %v", user, err)
	}
	// A device code yields one session.
	c.add(time.Minute)
	if err := poll(); !errors.Is(err, deviceauth.ErrUsed) {
		t.Fatalf("second collection: %v, want invalid_grant", err)
	}
	if got, _ := s.Find(ctx, issued.UserCode, zz); got.StateAt(c.now()) != deviceauth.StateSignedIn || got.ConsumedAt == nil {
		t.Fatalf("after collection: %+v", got)
	}
	// Approving it again is a retry: it answers as it is.
	if got, err := s.Approve(ctx, issued.UserCode, zz); err != nil || got.StateAt(c.now()) != deviceauth.StateSignedIn {
		t.Fatalf("approve again: %+v, %v", got, err)
	}
	if _, err := s.Poll(ctx, deviceauth.ClientCLI, "not-a-device-code"); !errors.Is(err, deviceauth.ErrUsed) {
		t.Fatalf("unknown device code: %v", err)
	}
	if _, err := s.Poll(ctx, "claude-desktop", issued.DeviceCode); !errors.Is(err, deviceauth.ErrUnknownClient) {
		t.Fatalf("another client: %v", err)
	}
}

func TestDeniedAndExpiredCodes(t *testing.T) {
	t.Parallel()
	s, pool, c := setup(t)
	ctx := context.Background()
	zz, jy := person(t, pool, "zz"), person(t, pool, "jy")

	denied := start(t, s, "")
	if _, err := s.Deny(ctx, denied.UserCode, zz); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Poll(ctx, deviceauth.ClientCLI, denied.DeviceCode); !errors.Is(err, deviceauth.ErrDenied) {
		t.Fatalf("poll a denied code: %v, want access_denied", err)
	}
	var decided *deviceauth.DecidedError
	if _, err := s.Approve(ctx, denied.UserCode, zz); !errors.As(err, &decided) || decided.State != deviceauth.StateDenied {
		t.Fatalf("approve a denied code: %v", err)
	}
	// Someone else never learns the code exists.
	for _, try := range []func() error{
		func() error { _, err := s.Find(ctx, denied.UserCode, jy); return err },
		func() error { _, err := s.Approve(ctx, denied.UserCode, jy); return err },
		func() error { _, err := s.Deny(ctx, denied.UserCode, jy); return err },
	} {
		if err := try(); !errors.Is(err, deviceauth.ErrNotFound) {
			t.Fatalf("another person: %v, want not found", err)
		}
	}

	expired := start(t, s, "")
	approvedLate := start(t, s, "")
	if _, err := s.Approve(ctx, approvedLate.UserCode, zz); err != nil {
		t.Fatal(err)
	}
	c.add(deviceauth.Lifetime)
	if _, err := s.Poll(ctx, deviceauth.ClientCLI, expired.DeviceCode); !errors.Is(err, deviceauth.ErrExpired) {
		t.Fatalf("poll after 10 minutes: %v, want expired_token", err)
	}
	// Confirmed, but not collected in time: the session is never issued.
	if _, err := s.Poll(ctx, deviceauth.ClientCLI, approvedLate.DeviceCode); !errors.Is(err, deviceauth.ErrExpired) {
		t.Fatalf("collect after 10 minutes: %v, want expired_token", err)
	}
	if _, err := s.Approve(ctx, expired.UserCode, zz); !errors.As(err, &decided) || decided.State != deviceauth.StateExpired {
		t.Fatalf("approve an expired code: %v", err)
	}
	if got, err := s.Find(ctx, expired.UserCode, jy); err != nil || got.StateAt(c.now()) != deviceauth.StateExpired {
		t.Fatalf("find an expired code: %+v, %v", got, err)
	}
	if _, err := s.Find(ctx, "BCDF-0000", zz); !errors.Is(err, deviceauth.ErrNotFound) {
		t.Fatalf("a code nobody asked for: %v", err)
	}
	if _, err := s.Find(ctx, "not a code", zz); !errors.Is(err, deviceauth.ErrNotFound) {
		t.Fatalf("a malformed code: %v", err)
	}
}

// TestOneDecisionPerCode: two people racing to decide one code, one wins.
func TestOneDecisionPerCode(t *testing.T) {
	t.Parallel()
	s, pool, _ := setup(t)
	ctx := context.Background()
	issued := start(t, s, "")
	people := []uuid.UUID{person(t, pool, "a"), person(t, pool, "b"), person(t, pool, "c"), person(t, pool, "d")}
	var wg sync.WaitGroup
	wins := make(chan uuid.UUID, len(people))
	for _, p := range people {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.Approve(ctx, issued.UserCode, p); err == nil {
				wins <- p
			}
		}()
	}
	wg.Wait()
	close(wins)
	if n := len(wins); n != 1 {
		t.Fatalf("%d people confirmed one code", n)
	}
	winner := <-wins
	if user, err := s.Poll(ctx, deviceauth.ClientCLI, issued.DeviceCode); err != nil || user != winner {
		t.Fatalf("the session is for %s (%v), want %s", user, err, winner)
	}
}

func TestCreateLimitsAndClient(t *testing.T) {
	t.Parallel()
	s, _, c := setup(t)
	ctx := context.Background()
	if _, err := s.Create(ctx, deviceauth.Start{ClientID: "claude-desktop"}); !errors.Is(err, deviceauth.ErrUnknownClient) {
		t.Fatalf("another client: %v", err)
	}
	for range deviceauth.PerIPMax {
		start(t, s, "198.51.100.7")
	}
	if _, err := s.Create(ctx, deviceauth.Start{ClientID: deviceauth.ClientCLI, IP: "198.51.100.7"}); !errors.Is(err, deviceauth.ErrRateLimited) {
		t.Fatalf("code %d from one address: %v, want rate limited", deviceauth.PerIPMax+1, err)
	}
	start(t, s, "198.51.100.8") // another address isn't held up
	c.add(deviceauth.PerIPWindow + time.Second)
	start(t, s, "198.51.100.7") // the window passed
}

// TestWhatTheDeviceSaysIsCleaned: names lose control and bidi characters
// and long tails; unshaped versions, systems and spaces are dropped.
func TestWhatTheDeviceSaysIsCleaned(t *testing.T) {
	t.Parallel()
	s, pool, _ := setup(t)
	ctx := context.Background()
	zz := person(t, pool, "zz")
	got, err := s.Create(ctx, deviceauth.Start{
		ClientID: deviceauth.ClientCLI, ClientVersion: "2.0.0; rm -rf /", DeviceName: "zz‮mac\x07" + strings.Repeat("x", 100),
		DeviceOS: "TempleOS", Space: "Not A Slug",
	})
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.Find(ctx, got.UserCode, zz)
	if err != nil {
		t.Fatal(err)
	}
	if r.ClientVersion != "" || r.DeviceOS != "other" || r.Space != "" ||
		strings.ContainsAny(r.DeviceName, "‮\x07") || len([]rune(r.DeviceName)) != 64 {
		t.Fatalf("kept %+v", r)
	}
}
