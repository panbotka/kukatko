import { render, screen } from '@testing-library/react'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
import i18n from '../i18n'
import { type HealthResponse } from '../services/health'

import { HomePage } from './HomePage'

vi.mock('../services/health', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/health')>()
  return { ...actual, fetchHealth: vi.fn() }
})

const { fetchHealth } = await import('../services/health')
const healthMock = vi.mocked(fetchHealth)

const OK_HEALTH: HealthResponse = { status: 'ok', version: { version: '1.2.3', commit: 'abcdef0' } }

/** Builds a minimal auth context value with the given write capability. */
function auth(canWrite: boolean): AuthContextValue {
  return {
    status: 'authenticated',
    user: { uid: 'u1', username: 'u', display_name: 'U', role: canWrite ? 'editor' : 'viewer' },
    role: canWrite ? 'editor' : 'viewer',
    downloadToken: null,
    canWrite,
    isAdmin: false,
    login: vi.fn(),
    logout: vi.fn(),
    refresh: vi.fn(),
  } as unknown as AuthContextValue
}

function renderHome(canWrite = true) {
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={auth(canWrite)}>
        <MemoryRouter>
          <HomePage />
        </MemoryRouter>
      </AuthContext.Provider>
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  healthMock.mockReset()
  healthMock.mockResolvedValue(OK_HEALTH)
})

afterEach(() => {
  vi.clearAllMocks()
})

describe('HomePage', () => {
  it('welcomes the user and links to the main destinations', async () => {
    renderHome(true)

    expect(screen.getByRole('heading', { name: 'Welcome to Kukátko' })).toBeInTheDocument()

    // Whole-card links to the everyday destinations.
    const library = screen.getByRole('link', { name: /Library/ })
    expect(library).toHaveAttribute('href', '/library')
    expect(screen.getByRole('link', { name: /Search/ })).toHaveAttribute('href', '/search')
    expect(screen.getByRole('link', { name: /Albums/ })).toHaveAttribute('href', '/albums')
    expect(screen.getByRole('link', { name: /People/ })).toHaveAttribute('href', '/people')
    expect(screen.getByRole('link', { name: /Map/ })).toHaveAttribute('href', '/map')

    // Upload is a write action, shown to editors.
    expect(screen.getByRole('link', { name: /Upload/ })).toHaveAttribute('href', '/upload')

    // The status line quietly confirms reachability once health resolves.
    expect(await screen.findByText('Everything is working')).toBeInTheDocument()
    expect(screen.getByText(/1\.2\.3/)).toBeInTheDocument()
  })

  it('hides the upload tile from viewers', async () => {
    renderHome(false)

    await screen.findByRole('link', { name: /Library/ })
    expect(screen.queryByRole('link', { name: /Upload/ })).not.toBeInTheDocument()
  })

  it('shows an unavailable status when the health check fails', async () => {
    healthMock.mockRejectedValue(new Error('offline'))
    renderHome(true)

    expect(await screen.findByText('The app is currently unavailable')).toBeInTheDocument()
    // Navigation is still fully usable even when the backend probe fails.
    expect(screen.getByRole('link', { name: /Library/ })).toBeInTheDocument()
  })
})
