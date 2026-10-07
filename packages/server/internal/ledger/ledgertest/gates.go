package ledgertest

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// GateFixture is the range and moment to read SeedGateFixture's metrics
// at.
type GateFixture struct {
	From, To, AsOf time.Time
}

// SeedGateFixture writes people with known timelines for the product
// metrics (migration 053) and returns the range to ask for and the moment
// to ask at (2026-10-03 00:00 UTC). Every time is UTC.
//
// New people, week of Aug 3:
//
//	Ada   signs up Mon 10:00; init's import 10:03 (three proposals: two
//	      kept 10:05, one rejected 10:09); first file 10:06; Claude Code
//	      10:05, Codex 14:00; Claude Code reads 10:30; her Claude Code
//	      keeps a memory at once 12:00. Keeps one herself on day 23.
//	      Grace joins her space on day 30. Activated, keeping in week 4,
//	      pulled a teammate.
//	Ben   Mon 11:00; import 11:05 (two proposals: one folded by the judge
//	      11:06, one left open); file 11:13 (8 minutes); Claude Code 11:10,
//	      Cursor on Aug 6. One agent in the session: not activated.
//	Cy    Tue 09:00; import 09:02 (a proposal he forgets 09:30); Claude
//	      Code 09:05, Codex 09:20, a read 09:10. No file (one delivered
//	      after the as-of moment doesn't count): not activated.
//	Dee   Wed 08:00; import 08:02, file 08:04; Claude Code 08:03 and on a
//	      second laptop 20:00 (two connections, one kind); her hook reports
//	      a compile load 09:00. Activated; no keep in week 4.
//	Fay   Fri 09:00, then nothing until Aug 10: space, import 10:00, Claude
//	      Code, Codex, file 10:04. Her first session was empty: not
//	      activated, but a first file in four minutes.
//
// From V1, week of Aug 3:
//
//	Eve   account and V1 memories from 2025; switches her personal space
//	      Thu Aug 6 15:00, which connects her V1 Claude Code and Codex (as
//	      Memax); file 15:20, no init import. Activated; keeps on day 25.
//
// New, week of Aug 10:
//
//	Kim   Tue 09:00; import 09:07, file 09:10; Claude Code 09:05, Codex
//	      09:06 disconnected 09:30. One agent left: not activated.
//	Jo    Wed 10:00; import 10:02, file 10:05; Claude Code 10:03, Gemini
//	      CLI 10:04 (disconnected Aug 20). Activated; keeps on day 27.
//
// New, week of Aug 31:
//
//	Grace Wed Sep 2 09:55, joins Ada's space 10:00. Nothing else.
//
// New, week of Sep 28:
//
//	Kai   Wed Sep 30 08:00; import 08:02 (two proposals: one kept 09:02,
//	      one kept after the as-of moment), file 08:06; Claude Code, Codex.
//	      Activated; week 4 not reached.
//	Lu    Fri Oct 2 12:00; import 12:02, file 12:05, two agents. The
//	      session hasn't closed at the as-of moment.
//
// Left out: Hal (staff, activated), Ivy (V1 only, never on V2), Old (new,
// activated, but signed up on Jul 28, before the range).
func SeedGateFixture(t testing.TB, pool *pgxpool.Pool) GateFixture {
	t.Helper()
	at := func(s string) time.Time {
		v, err := time.Parse("2006-01-02 15:04", s)
		if err != nil {
			t.Fatalf("ledgertest: %v", err)
		}
		return v.UTC()
	}
	day := 24 * time.Hour
	tl := NewTimeline(t, pool)

	// Ada
	ada := tl.Person("ada", at("2026-08-03 10:00"))
	tl.V1Hub(ada, at("2026-08-03 10:00"))
	sAda := tl.Space(ada, at("2026-08-03 10:02"))
	tl.Import(ada, sAda, at("2026-08-03 10:03"))
	p1, p2, p3 := tl.Propose(ada, sAda, at("2026-08-03 10:03")), tl.Propose(ada, sAda, at("2026-08-03 10:03")),
		tl.Propose(ada, sAda, at("2026-08-03 10:03"))
	tl.Decide(ada, sAda, p1, "kept", 2, at("2026-08-03 10:05"))
	tl.Decide(ada, sAda, p2, "kept", 2, at("2026-08-03 10:05"))
	tl.Decide(ada, sAda, p3, "rejected", 2, at("2026-08-03 10:09"))
	adaCC := tl.Connect(ada, sAda, "claude-code", at("2026-08-03 10:05"), false)
	tl.Connect(ada, sAda, "codex", at("2026-08-03 14:00"), false)
	tl.Deliver(ada, sAda, at("2026-08-03 10:06"))
	tl.Read(ada, sAda, adaCC, "claude-code", at("2026-08-03 10:30"))
	tl.AgentKeep(adaCC, sAda, "claude-code", at("2026-08-03 12:00"))
	tl.Keep(ada, sAda, at("2026-08-03 10:00").Add(23*day+2*time.Hour))

	// Ben
	ben := tl.Person("ben", at("2026-08-03 11:00"))
	sBen := tl.Space(ben, at("2026-08-03 11:01"))
	tl.Import(ben, sBen, at("2026-08-03 11:05"))
	p4 := tl.Propose(ben, sBen, at("2026-08-03 11:05"))
	tl.Propose(ben, sBen, at("2026-08-03 11:05"))
	tl.Decide(ben, sBen, p4, "merged", 2, at("2026-08-03 11:06"))
	tl.Connect(ben, sBen, "claude-code", at("2026-08-03 11:10"), false)
	tl.Deliver(ben, sBen, at("2026-08-03 11:13"))
	tl.Connect(ben, sBen, "cursor", at("2026-08-06 09:00"), false)
	tl.Keep(ben, sBen, at("2026-08-25 12:00"))

	// Cy
	cy := tl.Person("cy", at("2026-08-04 09:00"))
	sCy := tl.Space(cy, at("2026-08-04 09:01"))
	tl.Import(cy, sCy, at("2026-08-04 09:02"))
	p6 := tl.Propose(cy, sCy, at("2026-08-04 09:02"))
	tl.Decide(cy, sCy, p6, "forgot", 2, at("2026-08-04 09:30"))
	cyCC := tl.Connect(cy, sCy, "claude-code", at("2026-08-04 09:05"), false)
	tl.Connect(cy, sCy, "codex", at("2026-08-04 09:20"), false)
	tl.Read(cy, sCy, cyCC, "claude-code", at("2026-08-04 09:10"))
	tl.Deliver(cy, sCy, at("2026-10-03 06:00")) // after the as-of moment

	// Dee
	dee := tl.Person("dee", at("2026-08-05 08:00"))
	sDee := tl.Space(dee, at("2026-08-05 08:01"))
	tl.Import(dee, sDee, at("2026-08-05 08:02"))
	tl.Connect(dee, sDee, "claude-code", at("2026-08-05 08:03"), false)
	tl.Deliver(dee, sDee, at("2026-08-05 08:04"))
	tl.Connect(dee, sDee, "claude-code", at("2026-08-05 20:00"), false)
	tl.CompileLoad(dee, sDee, "claude-code", at("2026-08-05 09:00"))
	tl.Keep(dee, sDee, at("2026-08-07 08:00"))

	// Fay
	fay := tl.Person("fay", at("2026-08-07 09:00"))
	tl.V1Hub(fay, at("2026-08-07 09:00"))
	sFay := tl.Space(fay, at("2026-08-10 09:59"))
	tl.Import(fay, sFay, at("2026-08-10 10:00"))
	tl.Connect(fay, sFay, "claude-code", at("2026-08-10 10:01"), false)
	tl.Connect(fay, sFay, "codex", at("2026-08-10 10:02"), false)
	tl.Deliver(fay, sFay, at("2026-08-10 10:04"))

	// Eve, from V1
	eve := tl.Person("eve", at("2025-01-10 12:00"))
	eveHub := tl.V1Hub(eve, at("2025-01-10 12:00"))
	tl.V1Memory(eve, eveHub, at("2025-01-10 12:00"), true)
	tl.V1Memory(eve, eveHub, at("2025-02-01 12:00"), false)
	tl.Switch(eveHub, eve, at("2026-08-06 15:00"))
	tl.Connect(eve, eveHub, "claude-code", at("2026-08-06 15:00"), true)
	tl.Connect(eve, eveHub, "codex", at("2026-08-06 15:00"), true)
	tl.Deliver(eve, eveHub, at("2026-08-06 15:20"))
	tl.Keep(eve, eveHub, at("2026-08-31 16:00"))

	// Kim
	kim := tl.Person("kim", at("2026-08-11 09:00"))
	sKim := tl.Space(kim, at("2026-08-11 09:01"))
	tl.Import(kim, sKim, at("2026-08-11 09:07"))
	tl.Connect(kim, sKim, "claude-code", at("2026-08-11 09:05"), false)
	kimCX := tl.Connect(kim, sKim, "codex", at("2026-08-11 09:06"), false)
	tl.Deliver(kim, sKim, at("2026-08-11 09:10"))
	tl.Disconnect(kim, sKim, kimCX, at("2026-08-11 09:30"))

	// Jo
	jo := tl.Person("jo", at("2026-08-12 10:00"))
	sJo := tl.Space(jo, at("2026-08-12 10:01"))
	tl.Import(jo, sJo, at("2026-08-12 10:02"))
	tl.Connect(jo, sJo, "claude-code", at("2026-08-12 10:03"), false)
	joGem := tl.Connect(jo, sJo, "gemini-cli", at("2026-08-12 10:04"), false)
	tl.Deliver(jo, sJo, at("2026-08-12 10:05"))
	tl.Disconnect(jo, sJo, joGem, at("2026-08-20 10:00"))
	tl.Keep(jo, sJo, at("2026-09-08 10:00"))

	// Grace joins Ada's space.
	grace := tl.Person("grace", at("2026-09-02 09:55"))
	tl.Join(sAda, grace, at("2026-09-02 10:00"))

	// Kai
	kai := tl.Person("kai", at("2026-09-30 08:00"))
	sKai := tl.Space(kai, at("2026-09-30 08:01"))
	tl.Import(kai, sKai, at("2026-09-30 08:02"))
	p7, p8 := tl.Propose(kai, sKai, at("2026-09-30 08:02")), tl.Propose(kai, sKai, at("2026-09-30 08:02"))
	tl.Decide(kai, sKai, p7, "kept", 2, at("2026-09-30 09:02"))
	tl.Decide(kai, sKai, p8, "kept", 2, at("2026-10-03 06:00")) // after the as-of moment
	tl.Connect(kai, sKai, "claude-code", at("2026-09-30 08:03"), false)
	tl.Connect(kai, sKai, "codex", at("2026-09-30 08:04"), false)
	tl.Deliver(kai, sKai, at("2026-09-30 08:06"))

	// Lu
	lu := tl.Person("lu", at("2026-10-02 12:00"))
	sLu := tl.Space(lu, at("2026-10-02 12:01"))
	tl.Import(lu, sLu, at("2026-10-02 12:02"))
	tl.Connect(lu, sLu, "claude-code", at("2026-10-02 12:03"), false)
	tl.Connect(lu, sLu, "codex", at("2026-10-02 12:04"), false)
	tl.Deliver(lu, sLu, at("2026-10-02 12:05"))

	// Left out: staff, V1 only, and before the range.
	hal := tl.Person("hal", at("2026-08-04 12:00"))
	tl.Staff(hal)
	sHal := tl.Space(hal, at("2026-08-04 12:01"))
	tl.Connect(hal, sHal, "claude-code", at("2026-08-04 12:02"), false)
	tl.Connect(hal, sHal, "codex", at("2026-08-04 12:03"), false)
	tl.Deliver(hal, sHal, at("2026-08-04 12:04"))

	ivy := tl.Person("ivy", at("2026-08-05 10:00"))
	ivyHub := tl.V1Hub(ivy, at("2026-08-05 10:00"))
	tl.V1Memory(ivy, ivyHub, at("2026-08-05 10:05"), false)

	old := tl.Person("old", at("2026-07-28 10:00"))
	sOld := tl.Space(old, at("2026-07-28 10:01"))
	tl.Connect(old, sOld, "claude-code", at("2026-07-28 10:02"), false)
	tl.Connect(old, sOld, "codex", at("2026-07-28 10:03"), false)
	tl.Deliver(old, sOld, at("2026-07-28 10:04"))

	tl.Commit()
	return GateFixture{From: at("2026-08-03 00:00"), To: at("2026-10-05 00:00"), AsOf: at("2026-10-03 00:00")}
}
