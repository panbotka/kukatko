import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter, useLocation } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
import i18n from '../i18n'
import { type AuditListResponse, type AuditRecord } from '../services/audit'
import { type AdminUser } from '../services/users'

import { AuditPage } from './AuditPage'

// Mock the network layer only, keeping the real types and helpers.
vi.mock('../services/audit', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/audit')>()
  return { ...actual, fetchAuditLog: vi.fn() }
})
vi.mock('../services/users', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/users')>()
  return { ...actual, fetchUsers: vi.fn() }
})

const { fetchAuditLog } = await import('../services/audit')
const { fetchUsers } = await import('../services/users')
const fetchAuditMock = vi.mocked(fetchAuditLog)
const fetchUsersMock = vi.mocked(fetchUsers)

const ME = 'me-admin'

/** A stub auth context: admin (default) or a viewer to exercise the guard. */
function auth(isAdmin: boolean): AuthContextValue {
  const role = isAdmin ? 'admin' : 'viewer'
  return {
    status: 'authenticated',
    user: { uid: ME, username: 'root', display_name: 'Root', role },
    role,
    downloadToken: null,
    canWrite: isAdmin,
    isAdmin,
    canImport: isAdmin,
    login: vi.fn(),
    logout: vi.fn(),
    refresh: vi.fn(),
  } as unknown as AuthContextValue
}

function record(overrides: Partial<AuditRecord> = {}): AuditRecord {
  return {
    id: 1,
    actor_uid: 'us1',
    action: 'photo.update',
    target_type: 'photos',
    target_uid: 'ph9',
    details: { field: 'title' },
    ip: '10.0.0.1',
    user_agent: 'curl/8',
    created_at: '2026-07-11T10:00:00Z',
    ...overrides,
  }
}

function response(
  entries: AuditRecord[],
  nextOffset: number | null = null,
  total = entries.length,
  offset = 0,
): AuditListResponse {
  return { entries, total, limit: 100, offset, next_offset: nextOffset }
}

const ROSTER: AdminUser[] = [
  {
    uid: 'us1',
    username: 'ada',
    display_name: 'Ada Admin',
    email: '',
    role: 'admin',
    disabled: false,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    note: '',
  },
]

/** The shared setup stubs a non-matching (desktop) `matchMedia`; restore it after. */
const realMatchMedia = window.matchMedia

/**
 * Points `window.matchMedia` at a fixed phone/desktop answer, so
 * `useIsNarrowViewport` — and through it the log's table/card choice — takes the
 * branch under test.
 */
function mockViewport(narrow: boolean): void {
  window.matchMedia = vi.fn().mockImplementation((query: string) => ({
    matches: narrow,
    media: query,
    onchange: null,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    addListener: vi.fn(),
    removeListener: vi.fn(),
    dispatchEvent: vi.fn(),
  }))
}

/** Surfaces the current location for URL-state assertions. */
function LocationProbe() {
  const location = useLocation()
  return <span data-testid="location">{location.pathname + location.search}</span>
}

function renderPage(isAdmin = true, initialEntry = '/audit') {
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={auth(isAdmin)}>
        <MemoryRouter initialEntries={[initialEntry]}>
          <AuditPage />
          <LocationProbe />
        </MemoryRouter>
      </AuthContext.Provider>
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  fetchAuditMock.mockReset()
  fetchUsersMock.mockReset()
  fetchUsersMock.mockResolvedValue(ROSTER)
  fetchAuditMock.mockResolvedValue(response([record()]))
})

afterEach(() => {
  window.matchMedia = realMatchMedia
})

describe('AuditPage', () => {
  it('renders entries from the service and resolves the actor name', async () => {
    fetchAuditMock.mockResolvedValue(response([record()], null, 42))
    renderPage()

    // The actor UID is shown as the resolved roster name once users load.
    expect(await screen.findByRole('cell', { name: 'Ada Admin' })).toBeInTheDocument()
    expect(screen.getByRole('cell', { name: 'photo.update' })).toBeInTheDocument()
    expect(screen.getByText('ph9')).toBeInTheDocument()
    expect(screen.getByText(/Showing 1–1 of 42/)).toBeInTheDocument()
  })

  it.each([
    ['photos', 'ph9', '/photos/ph9'],
    ['albums', 'al7', '/albums/al7'],
    ['labels', 'lb5', '/labels/lb5'],
    ['subjects', 'su3', '/people/su3'],
  ])('links a %s target straight to the thing that was edited', async (type, uid, href) => {
    fetchAuditMock.mockResolvedValue(
      response([record({ target_type: type, target_uid: uid, details: null })]),
    )
    renderPage()

    expect(await screen.findByRole('link', { name: uid })).toHaveAttribute('href', href)
  })

  it('links a face entry to the photo its marker sits on, on that person', async () => {
    fetchAuditMock.mockResolvedValue(
      response([
        record({
          action: 'face.assign',
          target_type: 'markers',
          target_uid: 'mk3',
          details: { action: 'create_marker', photo_uid: 'ph8', subject_uid: 'su4' },
        }),
      ]),
    )
    renderPage()

    // A marker UID addresses no page of its own, so the target link is routed
    // through the photo the details name.
    expect(await screen.findByRole('link', { name: 'mk3' })).toHaveAttribute(
      'href',
      '/photos/ph8?person=su4&info=1',
    )
  })

  it('leaves a target with no page of its own as plain text', async () => {
    fetchAuditMock.mockResolvedValue(
      response([record({ action: 'user.update', target_type: 'users', target_uid: 'us7' })]),
    )
    renderPage()

    expect(await screen.findByText('us7')).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: 'us7' })).toBeNull()
  })

  it('links the UIDs inside the details, where a bulk action lists its targets', async () => {
    const user = userEvent.setup()
    fetchAuditMock.mockResolvedValue(
      response([
        record({
          action: 'photos.bulk',
          target_type: 'photos',
          target_uid: null,
          details: { photo_uids: ['ph1', 'ph2'], album_uid: 'al7', count: 2 },
        }),
      ]),
    )
    renderPage()
    await screen.findByRole('table')

    await user.click(screen.getByRole('button', { name: 'Show details' }))

    const links = screen.getByTestId('audit-links')
    expect(within(links).getByText('photo_uids')).toBeInTheDocument()
    expect(within(links).getByRole('link', { name: 'ph1' })).toHaveAttribute('href', '/photos/ph1')
    expect(within(links).getByRole('link', { name: 'ph2' })).toHaveAttribute('href', '/photos/ph2')
    expect(within(links).getByRole('link', { name: 'al7' })).toHaveAttribute('href', '/albums/al7')
    // The raw payload is still there underneath, unchanged.
    expect(screen.getByText(/"count": 2/)).toBeInTheDocument()
  })

  it('links the photo a label rejection was made on, not just the label', async () => {
    const user = userEvent.setup()
    fetchAuditMock.mockResolvedValue(
      response([
        record({
          action: 'label.reject',
          target_type: 'labels',
          target_uid: 'lbl4',
          details: { via: 'review', photo_uid: 'ph9e' },
        }),
      ]),
    )
    renderPage()
    await screen.findByRole('table')

    await user.click(screen.getByRole('button', { name: 'Show details' }))

    // The target is the label, but the entry happened on a photo — reachable
    // only through the details.
    expect(screen.getByRole('link', { name: 'lbl4' })).toHaveAttribute('href', '/labels/lbl4')
    expect(
      within(screen.getByTestId('audit-links')).getByRole('link', { name: 'ph9e' }),
    ).toHaveAttribute('href', '/photos/ph9e')
  })

  it('applies a filter to the request params and reflects it in the URL', async () => {
    const user = userEvent.setup()
    renderPage()
    await screen.findByRole('table')

    await user.type(screen.getByLabelText('Action'), 'photo.delete')
    await user.click(screen.getByRole('button', { name: 'Filter' }))

    await waitFor(() => {
      expect(fetchAuditMock).toHaveBeenCalledWith(
        expect.objectContaining({ action: 'photo.delete', offset: 0 }),
        expect.anything(),
      )
    })
    expect(screen.getByTestId('location')).toHaveTextContent('action=photo.delete')
  })

  it('filters by actor through the roster select', async () => {
    const user = userEvent.setup()
    renderPage()
    await screen.findByRole('option', { name: 'Ada Admin' })

    await user.selectOptions(screen.getByLabelText('Actor'), 'us1')
    await user.click(screen.getByRole('button', { name: 'Filter' }))

    await waitFor(() => {
      expect(fetchAuditMock).toHaveBeenCalledWith(
        expect.objectContaining({ user: 'us1' }),
        expect.anything(),
      )
    })
    expect(screen.getByTestId('location')).toHaveTextContent('user=us1')
  })

  it('paginates to the next page using next_offset', async () => {
    const user = userEvent.setup()
    fetchAuditMock.mockResolvedValueOnce(response([record({ id: 1 })], 100, 150))
    renderPage()
    await screen.findByRole('table')

    fetchAuditMock.mockResolvedValueOnce(
      response([record({ id: 2, action: 'photo.delete' })], null, 150, 100),
    )
    await user.click(screen.getByRole('button', { name: 'Next' }))

    await waitFor(() => {
      expect(fetchAuditMock).toHaveBeenLastCalledWith(
        expect.objectContaining({ offset: 100 }),
        expect.anything(),
      )
    })
    expect(screen.getByTestId('location')).toHaveTextContent('offset=100')
  })

  it('reads the filter state from the URL on load', async () => {
    renderPage(true, '/audit?action=photo.delete&entity_type=photos')

    await waitFor(() => {
      expect(fetchAuditMock).toHaveBeenCalledWith(
        expect.objectContaining({ action: 'photo.delete', entity_type: 'photos' }),
        expect.anything(),
      )
    })
    expect(screen.getByLabelText('Action')).toHaveValue('photo.delete')
    expect(screen.getByLabelText('Entity type')).toHaveValue('photos')
  })

  it('reveals the raw details payload when a row is expanded', async () => {
    const user = userEvent.setup()
    renderPage()
    await screen.findByRole('table')

    await user.click(screen.getByRole('button', { name: 'Show details' }))

    expect(screen.getByText(/"field": "title"/)).toBeInTheDocument()
  })

  it('renders a details.changes map as an old → new table', async () => {
    const user = userEvent.setup()
    fetchAuditMock.mockResolvedValue(
      response([
        record({
          details: {
            fields: ['title', 'lat'],
            changes: {
              title: { old: 'stary popisek', new: 'novy popisek' },
              lat: { old: 50.1, new: null },
            },
          },
        }),
      ]),
    )
    renderPage()
    await screen.findByRole('table')

    await user.click(screen.getByRole('button', { name: 'Show details' }))

    // A dedicated old → new table replaces the raw-JSON dump for an edit record.
    const table = screen.getByTestId('audit-changes')
    expect(within(table).getByText('Field')).toBeInTheDocument()
    expect(within(table).getByText('Old')).toBeInTheDocument()
    expect(within(table).getByText('New')).toBeInTheDocument()
    // Both the previous and the new caption are shown for the title field.
    expect(within(table).getByText('stary popisek')).toBeInTheDocument()
    expect(within(table).getByText('novy popisek')).toBeInTheDocument()
    // A cleared field (lat → null) shows its old value and a muted em-dash for the
    // new one, not the literal "null".
    expect(within(table).getByText('50.1')).toBeInTheDocument()
    expect(within(table).getByText('—')).toBeInTheDocument()
    expect(table.querySelector('pre')).toBeNull()
  })

  it('keeps the six-column table on a wide viewport', async () => {
    renderPage()

    const table = await screen.findByRole('table')
    expect(
      within(table)
        .getAllByRole('columnheader')
        .map((th) => th.textContent),
    ).toEqual(['When', 'Who', 'Action', 'Target', 'IP', 'Details'])
    expect(screen.queryByRole('listitem')).toBeNull()
  })

  it('reflows each entry into a stacked card on a phone', async () => {
    mockViewport(true)
    renderPage()

    expect(await screen.findByText('photo.update')).toBeInTheDocument()
    // The wide table is gone entirely — nothing left to drag sideways.
    expect(screen.queryByRole('table')).toBeNull()

    const card = screen.getByRole('listitem')
    for (const label of ['When', 'Who', 'Action', 'Target', 'IP', 'Details']) {
      expect(within(card).getByText(label)).toBeInTheDocument()
    }
    expect(within(card).getByText('Ada Admin')).toBeInTheDocument()
    expect(within(card).getByText('ph9')).toBeInTheDocument()
    expect(within(card).getByText('10.0.0.1')).toBeInTheDocument()
    // Pagination still frames the card stack.
    expect(screen.getByText(/Showing 1–1 of 1/)).toBeInTheDocument()
  })

  it('expands the details inside the entry’s own card on a phone', async () => {
    mockViewport(true)
    const user = userEvent.setup()
    renderPage()

    expect(await screen.findByText('photo.update')).toBeInTheDocument()
    const toggle = screen.getByRole('button', { name: 'Show details' })
    await user.click(toggle)

    // The payload lands in the same card, and the toggle still names the block it
    // controls — the reflow must not break the expand/collapse wiring.
    const card = screen.getByRole('listitem')
    const payload = within(card).getByText(/"field": "title"/)
    expect(payload).toBeInTheDocument()
    expect(toggle).toHaveAttribute('aria-expanded', 'true')
    expect(payload.closest('dl')).toHaveAttribute('id', toggle.getAttribute('aria-controls'))
    // The raw JSON wraps inside its own box instead of widening the listing.
    expect(payload).toHaveClass('kk-audit-payload')
  })

  it('gives the raw payload its own wrapping box in the expanded table row', async () => {
    const user = userEvent.setup()
    renderPage()
    await screen.findByRole('table')

    await user.click(screen.getByRole('button', { name: 'Show details' }))

    expect(screen.getByText(/"field": "title"/)).toHaveClass('kk-audit-payload')
  })

  it('shows the empty state when no entries match', async () => {
    fetchAuditMock.mockResolvedValue(response([]))
    renderPage()

    expect(await screen.findByText('No entries')).toBeInTheDocument()
  })

  it('shows an error with a retry that refetches', async () => {
    const user = userEvent.setup()
    fetchAuditMock.mockRejectedValueOnce(new Error('boom'))
    renderPage()

    expect(await screen.findByText('The audit log could not be loaded.')).toBeInTheDocument()

    fetchAuditMock.mockResolvedValueOnce(response([record()]))
    await user.click(screen.getByRole('button', { name: 'Try again' }))

    expect(await screen.findByRole('table')).toBeInTheDocument()
  })

  it('denies access to non-admins and never fetches', () => {
    renderPage(false)

    expect(screen.getByText('This page is available to administrators only.')).toBeInTheDocument()
    expect(fetchAuditMock).not.toHaveBeenCalled()
  })
})
