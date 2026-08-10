/**
 * Runtime half of the PWA: registering the service worker and driving its
 * update handshake.
 *
 * The worker itself (build/service-worker.js, emitted as /sw.js) parks in
 * "waiting" when a new deployment installs, so the page a reader is looking at
 * keeps the shell and the hashed assets it was loaded with. This module notices
 * that, tells the app via `onUpdateReady`, and — once the reader accepts the
 * refresh — posts SKIP_WAITING and reloads when the new worker takes control.
 * That is the whole update flow: no silently swapped assets mid-session, and no
 * shell that stays stale until every tab is closed.
 *
 * Registration is gated on an explicit `enabled` flag rather than sniffing the
 * environment here, so a test can drive both branches. The app passes
 * `import.meta.env.PROD`: `vite dev` emits no worker, and a dev session on an
 * origin that once served production would otherwise be handed the old shell —
 * hence the disabled branch actively unregisters whatever it finds.
 */

/** Where the built worker is served from; root scope, see build/pwa.ts. */
const SERVICE_WORKER_URL = '/sw.js'

/** The message the waiting worker listens for; see build/service-worker.js. */
const SKIP_WAITING_MESSAGE = { type: 'SKIP_WAITING' }

/** What {@link registerServiceWorker} needs from its caller. */
export interface RegisterOptions {
  /** Register at all? False also unregisters any worker already installed. */
  enabled: boolean
  /** Called once a newly deployed worker is installed and waiting to take over. */
  onUpdateReady: () => void
  /**
   * How to reload the page once the new worker has taken control. Defaults to a
   * full navigation; tests pass a spy, since jsdom cannot navigate.
   */
  reload?: () => void
}

/**
 * The registration this module is driving, kept so {@link applyServiceWorkerUpdate}
 * can reach the waiting worker without the caller threading it through.
 */
let current: ServiceWorkerRegistration | null = null

/** How to reload once the new worker takes control (see {@link RegisterOptions}). */
let reloadPage: () => void = () => {
  window.location.reload()
}

/** Guards against registering twice (React StrictMode mounts effects twice). */
let started = false

/** Guards against reloading more than once per controller handover. */
let reloading = false

/**
 * Resets the module's one-shot guards. Test-only: production registers exactly
 * once per page load, which is precisely what the guards enforce.
 */
export function resetServiceWorkerStateForTests(): void {
  current = null
  started = false
  reloading = false
  reloadPage = () => {
    window.location.reload()
  }
}

/** The `serviceWorker` container, or null where the browser has none (or on http). */
function container(): ServiceWorkerContainer | null {
  if (typeof navigator === 'undefined' || !('serviceWorker' in navigator)) {
    return null
  }
  return navigator.serviceWorker
}

/**
 * Watches a registration for a newly installed worker and reports it upwards.
 *
 * The `controller` check is what distinguishes an *update* from a *first*
 * install: with no controller this page is not being managed by an older
 * worker, so the freshly installed one is simply the app's first worker and
 * there is nothing to prompt about.
 */
function watchForUpdate(
  registration: ServiceWorkerRegistration,
  swContainer: ServiceWorkerContainer,
  onUpdateReady: () => void,
): void {
  if (registration.waiting && swContainer.controller) {
    onUpdateReady()
  }
  registration.addEventListener('updatefound', () => {
    const installing = registration.installing
    if (!installing) {
      return
    }
    installing.addEventListener('statechange', () => {
      if (installing.state === 'installed' && swContainer.controller) {
        onUpdateReady()
      }
    })
  })
}

/**
 * Asks the browser to re-check /sw.js whenever the tab comes back to the
 * foreground. Without it a long-lived installed app (which is never navigated,
 * only resumed) could sit on a superseded shell for as long as it stays open.
 */
function checkOnForeground(registration: ServiceWorkerRegistration): void {
  document.addEventListener('visibilitychange', () => {
    if (document.visibilityState !== 'visible') {
      return
    }
    void Promise.resolve(registration.update()).catch(() => undefined)
  })
}

/**
 * Registers the service worker (when enabled) and starts watching for updates.
 * Resolves with the registration, or null when there is nothing to register:
 * the browser has no service worker support, the flag is off, or registration
 * failed — none of which is fatal, the app simply runs without a worker.
 */
export async function registerServiceWorker(
  options: RegisterOptions,
): Promise<ServiceWorkerRegistration | null> {
  const swContainer = container()
  if (!swContainer) {
    return null
  }
  if (!options.enabled) {
    await unregisterServiceWorkers(swContainer)
    return null
  }
  if (started) {
    return current
  }
  started = true
  if (options.reload) {
    reloadPage = options.reload
  }

  try {
    const registration = await swContainer.register(SERVICE_WORKER_URL, { scope: '/' })
    current = registration
    watchForUpdate(registration, swContainer, options.onUpdateReady)
    checkOnForeground(registration)
    return registration
  } catch (error) {
    // A missing or rejected worker must never break the app, so this is a
    // warning and not a throw: everything still works, just online-only.
    console.warn('service worker registration failed', error)
    started = false
    return null
  }
}

/**
 * Tears down every worker registered for this origin. Used by the disabled
 * branch so a dev server (or a rollback to a non-PWA build) is never shadowed
 * by a worker that outlived the deployment that installed it.
 */
async function unregisterServiceWorkers(swContainer: ServiceWorkerContainer): Promise<void> {
  try {
    const registrations = await swContainer.getRegistrations()
    await Promise.all(registrations.map((registration) => registration.unregister()))
  } catch {
    // Nothing to do: the page works with or without a worker.
  }
}

/**
 * Accepts a pending update: tells the waiting worker to activate and reloads as
 * soon as it has taken control, so the page comes back on the new shell.
 *
 * With no worker waiting (the reader clicked twice, or the update landed some
 * other way) it just reloads — the outcome the reader asked for either way.
 */
export function applyServiceWorkerUpdate(): void {
  const swContainer = container()
  const waiting = current?.waiting
  if (!swContainer || !waiting) {
    reloadPage()
    return
  }
  swContainer.addEventListener('controllerchange', () => {
    if (reloading) {
      return
    }
    reloading = true
    reloadPage()
  })
  waiting.postMessage(SKIP_WAITING_MESSAGE)
}
