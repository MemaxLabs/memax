package passkeys

import "github.com/google/uuid"

// providers names the passkey providers people use most, by AAGUID (from
// the community list at github.com/passkeydeveloper/passkey-authenticator-aaguids).
// Memax asks for no attestation, so an AAGUID is the authenticator's own
// claim: it names a passkey in Settings, and decides nothing.
var providers = map[uuid.UUID]string{
	uuid.MustParse("fbfc3007-154e-4ecc-8c0b-6e020557d7bd"): "iCloud Keychain",
	uuid.MustParse("dd4ec289-e01d-41c9-bb89-70fa845d4bf2"): "iCloud Keychain",
	uuid.MustParse("ea9b8d66-4d01-1d21-3ce4-b6b48cb575d4"): "Google Password Manager",
	uuid.MustParse("adce0002-35bc-c60a-648b-0b25f1f05503"): "Chrome on Mac",
	uuid.MustParse("08987058-cadc-4b81-b6e1-30de50dcbe96"): "Windows Hello",
	uuid.MustParse("9ddd1817-af5a-4672-a2b9-3e3dd95000a9"): "Windows Hello",
	uuid.MustParse("6028b017-b1d4-4c02-b4b3-afcdafc96bb2"): "Windows Hello",
	uuid.MustParse("bada5566-a7aa-401f-bd96-45619a55120d"): "1Password",
	uuid.MustParse("d548826e-79b4-db40-a3d8-11116f7e8349"): "Bitwarden",
	uuid.MustParse("531126d6-e717-415c-9320-3d9aa6981239"): "Dashlane",
	uuid.MustParse("53414d53-554e-4700-0000-000000000000"): "Samsung Pass",
	uuid.MustParse("b84e4048-15dc-4dd0-8640-f4f60813c8af"): "NordPass",
	uuid.MustParse("0ea242b4-43c4-4a1b-8b17-dd6d0b6baec6"): "Keeper",
	uuid.MustParse("f3809540-7f14-49c1-a8b3-8f813b225541"): "Enpass",
}

// ProviderName is the provider an AAGUID names, or "".
func ProviderName(aaguid uuid.UUID) string { return providers[aaguid] }
