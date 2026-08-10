import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { act } from 'react'
import { I18nextProvider } from 'react-i18next'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'

import { PwaStatus } from './PwaStatus'

/** What the component passes to the registration; only the callback matters here. */
interface StubRegisterOptions {
  enabled: boolean
  onUpdateReady: () => void
}

const registerServiceWorker = vi.fn((_options: StubRegisterOptions) => Promise.resolve(null))
const applyServiceWorkerUpdate = vi.fn()

// The registration module is exercised on its own in src/pwa/register.test.ts;
// here it is stubbed so the component's own behaviour — what it renders, and
// what it does with the callbacks — is what is under test.
vi.mock('../../pwa/register', () => ({
  registerServiceWorker: (options: StubRegisterOptions) => registerServiceWorker(options),
  applyServiceWorkerUpdate: () => {
    applyServiceWorkerUpdate()
  },
}))

/** Renders the component with the app's own i18n instance, as the app mounts it. */
function renderStatus() {
  return render(
    <I18nextProvider i18n={i18n}>
      <PwaStatus />
    </I18nextProvider>,
  )
}

/** Sets navigator.onLine, which jsdom leaves at true. */
function setOnline(online: boolean): void {
  Object.defineProperty(navigator, 'onLine', { value: online, configurable: true })
}

/** Runs the `onUpdateReady` callback the component handed to the registration. */
function announceUpdate(): void {
  const options = registerServiceWorker.mock.calls.at(-1)?.[0]
  act(() => {
    options?.onUpdateReady()
  })
}

/** The English copy the assertions below read, so a wording change fails loudly. */
const OFFLINE_TEXT = 'You are offline. Kukátko shows only what it has stored.'
const UPDATE_TEXT = 'A new version of Kukátko is available.'

beforeEach(async () => {
  await i18n.changeLanguage('en')
  setOnline(true)
})

describe('PwaStatus', () => {
  it('renders nothing while the app is online and up to date', () => {
    const { container } = renderStatus()

    expect(container).toBeEmptyDOMElement()
  })

  it('registers the service worker on mount', () => {
    renderStatus()

    expect(registerServiceWorker).toHaveBeenCalledTimes(1)
  })

  it('announces a dropped connection and clears it when the network returns', () => {
    renderStatus()

    act(() => {
      setOnline(false)
      window.dispatchEvent(new Event('offline'))
    })
    expect(screen.getByText(OFFLINE_TEXT)).toBeInTheDocument()

    act(() => {
      setOnline(true)
      window.dispatchEvent(new Event('online'))
    })
    expect(screen.queryByText(OFFLINE_TEXT)).not.toBeInTheDocument()
  })

  it('starts offline when the page loads with no connection', () => {
    setOnline(false)

    renderStatus()

    expect(screen.getByText(OFFLINE_TEXT)).toBeInTheDocument()
  })

  it('offers a refresh once a new version is waiting, and applies it on click', async () => {
    const user = userEvent.setup()
    renderStatus()

    announceUpdate()
    expect(screen.getByText(UPDATE_TEXT)).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Reload' }))

    expect(applyServiceWorkerUpdate).toHaveBeenCalledTimes(1)
    expect(screen.queryByText(UPDATE_TEXT)).not.toBeInTheDocument()
  })

  it('lets the reader postpone the refresh without reloading', async () => {
    const user = userEvent.setup()
    renderStatus()

    announceUpdate()
    await user.click(screen.getByRole('button', { name: 'Not now' }))

    expect(applyServiceWorkerUpdate).not.toHaveBeenCalled()
    expect(screen.queryByText(UPDATE_TEXT)).not.toBeInTheDocument()
  })
})
