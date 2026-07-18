import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter, useLocation } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
import i18n from '../i18n'
import { type AuditListResponse, type AuditRecord } from '../services/audit'
import { type Leaderboard } from '../services/review'

import { ReviewDecisionsPage } from './ReviewDecisionsPage'

vi.mock('../services/audit', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/audit')>()
  return { ...actual, fetchAuditLog: vi.fn() }
})
vi.mock('../services/review', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/review')>()
  return { ...actual, fetchLeaderboard: vi.fn() }
})
vi.mock('../services/people', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/people')>()
  return { ...actual, fetchSubjects: vi.fn() }
})
vi.mock('../services/organize', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/organize')>()
  return { ...actual, fetchLabels: vi.fn() }
})

const { fetchAuditLog } = await import('../services/audit')
const { fetchLeaderboard } = await import('../services/review')
const { fetchSubjects } = await import('../services/people')
const { fetchLabels } = await import('../services/organize')
const auditMock = vi.mocked(fetchAuditLog)
const boardMock = vi.mocked(fetchLeaderboard)
const subjectsMock = vi.mocked(fetchSubjects)
const labelsMock = vi.mocked(fetchLabels)

/** Builds one via=review audit record for the decision view. */
function record(
  id: number,
  action: string,
  targetUid: string | null,
  details: Record<string, unknown>,
): AuditRecord {
  return {
    id,
    actor_uid: 'u1',
    action,
    target_type: '',
    target_uid: targetUid,
    details: { via: 'review', ...details },
    ip: null,
    user_agent: null,
    created_at: '2026-07-01T10:00:00Z',
  }
}

/** Wraps records in an audit list response with paging metadata. */
function response(entries: AuditRecord[], total = entries.length): AuditListResponse {
  return { entries, total, limit: 60, offset: 0, next_offset: null }
}

/** A one-row leaderboard so the header resolves u1's name and tallies. */
function leaderboard(): Leaderboard {
  return {
    window: 'all',
    caller_uid: 'admin',
    entries: [
      { user_uid: 'u1', display_name: 'Alice', yes_count: 3, no_count: 1, total: 4, is_me: false },
    ],
  }
}

/** A signed-in auth context; `isAdmin` gates the whole view. */
function auth(isAdmin: boolean): AuthContextValue {
  const role = isAdmin ? 'admin' : 'viewer'
  return {
    status: 'authenticated',
    user: { uid: 'admin', username: 'root', display_name: 'Root', role },
    role,
    downloadToken: null,
    canWrite: isAdmin,
    isAdmin,
    login: vi.fn(),
    logout: vi.fn(),
    refresh: vi.fn(),
  } as unknown as AuthContextValue
}

/** Reflects the current path + query so a test can assert the URL state. */
function LocationProbe() {
  const location = useLocation()
  return <span data-testid="location">{location.pathname + location.search}</span>
}

function renderPage(isAdmin = true, initialEntry = '/audit/reviews?user=u1') {
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={auth(isAdmin)}>
        <MemoryRouter initialEntries={[initialEntry]}>
          <ReviewDecisionsPage />
          <LocationProbe />
        </MemoryRouter>
      </AuthContext.Provider>
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  auditMock.mockReset()
  boardMock.mockReset()
  subjectsMock.mockReset()
  labelsMock.mockReset()
  boardMock.mockResolvedValue(leaderboard())
  subjectsMock.mockResolvedValue([])
  labelsMock.mockResolvedValue([])
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('ReviewDecisionsPage', () => {
  it('renders decisions split into Ano/Ne with a photo thumbnail', async () => {
    auditMock.mockResolvedValue(
      response([
        record(1, 'face.assign', 'mk-1', {
          photo_uid: 'ph-1',
          subject_uid: 'su-1',
          subject_name: 'Grandma',
          face_index: 0,
        }),
        record(2, 'label.reject', 'lb-1', { photo_uid: 'ph-2' }),
      ]),
    )

    renderPage()

    // The header resolves the user's name and tallies from the leaderboard.
    expect(await screen.findByRole('heading', { name: 'Alice' })).toBeInTheDocument()
    const tallies = screen.getByTestId('decision-tallies')
    expect(tallies).toHaveTextContent('3')
    expect(tallies).toHaveTextContent('1')
    expect(tallies).toHaveTextContent('4')

    // The confirmation is an "Ano" (Yes) on the resolved subject, with its photo.
    const yesRow = screen.getByTestId('decision-row-1')
    expect(within(yesRow).getByText('Yes')).toBeInTheDocument()
    expect(within(yesRow).getByText('Grandma')).toBeInTheDocument()
    const thumb = within(yesRow).getByTestId('decision-thumb')
    expect(thumb).toHaveAttribute('src', expect.stringContaining('ph-1'))

    // The rejection is a "Ne" (No).
    const noRow = screen.getByTestId('decision-row-2')
    expect(within(noRow).getByText('No')).toBeInTheDocument()

    // The listing was filtered to this user's review decisions.
    expect(auditMock).toHaveBeenCalledWith(
      expect.objectContaining({ user: 'u1', via: 'review' }),
      expect.any(AbortSignal),
    )
  })

  it('shows an empty state when the user has no recorded decisions', async () => {
    auditMock.mockResolvedValue(response([]))

    renderPage()

    expect(await screen.findByTestId('empty-state')).toBeInTheDocument()
    expect(screen.getByText('No decisions')).toBeInTheDocument()
  })

  it('filters to the Ne bucket, updating the URL and refetching', async () => {
    const user = userEvent.setup()
    auditMock.mockResolvedValue(
      response([record(2, 'label.reject', 'lb-1', { photo_uid: 'ph-2' })]),
    )

    renderPage()

    await screen.findByTestId('decision-row-2')
    await user.click(screen.getByRole('button', { name: 'No' }))

    await waitFor(() => {
      expect(auditMock).toHaveBeenLastCalledWith(
        expect.objectContaining({ decision: 'no' }),
        expect.any(AbortSignal),
      )
    })
    expect(screen.getByTestId('location')).toHaveTextContent('decision=no')
  })

  it('prompts to pick a player when no user is selected', async () => {
    renderPage(true, '/audit/reviews')

    expect(await screen.findByText('No user selected')).toBeInTheDocument()
    // With no user, the listing endpoint is never hit.
    expect(auditMock).not.toHaveBeenCalled()
  })

  it('refuses to render for a non-admin', () => {
    renderPage(false)

    expect(screen.getByText('This view is available to administrators only.')).toBeInTheDocument()
    expect(auditMock).not.toHaveBeenCalled()
  })
})
