import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
import i18n from '../i18n'
import { type HealthResponse } from '../services/health'

import { AccountPage } from './AccountPage'

vi.mock('../services/health', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/health')>()
  return { ...actual, fetchHealth: vi.fn() }
})

// The API-tokens section loads the caller's tokens on mount; it has its own
// tests, so here it only has to answer without reaching for the network.
vi.mock('../services/auth', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/auth')>()
  return { ...actual, fetchApiTokens: vi.fn() }
})

const { fetchHealth } = await import('../services/health')
const healthMock = vi.mocked(fetchHealth)
const { fetchApiTokens } = await import('../services/auth')
const tokensMock = vi.mocked(fetchApiTokens)

const OK_HEALTH: HealthResponse = { status: 'ok', version: { version: '1.2.3', commit: 'abcdef0' } }

/** A signed-in editor whose identity the page renders above the password form. */
const editorAuth = {
  status: 'authenticated',
  user: { uid: 'u1', username: 'jana', display_name: 'Jana', role: 'editor' },
  role: 'editor',
  downloadToken: null,
  canWrite: true,
  isAdmin: false,
  login: vi.fn(),
  logout: vi.fn(),
  refresh: vi.fn(),
} as unknown as AuthContextValue

function renderAccount() {
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={editorAuth}>
        <MemoryRouter initialEntries={['/account']}>
          <AccountPage />
        </MemoryRouter>
      </AuthContext.Provider>
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  healthMock.mockReset()
  healthMock.mockResolvedValue(OK_HEALTH)
  tokensMock.mockReset()
  tokensMock.mockResolvedValue([])
})

afterEach(() => {
  vi.clearAllMocks()
})

describe('AccountPage', () => {
  it('shows the signed-in identity and the password form', async () => {
    renderAccount()

    expect(screen.getByRole('heading', { name: 'My account' })).toBeInTheDocument()
    expect(screen.getByText('jana')).toBeInTheDocument()
    expect(await screen.findByLabelText('Current password')).toBeInTheDocument()
  })

  it('reports the API status and the build version once health resolves', async () => {
    renderAccount()

    expect(await screen.findByText('Everything is working')).toBeInTheDocument()
    // The version, but never the raw commit hash: that is noise for a reader.
    expect(screen.getByText(/1\.2\.3/)).toBeInTheDocument()
    expect(screen.queryByText(/abcdef0/)).not.toBeInTheDocument()
  })

  it('reports an unavailable API when the health check fails', async () => {
    healthMock.mockRejectedValue(new Error('offline'))
    renderAccount()

    expect(await screen.findByText('The app is currently unavailable')).toBeInTheDocument()
    // The account itself stays usable even when the probe cannot reach the API.
    expect(screen.getByRole('button', { name: 'Change password' })).toBeInTheDocument()
  })
})

describe('AccountPage activity section', () => {
  it('offers the way to the own-activity page', () => {
    renderAccount()

    expect(screen.getByRole('heading', { name: 'My activity' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /Show my activity/ })).toHaveAttribute(
      'href',
      '/account/activity',
    )
  })
})

describe('AccountPage API tokens section', () => {
  it('carries the API-tokens section beside the password', async () => {
    renderAccount()

    expect(await screen.findByRole('heading', { name: 'API tokens' })).toBeInTheDocument()
    expect(screen.getByLabelText('Name of the new token')).toBeInTheDocument()
  })
})

describe('AccountPage language section', () => {
  it('hosts the language switcher, offering Czech and English', () => {
    renderAccount()

    expect(screen.getByRole('heading', { name: 'Language' })).toBeInTheDocument()
    const group = screen.getByRole('group', { name: 'Switch language' })
    expect(group).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Čeština' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'English' })).toBeInTheDocument()
  })

  it('marks the active language and switches the page to Czech on click', async () => {
    const user = userEvent.setup()
    renderAccount()

    expect(screen.getByRole('button', { name: 'English' })).toHaveAttribute('aria-pressed', 'true')

    await user.click(screen.getByRole('button', { name: 'Čeština' }))

    expect(i18n.language).toBe('cs')
    // The whole page follows the switch, not just the button group.
    expect(screen.getByRole('heading', { name: 'Můj účet' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Změnit heslo' })).toBeInTheDocument()
  })
})
