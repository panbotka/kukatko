import { render, screen } from '@testing-library/react'
import { I18nextProvider } from 'react-i18next'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { App } from './App'
import i18n from './i18n'

// The registration module is exercised on its own in src/pwa/register.test.ts.
// Here it must not run at all: a real registration in jsdom would go nowhere,
// and what is under test is what the app puts on screen.
vi.mock('./pwa/register', () => ({
  registerServiceWorker: () => Promise.resolve(null),
  applyServiceWorkerUpdate: () => undefined,
}))

/** Sets navigator.onLine, which jsdom leaves at true. */
function setOnline(online: boolean): void {
  Object.defineProperty(navigator, 'onLine', { value: online, configurable: true })
}

/**
 * Cold-launches the installed app at `path` with every request failing at the
 * transport, which is what the service worker's shell does on a phone with no
 * signal: the document paints from cache, and nothing it asks for arrives.
 */
function coldLaunch(path: string) {
  window.history.replaceState({}, '', path)
  vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('Failed to fetch')))
  return render(
    <I18nextProvider i18n={i18n}>
      <App />
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  setOnline(false)
})

afterEach(async () => {
  setOnline(true)
  window.history.replaceState({}, '', '/')
  await i18n.changeLanguage('cs')
})

/**
 * The installed app opening with no network. These mount the real {@link App} —
 * `BrowserRouter`, `AuthProvider` and all — because the defect lived in how
 * those three compose, not in any one of them.
 */
describe('cold launch with no network', () => {
  it('explains the outage instead of dropping the reader on a login form', async () => {
    coldLaunch('/')

    expect(await screen.findByTestId('offline-page')).toBeInTheDocument()
    // The old behaviour: `GET /auth/me` fails, the app calls that signed out,
    // the guard redirects, and the form then blames the password.
    expect(screen.queryByLabelText('Password')).not.toBeInTheDocument()
  })

  it('reaches the login screen with the offline banner when that is where it opens', async () => {
    // App.tsx mounts PwaStatus outside the auth gate specifically so this can
    // happen; this is the assertion that keeps it there.
    coldLaunch('/login')

    expect(await screen.findByLabelText('Password')).toBeInTheDocument()
    expect(
      screen.getByText(
        'You are offline. Kukátko opens, but photos and the library need a connection.',
      ),
    ).toBeInTheDocument()
  })

  it('warns on the login form itself, independently of navigator.onLine', async () => {
    // navigator.onLine is not trustworthy enough to be the only signal: a
    // captive portal or a downed server is "online" by definition, and a
    // devtools-emulated outage only reaches the CDP target it was set on, so a
    // document opened afterwards reads `true` (measured, see docs/FRONTEND.md).
    // The failed session probe is the signal that always holds.
    setOnline(true)
    coldLaunch('/login')

    expect(await screen.findByTestId('login-offline-notice')).toBeInTheDocument()
  })
})
