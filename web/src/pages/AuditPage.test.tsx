import { render, screen, waitFor } from '@testing-library/react'
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
  vi.restoreAllMocks()
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
