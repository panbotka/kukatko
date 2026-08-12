import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { ApiError, NetworkError, type AuthSession } from '../services/auth'

import { useAuth } from './AuthContext'
import { AuthProvider } from './AuthProvider'

const fetchMe = vi.fn<() => Promise<AuthSession | null>>()

vi.mock('../services/auth', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/auth')>()
  return { ...actual, fetchMe: () => fetchMe() }
})

const SESSION: AuthSession = {
  user: {
    uid: 'u1',
    username: 'alice',
    display_name: 'Alice',
    email: 'alice@example.com',
    role: 'editor',
    disabled: false,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
  },
  download_token: 'tok-123',
}

/** Prints the resolved status, and offers the retry the offline page uses. */
function StatusProbe() {
  const { status, refresh } = useAuth()
  return (
    <div>
      <span data-testid="status">{status}</span>
      <button
        onClick={() => {
          void refresh()
        }}
      >
        retry
      </button>
    </div>
  )
}

function renderProvider() {
  return render(
    <AuthProvider>
      <StatusProbe />
    </AuthProvider>,
  )
}

// No mock reset here on purpose: `restoreMocks: true` (vite.config.ts) already
// restores before each test, and resetting afterwards races RTL cleanup — see
// src/test/setup.ts. Every test below sets its own implementation first thing.
describe('AuthProvider session probe', () => {
  it('reports a signed-in visitor as authenticated', async () => {
    fetchMe.mockResolvedValue(SESSION)

    renderProvider()

    expect(await screen.findByTestId('status')).toHaveTextContent('authenticated')
  })

  it('reports a 401 (no session) as unauthenticated', async () => {
    // fetchMe turns the backend's 401 into a null session — a real answer.
    fetchMe.mockResolvedValue(null)

    renderProvider()

    expect(await screen.findByTestId('status')).toHaveTextContent('unauthenticated')
  })

  it('reports an unreachable backend as unreachable, not as signed out', async () => {
    // The whole defect in one assertion: the installed app cold-launching with
    // no network used to land here and call the reader signed out, which sent
    // them to a login form that then blamed their password for the outage.
    fetchMe.mockRejectedValue(new NetworkError('Failed to fetch'))

    renderProvider()

    expect(await screen.findByTestId('status')).toHaveTextContent('unreachable')
  })

  it('still falls back to signed out when the server itself answered badly', async () => {
    // A 500 came *from* the server, so the difference is not knowable here and
    // the old recover-by-signing-out behaviour stands.
    fetchMe.mockRejectedValue(new ApiError(500, 'boom'))

    renderProvider()

    expect(await screen.findByTestId('status')).toHaveTextContent('unauthenticated')
  })

  it('recovers on retry once the backend answers again', async () => {
    const user = userEvent.setup()
    fetchMe.mockRejectedValueOnce(new NetworkError('Failed to fetch')).mockResolvedValue(SESSION)

    renderProvider()
    expect(await screen.findByTestId('status')).toHaveTextContent('unreachable')

    await user.click(screen.getByRole('button', { name: 'retry' }))

    expect(await screen.findByTestId('status')).toHaveTextContent('authenticated')
  })

  it('keeps refresh() from rejecting when the backend is still unreachable', async () => {
    // The offline page awaits refresh() to clear its spinner; a rejection there
    // would be an unhandled one, since there is nothing useful to catch.
    const user = userEvent.setup()
    fetchMe.mockRejectedValue(new NetworkError('Failed to fetch'))
    const unhandled = vi.fn()
    window.addEventListener('unhandledrejection', unhandled)

    renderProvider()
    expect(await screen.findByTestId('status')).toHaveTextContent('unreachable')
    await user.click(screen.getByRole('button', { name: 'retry' }))

    expect(unhandled).not.toHaveBeenCalled()
    expect(screen.getByTestId('status')).toHaveTextContent('unreachable')
    window.removeEventListener('unhandledrejection', unhandled)
  })
})
