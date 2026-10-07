package policy

import "fmt"

// Assurance levels, and the passkey re-check (plan 25 §5.15 and the
// carry-over of "Agent connections and web assurance": "a passkey re-check
// at Keep time is the strong guarantee for quarantine and D15").
//
// A person's changes carry one of three levels, strongest last:
//
//   - client_attested: a client says a person did it (the CLI, an answer in
//     the agent). An agent can drive the CLI with the person's login, so
//     this proves nothing about who was at the keyboard.
//   - human_web: the request came through the web app's proxy, signed, on a
//     session issued to the web app (internal/websurface). It defeats an
//     agent holding the CLI login; it doesn't defeat one that copied the
//     browser's cookies, or one driving the person's real browser.
//   - human_web_verified: human_web, plus a fresh user-verified WebAuthn
//     assertion (a passkey with Touch ID, Windows Hello, a PIN) bound to
//     this person, this session and this very request
//     (internal/passkeys). Neither a cookie thief nor a browser driver can
//     produce one without the person.
//
// The decisions that need a person (keeping a quarantined proposal,
// keeping or answering a decision where the space needs a person on the
// web (D15), raising what an agent may do, Forget, forgetting the account,
// removing a passkey) need human_web from a person without a passkey, as
// before. A person with a passkey has said decisions only they should make
// must ask for it, so those need human_web_verified: the re-check is on by
// default once a passkey exists, never opt-in (see Actor.Passkey). The CLI
// and agents never reach it, since only the web carries an assertion.

// The assurance levels.
const (
	AssuranceHumanWeb         Assurance = "human_web"
	AssuranceHumanWebVerified Assurance = "human_web_verified"
	AssuranceClientAttested   Assurance = "client_attested"
)

// Assurances lists the levels, weakest first.
var Assurances = []Assurance{AssuranceClientAttested, AssuranceHumanWeb, AssuranceHumanWebVerified}

// rank orders the levels; "" (not a person) ranks below every level.
func (a Assurance) rank() int {
	switch a {
	case AssuranceClientAttested:
		return 1
	case AssuranceHumanWeb:
		return 2
	case AssuranceHumanWebVerified:
		return 3
	}
	return 0
}

// AtLeast reports whether a is as strong as b or stronger.
func (a Assurance) AtLeast(b Assurance) bool { return a.rank() >= b.rank() }

// Valid reports whether a is a known level.
func (a Assurance) Valid() bool { return a.rank() > 0 }

// The decision codes the re-check adds; all refusals.
const (
	// CodeNeedsPasskey: the person has a passkey, so this decision asks for
	// it (human_web_verified). On the web the API answers 403 needs_passkey
	// with a challenge; elsewhere it is a refusal that says to do it on the
	// web.
	CodeNeedsPasskey = "needs_passkey"
	// CodeAccountByPerson: only the person, on a session, manages their
	// account (agents and API keys never do).
	CodeAccountByPerson = "account_by_person"
	// CodeAccountNeedsWeb: passkeys, sign-in methods and forgetting the
	// account are managed on the web.
	CodeAccountNeedsWeb = "account_needs_web"
	// CodeNeedsSignIn: adding a passkey or a sign-in method needs a sign-in
	// in the last few minutes (or, with a passkey already, that passkey), so
	// a session someone copied can't add a way in of their own.
	CodeNeedsSignIn = "needs_sign_in"
)

// SuggestPasskey is Decision.Suggest on a decision that needed a person and
// was made without a passkey: the web nudges the person to add one.
const SuggestPasskey = "passkey"

// verified is the level a decision that needs a person asks of a: human_web
// without a passkey, human_web_verified with one.
func (a Actor) verified() Assurance {
	if a.Passkey {
		return AssuranceHumanWebVerified
	}
	return AssuranceHumanWeb
}

// needsPerson checks a decision that needs a person on the web. Below
// human_web it is refused with webCode and webMessage (today's rule); a
// person with a passkey and no fresh assertion is refused with
// needs_passkey. It returns nil when the actor may go ahead.
func needsPerson(a Actor, webCode, webMessage, what string) *Decision {
	if !a.Assurance().AtLeast(AssuranceHumanWeb) {
		d := refuse(webCode, webMessage)
		return &d
	}
	if !a.Assurance().AtLeast(a.verified()) {
		d := refuse(CodeNeedsPasskey, passkeyMessage(what))
		return &d
	}
	return nil
}

// passkeyMessage asks for the passkey, naming what it confirms.
func passkeyMessage(what string) string {
	if what == "" {
		what = "this"
	}
	return fmt.Sprintf("You have a passkey, so %s asks for it. Confirm with your passkey on memax.app.", what)
}

// applyChecked is apply() for a decision that needed a person: it suggests
// a passkey to a person without one.
func applyChecked(a Actor) Decision {
	d := apply()
	if a.Kind == ActorPerson && !a.Passkey {
		d.Suggest = SuggestPasskey
	}
	return d
}

// AccountAction is what a person does with their account (Settings ›
// Account). None of it is part of a space's record, so none writes a
// receipt, except forgetting the account, whose Forgets do.
type AccountAction string

// The account actions.
const (
	AccountView             AccountAction = "view"               // read the profile, sign-in methods and passkeys
	AccountRename           AccountAction = "rename"             // change the name receipts show
	AccountAddPasskey       AccountAction = "add_passkey"        // register a passkey
	AccountRenamePasskey    AccountAction = "rename_passkey"     // name one
	AccountRemovePasskey    AccountAction = "remove_passkey"     // take one off the account
	AccountConnectSignIn    AccountAction = "connect_sign_in"    // link GitHub or Google
	AccountDisconnectSignIn AccountAction = "disconnect_sign_in" // unlink one
	AccountForget           AccountAction = "forget"             // forget the account
)

// DecideAccount decides whether the actor may act on their account. fresh
// says the session signed in within the last few minutes (passkeys'
// EnrollWindow).
//
//   - Only the person, on a session (never an agent or an API key).
//     Viewing it and renaming themselves need only that.
//   - Everything that changes how they sign in or confirm happens on the
//     web (human_web): an agent holding the CLI login must not add or
//     remove a way into the account, and the CLI can't make an assertion
//     for memax.app anyway.
//   - Adding a passkey or a sign-in method needs a fresh sign-in, or the
//     person's passkey when they have one. Otherwise anyone holding the web
//     session's cookies could add a way in of their own (their passkey,
//     their GitHub) and pass every re-check after. A person who lost every
//     passkey signs in again (GitHub, Google, an email code) and adds one.
//   - Removing a passkey or a sign-in method needs the passkey when they
//     have one: otherwise a cookie thief would remove the passkey and turn
//     the re-check off.
//   - Forgetting the account is the largest Forget there is, and nothing
//     undoes it: human_web, and the passkey when they have one. (The
//     handler also asks them to type their email.)
func DecideAccount(a Actor, act AccountAction, fresh bool) Decision {
	if a.Kind != ActorPerson || a.Credential != CredentialSession {
		return refuse(CodeAccountByPerson, "Only you manage your account, signed in on memax.app.")
	}
	const onWeb = "Change how you sign in on memax.app, in Settings › Account, so an agent using this login can't."
	switch act {
	case AccountView, AccountRename:
		return apply()
	case AccountRenamePasskey:
		if !a.Assurance().AtLeast(AssuranceHumanWeb) {
			return refuse(CodeAccountNeedsWeb, onWeb)
		}
		return apply()
	case AccountAddPasskey, AccountConnectSignIn:
		if !a.Assurance().AtLeast(AssuranceHumanWeb) {
			return refuse(CodeAccountNeedsWeb, onWeb)
		}
		if fresh || a.Assurance().AtLeast(AssuranceHumanWebVerified) {
			return apply()
		}
		what := "a passkey"
		if act == AccountConnectSignIn {
			what = "a way to sign in"
		}
		if a.Passkey {
			return refuse(CodeNeedsPasskey, fmt.Sprintf(
				"Confirm with a passkey you already have to add %s, or sign in again if you lost it.", what))
		}
		return refuse(CodeNeedsSignIn, fmt.Sprintf(
			"Sign in again to add %s, so only someone who can sign in as you can add one.", what))
	case AccountRemovePasskey:
		if d := needsPerson(a, CodeAccountNeedsWeb, onWeb, "removing a passkey"); d != nil {
			return *d
		}
		return apply()
	case AccountDisconnectSignIn:
		if d := needsPerson(a, CodeAccountNeedsWeb, onWeb, "removing a way to sign in"); d != nil {
			return *d
		}
		return applyChecked(a)
	case AccountForget:
		if d := needsPerson(a, CodeAccountNeedsWeb,
			"Forget your account on memax.app, in Settings › Account, so an agent using this login can't.",
			"forgetting your account"); d != nil {
			return *d
		}
		return applyChecked(a)
	}
	return refuse(CodeUnknownAction, "Memax doesn't know how to do that with an account.")
}
