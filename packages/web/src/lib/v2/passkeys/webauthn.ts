/**
 * The browser's half of passkeys (WebAuthn Level 3): creating one, and
 * getting an assertion with one, from the JSON options /v2 serves
 * (PublicKeyCredentialCreationOptionsJSON and
 * PublicKeyCredentialRequestOptionsJSON), answered in the JSON a
 * credential's toJSON() gives (base64url throughout).
 *
 * No library: current browsers parse and serialise these forms themselves
 * (PublicKeyCredential.parseCreationOptionsFromJSON, toJSON); the fallback
 * below does the same base64url work for browsers that don't yet, which
 * is all @simplewebauthn/browser would add.
 */

export interface CredentialDescriptorJSON {
  type: "public-key";
  id: string;
  transports?: string[];
}

export interface CreationOptionsJSON {
  rp: { id: string; name: string };
  user: { id: string; name: string; displayName: string };
  challenge: string;
  pubKeyCredParams: { type: "public-key"; alg: number }[];
  timeout: number;
  excludeCredentials: CredentialDescriptorJSON[];
  authenticatorSelection: {
    residentKey: "required";
    requireResidentKey: boolean;
    userVerification: "required";
  };
  attestation: "none";
}

export interface RequestOptionsJSON {
  challenge: string;
  timeout: number;
  rpId: string;
  allowCredentials: CredentialDescriptorJSON[];
  userVerification: "required";
}

/** A credential's JSON: RegistrationResponseJSON or AuthenticationResponseJSON. */
export type CredentialJSON = Record<string, unknown> & {
  id: string;
  type: string;
};

/** Why a ceremony didn't finish, in the terms the page words. */
export type PasskeyProblem =
  /** No WebAuthn here (an old browser, or not a secure context). */
  | "unsupported"
  /** The person closed the prompt, it timed out, or no passkey matched. */
  | "cancelled"
  /** This authenticator already holds a passkey for the account. */
  | "exists"
  /** Anything else the browser threw. */
  | "failed";

export class PasskeyError extends Error {
  constructor(
    readonly problem: PasskeyProblem,
    cause?: unknown,
  ) {
    super(`passkey: ${problem}`);
    this.name = "PasskeyError";
    if (cause !== undefined) (this as { cause?: unknown }).cause = cause;
  }
}

/** Whether this browser can use passkeys at all. */
export function passkeysSupported(): boolean {
  return (
    typeof window !== "undefined" &&
    typeof window.PublicKeyCredential === "function" &&
    typeof navigator !== "undefined" &&
    typeof navigator.credentials?.create === "function"
  );
}

export function base64urlToBytes(value: string): Uint8Array<ArrayBuffer> {
  const base64 = value.replace(/-/g, "+").replace(/_/g, "/");
  const padded = base64 + "===".slice((base64.length + 3) % 4);
  const raw = atob(padded);
  const out = new Uint8Array(raw.length);
  for (let i = 0; i < raw.length; i++) out[i] = raw.charCodeAt(i);
  return out;
}

export function bytesToBase64url(value: ArrayBuffer | ArrayBufferView): string {
  const bytes =
    value instanceof ArrayBuffer
      ? new Uint8Array(value)
      : new Uint8Array(value.buffer, value.byteOffset, value.byteLength);
  let raw = "";
  for (const b of bytes) raw += String.fromCharCode(b);
  return btoa(raw).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

function descriptors(
  list: CredentialDescriptorJSON[] | undefined,
): PublicKeyCredentialDescriptor[] {
  return (list ?? []).map((d) => ({
    type: "public-key",
    id: base64urlToBytes(d.id),
    transports: d.transports as AuthenticatorTransport[] | undefined,
  }));
}

/** The browser's own JSON parsers, where it has them. */
type JSONParsers = {
  parseCreationOptionsFromJSON?: (
    o: unknown,
  ) => PublicKeyCredentialCreationOptions;
  parseRequestOptionsFromJSON?: (
    o: unknown,
  ) => PublicKeyCredentialRequestOptions;
};

export function creationOptions(
  o: CreationOptionsJSON,
): PublicKeyCredentialCreationOptions {
  const native = (globalThis.PublicKeyCredential as unknown as JSONParsers)
    ?.parseCreationOptionsFromJSON;
  if (native) return native(o);
  return {
    rp: o.rp,
    user: { ...o.user, id: base64urlToBytes(o.user.id) },
    challenge: base64urlToBytes(o.challenge),
    pubKeyCredParams: o.pubKeyCredParams,
    timeout: o.timeout,
    excludeCredentials: descriptors(o.excludeCredentials),
    authenticatorSelection: o.authenticatorSelection,
    attestation: o.attestation,
  };
}

export function requestOptions(
  o: RequestOptionsJSON,
): PublicKeyCredentialRequestOptions {
  const native = (globalThis.PublicKeyCredential as unknown as JSONParsers)
    ?.parseRequestOptionsFromJSON;
  if (native) return native(o);
  return {
    challenge: base64urlToBytes(o.challenge),
    timeout: o.timeout,
    rpId: o.rpId,
    allowCredentials: descriptors(o.allowCredentials),
    userVerification: o.userVerification,
  };
}

/** A credential as JSON: its own toJSON(), or the same by hand. */
export function credentialJSON(
  credential: PublicKeyCredential,
): CredentialJSON {
  const own = (credential as { toJSON?: () => unknown }).toJSON;
  if (typeof own === "function") {
    return own.call(credential) as CredentialJSON;
  }
  const r = credential.response;
  const response: Record<string, unknown> = {
    clientDataJSON: bytesToBase64url(r.clientDataJSON),
  };
  if ("attestationObject" in r) {
    const a = r as AuthenticatorAttestationResponse;
    response.attestationObject = bytesToBase64url(a.attestationObject);
    response.transports = a.getTransports?.() ?? [];
  } else {
    const a = r as AuthenticatorAssertionResponse;
    response.authenticatorData = bytesToBase64url(a.authenticatorData);
    response.signature = bytesToBase64url(a.signature);
    if (a.userHandle) response.userHandle = bytesToBase64url(a.userHandle);
  }
  return {
    id: credential.id,
    rawId: bytesToBase64url(credential.rawId),
    type: credential.type,
    response,
    authenticatorAttachment: credential.authenticatorAttachment ?? undefined,
    clientExtensionResults: credential.getClientExtensionResults?.() ?? {},
  };
}

function problemOf(err: unknown): PasskeyProblem {
  const name =
    err && typeof err === "object" && "name" in err
      ? String((err as { name: unknown }).name)
      : "";
  if (name === "NotAllowedError" || name === "AbortError") return "cancelled";
  if (name === "InvalidStateError") return "exists";
  if (name === "NotSupportedError" || name === "SecurityError") {
    return "unsupported";
  }
  return "failed";
}

/** Creates a passkey (navigator.credentials.create) and answers its JSON. */
export async function createPasskey(
  options: CreationOptionsJSON,
  signal?: AbortSignal,
): Promise<CredentialJSON> {
  if (!passkeysSupported()) throw new PasskeyError("unsupported");
  let credential: Credential | null;
  try {
    credential = await navigator.credentials.create({
      publicKey: creationOptions(options),
      signal,
    });
  } catch (err) {
    throw new PasskeyError(problemOf(err), err);
  }
  if (!credential) throw new PasskeyError("cancelled");
  return credentialJSON(credential as PublicKeyCredential);
}

/** Gets an assertion (navigator.credentials.get) and answers its JSON. */
export async function getPasskey(
  options: RequestOptionsJSON,
  signal?: AbortSignal,
): Promise<CredentialJSON> {
  if (!passkeysSupported()) throw new PasskeyError("unsupported");
  let credential: Credential | null;
  try {
    credential = await navigator.credentials.get({
      publicKey: requestOptions(options),
      signal,
    });
  } catch (err) {
    throw new PasskeyError(problemOf(err), err);
  }
  if (!credential) throw new PasskeyError("cancelled");
  return credentialJSON(credential as PublicKeyCredential);
}
