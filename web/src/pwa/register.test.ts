import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import {
  applyServiceWorkerUpdate,
  registerServiceWorker,
  resetServiceWorkerStateForTests,
} from './register'

/** A worker instance: enough of ServiceWorker for the update handshake. */
class FakeWorker extends EventTarget {
  state = 'installing'
  postMessage = vi.fn()

  /** Moves the worker to state and fires the statechange the module listens for. */
  transitionTo(state: string): void {
    this.state = state
    this.dispatchEvent(new Event('statechange'))
  }
}

/** A registration: the installing/waiting pair plus update() and unregister(). */
class FakeRegistration extends EventTarget {
  installing: FakeWorker | null = null
  waiting: FakeWorker | null = null
  update = vi.fn(() => Promise.resolve())
  unregister = vi.fn(() => Promise.resolve(true))

  /** Announces a newly installing worker the way the browser does. */
  startInstalling(worker: FakeWorker): void {
    this.installing = worker
    this.dispatchEvent(new Event('updatefound'))
  }
}

/** The navigator.serviceWorker container. */
class FakeContainer extends EventTarget {
  controller: unknown = null
  registration = new FakeRegistration()
  existing: FakeRegistration[] = []
  register = vi.fn(() => Promise.resolve(this.registration))
  getRegistrations = vi.fn(() => Promise.resolve(this.existing))
}

let container: FakeContainer

/** Installs `container` as navigator.serviceWorker for the current test. */
function withServiceWorkerSupport(): void {
  Object.defineProperty(navigator, 'serviceWorker', {
    value: container,
    configurable: true,
    writable: true,
  })
}

/** Removes navigator.serviceWorker, as on an insecure origin or an old browser. */
function withoutServiceWorkerSupport(): void {
  Reflect.deleteProperty(navigator, 'serviceWorker')
}

beforeEach(() => {
  resetServiceWorkerStateForTests()
  container = new FakeContainer()
  withServiceWorkerSupport()
})

afterEach(() => {
  withoutServiceWorkerSupport()
})

describe('registerServiceWorker', () => {
  it('registers the worker at the root scope when enabled', async () => {
    const registration = await registerServiceWorker({ enabled: true, onUpdateReady: vi.fn() })

    expect(container.register).toHaveBeenCalledWith('/sw.js', { scope: '/' })
    expect(registration).toBe(container.registration)
  })

  it('registers only once, however often it is called', async () => {
    const options = { enabled: true, onUpdateReady: vi.fn() }

    await registerServiceWorker(options)
    await registerServiceWorker(options)

    expect(container.register).toHaveBeenCalledTimes(1)
  })

  it('unregisters any leftover worker instead of registering when disabled', async () => {
    const leftover = new FakeRegistration()
    container.existing = [leftover]

    const registration = await registerServiceWorker({ enabled: false, onUpdateReady: vi.fn() })

    expect(registration).toBeNull()
    expect(container.register).not.toHaveBeenCalled()
    expect(leftover.unregister).toHaveBeenCalledTimes(1)
  })

  it('does nothing where the browser has no service worker support', async () => {
    withoutServiceWorkerSupport()

    await expect(
      registerServiceWorker({ enabled: true, onUpdateReady: vi.fn() }),
    ).resolves.toBeNull()
  })

  it('survives a failing registration without breaking the app', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => undefined)
    container.register = vi.fn(() => Promise.reject(new Error('nope')))

    await expect(
      registerServiceWorker({ enabled: true, onUpdateReady: vi.fn() }),
    ).resolves.toBeNull()
    expect(warn).toHaveBeenCalled()
  })

  it('reports an update when a new worker is already waiting on this controlled page', async () => {
    const onUpdateReady = vi.fn()
    container.controller = new FakeWorker()
    container.registration.waiting = new FakeWorker()

    await registerServiceWorker({ enabled: true, onUpdateReady })

    expect(onUpdateReady).toHaveBeenCalledTimes(1)
  })

  it('reports an update once a newly found worker finishes installing', async () => {
    const onUpdateReady = vi.fn()
    container.controller = new FakeWorker()
    await registerServiceWorker({ enabled: true, onUpdateReady })

    const incoming = new FakeWorker()
    container.registration.startInstalling(incoming)
    expect(onUpdateReady).not.toHaveBeenCalled()

    incoming.transitionTo('installed')

    expect(onUpdateReady).toHaveBeenCalledTimes(1)
  })

  it('stays quiet for a first install, which supersedes nothing', async () => {
    const onUpdateReady = vi.fn()
    container.controller = null
    await registerServiceWorker({ enabled: true, onUpdateReady })

    const incoming = new FakeWorker()
    container.registration.startInstalling(incoming)
    incoming.transitionTo('installed')

    expect(onUpdateReady).not.toHaveBeenCalled()
  })

  it('re-checks for a new worker when the tab comes back to the foreground', async () => {
    await registerServiceWorker({ enabled: true, onUpdateReady: vi.fn() })

    document.dispatchEvent(new Event('visibilitychange'))

    expect(container.registration.update).toHaveBeenCalledTimes(1)
  })
})

describe('applyServiceWorkerUpdate', () => {
  it('tells the waiting worker to activate and reloads when it takes control', async () => {
    const reload = vi.fn()
    const waiting = new FakeWorker()
    container.controller = new FakeWorker()
    container.registration.waiting = waiting
    await registerServiceWorker({ enabled: true, onUpdateReady: vi.fn(), reload })

    applyServiceWorkerUpdate()

    expect(waiting.postMessage).toHaveBeenCalledWith({ type: 'SKIP_WAITING' })
    expect(reload).not.toHaveBeenCalled()

    container.dispatchEvent(new Event('controllerchange'))

    expect(reload).toHaveBeenCalledTimes(1)
  })

  it('reloads at most once however many times control changes hands', async () => {
    const reload = vi.fn()
    container.controller = new FakeWorker()
    container.registration.waiting = new FakeWorker()
    await registerServiceWorker({ enabled: true, onUpdateReady: vi.fn(), reload })

    applyServiceWorkerUpdate()
    container.dispatchEvent(new Event('controllerchange'))
    container.dispatchEvent(new Event('controllerchange'))

    expect(reload).toHaveBeenCalledTimes(1)
  })

  it('just reloads when no worker is waiting', async () => {
    const reload = vi.fn()
    await registerServiceWorker({ enabled: true, onUpdateReady: vi.fn(), reload })

    applyServiceWorkerUpdate()

    expect(reload).toHaveBeenCalledTimes(1)
  })
})
