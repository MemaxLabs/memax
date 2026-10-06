package policy

// Trust is the class of a source, and of a memory: a memory's trust is
// the minimum over its sources (plan 25 §5.6). Merging, summarising or
// rewriting can't raise it; only the classes of the inputs count.
type Trust string

// The trust classes, highest first. The order follows the plan's list
// (person, agent_own_work, repository, external).
const (
	TrustPerson       Trust = "person"
	TrustAgentOwnWork Trust = "agent_own_work"
	TrustRepository   Trust = "repository"
	TrustExternal     Trust = "external"
)

// Trusts lists the classes, highest first.
var Trusts = []Trust{TrustPerson, TrustAgentOwnWork, TrustRepository, TrustExternal}

// rank orders the classes; unknown values rank below external so they
// can never raise a minimum.
func (t Trust) rank() int {
	switch t {
	case TrustPerson:
		return 3
	case TrustAgentOwnWork:
		return 2
	case TrustRepository:
		return 1
	case TrustExternal:
		return 0
	}
	return -1
}

// Valid reports whether t is a known class.
func (t Trust) Valid() bool { return t.rank() >= 0 }

// External reports whether content of this class is quarantined: it
// shows the ochre notice and an agent can't keep it.
func (t Trust) External() bool { return t == TrustExternal || !t.Valid() }

// MinTrust returns the lowest class among ts. Unknown classes count as
// external. With no inputs it returns "".
func MinTrust(ts ...Trust) Trust {
	var low Trust
	for i, t := range ts {
		if !t.Valid() {
			t = TrustExternal
		}
		if i == 0 || t.rank() < low.rank() {
			low = t
		}
	}
	return low
}

// ActorTrust is the class of what an actor writes on its own authority,
// before any cited source: a person's own words, an agent's own work,
// the repository. Dream and Memax only rearrange what others wrote, so
// they get no more than an agent's own work. Content arriving through an
// integration (email, Slack, GitHub comments) is external whoever the
// actor is.
func ActorTrust(kind ActorKind, via Via) Trust {
	if via.Integration() {
		return TrustExternal
	}
	switch kind {
	case ActorPerson:
		return TrustPerson
	case ActorRepository:
		return TrustRepository
	case ActorAgent, ActorDream, ActorMemax:
		return TrustAgentOwnWork
	}
	return TrustExternal
}
