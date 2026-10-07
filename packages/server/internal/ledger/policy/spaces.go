package policy

import "fmt"

// Spaces (plan 25 §5.4): who may create a space, and switch an empty one
// to the V2 record. A space isn't part of its own record, so neither
// writes a receipt; both are people's decisions, never an agent's.

// MaxOwnedProjectSpaces is how many project spaces one person may own. It
// is a fair-use limit during the free alpha, not a plan limit (D9 plan
// limits wait for V2 billing, as the Ask limit does).
const MaxOwnedProjectSpaces = 50

// The space decision codes; all refusals.
const (
	CodeSpaceByPerson = "space_by_person" // agents and API keys don't create or switch spaces
	CodeSpaceKind     = "space_kind"      // only project spaces are created here
	CodeSpaceLimit    = "space_limit"     // MaxOwnedProjectSpaces reached
	CodeSwitchByOwner = "switch_by_owner" // only a space's owner switches it
)

// DecideCreateSpace decides whether the actor may create a space of this
// kind, owning `owned` project spaces already. Only a signed-in person
// creates spaces, and here only project spaces: each person has one
// personal space, made at sign-up, and team spaces come with Team (Phase
// 4).
func DecideCreateSpace(a Actor, kind SpaceKind, owned int) Decision {
	if a.Kind != ActorPerson || a.Credential != CredentialSession {
		return refuse(CodeSpaceByPerson, "Only a person creates spaces. Sign in with memax login, or create it on the web.")
	}
	if kind != SpaceProject {
		return refuse(CodeSpaceKind, "Only project spaces can be created here. Your personal space already exists, and team spaces come with Team.")
	}
	if owned >= MaxOwnedProjectSpaces {
		return refuse(CodeSpaceLimit, fmt.Sprintf(
			"You own %d project spaces, the most one person can during the alpha. Use one of them with --space.", MaxOwnedProjectSpaces))
	}
	return apply()
}

// DecideSwitchSpace decides whether the actor may switch a space to the
// V2 record. Only its owner may, as a signed-in person.
func DecideSwitchSpace(a Actor) Decision {
	if a.Kind != ActorPerson || a.Credential != CredentialSession {
		return refuse(CodeSpaceByPerson, "Only a person switches a space to the V2 record.")
	}
	if a.Role != RoleOwner {
		return refuse(CodeSwitchByOwner, "Only the space's owner can switch it to the V2 record. Ask them.")
	}
	return apply()
}
