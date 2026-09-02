import { type AuthSession } from './auth'

/** Base path all versioned backend endpoints share. */
const API_BASE = '/api/v1'

/**
 * Longest passkey name the backend accepts (`auth.MaxPasskeyNameLen`, counted in
 * runes). Mirrored here so the input stops at the limit instead of letting a long
 * name travel to the server only to come back a 400.
 */
export const PASSKEY_NAME_MAX_LENGTH = 64

/**
 * One passkey of the signed-in account, mirroring the backend `auth.PasskeyView`
 * JSON shape. It carries nothing of the credential itself beyond the transports
 * the authenticator announced — enough to tell a phone from a security key, and
 * never the public key or a counter.
 *
 * `name` may be empty: the backend accepts an unnamed key rather than inventing
 * one, so a reader has to have a fallback for it.
 */
export interface Passkey {
  id: string
  name: string
  transports: string[]
  created_at: string
  last_used_at?: string
}

/**
 * Why a passkey operation did not happen, in the vocabulary the interface needs
 * rather than the one the platform speaks. Every rejection out of this module is
 * one of these, so no `DOMException` message and no backend error string ever
 * reaches a reader: WebAuthn's own wording ("The operation either timed out or
 * was not allowed…") is unreadable, and a raw exception in a sign-in form is how
 * a cancelled prompt comes to look like a broken app.
 *
 * - `unsupported` — this browser has no WebAuthn at all.
 * - `unavailable` — the instance has no relying party configured (HTTP 501).
 * - `cancelled` — the prompt was dismissed, timed out, or the authenticator
 *   offered nothing to pick: `NotAllowedError`, which the platform deliberately
 *   does not tell apart, so the message must cover all three.
 * - `duplicate` — this authenticator already holds a key for the account.
 * - `refused` — the answer did not verify (or names no credential this instance
 *   knows). Nothing about which, on purpose.
 * - `pendingApproval` — the signature was good but the account is still waiting
 *   for an administrator.
 * - `rateLimited` — the per-address sign-in budget is spent.
 * - `offline` — the request never reached the server, so nothing judged it.
 * - `generic` — anything else.
 */
export type PasskeyErrorReason =
  | 'unsupported'
  | 'unavailable'
  | 'cancelled'
  | 'duplicate'
  | 'refused'
  | 'pendingApproval'
  | 'rateLimited'
  | 'offline'
  | 'generic'

/**
 * A failed passkey ceremony, carrying the {@link PasskeyErrorReason} the caller
 * translates. The `message` is for the console and for tests; nothing in the UI
 * prints it.
 */
export class PasskeyError extends Error {
  readonly reason: PasskeyErrorReason

  constructor(reason: PasskeyErrorReason, message?: string, options?: ErrorOptions) {
    super(message ?? reason, options)
    this.name = 'PasskeyError'
    this.reason = reason
  }
}

/**
 * Reports whether this browser can run a WebAuthn ceremony at all.
 *
 * Both halves matter: `PublicKeyCredential` is missing on a browser that never
 * had WebAuthn, while `navigator.credentials` is absent in an insecure context
 * (plain HTTP over a LAN address is exactly how this app gets opened at home), so
 * a check for one alone still throws on the other.
 */
export function isPasskeySupported(): boolean {
  if (typeof window === 'undefined' || typeof window.PublicKeyCredential === 'undefined') {
    return false
  }
  // The cast widens away a lie in the DOM typings: `navigator.credentials` is
  // declared as always present, and in an insecure context it simply is not.
  const credentials = navigator.credentials as CredentialsContainer | undefined
  return typeof credentials?.create === 'function' && typeof credentials.get === 'function'
}

/**
 * Decodes base64url (padded or not, and tolerating standard base64) into a plain
 * `ArrayBuffer` — not a `Uint8Array`, whose backing store TypeScript types as
 * possibly shared and therefore not a `BufferSource`.
 */
function fromBase64Url(value: string): ArrayBuffer {
  const padded = value.replace(/-/g, '+').replace(/_/g, '/')
  const binary = atob(padded.padEnd(Math.ceil(padded.length / 4) * 4, '='))
  const buffer = new ArrayBuffer(binary.length)
  const bytes = new Uint8Array(buffer)
  for (let i = 0; i < binary.length; i += 1) {
    bytes[i] = binary.charCodeAt(i)
  }
  return buffer
}

/**
 * Encodes bytes as unpadded base64url — the encoding every binary field of the
 * WebAuthn JSON wire format uses, and the one `protocol.URLEncodedBase64` on the
 * Go side decodes.
 */
function toBase64Url(buffer: ArrayBuffer): string {
  const bytes = new Uint8Array(buffer)
  let binary = ''
  for (const byte of bytes) {
    binary += String.fromCharCode(byte)
  }
  return btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '')
}

/** A credential descriptor as the server sends it: the id is base64url text. */
interface CredentialDescriptorJSON {
  type: string
  id: string
  transports?: string[]
}

/** The registration options as JSON, before the binary fields are decoded. */
interface CreationOptionsJSON {
  challenge: string
  rp: PublicKeyCredentialRpEntity
  user: { id: string; name: string; displayName: string }
  pubKeyCredParams: PublicKeyCredentialParameters[]
  timeout?: number
  excludeCredentials?: CredentialDescriptorJSON[]
  authenticatorSelection?: AuthenticatorSelectionCriteria
  attestation?: string
  extensions?: AuthenticationExtensionsClientInputs
}

/** The sign-in options as JSON, before the binary fields are decoded. */
interface RequestOptionsJSON {
  challenge: string
  timeout?: number
  rpId?: string
  allowCredentials?: CredentialDescriptorJSON[]
  userVerification?: string
  extensions?: AuthenticationExtensionsClientInputs
}

/** Body of both begin endpoints: the options object, nested as WebAuthn wants it. */
interface BeginResponse<T> {
  options: { publicKey: T }
}

/** Decodes the base64url ids of an exclude/allow list into the browser's shape. */
function toDescriptors(list?: CredentialDescriptorJSON[]): PublicKeyCredentialDescriptor[] {
  return (list ?? []).map((descriptor) => ({
    type: 'public-key',
    id: fromBase64Url(descriptor.id),
    transports: descriptor.transports as AuthenticatorTransport[] | undefined,
  }))
}

/**
 * Turns the server's registration options into the object
 * `navigator.credentials.create` takes.
 *
 * The conversion is done by hand rather than through
 * `PublicKeyCredential.parseCreationOptionsFromJSON`, which is too new to rely on
 * (and absent from the test environment): only three fields are binary, and
 * everything else is passed through untouched, so an option the server starts
 * sending later reaches the browser without a change here.
 */
function toCreationOptions(json: CreationOptionsJSON): PublicKeyCredentialCreationOptions {
  const { challenge, user, excludeCredentials, attestation, ...rest } = json
  return {
    ...rest,
    challenge: fromBase64Url(challenge),
    user: { ...user, id: fromBase64Url(user.id) },
    excludeCredentials: toDescriptors(excludeCredentials),
    attestation: attestation as AttestationConveyancePreference | undefined,
  }
}

/**
 * Turns the server's sign-in options into the object `navigator.credentials.get`
 * takes. A discoverable ("usernameless") ceremony sends no allow-list, which is
 * what lets the authenticator decide whose account this is.
 */
function toRequestOptions(json: RequestOptionsJSON): PublicKeyCredentialRequestOptions {
  const { challenge, allowCredentials, userVerification, ...rest } = json
  return {
    ...rest,
    challenge: fromBase64Url(challenge),
    allowCredentials: toDescriptors(allowCredentials),
    userVerification: userVerification as UserVerificationRequirement | undefined,
  }
}

/** The attestation half of a freshly created credential. */
interface AttestationResponseLike {
  clientDataJSON: ArrayBuffer
  attestationObject: ArrayBuffer
  getTransports?: () => string[]
}

/** The assertion half of a credential returned by a sign-in. */
interface AssertionResponseLike {
  clientDataJSON: ArrayBuffer
  authenticatorData: ArrayBuffer
  signature: ArrayBuffer
  userHandle?: ArrayBuffer | null
}

/**
 * What this module needs of a `PublicKeyCredential`. It is structural on purpose:
 * the ceremony is driven through whatever `navigator.credentials` returns, which
 * in a test is a plain object rather than a platform class, so nothing here may
 * depend on `instanceof`.
 */
interface CredentialLike {
  id: string
  rawId: ArrayBuffer
  type: string
  authenticatorAttachment?: string | null
  response: AttestationResponseLike | AssertionResponseLike
  getClientExtensionResults?: () => AuthenticationExtensionsClientOutputs
}

/** True when the credential carries a registration answer rather than a sign-in one. */
function isAttestation(
  response: AttestationResponseLike | AssertionResponseLike,
): response is AttestationResponseLike {
  return 'attestationObject' in response
}

/**
 * Serializes a credential into the JSON the backend's WebAuthn parser expects.
 *
 * It is built field by field rather than via `credential.toJSON()` (again too new
 * to rely on) — and it must stay byte-exact, because what the server verifies is
 * the signature over `clientDataJSON` and the authenticator data: reshaping or
 * re-encoding either one turns a good answer into a refused one.
 */
function serializeCredential(credential: CredentialLike): unknown {
  const base = {
    id: credential.id,
    rawId: toBase64Url(credential.rawId),
    type: credential.type,
    authenticatorAttachment: credential.authenticatorAttachment ?? undefined,
    clientExtensionResults: credential.getClientExtensionResults?.() ?? {},
  }
  const response = credential.response
  if (isAttestation(response)) {
    return {
      ...base,
      response: {
        clientDataJSON: toBase64Url(response.clientDataJSON),
        attestationObject: toBase64Url(response.attestationObject),
        transports: response.getTransports?.() ?? [],
      },
    }
  }
  return {
    ...base,
    response: {
      clientDataJSON: toBase64Url(response.clientDataJSON),
      authenticatorData: toBase64Url(response.authenticatorData),
      signature: toBase64Url(response.signature),
      userHandle:
        response.userHandle === undefined || response.userHandle === null
          ? undefined
          : toBase64Url(response.userHandle),
    },
  }
}

/** The reasons every passkey endpoint shares, whatever the ceremony. */
function sharedReason(status: number): PasskeyErrorReason {
  if (status === 501) {
    return 'unavailable'
  }
  if (status === 429) {
    return 'rateLimited'
  }
  return 'generic'
}

/**
 * Performs one call against the passkey API and rejects with a
 * {@link PasskeyError} for anything that is not a 2xx.
 *
 * Every request carries the cookies: the session for the two account-side
 * endpoints, and — for all four ceremony halves — the HttpOnly cookie naming the
 * challenge the server is holding. Without it the finish half has no ceremony to
 * spend and refuses a perfectly good signature.
 *
 * @param map turns a status this caller cares about into its reason; anything it
 *   does not name falls through to {@link sharedReason}.
 */
async function passkeyFetch(
  path: string,
  init: RequestInit,
  map: Partial<Record<number, PasskeyErrorReason>> = {},
): Promise<Response> {
  let res: Response
  try {
    res = await fetch(`${API_BASE}${path}`, { credentials: 'same-origin', ...init })
  } catch (error: unknown) {
    throw new PasskeyError('offline', 'the server could not be reached', { cause: error })
  }
  if (res.ok) {
    return res
  }
  throw new PasskeyError(map[res.status] ?? sharedReason(res.status), `passkey: ${res.status}`)
}

/**
 * Runs a WebAuthn ceremony, translating what the platform throws.
 *
 * `NotAllowedError` is the one that matters: the browser answers a dismissed
 * prompt, an expired one and an authenticator with nothing to offer with exactly
 * that, on purpose (telling them apart would say whether an account has a key).
 * So it becomes `cancelled`, and the sentence the reader sees has to cover all
 * three. `InvalidStateError` is the other named one — the authenticator already
 * holds a key for this account, which is a fact, not a failure.
 */
async function runCeremony(ceremony: () => Promise<Credential | null>): Promise<CredentialLike> {
  let credential: Credential | null
  try {
    credential = await ceremony()
  } catch (error: unknown) {
    if (error instanceof Error && error.name === 'InvalidStateError') {
      throw new PasskeyError('duplicate', error.message, { cause: error })
    }
    if (
      error instanceof Error &&
      (error.name === 'NotAllowedError' || error.name === 'AbortError')
    ) {
      throw new PasskeyError('cancelled', error.message, { cause: error })
    }
    throw new PasskeyError('generic', error instanceof Error ? error.message : 'ceremony failed', {
      cause: error,
    })
  }
  if (credential === null) {
    // The spec allows a null resolution instead of a rejection; a reader who saw
    // no prompt and got no key has cancelled, as far as this app is concerned.
    throw new PasskeyError('cancelled', 'the ceremony returned no credential')
  }
  return credential as unknown as CredentialLike
}

/** Response body of `GET /api/v1/auth/passkeys`. */
interface PasskeyListResponse {
  passkeys: Passkey[]
}

/**
 * Lists the signed-in account's own passkeys, newest first. The backend scopes
 * the listing to the caller, so this never sends an owner.
 *
 * @throws PasskeyError with `reason` `unavailable` on an instance that has none
 *   configured, `offline`, or `generic`.
 */
export async function fetchPasskeys(signal?: AbortSignal): Promise<Passkey[]> {
  const res = await passkeyFetch('/auth/passkeys', { method: 'GET', signal })
  return ((await res.json()) as PasskeyListResponse).passkeys
}

/**
 * Adds a passkey to the signed-in account: asks the server for the creation
 * options, has the authenticator mint a credential, and stores the answer under
 * `name` (which may be empty — the backend allows it).
 *
 * The two halves must stay one call: the ceremony cookie the begin half sets is
 * spent by the finish half exactly once, whether it verifies or not, so there is
 * no way to retry the second half on its own.
 *
 * @throws PasskeyError — `unsupported`, `unavailable`, `cancelled`, `duplicate`
 *   (this authenticator already holds a key for the account, whether the browser
 *   or the server noticed), `refused`, `offline` or `generic`.
 */
export async function registerPasskey(name: string, signal?: AbortSignal): Promise<Passkey> {
  if (!isPasskeySupported()) {
    throw new PasskeyError('unsupported', 'this browser has no WebAuthn')
  }
  const begin = await passkeyFetch('/auth/passkeys/register/begin', { method: 'POST', signal })
  const { options } = (await begin.json()) as BeginResponse<CreationOptionsJSON>
  const credential = await runCeremony(() =>
    navigator.credentials.create({ publicKey: toCreationOptions(options.publicKey), signal }),
  )
  const finish = await passkeyFetch(
    '/auth/passkeys/register/finish',
    {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name, credential: serializeCredential(credential) }),
      signal,
    },
    { 409: 'duplicate', 400: 'refused' },
  )
  return (await finish.json()) as Passkey
}

/**
 * Signs in with a passkey: a discoverable ceremony names no account, so which one
 * is being entered is decided by the credential the authenticator offers. On
 * success the backend has set the same sliding session cookie a password login
 * sets, and the session it returns is the same shape.
 *
 * @throws PasskeyError — `unsupported`, `unavailable`, `cancelled` (dismissed, or
 *   no passkey for this site on the device), `refused`, `pendingApproval`,
 *   `rateLimited`, `offline` or `generic`.
 */
export async function signInWithPasskey(signal?: AbortSignal): Promise<AuthSession> {
  if (!isPasskeySupported()) {
    throw new PasskeyError('unsupported', 'this browser has no WebAuthn')
  }
  const begin = await passkeyFetch('/auth/passkeys/login/begin', { method: 'POST', signal })
  const { options } = (await begin.json()) as BeginResponse<RequestOptionsJSON>
  const credential = await runCeremony(() =>
    navigator.credentials.get({ publicKey: toRequestOptions(options.publicKey), signal }),
  )
  const finish = await passkeyFetch(
    '/auth/passkeys/login/finish',
    {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ credential: serializeCredential(credential) }),
      signal,
    },
    { 401: 'refused', 403: 'pendingApproval' },
  )
  return (await finish.json()) as AuthSession
}

/**
 * Removes one of the signed-in account's passkeys. Removing the last one is
 * allowed — the password never stopped working — and somebody else's is a
 * `generic` failure behind the backend's 404, never a forbidden one.
 *
 * @throws PasskeyError with `reason` `unavailable`, `offline` or `generic`.
 */
export async function deletePasskey(id: string, signal?: AbortSignal): Promise<void> {
  await passkeyFetch(`/auth/passkeys/${encodeURIComponent(id)}`, { method: 'DELETE', signal })
}
