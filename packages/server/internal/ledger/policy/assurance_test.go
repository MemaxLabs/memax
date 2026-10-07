package policy

import "testing"

func TestAssuranceLevels(t *testing.T) {
	t.Parallel()
	verified := func(v Via) Actor {
		return with(person(RoleOwner, v), func(a *Actor) { a.Passkey, a.Verified = true, true })
	}
	cases := []struct {
		name  string
		actor Actor
		want  Assurance
	}{
		{"web", person(RoleOwner, ViaWeb), AssuranceHumanWeb},
		{"web with a fresh assertion", verified(ViaWeb), AssuranceHumanWebVerified},
		{"Review with a fresh assertion", verified(ViaReview), AssuranceHumanWebVerified},
		{"the CLI never reaches it, assertion or not", verified(ViaCLI), AssuranceClientAttested},
		{"an in-agent confirmation", verified(ViaMCP), AssuranceClientAttested},
		{"a passkey alone is no assertion", with(person(RoleOwner, ViaWeb), func(a *Actor) { a.Passkey = true }), AssuranceHumanWeb},
		{"agents have none", with(agent(RoleOwner, AutonomyWrite), func(a *Actor) { a.Verified = true }), ""},
	}
	for _, c := range cases {
		if got := c.actor.Assurance(); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
	if !AssuranceHumanWebVerified.AtLeast(AssuranceHumanWeb) || AssuranceHumanWeb.AtLeast(AssuranceHumanWebVerified) ||
		!AssuranceHumanWeb.AtLeast(AssuranceClientAttested) || Assurance("").AtLeast(AssuranceClientAttested) {
		t.Error("the order is client_attested < human_web < human_web_verified")
	}
}

// TestPasskeyReCheck: every decision that needs a person keeps today's
// human_web rule without a passkey (and suggests one), asks a person with
// a passkey for a fresh assertion, and never lets the CLI through.
func TestPasskeyReCheck(t *testing.T) {
	t.Parallel()
	type who struct {
		name  string
		actor Actor
	}
	web := person(RoleOwner, ViaWeb)
	webPK := with(web, func(a *Actor) { a.Passkey = true })
	webOK := with(web, func(a *Actor) { a.Passkey, a.Verified = true, true })
	cli := person(RoleOwner, ViaCLI)
	cliPK := with(cli, func(a *Actor) { a.Passkey, a.Verified = true, true })

	type want struct {
		effect  Effect
		code    string
		suggest bool
	}
	ok := want{EffectApply, "", false}
	nudge := want{EffectApply, "", true}
	check := want{EffectRefuse, CodeNeedsPasskey, false}

	cases := []struct {
		name              string
		decide            func(Actor) Decision
		web, webPK, webOK want
		cli, cliPK        want
	}{
		{"keep a quarantined proposal", func(a Actor) Decision { return Decide(a, ActionKeep, propExt, project) },
			nudge, check, ok, want{EffectRefuse, CodeExternalNeedsReview, false}, want{EffectRefuse, CodeExternalNeedsReview, false}},
		{"keep a team decision (D15)", func(a Actor) Decision { return Decide(a, ActionKeep, propDec, team) },
			nudge, check, ok, want{EffectRefuse, CodeDecisionNeedsWeb, false}, want{EffectRefuse, CodeDecisionNeedsWeb, false}},
		{"keep a project fact", func(a Actor) Decision { return Decide(a, ActionKeep, proposal, project) },
			ok, ok, ok, ok, ok},
		{"settle a quarantined conflict", func(a Actor) Decision { return Decide(a, ActionResolveConflict, propExt, project) },
			nudge, check, ok, want{EffectRefuse, CodeExternalNeedsReview, false}, want{EffectRefuse, CodeExternalNeedsReview, false}},
		{"answer a team gate", func(a Actor) Decision { return Decide(a, ActionAnswerGate, gate, team) },
			nudge, check, ok, want{EffectRefuse, CodeDecisionNeedsWeb, false}, want{EffectRefuse, CodeDecisionNeedsWeb, false}},
		{"remember a team decision", func(a Actor) Decision { return Decide(a, ActionRemember, newDecision, team) },
			nudge, check, ok, want{EffectPropose, CodeDecisionNeedsWeb, false}, want{EffectPropose, CodeDecisionNeedsWeb, false}},
		{"edit a team decision", func(a Actor) Decision { return Decide(a, ActionEdit, keptDec, team) },
			nudge, check, ok, want{EffectPropose, CodeDecisionNeedsWeb, false}, want{EffectPropose, CodeDecisionNeedsWeb, false}},
		{"forget a project fact", func(a Actor) Decision { return Decide(a, ActionForget, kept, project) },
			ok, check, ok, ok, check},
		{"keep what an agent asked to forget", func(a Actor) Decision { return Decide(a, ActionDeclineForget, kept, project) },
			ok, ok, ok, ok, ok},
		{"restore a faded quarantined memory", func(a Actor) Decision { return Decide(a, ActionRestore, keptExt, project) },
			nudge, check, ok, want{EffectRefuse, CodeExternalNeedsReview, false}, want{EffectRefuse, CodeExternalNeedsReview, false}},
		{"forget a team decision", func(a Actor) Decision { return Decide(a, ActionForget, keptDec, team) },
			nudge, check, ok, want{EffectRefuse, CodeDecisionNeedsWeb, false}, want{EffectRefuse, CodeDecisionNeedsWeb, false}},
		{"raise an agent", func(a Actor) Decision {
			return DecideConnection(a, ConnectionSetAutonomy, Connection{Name: "Codex", Mine: true, From: AutonomyPropose, To: AutonomyWrite}, project)
		}, nudge, check, ok, want{EffectRefuse, CodeAutonomyNeedsWeb, false}, want{EffectRefuse, CodeAutonomyNeedsWeb, false}},
		{"resume an agent", func(a Actor) Decision {
			return DecideConnection(a, ConnectionResume, Connection{Name: "Codex", Mine: true}, project)
		}, nudge, check, ok, want{EffectRefuse, CodeAutonomyNeedsWeb, false}, want{EffectRefuse, CodeAutonomyNeedsWeb, false}},
		{"lower an agent", func(a Actor) Decision {
			return DecideConnection(a, ConnectionSetAutonomy, Connection{Name: "Codex", Mine: true, From: AutonomyWrite, To: AutonomyRead}, project)
		}, ok, ok, ok, ok, ok},
		{"connect an agent quietly", func(a Actor) Decision {
			return DecideConnection(a, ConnectionConnect, Connection{Name: "Codex", Mine: true, To: AutonomyPropose}, project)
		}, ok, ok, ok, ok, ok},
		{"forget the account", func(a Actor) Decision { return DecideAccount(a, AccountForget, false) },
			nudge, check, ok, want{EffectRefuse, CodeAccountNeedsWeb, false}, want{EffectRefuse, CodeAccountNeedsWeb, false}},
		{"remove a passkey", func(a Actor) Decision { return DecideAccount(a, AccountRemovePasskey, false) },
			ok, check, ok, want{EffectRefuse, CodeAccountNeedsWeb, false}, want{EffectRefuse, CodeAccountNeedsWeb, false}},
		{"disconnect GitHub", func(a Actor) Decision { return DecideAccount(a, AccountDisconnectSignIn, false) },
			nudge, check, ok, want{EffectRefuse, CodeAccountNeedsWeb, false}, want{EffectRefuse, CodeAccountNeedsWeb, false}},
		{"sign another session out", func(a Actor) Decision { return DecideSession(a, SessionRevoke) },
			ok, ok, ok, want{EffectRefuse, CodeSessionNeedsWeb, false}, want{EffectRefuse, CodeSessionNeedsWeb, false}},
		{"confirm a device", func(a Actor) Decision { return DecideDevice(a, DeviceApprove) },
			ok, ok, ok, want{EffectRefuse, CodeDeviceNeedsWeb, false}, want{EffectRefuse, CodeDeviceNeedsWeb, false}},
	}
	for _, c := range cases {
		for _, w := range []struct {
			who
			want want
		}{{who{"web, no passkey", web}, c.web}, {who{"web, passkey", webPK}, c.webPK}, {who{"web, passkey checked", webOK}, c.webOK},
			{who{"CLI, no passkey", cli}, c.cli}, {who{"CLI, passkey", cliPK}, c.cliPK}} {
			got := c.decide(w.actor)
			if got.Effect != w.want.effect || got.Code != w.want.code || (got.Suggest == SuggestPasskey) != w.want.suggest {
				t.Errorf("%s, %s: {%s %s suggest=%q %q}, want {%s %s suggest=%v}", c.name, w.name,
					got.Effect, got.Code, got.Suggest, got.Message, w.want.effect, w.want.code, w.want.suggest)
			}
		}
	}
}

// TestDecideAccount: account changes are the person's, on a session; what
// adds a way in needs a fresh sign-in or the passkey.
func TestDecideAccount(t *testing.T) {
	t.Parallel()
	web := person(RoleOwner, ViaWeb)
	webPK := with(web, func(a *Actor) { a.Passkey = true })
	webOK := with(web, func(a *Actor) { a.Passkey, a.Verified = true, true })
	key := with(person(RoleOwner, ViaAPI), func(a *Actor) { a.Credential = CredentialAPIKey })
	cases := []struct {
		name   string
		actor  Actor
		act    AccountAction
		fresh  bool
		effect Effect
		code   string
	}{
		{"view", person(RoleOwner, ViaCLI), AccountView, false, EffectApply, ""},
		{"rename from the CLI", person(RoleOwner, ViaCLI), AccountRename, false, EffectApply, ""},
		{"an API key can't look", key, AccountView, false, EffectRefuse, CodeAccountByPerson},
		{"an agent can't add a passkey", with(agent(RoleOwner, AutonomyWrite), func(a *Actor) { a.Credential = CredentialOAuth }), AccountAddPasskey, true, EffectRefuse, CodeAccountByPerson},
		{"add a passkey right after signing in", web, AccountAddPasskey, true, EffectApply, ""},
		{"add a passkey on an old session", web, AccountAddPasskey, false, EffectRefuse, CodeNeedsSignIn},
		{"add another with the first", webOK, AccountAddPasskey, false, EffectApply, ""},
		{"add another on an old session asks for one", webPK, AccountAddPasskey, false, EffectRefuse, CodeNeedsPasskey},
		{"lost every passkey: sign in again, then add", webPK, AccountAddPasskey, true, EffectApply, ""},
		{"never from the CLI, fresh or not", person(RoleOwner, ViaCLI), AccountAddPasskey, true, EffectRefuse, CodeAccountNeedsWeb},
		{"connect GitHub on an old session", web, AccountConnectSignIn, false, EffectRefuse, CodeNeedsSignIn},
		{"connect GitHub right after signing in", web, AccountConnectSignIn, true, EffectApply, ""},
		{"connect Google with the passkey", webOK, AccountConnectSignIn, false, EffectApply, ""},
		{"rename a passkey on the web", webPK, AccountRenamePasskey, false, EffectApply, ""},
		{"rename a passkey from the CLI", person(RoleOwner, ViaCLI), AccountRenamePasskey, false, EffectRefuse, CodeAccountNeedsWeb},
		{"unknown", web, AccountAction("delete"), false, EffectRefuse, CodeUnknownAction},
	}
	for _, c := range cases {
		got := DecideAccount(c.actor, c.act, c.fresh)
		if got.Effect != c.effect || got.Code != c.code {
			t.Errorf("%s: {%s %s %q}, want {%s %s}", c.name, got.Effect, got.Code, got.Message, c.effect, c.code)
		}
	}
}
