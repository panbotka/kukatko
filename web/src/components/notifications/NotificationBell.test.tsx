import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'
import { NotificationInboxContext } from '../../notifications/NotificationInboxContext'
import { type NotificationRecord } from '../../services/notifications'

import { badgeText, BELL_LIMIT } from './bell'
import { NotificationBell } from './NotificationBell'

vi.mock('../../services/notifications', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../services/notifications')>()
  return {
    ...actual,
    fetchNotifications: vi.fn(),
    markAllNotificationsRead: vi.fn(),
    markNotificationRead: vi.fn(),
  }
})

const api = await import('../../services/notifications')
const listMock = vi.mocked(api.fetchNotifications)
const markAllMock = vi.mocked(api.markAllNotificationsRead)
const markOneMock = vi.mocked(api.markNotificationRead)

const UNREAD: NotificationRecord = {
  uid: 'nt1',
  kind: 'tagged',
  title: 'You were tagged in photos',
  body: 'You were tagged in 3 photos.',
  link: '/n/nt1',
  created_at: new Date(Date.now() - 5 * 60_000).toISOString(),
  read_at: null,
}

const READ: NotificationRecord = {
  uid: 'nt2',
  kind: 'registration_pending',
  title: 'A new registration is waiting',
  body: '',
  link: '/users',
  created_at: new Date(Date.now() - 3 * 86_400_000).toISOString(),
  read_at: new Date().toISOString(),
}

/** Prints where the router is, so a test can see a link was followed. */
function Where() {
  return <output data-testid="where">{useLocation().pathname}</output>
}

/** Mounts the bell over an inbox context holding `unread`. */
function renderBell(unread: number | null, refresh = vi.fn()) {
  render(
    <I18nextProvider i18n={i18n}>
      <NotificationInboxContext.Provider value={{ unread, unreadByProject: null, refresh }}>
        <MemoryRouter initialEntries={['/']}>
          <NotificationBell />
          <Routes>
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
  listMock.mockResolvedValue({ notifications: [UNREAD, READ], total: 2, limit: 8, offset: 0 })
  markAllMock.mockReset()
  markAllMock.mockResolvedValue(1)
  markOneMock.mockReset()
  markOneMock.mockResolvedValue({ ...UNREAD, read_at: new Date().toISOString() })
})

afterEach(async () => {
  await i18n.changeLanguage('en')
})

describe('NotificationBell', () => {
  it('shows the unread count on its corner and says it out loud', () => {
    renderBell(3)

    const bell = screen.getByRole('button', { name: 'Notifications — 3 unread' })
    expect(within(bell).getByTestId('bell-badge')).toHaveTextContent('3')
    expect(bell.querySelector('.bi-bell-fill')).not.toBeNull()
  })

  it('draws no badge for zero or an unknown count', () => {
    renderBell(null)
    const bell = screen.getByRole('button', { name: 'Notifications' })
    expect(within(bell).queryByTestId('bell-badge')).toBeNull()
    expect(bell.querySelector('.bi-bell')).not.toBeNull()
  })

  it('caps the badge at 99+', () => {
    expect(badgeText(7)).toBe('7')
    expect(badgeText(99)).toBe('99')
    expect(badgeText(100)).toBe('99+')
  })

  it('lists the newest notifications when opened, the unread one marked', async () => {
    const user = userEvent.setup()
    const refresh = renderBell(1)

    await user.click(screen.getByRole('button', { name: /Notifications/ }))

    expect(listMock).toHaveBeenCalledWith({ limit: BELL_LIMIT }, expect.any(AbortSignal))
    expect(refresh).toHaveBeenCalled()
    const unread = await screen.findByRole('link', { name: /Unread: You were tagged in photos/ })
    expect(unread).toHaveAttribute('href', '/p/v/n/nt1')
    expect(unread.querySelector('time')).toHaveTextContent('5m ago')
    const read = screen.getByRole('link', { name: /A new registration is waiting/ })
    expect(read).toHaveAttribute('href', '/p/v/users')
    expect(read).not.toHaveAccessibleName(/Unread/)
    expect(screen.getByRole('link', { name: 'Show all notifications' })).toHaveAttribute(
      'href',
      '/p/v/notifications',
    )
  })

  it('marks an unread entry read when it is chosen and follows its link', async () => {
    const user = userEvent.setup()
    const refresh = renderBell(1)
    await user.click(screen.getByRole('button', { name: /Notifications/ }))
    refresh.mockClear()

    await user.click(await screen.findByRole('link', { name: /You were tagged in photos/ }))

    expect(markOneMock).toHaveBeenCalledWith('nt1')
    expect(screen.getByTestId('where')).toHaveTextContent('/p/v/n/nt1')
    await waitFor(() => {
      expect(refresh).toHaveBeenCalled()
    })
  })

  it('does not mark an entry that was already read', async () => {
    const user = userEvent.setup()
    renderBell(1)
    await user.click(screen.getByRole('button', { name: /Notifications/ }))

    await user.click(await screen.findByRole('link', { name: /A new registration is waiting/ }))

    expect(markOneMock).not.toHaveBeenCalled()
    expect(screen.getByTestId('where')).toHaveTextContent('/p/v/users')
  })

  it('marks everything read and refreshes the count', async () => {
    const user = userEvent.setup()
    const refresh = renderBell(1)
    await user.click(screen.getByRole('button', { name: /Notifications/ }))
    await screen.findByRole('link', { name: /Unread: You were tagged/ })
    refresh.mockClear()

    await user.click(screen.getByRole('button', { name: 'Mark all as read' }))

    expect(markAllMock).toHaveBeenCalledTimes(1)
    await waitFor(() => {
      expect(refresh).toHaveBeenCalled()
    })
    expect(screen.queryByRole('link', { name: /Unread:/ })).toBeNull()
  })

  it('says the inbox is empty and where to switch it on', async () => {
    const user = userEvent.setup()
    listMock.mockResolvedValue({ notifications: [], total: 0, limit: 8, offset: 0 })
    renderBell(0)

    await user.click(screen.getByRole('button', { name: 'Notifications' }))

    expect(await screen.findByText('No notifications yet.')).toBeInTheDocument()
    expect(screen.getByText(/in your account settings/)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Mark all as read' })).toBeNull()
  })

  it('says so when the list cannot be read', async () => {
    const user = userEvent.setup()
    listMock.mockRejectedValue(new Error('offline'))
    renderBell(0)

    await user.click(screen.getByRole('button', { name: 'Notifications' }))

    expect(await screen.findByText('The notifications could not be loaded.')).toBeInTheDocument()
  })

  it('speaks Czech', async () => {
    await i18n.changeLanguage('cs')
    renderBell(3)

    expect(screen.getByRole('button', { name: 'Oznámení – 3 nepřečtená' })).toBeInTheDocument()
  })
})
