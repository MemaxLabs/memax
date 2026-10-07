package policy

// Sessions (internal/sessions; plan 25 §5.15): everywhere a person is
// signed in. Like device codes they aren't part of any space's record, so
// signing one out writes no receipt; and like device codes, managing them
// is a person's decision, never an agent's.

// The session decision codes; all refusals.
const (
	CodeSessionByPerson = "session_by_person" // agents and API keys don't list or sign out sessions
	CodeSessionNeedsWeb = "session_needs_web" // signing out another session needs a person on the web
)

// SessionAction is what a person does with their sessions.
type SessionAction string

const (
	// SessionList reads where the person is signed in.
	SessionList SessionAction = "list"
	// SessionRevokeOwn signs out the session the request comes from.
	SessionRevokeOwn SessionAction = "revoke_own"
	// SessionRevoke signs out another of the person's sessions, or all of
	// them but this one.
	SessionRevoke SessionAction = "revoke"
)

// DecideSession decides whether the actor may act on their sessions.
//
//   - Only a signed-in person, on a session (not an API key, an OAuth grant
//     or an agent token): sessions are the person's.
//   - Listing and signing out the session in hand need only that.
//   - Signing out any other session needs the person on the web app
//     (assurance human_web): an agent holding the person's CLI login could
//     otherwise sign the person out of the web and fight them for the
//     account. A local agent can still sign its own session out.
func DecideSession(a Actor, act SessionAction) Decision {
	if a.Kind != ActorPerson || a.Credential != CredentialSession {
		return refuse(CodeSessionByPerson,
			"Only a person manages where they are signed in. Open Settings on memax.app, signed in.")
	}
	switch act {
	case SessionList, SessionRevokeOwn:
		return apply()
	case SessionRevoke:
		if a.Assurance() != AssuranceHumanWeb {
			return refuse(CodeSessionNeedsWeb,
				"Sign other sessions out on memax.app, in Settings, so an agent using this login can't sign you out elsewhere.")
		}
		return apply()
	}
	return refuse(CodeUnknownAction, "Memax doesn't know how to do that with a session.")
}
