import { render, screen, waitFor } from '@testing-library/react'
import { I18nextProvider } from 'react-i18next'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
import i18n from '../i18n'
import { type JobStats } from '../services/import'

import { JobQueueBadges } from './JobQueueBadges'

vi.mock('../services/import', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/import')>()
  return {
    ...actual,
    fetchJobStats: vi.fn(),
  }
})

const { fetchJobStats } = await import('../services/import')
const statsMock = vi.mocked(fetchJobStats)

/** Builds an auth context value with the given role capabilities. */
function auth(isAdmin: boolean): AuthContextValue {
  const role = isAdmin ? 'admin' : 'viewer'
  return {
    status: 'authenticated',
    user: { uid: 'u1', username: 'u', display_name: 'User', role },
    role,
    downloadToken: null,
    canWrite: isAdmin,
    isAdmin,
    login: vi.fn(),
    logout: vi.fn(),
    refresh: vi.fn(),
  } as unknown as AuthContextValue
}

/** Builds a job-stats snapshot with the given per-state counts. */
function stats(byState: Record<string, number>): JobStats {
  const total = Object.values(byState).reduce((sum, n) => sum + n, 0)
  return { by_state: byState, by_type: {}, total }
}

function renderBadges(isAdmin: boolean) {
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={auth(isAdmin)}>
        <JobQueueBadges />
      </AuthContext.Provider>
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  statsMock.mockReset()
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('JobQueueBadges', () => {
  it('renders nothing and issues no request for a non-admin', () => {
    statsMock.mockResolvedValue(stats({ queued: 3 }))
    const { container } = renderBadges(false)
    expect(container).toBeEmptyDOMElement()
    expect(statsMock).not.toHaveBeenCalled()
  })

  it('renders one badge per non-empty state for an admin', async () => {
    statsMock.mockResolvedValue(stats({ queued: 3, running: 1, failed: 2, done: 500 }))
    renderBadges(true)
    // done is deliberately excluded; queued/running/failed each render a badge.
    expect(await screen.findByText('queued 3')).toBeInTheDocument()
    expect(screen.getByText('running 1')).toBeInTheDocument()
    expect(screen.getByText('failed 2')).toBeInTheDocument()
    expect(screen.queryByText(/^done/)).not.toBeInTheDocument()
  })

  it('styles a non-zero failed count with danger styling', async () => {
    statsMock.mockResolvedValue(stats({ failed: 4 }))
    renderBadges(true)
    const badge = await screen.findByText('failed 4')
    expect(badge).toHaveClass('bg-danger')
  })

  it('shows a single idle badge when every state is zero', async () => {
    statsMock.mockResolvedValue(stats({}))
    renderBadges(true)
    expect(await screen.findByText('idle')).toBeInTheDocument()
    expect(screen.queryByText(/queued/)).not.toBeInTheDocument()
  })

  it('hides the badges silently when the request rejects', async () => {
    statsMock.mockRejectedValue(new Error('boom'))
    const { container } = renderBadges(true)
    // The rejected request is awaited so the effect settles; nothing is rendered
    // and no error escapes to break the footer.
    await waitFor(() => {
      expect(statsMock).toHaveBeenCalled()
    })
    expect(container).toBeEmptyDOMElement()
  })
})
