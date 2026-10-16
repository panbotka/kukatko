import { render, screen, within } from '@testing-library/react'
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
  canCurate: true,
  canWrite: true,
  isAdmin: false,
  login: vi.fn(),
  logout: vi.fn(),
  refresh: vi.fn(),
  projects: [],
  isMaintainer: false,
} as unknown as AuthContextValue

/** A membership of the account, approved in an active project unless overridden. */
function membership(slug: string, name: string, role: string, over: Record<string, unknown> = {}) {
  return {
    uid: `pj-${slug}`,
    slug,
    name,
    description: '',
    active: true,
    role,
    approved: true,
    subject_uid: null,
    ...over,
  }
}

function renderAccount(auth: AuthContextValue = editorAuth) {
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={auth}>
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
  it('names the role in every project it may enter, not the last one opened', () => {
    renderAccount({
      ...editorAuth,
      projects: [
        membership('v', 'Veselice', 'curator'),
        membership('rodina', 'Rodina', 'admin'),
        membership('cekam', 'Čekárna', 'viewer', { approved: false }),
        membership('vypnuty', 'Vypnutý', 'editor', { active: false }),
      ],
    } as unknown as AuthContextValue)

    const roles = screen.getByTestId('account-roles')
    expect(screen.getByText('Roles in projects')).toBeInTheDocument()
    expect(Array.from(roles.querySelectorAll('li')).map((li) => li.textContent)).toEqual([
      'Veselice Curator',
      'Rodina Administrator',
    ])
  })

  it('names the instance role of a maintainer', () => {
    renderAccount({ ...editorAuth, isMaintainer: true })

    expect(screen.getByTestId('account-roles')).toHaveTextContent('Maintainer')
  })

  it('sits in the shared narrow reading column', async () => {
    renderAccount()

    expect(screen.getByTestId('narrow-page')).toContainElement(
      screen.getByRole('heading', { level: 1, name: 'My account' }),
    )
    // Let the health check settle so its update lands inside the test.
    expect(await screen.findByText('Everything is working')).toBeInTheDocument()
  })

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

describe('AccountPage menu position section', () => {
  it('offers top and left, top pressed until the account chooses', async () => {
    const user = userEvent.setup()
    window.localStorage.clear()
    renderAccount()

    expect(screen.getByRole('heading', { name: 'Menu position' })).toBeInTheDocument()
    const group = screen.getByRole('group', { name: 'Menu position' })
    expect(within(group).getByRole('button', { name: 'Top' })).toHaveAttribute(
      'aria-pressed',
      'true',
    )

    await user.click(within(group).getByRole('button', { name: 'Left' }))

    expect(within(group).getByRole('button', { name: 'Left' })).toHaveAttribute(
      'aria-pressed',
      'true',
    )
  })
})

describe('AccountPage notifications section', () => {
  it('carries the notifications section — the per-user page, not the instance settings', async () => {
    renderAccount()

    expect(await screen.findByRole('heading', { name: 'Notifications' })).toBeInTheDocument()
  })
})
