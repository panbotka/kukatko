import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter, useLocation } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { ADMIN_ITEMS, BROWSE_GROUP, PRIMARY_ITEMS, TOOLS_GROUP } from '../components/navItems'
import i18n from '../i18n'
import { ACTIVITY_PATH } from '../lib/activityView'
import { type AuditListResponse, type AuditRecord } from '../services/audit'

import { MyActivityPage } from './MyActivityPage'

// Mock the network layer only, keeping the real types and helpers.
vi.mock('../services/audit', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/audit')>()
  return { ...actual, fetchMyActivity: vi.fn() }
})

const { fetchMyActivity } = await import('../services/audit')
const fetchMock = vi.mocked(fetchMyActivity)

function record(overrides: Partial<AuditRecord> = {}): AuditRecord {
  return {
    id: 1,
    actor_uid: 'me',
    action: 'photo.update',
    target_type: 'photos',
    target_uid: 'ph9',
    details: null,
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
  return { entries, total, limit: 50, offset, next_offset: nextOffset }
}

/** Surfaces the current location so the URL-kept page offset can be asserted. */
function LocationProbe() {
  const location = useLocation()
  return <span data-testid="location">{location.pathname + location.search}</span>
}

function renderPage(initialEntry: string = ACTIVITY_PATH) {
  return render(
    <I18nextProvider i18n={i18n}>
      <MemoryRouter initialEntries={[initialEntry]}>
        <MyActivityPage />
        <LocationProbe />
      </MemoryRouter>
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  // Desktop by default, so the record table renders as a table.
  window.matchMedia = vi.fn().mockImplementation((query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    addListener: vi.fn(),
    removeListener: vi.fn(),
    dispatchEvent: vi.fn(),
  }))
  fetchMock.mockReset()
  fetchMock.mockResolvedValue(response([record()]))
})

describe('MyActivityPage', () => {
  it('lists the entries in words and never asks the server for another user', async () => {
    renderPage()

    expect(await screen.findByRole('heading', { name: 'My activity' })).toBeInTheDocument()
    // The action reads as a sentence, not as the raw `photo.update` label.
    expect(screen.getByText('Photo edited')).toBeInTheDocument()
    // No "who" column: the answer is always the reader.
    expect(screen.queryByRole('columnheader', { name: 'Who' })).not.toBeInTheDocument()
    expect(screen.getByRole('columnheader', { name: 'What' })).toBeInTheDocument()

    // The request carries paging only — the actor is the server's business.
    const params = fetchMock.mock.calls[0][0]
    expect(params).toEqual({ limit: 50, offset: 0 })
    expect(params).not.toHaveProperty('user')
  })

  it('links a row to the photo it changed', async () => {
    renderPage()

    const link = await screen.findByRole('link', { name: /Photo/ })
    expect(link).toHaveAttribute('href', '/photos/ph9')
  })

  it('links the photos of a bulk edit from its payload', async () => {
    fetchMock.mockResolvedValue(
      response([
        record({
          action: 'photos.bulk',
          target_type: '',
          target_uid: null,
          details: { photo_uids: ['ph1', 'ph2'] },
        }),
      ]),
    )
    renderPage()

    expect(await screen.findByText('Bulk photo edit')).toBeInTheDocument()
    const links = within(screen.getByTestId('activity-links')).getAllByRole('link')
    expect(links.map((link) => link.getAttribute('href'))).toEqual(['/photos/ph1', '/photos/ph2'])
  })

  it('falls back to the raw label for an action it has no words for', async () => {
    fetchMock.mockResolvedValue(response([record({ action: 'photo.teleport' })]))
    renderPage()

    expect(await screen.findByText('photo.teleport')).toBeInTheDocument()
  })

  it('pages forward and keeps the offset in the URL', async () => {
    const user = userEvent.setup()
    fetchMock.mockResolvedValue(response([record()], 50, 60))
    renderPage()

    await user.click(await screen.findByRole('button', { name: 'Next' }))

    expect(screen.getByTestId('location')).toHaveTextContent(`${ACTIVITY_PATH}?offset=50`)
    await waitFor(() => {
      expect(fetchMock).toHaveBeenLastCalledWith({ limit: 50, offset: 50 }, expect.anything())
    })
  })

  it('starts on the page named by the URL', async () => {
    fetchMock.mockResolvedValue(response([record()], null, 60, 50))
    renderPage(`${ACTIVITY_PATH}?offset=50`)

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith({ limit: 50, offset: 50 }, expect.anything())
    })
    // The first page is then one click back, not a fresh navigation.
    expect(screen.getByRole('button', { name: 'Previous' })).toBeEnabled()
  })

  it('shows an empty state when the user has done nothing yet', async () => {
    fetchMock.mockResolvedValue(response([]))
    renderPage()

    expect(await screen.findByText('Nothing here yet')).toBeInTheDocument()
  })

  it('offers a retry when the listing fails', async () => {
    fetchMock.mockRejectedValueOnce(new Error('boom'))
    renderPage()

    const retry = await screen.findByRole('button', { name: /Try again|Retry/ })
    fetchMock.mockResolvedValue(response([record()]))
    await userEvent.setup().click(retry)

    expect(await screen.findByText('Photo edited')).toBeInTheDocument()
  })
})

describe('own-activity navigation', () => {
  it('stays out of the navigation groups, the admin one above all', () => {
    for (const item of [...BROWSE_GROUP.items, ...TOOLS_GROUP.items, ...ADMIN_ITEMS]) {
      expect(item.to).not.toBe(ACTIVITY_PATH)
    }
    for (const item of PRIMARY_ITEMS) {
      expect(item.to).not.toBe(ACTIVITY_PATH)
    }
    // The admin audit log is a different page and stays where it was: behind the
    // admin gate, now in the user menu's "Správa" section.
    expect(ADMIN_ITEMS.filter((item) => item.gate === 'admin').map((item) => item.to)).toContain(
      '/audit',
    )
  })
})
