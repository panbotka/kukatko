import { beforeEach, describe, expect, it, vi } from 'vitest'

import {
  deletePasskey,
  fetchPasskeys,
  isPasskeySupported,
  PasskeyError,
  registerPasskey,
  signInWithPasskey,
} from './passkeys'

/**
 * These tests drive the real ceremony code against a fake authenticator: jsdom
 * has neither `navigator.credentials` nor `PublicKeyCredential`, so both are
 * installed here, and `fetch` answers the two halves of each ceremony.
 *
 * What they are really guarding is the wire format. The server verifies a
 * signature over bytes it decodes from base64url, so an id that arrives as
 * standard base64 (or padded, or re-encoded) turns a good authenticator answer
 * into a refused sign-in — a failure no amount of UI testing would catch.
 */

/** Bytes for a fixture buffer, as a real `ArrayBuffer`. */
function buffer(...bytes: number[]): ArrayBuffer {
  return new Uint8Array(bytes).buffer
}

/** Installs a fake `navigator.credentials` and the WebAuthn feature marker. */
function stubWebAuthn(credentials: Partial<CredentialsContainer>) {
  Object.defineProperty(window, 'PublicKeyCredential', {
    configurable: true,
    writable: true,
    value: function PublicKeyCredentialStub() {
      /* a feature marker; nothing constructs it */
    },
  })
  Object.defineProperty(navigator, 'credentials', {
    configurable: true,
    writable: true,
    value: credentials,
  })
}

/** Removes the WebAuthn marker, as an old browser or an insecure context has it. */
function stubNoWebAuthn() {
  Object.defineProperty(window, 'PublicKeyCredential', {
    configurable: true,
    writable: true,
    value: undefined,
  })
}

/** A JSON response with the given status and body. */
function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

/**
 * The registration options as the backend sends them: every binary field is
 * base64url text, `-` and `_` included, so the decoding is actually exercised.
 */
const CREATION_OPTIONS = {
  options: {
    publicKey: {
      challenge: 'w6_-8g',
      rp: { id: 'kukatko.test', name: 'Kukátko' },
      user: { id: 'dXNfYWJj', name: 'tomas', displayName: 'Tomáš' },
      pubKeyCredParams: [{ type: 'public-key', alg: -7 }],
      excludeCredentials: [{ type: 'public-key', id: 'a-b_cd', transports: ['internal'] }],
    },
  },
}

/** The sign-in options: discoverable, so there is no allow-list at all. */
const REQUEST_OPTIONS = {
  options: {
    publicKey: { challenge: 'w6_-8g', rpId: 'kukatko.test', userVerification: 'preferred' },
  },
}

/** A credential a fake authenticator returns from a registration ceremony. */
function attestationCredential() {
  return {
    id: 'Y3JlZC1pZA',
    rawId: buffer(0xff, 0x00, 0x10),
    type: 'public-key',
    authenticatorAttachment: 'platform',
    response: {
      clientDataJSON: buffer(0x7b, 0x7d),
      attestationObject: buffer(0x01, 0x02, 0x03),
      getTransports: () => ['internal', 'hybrid'],
    },
    getClientExtensionResults: () => ({}),
  }
}

/** A credential a fake authenticator returns from a sign-in ceremony. */
function assertionCredential() {
  return {
    id: 'Y3JlZC1pZA',
    rawId: buffer(0xff, 0x00, 0x10),
    type: 'public-key',
    response: {
      clientDataJSON: buffer(0x7b, 0x7d),
      authenticatorData: buffer(0x04, 0x05),
      signature: buffer(0x06, 0x07),
      userHandle: buffer(0x08),
    },
    getClientExtensionResults: () => ({}),
  }
}

/** The parsed JSON body of the n-th `fetch` call. */
function bodyOf(fetchMock: ReturnType<typeof vi.fn>, call: number): Record<string, unknown> {
  const init = fetchMock.mock.calls[call][1] as RequestInit
  return JSON.parse(init.body as string) as Record<string, unknown>
}

describe('isPasskeySupported', () => {
  it('is false in a browser without WebAuthn', () => {
    stubNoWebAuthn()
    expect(isPasskeySupported()).toBe(false)
  })

  it('is true once both halves of the API are present', () => {
    stubWebAuthn({ create: vi.fn(), get: vi.fn() })
    expect(isPasskeySupported()).toBe(true)
  })
})

describe('signInWithPasskey', () => {
  beforeEach(() => {
    stubWebAuthn({ create: vi.fn(), get: vi.fn() })
  })

  it('runs both halves and returns the session', async () => {
    const get = vi.fn().mockResolvedValue(assertionCredential())
    stubWebAuthn({ create: vi.fn(), get })
    const session = { user: { username: 'tomas' }, download_token: 'dl' }
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse(200, REQUEST_OPTIONS))
      .mockResolvedValueOnce(jsonResponse(200, session))
    vi.stubGlobal('fetch', fetchMock)

    await expect(signInWithPasskey()).resolves.toEqual(session)

    // The challenge reached the authenticator decoded, not as the text it arrived as.
    const options = get.mock.calls[0][0] as { publicKey: PublicKeyCredentialRequestOptions }
    expect([...new Uint8Array(options.publicKey.challenge as ArrayBuffer)]).toEqual([
      0xc3, 0xaf, 0xfe, 0xf2,
    ])
    // A discoverable ceremony names nobody: no allow-list travels to the browser.
    expect(options.publicKey.allowCredentials).toEqual([])

    // …and the answer goes back base64url-encoded, unpadded.
    const body = bodyOf(fetchMock, 1)
    const credential = body.credential as Record<string, unknown>
    expect(credential.rawId).toBe('_wAQ')
    expect(credential.response).toMatchObject({
      clientDataJSON: 'e30',
      authenticatorData: 'BAU',
      signature: 'Bgc',
      userHandle: 'CA',
    })
    // Both halves must carry the ceremony cookie, or the finish has nothing to spend.
    expect(fetchMock.mock.calls[0][1]).toMatchObject({ credentials: 'same-origin' })
    expect(fetchMock.mock.calls[1][1]).toMatchObject({ credentials: 'same-origin' })
  })

  it('reports a dismissed prompt as cancelled, and never finishes the ceremony', async () => {
    const notAllowed = new Error('The operation either timed out or was not allowed.')
    notAllowed.name = 'NotAllowedError'
    stubWebAuthn({ create: vi.fn(), get: vi.fn().mockRejectedValue(notAllowed) })
    const fetchMock = vi.fn().mockResolvedValueOnce(jsonResponse(200, REQUEST_OPTIONS))
    vi.stubGlobal('fetch', fetchMock)

    await expect(signInWithPasskey()).rejects.toMatchObject({ reason: 'cancelled' })
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('reports an authenticator that offered nothing as cancelled too', async () => {
    stubWebAuthn({ create: vi.fn(), get: vi.fn().mockResolvedValue(null) })
    vi.stubGlobal('fetch', vi.fn().mockResolvedValueOnce(jsonResponse(200, REQUEST_OPTIONS)))

    await expect(signInWithPasskey()).rejects.toMatchObject({ reason: 'cancelled' })
  })

  it('maps a refused answer, a waiting account and a spent budget', async () => {
    const cases: [number, string][] = [
      [401, 'refused'],
      [403, 'pendingApproval'],
      [429, 'rateLimited'],
    ]
    for (const [status, reason] of cases) {
      stubWebAuthn({ create: vi.fn(), get: vi.fn().mockResolvedValue(assertionCredential()) })
      vi.stubGlobal(
        'fetch',
        vi
          .fn()
          .mockResolvedValueOnce(jsonResponse(200, REQUEST_OPTIONS))
          .mockResolvedValueOnce(jsonResponse(status, { error: 'no' })),
      )
      await expect(signInWithPasskey()).rejects.toMatchObject({ reason })
    }
  })

  it('reports an instance with no passkeys configured as unavailable', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(jsonResponse(501, { error: 'not available' })))
    await expect(signInWithPasskey()).rejects.toMatchObject({ reason: 'unavailable' })
  })

  it('reports an unreachable server as offline, blaming no credential', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('Failed to fetch')))
    await expect(signInWithPasskey()).rejects.toMatchObject({ reason: 'offline' })
  })

  it('refuses to start where the browser has no WebAuthn', async () => {
    stubNoWebAuthn()
    const fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)

    await expect(signInWithPasskey()).rejects.toBeInstanceOf(PasskeyError)
    expect(fetchMock).not.toHaveBeenCalled()
  })
})

describe('registerPasskey', () => {
  it('sends the name with the credential and returns the stored passkey', async () => {
    const create = vi.fn().mockResolvedValue(attestationCredential())
    stubWebAuthn({ create, get: vi.fn() })
    const stored = {
      id: 'pk1',
      name: 'Tomášův telefon',
      transports: [],
      created_at: '2026-09-01T10:00:00Z',
    }
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse(200, CREATION_OPTIONS))
      .mockResolvedValueOnce(jsonResponse(201, stored))
    vi.stubGlobal('fetch', fetchMock)

    await expect(registerPasskey('Tomášův telefon')).resolves.toEqual(stored)

    // The exclusion list is what stops a second key for the same account, so its
    // ids have to reach the browser decoded.
    const options = create.mock.calls[0][0] as { publicKey: PublicKeyCredentialCreationOptions }
    expect([...new Uint8Array(options.publicKey.user.id as ArrayBuffer)]).toEqual([
      ...new TextEncoder().encode('us_abc'),
    ])
    expect(options.publicKey.excludeCredentials).toHaveLength(1)

    const body = bodyOf(fetchMock, 1)
    expect(body.name).toBe('Tomášův telefon')
    const credential = body.credential as { response: Record<string, unknown> }
    expect(credential.response).toMatchObject({
      clientDataJSON: 'e30',
      attestationObject: 'AQID',
      transports: ['internal', 'hybrid'],
    })
  })

  it('reports an authenticator that already holds a key as a duplicate', async () => {
    const invalidState = new Error('credential already registered')
    invalidState.name = 'InvalidStateError'
    stubWebAuthn({ create: vi.fn().mockRejectedValue(invalidState), get: vi.fn() })
    vi.stubGlobal('fetch', vi.fn().mockResolvedValueOnce(jsonResponse(200, CREATION_OPTIONS)))

    await expect(registerPasskey('phone')).rejects.toMatchObject({ reason: 'duplicate' })
  })

  it('reports the server-side duplicate (409) the same way', async () => {
    stubWebAuthn({ create: vi.fn().mockResolvedValue(attestationCredential()), get: vi.fn() })
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValueOnce(jsonResponse(200, CREATION_OPTIONS))
        .mockResolvedValueOnce(jsonResponse(409, { error: 'already registered' })),
    )

    await expect(registerPasskey('phone')).rejects.toMatchObject({ reason: 'duplicate' })
  })

  it('reports an answer that did not verify as refused', async () => {
    stubWebAuthn({ create: vi.fn().mockResolvedValue(attestationCredential()), get: vi.fn() })
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValueOnce(jsonResponse(200, CREATION_OPTIONS))
        .mockResolvedValueOnce(jsonResponse(400, { error: 'rejected' })),
    )

    await expect(registerPasskey('phone')).rejects.toMatchObject({ reason: 'refused' })
  })
})

describe('fetchPasskeys / deletePasskey', () => {
  it('unwraps the listing', async () => {
    const passkeys = [
      { id: 'pk1', name: 'phone', transports: [], created_at: '2026-09-01T10:00:00Z' },
    ]
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(jsonResponse(200, { passkeys })))

    await expect(fetchPasskeys()).resolves.toEqual(passkeys)
  })

  it('escapes the id and sends the session cookie', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(null, { status: 204 }))
    vi.stubGlobal('fetch', fetchMock)

    await deletePasskey('pk/1')

    expect(fetchMock).toHaveBeenCalledWith(
      '/api/v1/auth/passkeys/pk%2F1',
      expect.objectContaining({ method: 'DELETE', credentials: 'same-origin' }),
    )
  })

  it('turns a 501 listing into the unavailable reason', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(jsonResponse(501, { error: 'nope' })))
    await expect(fetchPasskeys()).rejects.toMatchObject({ reason: 'unavailable' })
  })
})
