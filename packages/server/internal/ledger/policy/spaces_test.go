package policy

import "testing"

// TestDecideCreateSpace: only a person on a session creates a space, and
// only a project space; each refusal has its own code, so the web words
// NewSpace's answer (records-copy).
func TestDecideCreateSpace(t *testing.T) {
	session := with(person(RoleOwner, ViaWeb), func(a *Actor) { a.Credential = CredentialSession })
	key := with(agent(RoleOwner, AutonomyWrite), func(a *Actor) { a.Credential = CredentialAPIKey })
	for _, c := range []struct {
		name  string
		actor Actor
		kind  SpaceKind
		owned int
		code  string
	}{
		{"a person makes a project space", session, SpaceProject, 0, ""},
		{"up to the alpha's limit", session, SpaceProject, MaxOwnedProjectSpaces - 1, ""},
		{"not past it", session, SpaceProject, MaxOwnedProjectSpaces, CodeSpaceLimit},
		{"a personal space exists already", session, SpacePersonal, 0, CodePersonalExists},
		{"team spaces come with Team", session, SpaceTeam, 0, CodeSpaceKind},
		{"an agent never makes one", key, SpaceProject, 0, CodeSpaceByPerson},
		{"nor a person's API key", with(session, func(a *Actor) { a.Credential = CredentialAPIKey }), SpaceProject, 0, CodeSpaceByPerson},
	} {
		t.Run(c.name, func(t *testing.T) {
			d := DecideCreateSpace(c.actor, c.kind, c.owned)
			if c.code == "" {
				if d.Effect != EffectApply {
					t.Fatalf("refused: %+v", d)
				}
				return
			}
			if d.Effect != EffectRefuse || d.Code != c.code || d.Message == "" {
				t.Fatalf("got %+v, want a refusal %s", d, c.code)
			}
		})
	}
}
