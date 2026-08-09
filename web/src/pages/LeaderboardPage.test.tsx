import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter, useSearchParams } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
import i18n from '../i18n'
import { type Role } from '../services/auth'
import { type Leaderboard, type LeaderboardEntry } from '../services/review'

import { LeaderboardPage } from './LeaderboardPage'

vi.mock('../services/review', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/review')>()
  return { ...actual, fetchLeaderboard: vi.fn() }
})

const { fetchLeaderboard } = await import('../services/review')
const fetchMock = vi.mocked(fetchLeaderboard)

/** One board row. Total is the sum of the yes/no split, as the backend sends. */
function entry(uid: string, name: string, yes: number, no: number): LeaderboardEntry {
  return {
    user_uid: uid,
    display_name: name,
    yes_count: yes,
    no_count: no,
    total: yes + no,
    streak_days: 0,
    is_me: false,
  }
}

/** Wraps rows in a leaderboard response for a window. */
function board(entries: LeaderboardEntry[], window: Leaderboard['window'] = 'all'): Leaderboard {
  return { window, caller_uid: 'u2', entries }
}

/** A signed-in user; `uid` decides which board row is highlighted as "you",
 * `isAdmin` whether the per-user decision click-through is offered, and
 * `canWrite` whether the invitations into the review game are shown at all. */
function auth(uid: string, isAdmin = false, canWrite = isAdmin): AuthContextValue {
  let role: Role = 'viewer'
  if (isAdmin) {
    role = 'admin'
  } else if (canWrite) {
    role = 'editor'
  }
  return {
    status: 'authenticated',
    user: { uid, username: 'me', display_name: 'Me', role },
    role,
    downloadToken: null,
    canWrite,
    isAdmin,
    login: vi.fn(),
    logout: vi.fn(),
    refresh: vi.fn(),
  } as unknown as AuthContextValue
}

/** Reflects the current `window` query param so a test can assert the URL. */
function WindowProbe() {
  const [params] = useSearchParams()
  return <span data-testid="window-probe">{params.get('window') ?? ''}</span>
}

function renderPage(uid = 'u2', isAdmin = false, canWrite = isAdmin) {
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={auth(uid, isAdmin, canWrite)}>
        <MemoryRouter initialEntries={['/leaderboard']}>
          <WindowProbe />
          <LeaderboardPage />
        </MemoryRouter>
      </AuthContext.Provider>
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  fetchMock.mockReset()
})

describe('LeaderboardPage', () => {
  it('renders the standings ranked with the Ano/Ne split', async () => {
    fetchMock.mockResolvedValue(
      board([entry('u1', 'Alice', 10, 2), entry('u2', 'Bob', 5, 1), entry('u3', 'Cyril', 3, 0)]),
    )
    renderPage()

    expect(await screen.findByText('Alice')).toBeInTheDocument()
    // Every player's total is shown alongside the yes/no counts.
    const aliceRow = screen.getByTestId('leaderboard-row-u1')
    expect(aliceRow).toHaveTextContent('10')
    expect(aliceRow).toHaveTextContent('2')
    expect(aliceRow).toHaveTextContent('12')
    expect(fetchMock).toHaveBeenCalledWith('all', expect.any(AbortSignal))
  })

  it("highlights the current user's row", async () => {
    fetchMock.mockResolvedValue(board([entry('u1', 'Alice', 10, 2), entry('u2', 'Bob', 5, 1)]))
    renderPage('u2')

    const myRow = await screen.findByTestId('leaderboard-row-u2')
    expect(myRow).toHaveClass('kk-leaderboard-row--me')
    expect(within(myRow).getByText('You')).toBeInTheDocument()
    // Another player's row is neither highlighted nor tagged.
    const otherRow = screen.getByTestId('leaderboard-row-u1')
    expect(otherRow).not.toHaveClass('kk-leaderboard-row--me')
    expect(within(otherRow).queryByText('You')).toBeNull()
  })

  it('renders a medal for each of the top three players', async () => {
    fetchMock.mockResolvedValue(
      board([
        entry('u1', 'Alice', 10, 2),
        entry('u2', 'Bob', 5, 1),
        entry('u3', 'Cyril', 3, 0),
        entry('u4', 'Dana', 1, 0),
      ]),
    )
    renderPage()

    await screen.findByText('Alice')
    expect(screen.getAllByTestId('leaderboard-medal')).toHaveLength(3)
    // The fourth place gets a plain rank number, not a medal.
    expect(
      within(screen.getByTestId('leaderboard-row-u4')).queryByTestId('leaderboard-medal'),
    ).toBeNull()
  })

  it('switches the window, updating the URL query param and refetching', async () => {
    const user = userEvent.setup()
    fetchMock.mockResolvedValue(board([entry('u2', 'Bob', 5, 1)]))
    renderPage()

    await screen.findByText('Bob')
    expect(screen.getByTestId('window-probe')).toHaveTextContent('')

    await user.click(screen.getByRole('button', { name: 'Today' }))

    await waitFor(() => {
      expect(fetchMock).toHaveBeenLastCalledWith('today', expect.any(AbortSignal))
    })
    expect(screen.getByTestId('window-probe')).toHaveTextContent('today')
  })

  it('shows the empty state when no one has sorted yet', async () => {
    fetchMock.mockResolvedValue(board([]))
    renderPage('u2', false, true)

    expect(await screen.findByTestId('empty-state')).toBeInTheDocument()
    expect(screen.getByText('No decisions yet')).toBeInTheDocument()
    // The empty state invites an editor into the review game.
    expect(screen.getByRole('link', { name: /start sorting/i })).toHaveAttribute('href', '/review')
  })

  it('does not invite a viewer into a game they may not play', async () => {
    fetchMock.mockResolvedValue(board([]))
    renderPage('u2')

    expect(await screen.findByTestId('empty-state')).toBeInTheDocument()
    // /review is editors-only: offering the button would send a viewer to a 403,
    // which is exactly the "broken button" this page was reported for.
    expect(screen.queryByRole('link', { name: /start sorting/i })).toBeNull()
    expect(screen.getByText(/once somebody starts sorting/i)).toBeInTheDocument()
  })

  it('links each player to their decision history for an admin', async () => {
    fetchMock.mockResolvedValue(board([entry('u1', 'Alice', 10, 2), entry('u2', 'Bob', 5, 1)]))
    renderPage('u2', true)

    const aliceRow = await screen.findByTestId('leaderboard-row-u1')
    const link = within(aliceRow).getByRole('link', { name: /Alice/i })
    expect(link).toHaveAttribute('href', '/audit/reviews?user=u1')
  })

  it('shows plain player names to a non-admin, with no click-through', async () => {
    fetchMock.mockResolvedValue(board([entry('u1', 'Alice', 10, 2)]))
    renderPage('u2')

    const aliceRow = await screen.findByTestId('leaderboard-row-u1')
    expect(within(aliceRow).queryByRole('link')).toBeNull()
    expect(within(aliceRow).getByText('Alice')).toBeInTheDocument()
  })

  it('hints the way in when the caller has no row yet', async () => {
    fetchMock.mockResolvedValue(board([entry('u1', 'Alice', 10, 2)]))
    renderPage('nobody', false, true)

    expect(await screen.findByTestId('leaderboard-not-on-board')).toBeInTheDocument()
    expect(
      within(screen.getByTestId('leaderboard-not-on-board')).getByRole('link'),
    ).toHaveAttribute('href', '/review')
  })

  it('keeps the way-in hint from a viewer, who can never take it', async () => {
    fetchMock.mockResolvedValue(board([entry('u1', 'Alice', 10, 2)]))
    renderPage('nobody')

    expect(await screen.findByText('Alice')).toBeInTheDocument()
    expect(screen.queryByTestId('leaderboard-not-on-board')).toBeNull()
  })
})
