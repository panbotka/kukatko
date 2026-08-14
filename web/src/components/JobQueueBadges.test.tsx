import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

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

type TestRole = 'viewer' | 'editor' | 'admin' | 'maintainer'

/** Builds an auth context value for the given role. */
function auth(role: TestRole): AuthContextValue {
  const isMaintainer = role === 'maintainer'
  const isAdmin = role === 'admin' || role === 'maintainer'
  return {
    status: 'authenticated',
    user: { uid: 'u1', username: 'u', display_name: 'User', role },
    role,
    downloadToken: null,
    canWrite: isAdmin || role === 'editor',
    isAdmin,
    isMaintainer,
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

/**
 * Renders the badges as the footer does, inside a router with a stand-in for the
 * system-status page so a real navigation can be observed.
 */
function renderBadges(role: TestRole) {
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={auth(role)}>
        <MemoryRouter initialEntries={['/']}>
          <Routes>
            <Route path="/" element={<JobQueueBadges />} />
            <Route path="/system" element={<p>system status page</p>} />
          </Routes>
        </MemoryRouter>
      </AuthContext.Provider>
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  statsMock.mockReset()
})

describe('JobQueueBadges', () => {
  it('renders nothing and issues no request for a non-maintainer (viewer or admin)', () => {
    // The /jobs stats endpoint is a maintainer-only operations capability, so even
    // a governance admin sees nothing and triggers no request. That is also the
    // threshold guarding the `/system` route the badges link to, so nobody is
    // offered a link to a page that would answer them with "forbidden".
    statsMock.mockResolvedValue(stats({ queued: 3 }))
    for (const role of ['viewer', 'admin'] as const) {
      const { container, unmount } = renderBadges(role)
      expect(container).toBeEmptyDOMElement()
      unmount()
    }
    expect(statsMock).not.toHaveBeenCalled()
  })

  it('renders one badge per non-empty state for a maintainer', async () => {
    statsMock.mockResolvedValue(stats({ queued: 3, running: 1, failed: 2, done: 500 }))
    renderBadges('maintainer')
    // done is deliberately excluded; queued/running/failed each render a badge.
    expect(await screen.findByText('queued 3')).toBeInTheDocument()
    expect(screen.getByText('running 1')).toBeInTheDocument()
    expect(screen.getByText('failed 2')).toBeInTheDocument()
    expect(screen.queryByText(/^done/)).not.toBeInTheDocument()
  })

  it('styles a non-zero failed count with danger styling', async () => {
    statsMock.mockResolvedValue(stats({ failed: 4 }))
    renderBadges('maintainer')
    const badge = await screen.findByText('failed 4')
    expect(badge).toHaveClass('bg-danger')
  })

  it('shows a single idle badge when every state is zero', async () => {
    statsMock.mockResolvedValue(stats({}))
    renderBadges('maintainer')
    expect(await screen.findByText('idle')).toBeInTheDocument()
    expect(screen.queryByText(/queued/)).not.toBeInTheDocument()
  })

  it('links the badges to the system-status page that holds the queue', async () => {
    statsMock.mockResolvedValue(stats({ queued: 16 }))
    renderBadges('maintainer')
    const link = await screen.findByRole('link')
    expect(link).toHaveAttribute('href', '/system')
    // The badge itself is inside the link — the whole row is one destination.
    expect(link).toContainElement(screen.getByText('queued 16'))
  })

  it('names the queue, its state and the destination, so the label stands alone', async () => {
    statsMock.mockResolvedValue(stats({ queued: 16, running: 1 }))
    renderBadges('maintainer')
    // Not just the bare numbers a screen reader would otherwise announce.
    expect(
      await screen.findByRole('link', {
        name: 'Job queue: queued 16, running 1 — open System status',
      }),
    ).toBeInTheDocument()
  })

  it('labels the idle row too', async () => {
    statsMock.mockResolvedValue(stats({}))
    renderBadges('maintainer')
    expect(
      await screen.findByRole('link', { name: 'Job queue: idle — open System status' }),
    ).toBeInTheDocument()
  })

  it('is reachable by keyboard and opens the page on Enter', async () => {
    const user = userEvent.setup()
    statsMock.mockResolvedValue(stats({ queued: 16 }))
    renderBadges('maintainer')
    const link = await screen.findByRole('link')

    await user.tab()
    expect(link).toHaveFocus()
    await user.keyboard('{Enter}')

    expect(screen.getByText('system status page')).toBeInTheDocument()
  })

  it('hides the badges silently when the request rejects', async () => {
    statsMock.mockRejectedValue(new Error('boom'))
    const { container } = renderBadges('maintainer')
    // The rejected request is awaited so the effect settles; nothing is rendered
    // and no error escapes to break the footer.
    await waitFor(() => {
      expect(statsMock).toHaveBeenCalled()
    })
    expect(container).toBeEmptyDOMElement()
  })
})
