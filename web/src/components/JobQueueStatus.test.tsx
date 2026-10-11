import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
import i18n from '../i18n'
import { type JobStats } from '../services/import'

import { JobQueueStatus } from './JobQueueStatus'

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
    canCurate: isAdmin || role === 'editor',
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
 * Renders the status line as the footer does, inside a router with a stand-in
 * for the system-status page so a real navigation can be observed.
 */
function renderStatus(role: TestRole) {
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={auth(role)}>
        <MemoryRouter initialEntries={['/']}>
          <Routes>
            <Route path="/" element={<JobQueueStatus />} />
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

describe('JobQueueStatus', () => {
  it('renders nothing and issues no request for a non-maintainer (viewer or admin)', () => {
    // The /jobs stats endpoint is a maintainer-only operations capability, so even
    // a governance admin sees nothing and triggers no request. That is also the
    // threshold guarding the `/system` route the line links to, so nobody is
    // offered a link to a page that would answer them with "forbidden".
    statsMock.mockResolvedValue(stats({ queued: 3 }))
    for (const role of ['viewer', 'admin'] as const) {
      const { container, unmount } = renderStatus(role)
      expect(container).toBeEmptyDOMElement()
      unmount()
    }
    expect(statsMock).not.toHaveBeenCalled()
  })

  it('spells out one full phrase per non-empty state, failures first', async () => {
    statsMock.mockResolvedValue(stats({ queued: 3, running: 1, failed: 2, dead: 17, done: 500 }))
    renderStatus('maintainer')
    const link = await screen.findByRole('link')
    // done is deliberately excluded; the failures lead, the live work follows.
    expect(link).toHaveTextContent(
      '17 jobs permanently failed2 jobs failed3 jobs queued1 job running',
    )
    expect(screen.queryByText(/done|500/)).not.toBeInTheDocument()
  })

  it('pluralises the English phrases', async () => {
    statsMock.mockResolvedValue(stats({ queued: 1, dead: 1 }))
    renderStatus('maintainer')
    expect(await screen.findByText('1 job queued')).toBeInTheDocument()
    expect(screen.getByText('1 job permanently failed')).toBeInTheDocument()
  })

  it.each([
    [{ dead: 1, queued: 1 }, ['1 úloha trvale selhala', '1 úloha ve frontě']],
    [{ dead: 3, queued: 2 }, ['3 úlohy trvale selhaly', '2 úlohy ve frontě']],
    [{ dead: 17, queued: 5 }, ['17 úloh trvale selhalo', '5 úloh ve frontě']],
    [{ failed: 4, running: 21 }, ['4 úlohy selhaly', '21 úloh běží']],
  ])('uses the Czech plural for %o', async (counts, phrases) => {
    await i18n.changeLanguage('cs')
    statsMock.mockResolvedValue(stats(counts))
    renderStatus('maintainer')
    for (const phrase of phrases) {
      expect(await screen.findByText(phrase)).toBeInTheDocument()
    }
  })

  it('leads every phrase with a decorative icon', async () => {
    statsMock.mockResolvedValue(stats({ dead: 2, queued: 1 }))
    renderStatus('maintainer')
    const failure = await screen.findByText('2 jobs permanently failed')
    expect(failure.querySelector('i.bi.bi-exclamation-triangle')).toHaveAttribute(
      'aria-hidden',
      'true',
    )
    const queued = screen.getByText('1 job queued')
    expect(queued.querySelector('i.bi.bi-hourglass-split')).toHaveAttribute('aria-hidden', 'true')
  })

  it('keeps the danger colour for failures and mutes the live work', async () => {
    statsMock.mockResolvedValue(stats({ failed: 4, dead: 1, queued: 1, running: 2 }))
    renderStatus('maintainer')
    expect(await screen.findByText('4 jobs failed')).toHaveClass('text-danger')
    expect(screen.getByText('1 job permanently failed')).toHaveClass('text-danger')
    for (const phrase of ['1 job queued', '2 jobs running']) {
      const item = screen.getByText(phrase)
      expect(item).toHaveClass('text-body-secondary')
      expect(item).not.toHaveClass('text-danger')
    }
  })

  it('renders nothing when there is nothing to report', async () => {
    // Only the terminal `done` history — no live work and no failures.
    statsMock.mockResolvedValue(stats({ done: 500 }))
    const { container } = renderStatus('maintainer')
    await waitFor(() => {
      expect(statsMock).toHaveBeenCalled()
    })
    // Let the resolved stats render before asserting the absence.
    await new Promise((resolve) => setTimeout(resolve, 0))
    expect(container).toBeEmptyDOMElement()
  })

  it('links the line to the system-status page that lists the failed jobs', async () => {
    statsMock.mockResolvedValue(stats({ dead: 17 }))
    renderStatus('maintainer')
    const link = await screen.findByRole('link')
    expect(link).toHaveAttribute('href', '/system')
    // The phrase itself is inside the link — the whole line is one destination.
    expect(link).toContainElement(screen.getByText('17 jobs permanently failed'))
  })

  it('names the queue, its state and the destination, so the label stands alone', async () => {
    statsMock.mockResolvedValue(stats({ queued: 16, running: 1 }))
    renderStatus('maintainer')
    expect(
      await screen.findByRole('link', {
        name: 'Background work: 16 jobs queued, 1 job running — open System status',
      }),
    ).toBeInTheDocument()
  })

  it('translates the label into Czech', async () => {
    await i18n.changeLanguage('cs')
    statsMock.mockResolvedValue(stats({ dead: 17, queued: 1 }))
    renderStatus('maintainer')
    expect(
      await screen.findByRole('link', {
        name: 'Práce na pozadí: 17 úloh trvale selhalo, 1 úloha ve frontě — otevřít Stav systému',
      }),
    ).toBeInTheDocument()
  })

  it('is reachable by keyboard and opens the page on Enter', async () => {
    const user = userEvent.setup()
    statsMock.mockResolvedValue(stats({ queued: 16 }))
    renderStatus('maintainer')
    const link = await screen.findByRole('link')

    await user.tab()
    expect(link).toHaveFocus()
    await user.keyboard('{Enter}')

    expect(screen.getByText('system status page')).toBeInTheDocument()
  })

  it('hides the line silently when the request rejects', async () => {
    statsMock.mockRejectedValue(new Error('boom'))
    const { container } = renderStatus('maintainer')
    // The rejected request is awaited so the effect settles; nothing is rendered
    // and no error escapes to break the footer.
    await waitFor(() => {
      expect(statsMock).toHaveBeenCalled()
    })
    expect(container).toBeEmptyDOMElement()
  })
})
