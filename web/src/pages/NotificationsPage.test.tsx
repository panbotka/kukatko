import { act, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { type ComponentType, type ReactNode } from 'react'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../i18n'
import { NotificationInboxContext } from '../notifications/NotificationInboxContext'
import { type NotificationRecord } from '../services/notifications'

import { NotificationsPage, PAGE_SIZE } from './NotificationsPage'

/**
 * jsdom has no layout, so the real Virtuoso mounts nothing. This stand-in
 * renders every item plus the footer, and exposes `endReached` so a test can
 * play the reader scrolling to the bottom.
 */
interface MockListProps {
  data: NotificationRecord[]
  context: unknown
  endReached: () => void
  components: { Footer: ComponentType<{ context: unknown }> }
  itemContent: (index: number, item: NotificationRecord) => ReactNode
  computeItemKey: (index: number, item: NotificationRecord) => string
}

const virtuoso = vi.hoisted(() => ({ endReached: null as (() => void) | null }))

vi.mock('react-virtuoso', () => ({
  Virtuoso: (props: MockListProps) => {
    virtuoso.endReached = props.endReached
    const { Footer } = props.components
    return (
      <div>
        {props.data.map((item, index) => (
          <div key={props.computeItemKey(index, item)}>{props.itemContent(index, item)}</div>
        ))}
        <Footer context={props.context} />
      </div>
    )
  },
}))

vi.mock('../services/notifications', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/notifications')>()
  return {
    ...actual,
    fetchNotifications: vi.fn(),
    markAllNotificationsRead: vi.fn(),
    markNotificationRead: vi.fn(),
  }
})

const api = await import('../services/notifications')
const listMock = vi.mocked(api.fetchNotifications)
const markAllMock = vi.mocked(api.markAllNotificationsRead)
const markOneMock = vi.mocked(api.markNotificationRead)

/** The i-th notification of a long inbox, newest first; even ones are unread. */
function note(i: number): NotificationRecord {
  return {
    uid: `nt${String(i)}`,
    kind: 'tagged',
    title: `Notification ${String(i)}`,
    body: '',
    link: '',
    created_at: new Date(Date.now() - i * 60_000).toISOString(),
    read_at: i % 2 === 0 ? null : new Date().toISOString(),
  }
}

/** Prints where the router is. */
function Where() {
  const location = useLocation()
  return <output data-testid="where">{location.pathname + location.search}</output>
}

function renderPage(path = '/notifications', refresh = vi.fn()) {
  render(
    <I18nextProvider i18n={i18n}>
      <NotificationInboxContext.Provider value={{ unread: 1, refresh }}>
        <MemoryRouter initialEntries={[path]}>
          <Routes>
            <Route
              path="/notifications"
              element={
                <>
                  <NotificationsPage />
                  <Where />
                </>
              }
            />
            <Route path="*" element={<Where />} />
          </Routes>
        </MemoryRouter>
      </NotificationInboxContext.Provider>
    </I18nextProvider>,
  )
  return refresh
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  listMock.mockReset()
  markAllMock.mockReset()
  markAllMock.mockResolvedValue(1)
  markOneMock.mockReset()
  markOneMock.mockResolvedValue(note(0))
  virtuoso.endReached = null
})

afterEach(async () => {
  await i18n.changeLanguage('en')
})

describe('NotificationsPage', () => {
  it('lists the inbox and loads the next page at the end of the list', async () => {
    const first = Array.from({ length: PAGE_SIZE }, (_, i) => note(i))
    listMock.mockResolvedValueOnce({
      notifications: first,
      total: PAGE_SIZE + 2,
      limit: PAGE_SIZE,
      offset: 0,
    })
    listMock.mockResolvedValueOnce({
      notifications: [note(PAGE_SIZE), note(PAGE_SIZE + 1)],
      total: PAGE_SIZE + 2,
      limit: PAGE_SIZE,
      offset: PAGE_SIZE,
    })
    renderPage()

    expect(await screen.findByRole('heading', { name: 'Notifications' })).toBeInTheDocument()
    expect(await screen.findByRole('link', { name: /Unread: Notification 0/ })).toHaveAttribute(
      'href',
      '/n/nt0',
    )
    expect(listMock).toHaveBeenLastCalledWith(
      { unread: false, limit: PAGE_SIZE },
      expect.any(AbortSignal),
    )

    virtuoso.endReached?.()
    expect(
      await screen.findByRole('link', {
        name: new RegExp(`^Notification ${String(PAGE_SIZE + 1)} `),
      }),
    ).toBeInTheDocument()
    expect(listMock).toHaveBeenLastCalledWith(
      { unread: false, limit: PAGE_SIZE, offset: PAGE_SIZE },
      expect.any(AbortSignal),
    )

    // Everything is loaded: reaching the end again asks for nothing.
    act(() => {
      virtuoso.endReached?.()
    })
    expect(listMock).toHaveBeenCalledTimes(2)
  })

  it('keeps the unread filter in the URL', async () => {
    const user = userEvent.setup()
    listMock.mockResolvedValue({ notifications: [note(0)], total: 1, limit: PAGE_SIZE, offset: 0 })
    renderPage()
    await screen.findByRole('link', { name: /Notification 0/ })

    await user.click(screen.getByRole('radio', { name: 'Unread' }))

    expect(screen.getByTestId('where')).toHaveTextContent('/notifications?unread=1')
    await waitFor(() => {
      expect(listMock).toHaveBeenLastCalledWith(
        { unread: true, limit: PAGE_SIZE },
        expect.any(AbortSignal),
      )
    })
  })

  it('marks an entry read when it is opened, and everything at once', async () => {
    const user = userEvent.setup()
    listMock.mockResolvedValue({
      notifications: [note(0), note(1)],
      total: 2,
      limit: PAGE_SIZE,
      offset: 0,
    })
    const refresh = renderPage()

    await user.click(await screen.findByRole('button', { name: 'Mark all as read' }))
    expect(markAllMock).toHaveBeenCalledTimes(1)
    await waitFor(() => {
      expect(refresh).toHaveBeenCalled()
    })
    await waitFor(() => {
      expect(listMock).toHaveBeenCalledTimes(2)
    })

    await user.click(await screen.findByRole('link', { name: /Notification 0/ }))
    expect(markOneMock).toHaveBeenCalledWith('nt0')
    expect(screen.getByTestId('where')).toHaveTextContent('/n/nt0')
  })

  it('says how to fill an empty inbox, and that an empty unread view means caught up', async () => {
    listMock.mockResolvedValue({ notifications: [], total: 0, limit: PAGE_SIZE, offset: 0 })
    renderPage()
    expect(await screen.findByText('No notifications yet.')).toBeInTheDocument()
    expect(screen.getByText(/in your account settings/)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Mark all as read' })).toBeNull()
  })

  it('shows the caught-up state for the unread filter', async () => {
    listMock.mockResolvedValue({ notifications: [], total: 0, limit: PAGE_SIZE, offset: 0 })
    renderPage('/notifications?unread=1')
    expect(await screen.findByText('You are all caught up.')).toBeInTheDocument()
  })

  it('offers a retry when the inbox cannot be read', async () => {
    const user = userEvent.setup()
    listMock.mockRejectedValueOnce(new Error('offline'))
    listMock.mockResolvedValue({ notifications: [note(1)], total: 1, limit: PAGE_SIZE, offset: 0 })
    renderPage()

    expect(await screen.findByText('The notifications could not be loaded.')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: /retry|try again/i }))
    expect(await screen.findByRole('link', { name: /Notification 1/ })).toBeInTheDocument()
  })
})
