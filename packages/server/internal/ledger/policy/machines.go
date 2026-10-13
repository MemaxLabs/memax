package policy

import "fmt"

// Machines (personal-layer design §5.2): the computers a person links to
// their Personal space, each with its own home files. A personal fact
// written into a machine's home folder loads in every session of every
// agent there, so linking a machine, changing what it holds and reporting
// what it wrote are its person's alone: never an agent's or an API key's,
// and never another member's.

// The machine decision codes; all refusals.
const (
	CodeMachinesByPerson = "machines_by_person" // agents and API keys don't link machines or write home files
	CodeMachinesPersonal = "machines_personal"  // only a personal space is linked to machines
	CodeMachinesByOwner  = "machines_by_owner"  // only the personal space's owner
)

// MachineAction is what a person does with a machine.
type MachineAction string

const (
	// MachineList reads the space's machines.
	MachineList MachineAction = "list"
	// MachineLink links a machine (or links it again), with its files.
	MachineLink MachineAction = "link"
	// MachineConfigure adds, changes or stops one of a machine's files,
	// renames the machine, or settles a hand edit there.
	MachineConfigure MachineAction = "configure"
	// MachineUnlink takes the personal space off the machine.
	MachineUnlink MachineAction = "unlink"
	// MachineReport reports what a machine's daemon wrote or saw.
	MachineReport MachineAction = "report"
)

// DecideMachine decides whether the actor may act on a personal space's
// machines.
//
//   - Only a signed-in person, on a session (the web app or their CLI):
//     an agent holding an API key or an OAuth grant could otherwise put
//     words into every agent's home files on the person's computers.
//   - Only in a personal space: a project's facts never land in a home
//     folder.
//   - Only the space's owner: a personal space is one person's.
//   - Linking and changing files only on V2 (CodeSpaceOnV1); unlinking,
//     reading and reporting work on a space still on V1.
//
// Any surface will do, the CLI included: linking is done at the machine,
// and a stolen CLI login can already read everything the link would write.
func DecideMachine(a Actor, act MachineAction, s Space) Decision {
	if a.Kind != ActorPerson || a.Credential != CredentialSession {
		return refuse(CodeMachinesByPerson,
			"Only you link a machine and choose what it holds. Run memax link --home on it, signed in with memax login.")
	}
	if s.Kind != SpacePersonal {
		return refuse(CodeMachinesPersonal, fmt.Sprintf(
			"%s isn't your Personal space. Only your Personal space is written into your agents' own files.", spaceName(s)))
	}
	if a.Role != RoleOwner {
		return refuse(CodeMachinesByOwner, fmt.Sprintf("Only the owner of %s links machines to it.", spaceName(s)))
	}
	if !s.OnV2 && !s.Switching && (act == MachineLink || act == MachineConfigure) {
		// Nothing compiles from a space still on V1 (Decide's onV1), so no
		// machine is linked to it and no machine's files change. Taking the
		// layer off still works, like disconnecting an agent, and so do
		// reads and the reports of what a machine already wrote.
		return refuseOnV1(a, ActionConfigureTarget, s)
	}
	switch act {
	case MachineList, MachineLink, MachineConfigure, MachineUnlink, MachineReport:
		return apply()
	}
	return refuse(CodeUnknownAction, "Memax doesn't know how to do that with a machine.")
}
