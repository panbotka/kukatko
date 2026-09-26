import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
import i18n from '../i18n'
import { isDirectEntry } from '../lib/directEntry'
import { ApiError } from '../services/auth'
import { type NotificationDetail } from '../services/notifications'
import { type Photo } from '../services/photos'

import { NotificationPage } from './NotificationPage'

// jsdom lays nothing out, so the real virtualizer mounts nothing: render it
// all instead (see `test/virtuoso`), through the real photo wall.
vi.mock('react-virtuoso', async () => (await import('../test/virtuoso')).virtuosoMock())

vi.mock('../services/notifications', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/notifications')>()
  return { ...actual, fetchNotification: vi.fn(), markNotificationRead: vi.fn() }
})

const { fetchNotification, markNotificationRead } = await import('../services/notifications')
const fetchMock = vi.mocked(fetchNotification)
const markMock = vi.mocked(markNotificationRead)

function photo(uid: string, name: string): Photo {
  return {
    uid,
    file_hash: uid,
    file_name: name,
    file_size: 1,
    file_mime: 'image/jpeg',
    file_width: 1,
    file_height: 1,
    taken_at_source: 'exif',
    thumb_url: `/api/v1/photos/${uid}/thumb/tile_500`,
    download_url: `/api/v1/photos/${uid}/download?original=true`,
    title: '',
    description: '',
    camera_make: '',
    camera_model: '',
    lens_model: '',
    is_favorite: false,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
  }
}

function detail(photos: Photo[], overrides: Partial<NotificationDetail> = {}): NotificationDetail {
  return {
    uid: 'nt1',
    kind: 'tagged',
    title: 'You were tagged in 3 photos',
    body: 'Jana tagged you.',
    link: '/n/nt1',
    created_at: '2026-09-20T10:00:00Z',
    read_at: null,
    photos,
    total_count: photos.length,
    dropped_count: 0,
    ...overrides,
  }
}

const viewer = {
  status: 'authenticated',
  user: { uid: 'u1', username: 'u', display_name: 'U', role: 'viewer' },
  role: 'viewer',
  downloadToken: null,
  canCurate: false,
  canWrite: false,
  isAdmin: false,
  login: vi.fn(),
  logout: vi.fn(),
  refresh: vi.fn(),
} as unknown as AuthContextValue

/** Where the router ended up, and the state it carried there. */
function LocationProbe() {
  const location = useLocation()
  return (
    <>
      <span data-testid="location">{`${location.pathname}${location.search}`}</span>
      <span data-testid="direct">{String(isDirectEntry(location.state))}</span>
    </>
  )
}

/** The page at `/n/nt1`, optionally with another page behind it in history. */
function tree(entries: string[]) {
  return (
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={viewer}>
        <MemoryRouter initialEntries={entries} initialIndex={entries.length - 1}>
          <Routes>
            <Route path="/n/:uid" element={<NotificationPage />} />
            <Route path="/photos/:uid" element={<div>the viewer</div>} />
            <Route path="/" element={<div>the library</div>} />
          </Routes>
          <LocationProbe />
        </MemoryRouter>
      </AuthContext.Provider>
    </I18nextProvider>
  )
}

/** Mounts {@link tree}; `rerender` re-renders the same page in place. */
function renderPage(entries: string[] = ['/n/nt1']) {
  const view = render(tree(entries))
  return {
    rerender: () => {
      view.rerender(tree(entries))
    },
  }
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  fetchMock.mockReset()
  markMock.mockReset()
  markMock.mockResolvedValue(detail([]))
})

describe('NotificationPage', () => {
  it('forwards a single photo straight to the viewer, replacing its own entry', async () => {
    fetchMock.mockResolvedValue(detail([photo('pha', 'a.jpg')]))
    renderPage()

    expect(await screen.findByText('the viewer')).toBeInTheDocument()
    expect(screen.getByTestId('location')).toHaveTextContent('/photos/pha')
    // Opened as the first entry (a push in a fresh window): the viewer is told
    // there is nothing behind it, so its close button reconstructs a way out.
    expect(screen.getByTestId('direct')).toHaveTextContent('true')
    // No grid of one ever flashed up on the way.
    expect(screen.queryByRole('link', { name: 'a.jpg' })).toBeNull()
  })

  it('leaves the viewer to step back when the app had a page behind the notification', async () => {
    fetchMock.mockResolvedValue(detail([photo('pha', 'a.jpg')]))
    renderPage(['/', '/n/nt1'])

    expect(await screen.findByText('the viewer')).toBeInTheDocument()
    expect(screen.getByTestId('direct')).toHaveTextContent('false')
  })

  it('renders several photos as the library grid, headed by the notification itself', async () => {
    fetchMock.mockResolvedValue(
      detail([photo('pha', 'a.jpg'), photo('phb', 'b.jpg'), photo('phc', 'c.jpg')]),
    )
    renderPage()

    expect(
      await screen.findByRole('heading', { name: 'You were tagged in 3 photos' }),
    ).toBeInTheDocument()
    expect(screen.getByText('Jana tagged you.')).toBeInTheDocument()
    // The frozen order, each tile opening the viewer.
    const tiles = ['a.jpg', 'b.jpg', 'c.jpg'].map((name) => screen.getByRole('link', { name }))
    expect(tiles[0]).toHaveAttribute('href', '/photos/pha')
    expect(tiles[2]).toHaveAttribute('href', '/photos/phc')
    expect(screen.getByTestId('location')).toHaveTextContent('/n/nt1')
    expect(screen.queryByTestId('notification-dropped')).toBeNull()
  })

  it('says so when the set holds no photos', async () => {
    fetchMock.mockResolvedValue(detail([]))
    renderPage()

    expect(await screen.findByText('No photos to show')).toBeInTheDocument()
    expect(screen.getByText('This notification has no photos.')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Back to the library' })).toBeInTheDocument()
  })

  it('states how many photos were dropped rather than silently showing fewer', async () => {
    fetchMock.mockResolvedValue(
      detail([photo('pha', 'a.jpg'), photo('phb', 'b.jpg')], {
        total_count: 4,
        dropped_count: 2,
      }),
    )
    renderPage()

    expect(await screen.findByText('2 photos are no longer available to you.')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'a.jpg' })).toBeInTheDocument()
  })

  it('keeps a set that shrank to one photo on its own page, to say what was dropped', async () => {
    fetchMock.mockResolvedValue(
      detail([photo('pha', 'a.jpg')], { total_count: 2, dropped_count: 1 }),
    )
    renderPage()

    expect(await screen.findByText('1 photo is no longer available to you.')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'a.jpg' })).toBeInTheDocument()
    expect(screen.getByTestId('location')).toHaveTextContent('/n/nt1')
  })

  it('explains an empty set whose photos were all dropped', async () => {
    fetchMock.mockResolvedValue(detail([], { total_count: 3, dropped_count: 3 }))
    renderPage()

    expect(await screen.findByText('No photos to show')).toBeInTheDocument()
    expect(screen.getByText('3 photos are no longer available to you.')).toBeInTheDocument()
  })

  it('renders a 404 as a friendly page with a way back to the library', async () => {
    const user = userEvent.setup()
    fetchMock.mockRejectedValue(new ApiError(404, 'notification not found'))
    renderPage()

    expect(await screen.findByText('This notification is no longer here')).toBeInTheDocument()
    // Not the error treatment, and never the raw backend message.
    expect(screen.queryByRole('alert')).toBeNull()
    expect(screen.queryByText('notification not found')).toBeNull()
    // Nothing to mark read on a notification that is not there.
    expect(markMock).not.toHaveBeenCalled()

    await user.click(screen.getByRole('link', { name: 'Back to the library' }))
    expect(await screen.findByText('the library')).toBeInTheDocument()
  })

  it('offers a retry when the notification could not be loaded', async () => {
    const user = userEvent.setup()
    fetchMock.mockRejectedValueOnce(new ApiError(500, 'boom'))
    fetchMock.mockResolvedValueOnce(detail([photo('pha', 'a.jpg'), photo('phb', 'b.jpg')]))
    renderPage()

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'The notification could not be loaded.',
    )
    await user.click(screen.getByRole('button', { name: 'Try again' }))

    expect(await screen.findByRole('link', { name: 'b.jpg' })).toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it('marks the notification read exactly once', async () => {
    fetchMock.mockResolvedValue(detail([photo('pha', 'a.jpg'), photo('phb', 'b.jpg')]))
    const { rerender } = renderPage()

    await screen.findByRole('link', { name: 'a.jpg' })
    await waitFor(() => {
      expect(markMock).toHaveBeenCalledWith('nt1')
    })
    // Re-rendering the page posts nothing more.
    rerender()
    rerender()
    await screen.findByRole('link', { name: 'b.jpg' })
    expect(markMock).toHaveBeenCalledTimes(1)
  })

  it('marks it read before forwarding a single photo to the viewer', async () => {
    fetchMock.mockResolvedValue(detail([photo('pha', 'a.jpg')]))
    renderPage()

    await screen.findByText('the viewer')
    expect(markMock).toHaveBeenCalledTimes(1)
    expect(markMock).toHaveBeenCalledWith('nt1')
  })

  it('does not post again for a notification already read', async () => {
    fetchMock.mockResolvedValue(
      detail([photo('pha', 'a.jpg'), photo('phb', 'b.jpg')], {
        read_at: '2026-09-20T11:00:00Z',
      }),
    )
    renderPage()

    await screen.findByRole('link', { name: 'a.jpg' })
    expect(markMock).not.toHaveBeenCalled()
  })

  it('speaks Czech by default, with Czech plurals', async () => {
    await i18n.changeLanguage('cs')
    fetchMock.mockResolvedValue(
      detail([photo('pha', 'a.jpg'), photo('phb', 'b.jpg')], { total_count: 5, dropped_count: 3 }),
    )
    renderPage()

    expect(await screen.findByText('3 fotky už pro vás nejsou dostupné.')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Zpět do knihovny' })).toBeInTheDocument()
  })
})
