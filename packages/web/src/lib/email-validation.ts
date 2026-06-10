// RFC 5322 is vast; we only need a defense-in-depth client check before
// hitting the server. The server is authoritative via net/mail.ParseAddress.
//
// This regex matches the HTML5 email input validation spec
// (https://html.spec.whatwg.org/multipage/input.html#valid-e-mail-address),
// which covers the practical cases: local@domain with TLD, hyphens,
// plus addresses, and dots in both local and domain parts. It deliberately
// rejects spaces, multiple @, and missing TLDs — the shapes users
// accidentally type.
const EMAIL_REGEX =
  /^[a-zA-Z0-9.!#$%&'*+/=?^_`{|}~-]+@[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(?:\.[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)+$/;

export function isValidEmail(value: string): boolean {
  const trimmed = value.trim();
  if (!trimmed) return false;
  if (trimmed.length > 254) return false;
  return EMAIL_REGEX.test(trimmed);
}
