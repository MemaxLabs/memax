package policy

// Device sign-in (RFC 8628; plan 25 §5.15, internal/deviceauth): a person
// confirms, on the web, the code the memax CLI shows on a machine with no
// browser, and the CLI gets that person's session. A code isn't part of
// any space's record, so confirming one writes no receipt; like spaces
// and agent connections, it is a person's decision, never an agent's.

// The device decision codes; all refusals.
const (
	CodeDeviceByPerson = "device_by_person" // agents and API keys don't sign devices in
	CodeDeviceNeedsWeb = "device_needs_web" // confirming or declining a code needs a person on the web
)

// DeviceAction is what a person does with a device's code.
type DeviceAction string

const (
	// DeviceLookup reads what the device says about itself.
	DeviceLookup DeviceAction = "lookup"
	// DeviceApprove signs the device in as the person.
	DeviceApprove DeviceAction = "approve"
	// DeviceDeny declines the code.
	DeviceDeny DeviceAction = "deny"
)

// DecideDevice decides whether the actor may act on a device's code.
//
//   - Only a signed-in person, on a session (not an API key, an OAuth
//     grant or an agent token): a code signs a person in.
//   - Confirming or declining needs that person on the web app (assurance
//     human_web). A confirmed code mints a new CLI session, so an agent
//     holding the person's CLI login could otherwise sign more machines
//     in with no one looking. Reading a code's details needs only the
//     session, so the page can say who is asking before it asks the
//     person to sign in again.
func DecideDevice(a Actor, act DeviceAction) Decision {
	if a.Kind != ActorPerson || a.Credential != CredentialSession {
		return refuse(CodeDeviceByPerson,
			"Only a person signs a device in. Open the link from your terminal in your browser, signed in to Memax.")
	}
	switch act {
	case DeviceLookup:
		return apply()
	case DeviceApprove, DeviceDeny:
		if !a.Assurance().AtLeast(AssuranceHumanWeb) {
			return refuse(CodeDeviceNeedsWeb,
				"Confirm this code on memax.app/device, signed in on the web, so an agent can't sign a device in for you.")
		}
		return apply()
	}
	return refuse(CodeUnknownAction, "Memax doesn't know how to do that with a device's code.")
}
