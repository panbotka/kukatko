import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import {
  decodeBase64Url,
  getPushPermission,
  getPushState,
  isIosOutsideInstalledApp,
  isPushEnabled,
  isPushSupported,
  requestPushPermission,
  subscribeToPush,
  unsubscribeFromPush,
} from './push'

/** A VAPID public key as the server hands it out: an uncompressed P-256 point, base64url. */
const KEY_BYTES = Uint8Array.from({ length: 65 }, (_, i) => (i === 0 ? 4 : i))
const PUBLIC_KEY = btoa(String.fromCharCode(...KEY_BYTES))
  .replace(/\+/g, '-')
  .replace(/\//g, '_')
  .replace(/=+$/, '')

/** Another instance's key, for a subscription made before a key rotation. */
const OTHER_KEY_BYTES = Uint8Array.from({ length: 65 }, (_, i) => (i === 0 ? 4 : 255 - i))

const ENDPOINT = 'https://push.example.test/send/abc+def'

/** A PushSubscription: its endpoint, the key it was made with, and unsubscribe/toJSON. */
class FakeSubscription {
  readonly unsubscribe = vi.fn(() => Promise.resolve(true))
  readonly endpoint: string
  readonly options: { applicationServerKey: ArrayBuffer | null }

  constructor(endpoint: string, key: Uint8Array) {
    this.endpoint = endpoint
    this.options = { applicationServerKey: key.slice().buffer }
  }

  toJSON() {
    return { endpoint: this.endpoint, expirationTime: null, keys: { p256dh: 'p256', auth: 'auth' } }
  }
}

/** The registration's pushManager: holds at most one subscription, like a browser. */
class FakePushManager {
  current: FakeSubscription | null = null
  readonly getSubscription = vi.fn(() => Promise.resolve(this.current))
  readonly subscribe = vi.fn(
    (options: { userVisibleOnly: boolean; applicationServerKey: Uint8Array }) => {
      this.current = new FakeSubscription(ENDPOINT, options.applicationServerKey)
      return Promise.resolve(this.current)
    },
  )
}

let pushManager: FakePushManager
let registration: { pushManager: FakePushManager } | undefined
let getRegistration: ReturnType<typeof vi.fn>
let permission: NotificationPermission
let requestPermission: ReturnType<typeof vi.fn>
let fetchMock: ReturnType<typeof vi.fn>

/** Installs every API push needs: worker container, PushManager, Notification. */
function withPushSupport(): void {
  Object.defineProperty(navigator, 'serviceWorker', {
    value: { getRegistration },
    configurable: true,
    writable: true,
  })
  vi.stubGlobal('PushManager', {})
  vi.stubGlobal('Notification', {
    get permission() {
      return permission
    },
    requestPermission,
  })
}

/** A JSON response. */
function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

/** What each route answers: a response, or an error to reject the request with. */
interface Routes {
  config?: Response | Error
  register?: Response | Error
  remove?: Response | Error
}

/**
 * Routes the module's requests: the push config (push on, with {@link PUBLIC_KEY}
 * unless overridden) and the subscription POST/DELETE.
 */
function serve({
  config = json({ enabled: true, public_key: PUBLIC_KEY }),
  register = new Response(null, { status: 200 }),
  remove = new Response(null, { status: 204 }),
}: Routes = {}): void {
  fetchMock.mockImplementation((url: string, init?: RequestInit) => {
    const answer =
      url === '/api/v1/push/config' ? config : init?.method === 'DELETE' ? remove : register
    return answer instanceof Error ? Promise.reject(answer) : Promise.resolve(answer)
  })
}

/** The calls made to one method of the subscription routes. */
function callsTo(method: string): [string, RequestInit][] {
  return (fetchMock.mock.calls as [string, RequestInit | undefined][]).filter(
    (call): call is [string, RequestInit] => call[1]?.method === method,
  )
}

beforeEach(() => {
  pushManager = new FakePushManager()
  registration = { pushManager }
  getRegistration = vi.fn(() => Promise.resolve(registration))
  permission = 'granted'
  requestPermission = vi.fn(() => Promise.resolve('granted'))
  fetchMock = vi.fn()
  vi.stubGlobal('fetch', fetchMock)
  serve()
  withPushSupport()
})

afterEach(() => {
  Reflect.deleteProperty(navigator, 'serviceWorker')
  vi.unstubAllGlobals()
})

describe('isPushSupported and getPushPermission', () => {
  it('reports a browser with a worker container, PushManager and Notification as supported', () => {
    expect(isPushSupported()).toBe(true)
    expect(getPushPermission()).toBe('granted')
  })

  it('reports a browser without service workers as unsupported', () => {
    Reflect.deleteProperty(navigator, 'serviceWorker')

    expect(isPushSupported()).toBe(false)
    expect(getPushPermission()).toBe('unsupported')
  })

  it('reports mobile Safari outside an installed app (no PushManager) as unsupported', () => {
    vi.stubGlobal('PushManager', undefined)
    Reflect.deleteProperty(window, 'PushManager')

    expect(isPushSupported()).toBe(false)
  })

  it.each(['default', 'denied'] as const)('passes the %s permission through', (value) => {
    permission = value

    expect(getPushPermission()).toBe(value)
  })
})

describe('requestPushPermission', () => {
  it('resolves with the answer to the prompt', async () => {
    permission = 'default'
    requestPermission.mockResolvedValueOnce('denied')

    await expect(requestPushPermission()).resolves.toBe('denied')
    expect(requestPermission).toHaveBeenCalledTimes(1)
  })

  it('does not prompt in a browser that cannot push', async () => {
    Reflect.deleteProperty(navigator, 'serviceWorker')

    await expect(requestPushPermission()).resolves.toBe('unsupported')
    expect(requestPermission).not.toHaveBeenCalled()
  })

  it('reports the permission as it stands when the prompt itself fails', async () => {
    permission = 'default'
    requestPermission.mockRejectedValueOnce(new Error('no user gesture'))

    await expect(requestPushPermission()).resolves.toBe('default')
  })
})

describe('getPushState', () => {
  it('reports the subscription this browser holds', async () => {
    pushManager.current = new FakeSubscription(ENDPOINT, KEY_BYTES)

    await expect(getPushState()).resolves.toEqual({ status: 'subscribed', endpoint: ENDPOINT })
  })

  it.each([
    ['granted', 'unsubscribed'],
    ['default', 'prompt'],
    ['denied', 'denied'],
  ] as const)('reports a %s permission without a subscription as %s', async (value, status) => {
    permission = value

    await expect(getPushState()).resolves.toEqual({ status, endpoint: null })
  })

  it('reports unsupported without touching anything', async () => {
    Reflect.deleteProperty(navigator, 'serviceWorker')

    await expect(getPushState()).resolves.toEqual({ status: 'unsupported', endpoint: null })
  })

  it('reports a page without a worker registration as no-worker', async () => {
    registration = undefined

    await expect(getPushState()).resolves.toEqual({ status: 'no-worker', endpoint: null })
  })

  it('reports no-worker when looking the registration up fails', async () => {
    getRegistration.mockRejectedValueOnce(new Error('insecure'))

    await expect(getPushState()).resolves.toMatchObject({ status: 'no-worker' })
  })

  it('reports failed when the push manager cannot be read', async () => {
    pushManager.getSubscription.mockRejectedValueOnce(new Error('broken'))

    await expect(getPushState()).resolves.toMatchObject({ status: 'failed' })
  })

  it('never asks the server', async () => {
    await getPushState()

    expect(fetchMock).not.toHaveBeenCalled()
  })
})

describe('subscribeToPush', () => {
  it('subscribes with the server key and registers the subscription with the server', async () => {
    await expect(subscribeToPush()).resolves.toEqual({ status: 'subscribed', endpoint: ENDPOINT })

    expect(pushManager.subscribe).toHaveBeenCalledTimes(1)
    const options = pushManager.subscribe.mock.calls[0][0]
    expect(options.userVisibleOnly).toBe(true)
    expect([...options.applicationServerKey]).toEqual([...KEY_BYTES])

    const [[url, init]] = callsTo('POST')
    expect(url).toBe('/api/v1/push/subscriptions')
    expect(init.credentials).toBe('same-origin')
    expect(JSON.parse(init.body as string)).toEqual({
      endpoint: ENDPOINT,
      expirationTime: null,
      keys: { p256dh: 'p256', auth: 'auth' },
      user_agent: navigator.userAgent,
    })
  })

  it('never raises the permission prompt itself', async () => {
    permission = 'default'

    await expect(subscribeToPush()).resolves.toEqual({ status: 'prompt', endpoint: null })
    expect(requestPermission).not.toHaveBeenCalled()
    expect(pushManager.subscribe).not.toHaveBeenCalled()
  })

  it('does nothing with a denied permission', async () => {
    permission = 'denied'

    await expect(subscribeToPush()).resolves.toMatchObject({ status: 'denied' })
    expect(pushManager.subscribe).not.toHaveBeenCalled()
  })

  it('reports an unsupported browser', async () => {
    Reflect.deleteProperty(navigator, 'serviceWorker')

    await expect(subscribeToPush()).resolves.toMatchObject({ status: 'unsupported' })
  })

  it('reports no-worker when there is no registration to subscribe', async () => {
    registration = undefined

    await expect(subscribeToPush()).resolves.toMatchObject({ status: 'no-worker' })
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it.each([
    ['push is off', json({ enabled: false, public_key: '' })],
    ['there is no key', json({ enabled: true, public_key: '' })],
  ])('reports disabled when %s on the instance', async (_label, config) => {
    serve({ config })

    await expect(subscribeToPush()).resolves.toMatchObject({ status: 'disabled' })
    expect(pushManager.subscribe).not.toHaveBeenCalled()
  })

  it.each([
    ['answers with an error', new Response('nope', { status: 500 })],
    ['cannot be reached', new TypeError('offline')],
  ])('reports failed when the config request %s', async (_label, config) => {
    serve({ config })

    await expect(subscribeToPush()).resolves.toMatchObject({ status: 'failed' })
    expect(pushManager.subscribe).not.toHaveBeenCalled()
  })

  it('reuses a subscription made with the same key, and registers it again', async () => {
    const existing = new FakeSubscription(ENDPOINT, KEY_BYTES)
    pushManager.current = existing

    await expect(subscribeToPush()).resolves.toMatchObject({ status: 'subscribed' })

    expect(pushManager.subscribe).not.toHaveBeenCalled()
    expect(existing.unsubscribe).not.toHaveBeenCalled()
    expect(callsTo('POST')).toHaveLength(1)
  })

  it('replaces a subscription made with a rotated key', async () => {
    const stale = new FakeSubscription('https://push.example.test/old', OTHER_KEY_BYTES)
    pushManager.current = stale

    await expect(subscribeToPush()).resolves.toEqual({ status: 'subscribed', endpoint: ENDPOINT })

    expect(stale.unsubscribe).toHaveBeenCalledTimes(1)
    expect(pushManager.subscribe).toHaveBeenCalledTimes(1)
  })

  it('reports unsupported when the browser refuses to subscribe (iOS outside the installed app)', async () => {
    pushManager.subscribe.mockRejectedValueOnce(new DOMException('no push', 'NotSupportedError'))

    await expect(subscribeToPush()).resolves.toMatchObject({ status: 'unsupported' })
    expect(callsTo('POST')).toHaveLength(0)
  })

  it('reports denied when subscribing revealed a blocked permission', async () => {
    pushManager.subscribe.mockImplementationOnce(() => {
      permission = 'denied'
      return Promise.reject(new DOMException('blocked', 'NotAllowedError'))
    })

    await expect(subscribeToPush()).resolves.toMatchObject({ status: 'denied' })
  })

  it('reports failed when the push service cannot be reached', async () => {
    pushManager.subscribe.mockRejectedValueOnce(new DOMException('down', 'AbortError'))

    await expect(subscribeToPush()).resolves.toMatchObject({ status: 'failed' })
    expect(callsTo('POST')).toHaveLength(0)
  })

  it.each([
    ['503 (push switched off meanwhile)', new Response(null, { status: 503 }), 'disabled'],
    ['500', new Response(null, { status: 500 }), 'failed'],
    ['a network error', new TypeError('offline'), 'failed'],
  ] as const)(
    'drops the browser subscription again when the server answers %s',
    async (_label, register, status) => {
      serve({ register })

      await expect(subscribeToPush()).resolves.toEqual({ status, endpoint: null })

      expect(pushManager.current?.unsubscribe).toHaveBeenCalledTimes(1)
    },
  )
})

describe('unsubscribeFromPush', () => {
  let subscription: FakeSubscription

  beforeEach(() => {
    subscription = new FakeSubscription(ENDPOINT, KEY_BYTES)
    pushManager.current = subscription
  })

  it('forgets the endpoint on the server and drops the browser subscription', async () => {
    await expect(unsubscribeFromPush()).resolves.toEqual({ status: 'unsubscribed', endpoint: null })

    const [[url, init]] = callsTo('DELETE')
    expect(url).toBe(`/api/v1/push/subscriptions?endpoint=${encodeURIComponent(ENDPOINT)}`)
    expect(init.credentials).toBe('same-origin')
    expect(subscription.unsubscribe).toHaveBeenCalledTimes(1)
  })

  it.each([
    ['already forgot it', new Response(null, { status: 404 })],
    ['cannot be reached', new TypeError('offline')],
  ])('still drops the browser subscription when the server %s', async (_label, remove) => {
    serve({ remove })

    await expect(unsubscribeFromPush()).resolves.toMatchObject({ status: 'unsubscribed' })
    expect(subscription.unsubscribe).toHaveBeenCalledTimes(1)
  })

  it('reports the permission state when there is nothing to unsubscribe', async () => {
    pushManager.current = null
    permission = 'default'

    await expect(unsubscribeFromPush()).resolves.toEqual({ status: 'prompt', endpoint: null })
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('reports failed when the browser subscription cannot be dropped', async () => {
    subscription.unsubscribe.mockRejectedValueOnce(new Error('broken'))

    await expect(unsubscribeFromPush()).resolves.toMatchObject({ status: 'failed' })
  })

  it('stays subscribed when the browser declines to drop the subscription', async () => {
    subscription.unsubscribe.mockResolvedValueOnce(false)

    await expect(unsubscribeFromPush()).resolves.toEqual({
      status: 'subscribed',
      endpoint: ENDPOINT,
    })
  })

  it('reports failed when the push manager cannot be read', async () => {
    pushManager.getSubscription.mockRejectedValueOnce(new Error('broken'))

    await expect(unsubscribeFromPush()).resolves.toMatchObject({ status: 'failed' })
  })

  it('reports unsupported and no-worker like every other export', async () => {
    registration = undefined
    await expect(unsubscribeFromPush()).resolves.toMatchObject({ status: 'no-worker' })

    Reflect.deleteProperty(navigator, 'serviceWorker')
    await expect(unsubscribeFromPush()).resolves.toMatchObject({ status: 'unsupported' })
  })
})

describe('decodeBase64Url', () => {
  it('decodes an unpadded base64url key into its bytes', () => {
    expect([...decodeBase64Url(PUBLIC_KEY)]).toEqual([...KEY_BYTES])
  })

  it('decodes the url-safe alphabet', () => {
    expect([...decodeBase64Url('-_8')]).toEqual([0xfb, 0xff])
  })
})

describe('isPushEnabled', () => {
  it('reports an instance with push on and a key as enabled', async () => {
    await expect(isPushEnabled()).resolves.toBe(true)
  })

  it.each([
    ['push switched off', json({ enabled: false, public_key: PUBLIC_KEY })],
    ['no VAPID key', json({ enabled: true, public_key: '' })],
    ['a failing config request', json({}, 500)],
    ['an unreachable server', new Error('offline')],
  ])('reports %s as not enabled', async (_name, config) => {
    serve({ config })

    await expect(isPushEnabled()).resolves.toBe(false)
  })
})

describe('isIosOutsideInstalledApp', () => {
  const IPHONE =
    'Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Mobile/15E148 Safari/604.1'

  /** Pretends to be a browser with `userAgent` and touch points. */
  function pretend(userAgent: string, maxTouchPoints = 0): void {
    // Own properties shadow the prototype's getters (jsdom has no
    // maxTouchPoints at all), and the afterEach below removes them again.
    for (const [name, value] of Object.entries({ userAgent, maxTouchPoints })) {
      Object.defineProperty(navigator, name, { value, configurable: true })
    }
  }

  afterEach(() => {
    for (const name of ['userAgent', 'maxTouchPoints']) {
      Reflect.deleteProperty(navigator, name)
    }
  })

  /** Answers `(display-mode: standalone)` with `standalone`. */
  function displayMode(standalone: boolean): void {
    vi.stubGlobal(
      'matchMedia',
      vi.fn(() => ({ matches: standalone })),
    )
  }

  it('reports an iPhone in a Safari tab', () => {
    pretend(IPHONE, 5)
    displayMode(false)

    expect(isIosOutsideInstalledApp()).toBe(true)
  })

  it('reports an iPad that calls itself a Mac', () => {
    pretend('Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)', 5)
    displayMode(false)

    expect(isIosOutsideInstalledApp()).toBe(true)
  })

  it('does not report an iPhone running the installed home-screen app', () => {
    pretend(IPHONE, 5)
    displayMode(true)

    expect(isIosOutsideInstalledApp()).toBe(false)
  })

  it('does not report a desktop Mac or an Android phone', () => {
    displayMode(false)
    pretend('Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)', 0)
    expect(isIosOutsideInstalledApp()).toBe(false)

    pretend('Mozilla/5.0 (Linux; Android 14; Pixel 8)', 5)
    expect(isIosOutsideInstalledApp()).toBe(false)
  })
})
