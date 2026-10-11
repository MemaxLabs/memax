package v2api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/ask"
	"github.com/MemaxLabs/memax/packages/server/internal/handler/v2api"
	"github.com/MemaxLabs/memax/packages/server/internal/judge"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
	"github.com/MemaxLabs/memax/packages/server/internal/queue"
	"github.com/MemaxLabs/memax/packages/server/internal/v2dream"
	"github.com/MemaxLabs/memax/packages/server/internal/v2recall"
)

// A space still on V1 (hubs.v2_enabled_at unset) is served by V1, and its
// V2 record takes no new words or decisions until its owner switches it
// (policy.CodeSpaceOnV1, decided in policy.Decide). Every /v2 command is in
// exactly one of three tables: refusedOnV1 (it reaches policy and is
// refused), passesOnV1 (it goes through on V1, and why) or notInASpace (it
// changes no space's record). A command added later fails
// TestEveryCommandIsClassifiedOnV1 until it is in one of them, and one in
// refusedOnV1 fails TestSpaceOnV1RefusesEveryCommand unless it really is
// refused.

// passesOnV1 are the commands a space still on V1 takes.
var passesOnV1 = map[string]string{
	"switchSpace":        "the switch itself, its owner's (policy.DecideSwitchSpace)",
	"forgetMemory":       "rule 7: Forget takes words out wherever they are, a switched-back record's included",
	"forgetNote":         "rule 7, for the notes a switch numbered",
	"exportSpace":        "rule 14: a person's copy of the record as it stands, one exported receipt",
	"findNearDuplicates": "a read, sent as a POST to keep the draft out of URLs (x-memax-read)",
	"recordCompileLoad":  "a read: the compile an agent loaded at session start",
	"recordDelivery":     "a device reports what it did with a file already compiled",
	"recordObservation":  "a device reports a hand edit of a file already compiled",
	"setAgentAutonomy":   "a connection is the agent's identity, not the space's record (policy.DecideConnection)",
	"pauseAgent":         "pausing an agent works wherever it is connected",
	"resumeAgent":        "a connection change (policy.DecideConnection)",
	"disconnectAgent":    "disconnecting an agent works wherever it is connected",
}

// notInASpace are the commands that change no space's record.
var notInASpace = map[string]string{
	"createSpace":                "makes a new space, on V2",
	"forgetAccount":              "the whole account: each space through ForgetSpace, which a space on V1 takes",
	"updateDreamSettings":        "a person's own settings",
	"unsubscribeDreamEmail":      "a person's own email",
	"updateNotificationSettings": "a person's own settings",
	"ackNotices":                 "an agent's own notices",
	"lookupDeviceAuthorization":  "a device's sign-in",
	"approveDeviceAuthorization": "a device's sign-in",
	"denyDeviceAuthorization":    "a device's sign-in",
	"revokeSession":              "a person's sessions",
	"revokeOtherSessions":        "a person's sessions",
	"updateAccount":              "a person's account",
	"connectSignInMethod":        "a person's account",
	"disconnectSignInMethod":     "a person's account",
	"addPasskey":                 "a person's account",
	"startPasskeyRegistration":   "a person's account",
	"renamePasskey":              "a person's account",
	"removePasskey":              "a person's account",
	"startPasskeySignIn":         "signing in",
	"finishPasskeySignIn":        "signing in",
}

// v1Fixture is a space with every kind of thing a command acts on, made on
// V2 and then switched back to V1 by its owner, as a person would.
type v1Fixture struct {
	*env
	owner                    uuid.UUID
	sp                       space
	tok, key                 string
	conn                     uuid.UUID
	kept, kept2, kept3       memory
	proposal, proposal2, fly memory
	undoable                 uuid.UUID
	gate                     gate
	agents                   uuid.UUID
	compileRef, sha, content string
	importID                 uuid.UUID
	importA                  string
	edition                  dreamEdition
	note                     string
}

// refusedOnV1 is a request per command that a space on V2 would take, and
// a space on V1 refuses.
var refusedOnV1 = map[string]func(f *v1Fixture) call{
	"rememberMemory": func(f *v1Fixture) call {
		return call{method: "POST", path: memoriesPath(f.sp), token: f.tok, body: remember("Use pnpm catalogs.", "conventions")}
	},
	"keepMemories": func(f *v1Fixture) call {
		return call{method: "POST", path: "/v2/spaces/" + f.sp.slug + "/memories:keep", token: f.tok,
			body: map[string]any{"items": []map[string]any{{"memory": f.proposal.Ref, "version": f.proposal.Version}}}}
	},
	"rejectMemories": func(f *v1Fixture) call {
		return call{method: "POST", path: "/v2/spaces/" + f.sp.slug + "/memories:reject", token: f.tok,
			body: map[string]any{"items": []map[string]any{{"memory": f.proposal2.Ref}}}}
	},
	"createImport": func(f *v1Fixture) call {
		return call{method: "POST", path: "/v2/spaces/" + f.sp.slug + "/imports", token: f.tok, header: map[string]string{"X-Memax-Via": "cli"},
			body: importBody(importLine("z", "CLAUDE.md:40", "Prefer named exports."))}
	},
	"settleImportConflict": func(f *v1Fixture) call {
		return call{method: "POST", path: "/v2/spaces/" + f.sp.slug + "/imports/" + f.importID.String() + "/conflicts/1:settle",
			token: f.tok, body: map[string]any{"choice": "keep_one", "keep": f.importA}}
	},
	"askSpace": func(f *v1Fixture) call {
		return call{method: "POST", path: "/v2/spaces/" + f.sp.slug + "/ask", token: f.tok, body: map[string]any{"question": "Why River?"}}
	},
	"keepMemory": func(f *v1Fixture) call {
		return call{method: "POST", path: memoryPath(f.proposal, ":keep"), token: f.tok}
	},
	"editMemory": func(f *v1Fixture) call {
		return call{method: "POST", path: memoryPath(f.kept, ":edit"), token: f.tok, body: map[string]any{"statement": "Jobs run on River."},
			header: map[string]string{"If-Match": `"1"`}}
	},
	"rejectMemory": func(f *v1Fixture) call {
		return call{method: "POST", path: memoryPath(f.proposal2, ":reject"), token: f.tok}
	},
	"resolveConflict": func(f *v1Fixture) call {
		return call{method: "POST", path: memoryPath(f.fly, ":resolve-conflict"), token: f.tok, body: map[string]any{"choice": "keep_this"}}
	},
	"requestForget": func(f *v1Fixture) call {
		return call{method: "POST", path: memoryPath(f.kept, ":request-forget"), token: f.key, body: map[string]any{"reason": "a test value"}}
	},
	"declineForget": func(f *v1Fixture) call {
		return call{method: "POST", path: memoryPath(f.kept2, ":decline-forget"), token: f.tok}
	},
	"undoReceipt": func(f *v1Fixture) call {
		return call{method: "POST", path: "/v2/receipts/" + f.undoable.String() + ":undo", token: f.tok}
	},
	"reviseBrief": func(f *v1Fixture) call {
		return call{method: "POST", path: briefPath(f.sp), token: f.tok, header: map[string]string{"If-Match": `"2"`},
			body: briefBody("Acme web", map[string]any{"key": "conventions", "heading": "Conventions",
				"items": []map[string]any{{"ref": f.kept.Ref}}})}
	},
	"restoreBriefVersion": func(f *v1Fixture) call {
		return call{method: "POST", path: briefPath(f.sp) + "/versions/1:restore", token: f.tok, header: map[string]string{"If-Match": `"2"`}}
	},
	"createTarget": func(f *v1Fixture) call {
		return call{method: "POST", path: targetsPath(f.sp), token: f.tok, body: map[string]any{"kind": "claude_md"}}
	},
	"configureTarget": func(f *v1Fixture) call {
		return call{method: "PATCH", path: targetPath(f.agents, ""), token: f.tok,
			body: map[string]any{"settings": map[string]any{"include": "kept_only"}}}
	},
	"compileTarget": func(f *v1Fixture) call {
		return call{method: "POST", path: targetPath(f.agents, ":compile"), token: f.tok}
	},
	"pullDrift": func(f *v1Fixture) call {
		return call{method: "POST", path: targetPath(f.agents, "/drift:pull"), token: f.tok}
	},
	"overwriteDrift": func(f *v1Fixture) call {
		return call{method: "POST", path: targetPath(f.agents, "/drift:overwrite"), token: f.tok, body: map[string]any{"reason": "the record wins"}}
	},
	"stopDrift": func(f *v1Fixture) call {
		return call{method: "POST", path: targetPath(f.agents, "/drift:stop"), token: f.tok}
	},
	"requestDecision": func(f *v1Fixture) call {
		return call{method: "POST", path: gatesPath(f.sp), token: f.key, body: deployGate()}
	},
	"answerGate": func(f *v1Fixture) call {
		return call{method: "POST", path: gatePath(f.sp, f.gate, ":answer"), token: f.tok, body: map[string]any{"option": 1}}
	},
	"withdrawGate": func(f *v1Fixture) call {
		return call{method: "POST", path: gatePath(f.sp, f.gate, ":withdraw"), token: f.key}
	},
	"undoEdition": func(f *v1Fixture) call {
		return call{method: "POST", path: "/v2/spaces/" + f.sp.slug + "/dream/editions/" + f.edition.Ref + ":undo", token: f.tok,
			body: map[string]any{"kind": "stale"}}
	},
	"runDream": func(f *v1Fixture) call {
		return call{method: "POST", path: "/v2/spaces/" + f.sp.slug + "/dream:run", token: f.tok}
	},
	"undoDreamAction": func(f *v1Fixture) call {
		return call{method: "POST", path: "/v2/dream/actions/" + f.edition.Actions[0].ID.String() + ":undo", token: f.tok}
	},
	"restoreMemory": func(f *v1Fixture) call {
		return call{method: "POST", path: memoryPath(f.kept, ":restore"), token: f.tok}
	},
}

// bulkOnV1 are the refused commands that act on many things at once: they
// answer 200 with each item refused (space_on_v1), as for any refusal.
var bulkOnV1 = map[string]bool{"keepMemories": true, "rejectMemories": true, "undoEdition": true}

// TestEveryCommandIsClassifiedOnV1: every command /v2 serves says what a
// space still on V1 does with it, once, and every entry names a command.
func TestEveryCommandIsClassifiedOnV1(t *testing.T) {
	t.Parallel()
	commands := map[string]bool{}
	for _, rt := range v2api.Routes() {
		if rt.Method == http.MethodGet || rt.Method == http.MethodHead {
			continue
		}
		commands[rt.OperationID] = true
		n := 0
		if _, ok := refusedOnV1[rt.OperationID]; ok {
			n++
		}
		if _, ok := passesOnV1[rt.OperationID]; ok {
			n++
		}
		if _, ok := notInASpace[rt.OperationID]; ok {
			n++
		}
		switch n {
		case 0:
			t.Errorf("%s %s (%s) isn't classified for a space still on V1: add it to refusedOnV1 (and let policy.Decide refuse it), "+
				"or to passesOnV1 or notInASpace with the reason", rt.Method, rt.Path, rt.OperationID)
		case 1:
		default:
			t.Errorf("%s is in more than one table", rt.OperationID)
		}
	}
	for _, table := range []map[string]bool{keys(refusedOnV1), keys(passesOnV1), keys(notInASpace)} {
		for id := range table {
			if !commands[id] {
				t.Errorf("%s is classified but isn't a /v2 command", id)
			}
		}
	}
}

func keys[V any](m map[string]V) map[string]bool {
	out := make(map[string]bool, len(m))
	for k := range m {
		out[k] = true
	}
	return out
}

// newV1Fixture builds the space on V2 (from a V1 hub, through its switch),
// fills it, and switches it back to V1.
func newV1Fixture(t *testing.T) *v1Fixture {
	t.Helper()
	e := newEnvWith(t, func(e *env) []v2api.Option {
		jobs, err := queue.InsertClient(e.pool)
		if err != nil {
			t.Fatal(err)
		}
		return []v2api.Option{
			v2api.WithDream(v2dream.New(e.ledger, nil, v2dream.Config{Log: quiet}), jobs),
			v2api.WithAsk(ask.New(e.ledger, v2recall.New(e.ledger), nil, ask.Config{Log: quiet})),
		}
	})
	f := &v1Fixture{env: e, owner: e.user("zz")}
	f.sp = e.v1Space(f.owner, policy.SpaceProject, "acme-web")
	e.v1Note(f.sp, f.owner, "The old deploy runbook.", "state = 'archived'")
	f.tok = e.session(f.owner)
	var keyID uuid.UUID
	f.key, keyID = e.apiKey(f.owner, keyOpts{agent: "codex"})
	f.conn = e.connection(keyID)
	f.switchTo("v2")

	var notes page[struct {
		Ref string `json:"ref"`
	}]
	e.do(call{method: "GET", path: "/v2/spaces/" + f.sp.slug + "/notes", token: f.tok}).ok(http.StatusOK, &notes)
	if len(notes.Items) != 1 {
		t.Fatalf("notes = %+v", notes.Items)
	}
	f.note = notes.Items[0].Ref

	// Memories: kept, proposed by the agent, and a decision in conflict.
	f.kept = e.remember(f.tok, f.sp, "Background jobs run on River.").Memory
	f.kept2 = e.remember(f.tok, f.sp, "pnpm workspaces only.").Memory
	f.kept3 = e.remember(f.tok, f.sp, "Commit messages are short and imperative.").Memory
	var railway, fly, p1, p2, p3 result
	e.do(call{method: "POST", path: memoriesPath(f.sp), token: f.tok,
		body: decisionBody("Deploy the v2 API to Railway for its preview environments.", "deploy target")}).ok(http.StatusCreated, &railway)
	e.do(call{method: "POST", path: memoriesPath(f.sp), token: f.key,
		body: decisionBody("Deploy the v2 API to Fly.io in iad and ams.", "deploy target")}).ok(http.StatusCreated, &fly)
	for _, p := range []struct {
		res       *result
		statement string
	}{{&p1, "Prefer named exports."}, {&p2, "Tests live beside the code."}, {&p3, "Lint before every commit."}} {
		e.do(call{method: "POST", path: memoriesPath(f.sp), token: f.key, body: remember(p.statement, "conventions")}).
			ok(http.StatusCreated, p.res)
	}
	e.judgeAll(judge.New(e.ledger, contradicting{}, judge.Config{Primary: judge.Tier{Model: "fake"}, Log: quiet}))
	f.fly, f.proposal, f.proposal2 = fly.Memory, p1.Memory, p2.Memory
	var kept result
	e.do(call{method: "POST", path: memoryPath(p3.Memory, ":keep"), token: f.tok}).ok(http.StatusOK, &kept)
	f.undoable = kept.Receipts[0].ID
	e.do(call{method: "POST", path: memoryPath(f.kept2, ":request-forget"), token: f.key,
		body: map[string]any{"reason": "it was a test value"}}).ok(http.StatusOK, nil)

	// A decision waiting on a person.
	var g gateResult
	e.do(call{method: "POST", path: gatesPath(f.sp), token: f.key, body: deployGate()}).ok(http.StatusCreated, &g)
	f.gate = g.Gate

	// The Brief in two versions, AGENTS.md compiled and delivered, then a hand edit.
	e.do(call{method: "POST", path: briefPath(f.sp), token: f.tok, body: briefBody("Acme web",
		map[string]any{"key": "conventions", "heading": "Conventions", "items": []map[string]any{{"ref": f.kept.Ref}}})}).
		ok(http.StatusCreated, nil)
	e.do(call{method: "POST", path: briefPath(f.sp), token: f.tok, header: map[string]string{"If-Match": `"1"`}, body: briefBody("Acme web",
		map[string]any{"key": "conventions", "heading": "Conventions", "items": []map[string]any{{"ref": f.kept.Ref}, {"ref": f.kept2.Ref}}})}).
		ok(http.StatusCreated, nil)
	var tr targetResult
	e.do(call{method: "POST", path: targetsPath(f.sp), token: f.tok, body: map[string]any{"kind": "agents_md"}}).ok(http.StatusCreated, &tr)
	f.agents = tr.Target.ID
	e.compileAll()
	var pv preview
	e.do(call{method: "GET", path: targetPath(f.agents, "/preview"), token: f.tok}).ok(http.StatusOK, &pv)
	f.compileRef, f.sha, f.content = pv.Compile.Ref, pv.Compile.DriftSHA256, pv.Files[0].Content
	e.do(call{method: "POST", path: targetPath(f.agents, "/deliveries"), token: f.tok, header: map[string]string{"X-Memax-Via": "cli"},
		body: map[string]any{"compile": f.compileRef, "sha256": f.sha}}).ok(http.StatusOK, nil)
	e.do(call{method: "POST", path: targetPath(f.agents, "/observations"), token: f.tok,
		body: map[string]any{"path": "AGENTS.md", "content": f.content + "- Prefer tabs.\n", "device_id": "zz-laptop"}}).ok(http.StatusCreated, nil)

	// An import whose two statements disagree.
	var imp importOut
	e.do(call{method: "POST", path: "/v2/spaces/" + f.sp.slug + "/imports", token: f.tok, header: map[string]string{"X-Memax-Via": "cli"},
		body: importBody(importLine("a", "CLAUDE.md:12", "Run tests with `pnpm test`."),
			importLine("b", "AGENTS.md:8", "Run `npm run test` before committing."))}).ok(http.StatusCreated, &imp)
	f.importID, f.importA = imp.Import.ID, imp.Items[0].Memory.Ref
	e.recordCheck(f.sp.id, imp.Import.ID, imp.Items[0].Memory.ID, imp.Items[1].Memory.ID)

	// Dream's edition: a memory past its stale_after is flagged.
	e.do(call{method: "POST", path: memoriesPath(f.sp), token: f.tok, body: map[string]any{"statement": "Ask answers with the Haiku tier.",
		"section": "conventions", "stale_after": time.Now().Add(-24 * time.Hour).UTC().Format(time.RFC3339)}}).ok(http.StatusCreated, nil)
	d := &dreamEnv{env: e, sp: f.sp}
	d.dream(time.Now(), nil)
	e.do(call{method: "GET", path: "/v2/spaces/" + f.sp.slug + "/dream/editions/latest", token: f.tok}).ok(http.StatusOK, &f.edition)
	if len(f.edition.Actions) == 0 {
		t.Fatalf("edition without actions: %+v", f.edition)
	}
	f.switchTo("v1")
	if n := e.count(`SELECT count(*) FROM hubs WHERE id = $1 AND v2_enabled_at IS NULL`, f.sp.id); n != 1 {
		t.Fatal("the space didn't switch back to V1")
	}
	return f
}

// switchTo switches the space as its owner, running a background switch
// as the worker would.
func (f *v1Fixture) switchTo(to string) {
	f.t.Helper()
	var st switchOut
	r := f.do(call{method: "POST", path: "/v2/spaces/" + f.sp.slug + ":switch", token: f.tok, body: map[string]any{"to": to}})
	if r.status == http.StatusAccepted {
		r.ok(http.StatusAccepted, &st)
		if _, err := f.ledger.RunSwitch(context.Background(), f.sp.id); err != nil {
			f.t.Fatal(err)
		}
		return
	}
	r.ok(http.StatusOK, &st)
	want := map[string]string{"v2": "switched", "v1": "off"}[to]
	if st.State != want {
		f.t.Fatalf("switch to %s: %+v", to, st)
	}
}

// recordWrites counts what a command would have written to the space's
// record: receipts, memories, gates, targets, briefs, imports and jobs.
func (f *v1Fixture) recordWrites() int {
	f.t.Helper()
	return f.count(`SELECT (SELECT count(*) FROM v2.receipts WHERE space_id = $1) + (SELECT count(*) FROM v2.memories WHERE space_id = $1)
	                     + (SELECT count(*) FROM v2.memory_versions WHERE space_id = $1)
	                     + (SELECT count(*) FROM v2.decision_gates WHERE space_id = $1) + (SELECT count(*) FROM v2.targets WHERE space_id = $1)
	                     + (SELECT count(*) FROM v2.brief_versions WHERE space_id = $1) + (SELECT count(*) FROM v2.imports WHERE space_id = $1)
	                     + (SELECT count(*) FROM v2.ask_usage) + (SELECT count(*) FROM river_job)`, f.sp.id)
}

// TestSpaceOnV1RefusesEveryCommand: on a space its owner switched back to
// V1, every command in refusedOnV1 answers 403 space_on_v1 and writes
// nothing, while what passesOnV1 lists still goes through; switching to V2
// lifts the refusal, and switching back restores it.
func TestSpaceOnV1RefusesEveryCommand(t *testing.T) {
	t.Parallel()
	f := newV1Fixture(t)
	before := f.recordWrites()
	for id, build := range refusedOnV1 {
		c := build(f)
		r := f.do(c)
		if bulkOnV1[id] {
			// A bulk command answers per item, as for any other refusal.
			var out struct {
				Data struct {
					Items []struct {
						Outcome string `json:"outcome"`
						Policy  struct {
							Code string `json:"code"`
						} `json:"policy"`
					} `json:"items"`
					Applied int `json:"applied"`
					Undone  []struct {
						ID uuid.UUID `json:"id"`
					} `json:"undone"`
					Refused []struct {
						Message string `json:"message"`
					} `json:"refused"`
				} `json:"data"`
			}
			_ = json.Unmarshal(r.body, &out)
			refused := len(out.Data.Items) + len(out.Data.Refused)
			for _, it := range out.Data.Items {
				if it.Outcome != "refused" || it.Policy.Code != policy.CodeSpaceOnV1 {
					refused = 0
				}
			}
			for _, it := range out.Data.Refused {
				if !strings.Contains(it.Message, "still on V1") {
					refused = 0
				}
			}
			if r.status != http.StatusOK || refused == 0 || out.Data.Applied != 0 || len(out.Data.Undone) != 0 {
				t.Errorf("%s (%s %s) on V1: %d, want every item refused %s: %s", id, c.method, c.path, r.status, policy.CodeSpaceOnV1, r.body)
			}
			continue
		}
		var env struct {
			Error apiErr `json:"error"`
		}
		_ = json.Unmarshal(r.body, &env)
		if r.status != http.StatusForbidden || env.Error.Code != "refused" || env.Error.Details.Policy.Code != policy.CodeSpaceOnV1 {
			t.Errorf("%s (%s %s) on V1: %d %s %s, want 403 refused %s: %s", id, c.method, c.path, r.status, env.Error.Code,
				env.Error.Details.Policy.Code, policy.CodeSpaceOnV1, r.body)
			continue
		}
		if !strings.Contains(env.Error.Message, "acme-web") || !strings.Contains(env.Error.Message, "still on V1") {
			t.Errorf("%s: %q doesn't say the space is still on V1", id, env.Error.Message)
		}
	}
	if after := f.recordWrites(); after != before {
		t.Errorf("refused commands wrote %d rows to a space on V1", after-before)
	}

	// What a space on V1 still takes.
	f.do(call{method: "POST", path: "/v2/spaces/" + f.sp.slug + "/memories:near-duplicates", token: f.tok,
		body: map[string]any{"statement": "Background jobs run on River."}}).ok(http.StatusOK, nil)
	f.do(call{method: "POST", path: "/v2/spaces/" + f.sp.slug + "/compile-loads", token: f.key,
		body: map[string]any{"compile": f.compileRef, "agent": "codex"}}).ok(http.StatusCreated, nil)
	f.do(call{method: "POST", path: targetPath(f.agents, "/deliveries"), token: f.tok, header: map[string]string{"X-Memax-Via": "cli"},
		body: map[string]any{"compile": f.compileRef, "sha256": f.sha}}).ok(http.StatusOK, nil)
	f.do(call{method: "POST", path: targetPath(f.agents, "/observations"), token: f.tok,
		body: map[string]any{"path": "AGENTS.md", "content": f.content + "- Prefer spaces.\n", "device_id": "zz-laptop"}}).ok(http.StatusCreated, nil)
	f.exportSpace(f.tok, f.sp, "export-on-v1").ok(http.StatusOK, nil)
	f.do(call{method: "POST", path: memoryPath(f.kept3, ":forget"), token: f.tok, header: map[string]string{"If-Match": `"1"`}}).
		ok(http.StatusOK, nil)
	f.do(call{method: "POST", path: "/v2/spaces/" + f.sp.slug + "/notes/" + f.note + ":forget", token: f.tok}).ok(http.StatusOK, nil)
	f.do(call{method: "PATCH", path: agentPath(f.conn, "/spaces/"+f.sp.slug), token: f.tok, body: map[string]any{"autonomy": "read"}}).
		ok(http.StatusOK, nil)
	f.do(call{method: "POST", path: agentPath(f.conn, ":pause"), token: f.tok}).ok(http.StatusOK, nil)

	// Switching to V2 lifts the refusal; switching back restores it.
	remember := refusedOnV1["rememberMemory"]
	f.switchTo("v2")
	f.do(remember(f)).ok(http.StatusCreated, nil)
	f.switchTo("v1")
	if got := f.do(remember(f)).fails(http.StatusForbidden, "refused"); got.Details.Policy.Code != policy.CodeSpaceOnV1 {
		t.Errorf("after switching back: %+v", got.Details.Policy)
	}
}

// TestSpaceOnV1ForAPersonWhoIsntItsOwner: a member is told to ask the
// owner, and a space never switched answers the same way, Ask included.
func TestSpaceOnV1ForAPersonWhoIsntItsOwner(t *testing.T) {
	t.Parallel()
	e := newEnvWith(t, func(e *env) []v2api.Option {
		return []v2api.Option{v2api.WithAsk(ask.New(e.ledger, v2recall.New(e.ledger), nil, ask.Config{Log: quiet}))}
	})
	zz, co := e.user("zz"), e.user("co")
	sp := e.v1Space(zz, policy.SpaceTeam, "acme-team")
	e.join(sp, co, "contributor")
	for _, c := range []struct {
		token string
		call  call
		says  string
	}{
		{e.session(co), call{method: "POST", path: memoriesPath(sp), body: remember("Use pnpm catalogs.", "conventions")}, "Ask its owner to switch it to V2."},
		{e.session(zz), call{method: "POST", path: memoriesPath(sp), body: remember("Use pnpm catalogs.", "conventions")}, "Switch it to V2 first"},
		{e.session(co), call{method: "POST", path: "/v2/spaces/" + sp.slug + "/ask", body: map[string]any{"question": "Why River?"}}, "Ask answers from the V2 record"},
		{e.session(zz), call{method: "POST", path: briefPath(sp), body: briefBody("Acme")}, "Switch it to V2 first"},
	} {
		c.call.token = c.token
		got := e.do(c.call).fails(http.StatusForbidden, "refused")
		if got.Details.Policy.Code != policy.CodeSpaceOnV1 || !strings.Contains(got.Message, c.says) {
			t.Errorf("%s %s: %+v %q, want %s saying %q", c.call.method, c.call.path, got.Details.Policy, got.Message, policy.CodeSpaceOnV1, c.says)
		}
	}
	// Reads stay as they are: an empty record.
	var list page[memory]
	e.do(call{method: "GET", path: memoriesPath(sp), token: e.session(co)}).ok(http.StatusOK, &list)
	if len(list.Items) != 0 {
		t.Errorf("a space never switched lists %d memories", len(list.Items))
	}
	e.do(call{method: "GET", path: briefPath(sp), token: e.session(co)}).fails(http.StatusNotFound, "not_found")
}
