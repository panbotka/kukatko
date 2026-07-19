import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { type ReactNode } from 'react'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
import i18n from '../i18n'
import { type Album } from '../services/organize'
import { type Photo, type PhotoListResponse } from '../services/photos'

import { AlbumDetailPage } from './AlbumDetailPage'

// Minimal stand-in for react-virtuoso's grid (jsdom has no layout).
interface MockGridProps {
  data: Photo[]
  itemContent: (index: number, item: Photo) => ReactNode
}
vi.mock('react-virtuoso', () => ({
  VirtuosoGrid: ({ data, itemContent }: MockGridProps) => (
    <div data-testid="grid">
      {data.map((item, index) => (
        <div key={item.uid}>{itemContent(index, item)}</div>
      ))}
    </div>
  ),
}))

vi.mock('../services/photos', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/photos')>()
  return { ...actual, fetchPhotos: vi.fn() }
})

vi.mock('../services/organize', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/organize')>()
  return {
    ...actual,
    fetchAlbum: vi.fn(),
    deleteAlbum: vi.fn(),
    removeAlbumPhotos: vi.fn(),
    updateAlbum: vi.fn(),
    fetchAlbums: vi.fn(),
    fetchLabels: vi.fn(),
  }
})

vi.mock('../services/bulk', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/bulk')>()
  return { ...actual, bulkUpdatePhotos: vi.fn() }
})

const { fetchPhotos } = await import('../services/photos')
const { bulkUpdatePhotos } = await import('../services/bulk')
const { fetchAlbum, deleteAlbum, removeAlbumPhotos, fetchAlbums, fetchLabels } =
  await import('../services/organize')
const fetchPhotosMock = vi.mocked(fetchPhotos)
const fetchAlbumMock = vi.mocked(fetchAlbum)
const deleteAlbumMock = vi.mocked(deleteAlbum)
const removeMock = vi.mocked(removeAlbumPhotos)
const bulkMock = vi.mocked(bulkUpdatePhotos)
const albumsMock = vi.mocked(fetchAlbums)
const labelsMock = vi.mocked(fetchLabels)

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
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
  }
}

function page(photos: Photo[]): PhotoListResponse {
  return { photos, total: photos.length, limit: 100, offset: 0, next_offset: null }
}

function album(): Album {
  return {
    uid: 'al_1',
    slug: 'holidays',
    title: 'Holidays',
    description: '',
    type: 'album',
    private: false,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
  }
}

function auth(canWrite: boolean): AuthContextValue {
  return {
    status: 'authenticated',
    user: { uid: 'u1', username: 'u', display_name: 'U', role: canWrite ? 'editor' : 'viewer' },
    role: canWrite ? 'editor' : 'viewer',
    downloadToken: null,
    canWrite,
    isAdmin: false,
    login: vi.fn(),
    logout: vi.fn(),
    refresh: vi.fn(),
  } as unknown as AuthContextValue
}

function renderPage(canWrite = true) {
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={auth(canWrite)}>
        <MemoryRouter initialEntries={['/albums/al_1']}>
          <Routes>
            <Route path="/albums/:uid" element={<AlbumDetailPage />} />
          </Routes>
        </MemoryRouter>
      </AuthContext.Provider>
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  fetchPhotosMock.mockReset()
  fetchAlbumMock.mockReset()
  deleteAlbumMock.mockReset()
  removeMock.mockReset()
  bulkMock.mockReset()
  albumsMock.mockReset()
  labelsMock.mockReset()
  removeMock.mockResolvedValue([])
  albumsMock.mockResolvedValue([])
  labelsMock.mockResolvedValue([])
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('AlbumDetailPage', () => {
  it('scopes the photo grid to the album from the URL', async () => {
    fetchAlbumMock.mockResolvedValue(album())
    fetchPhotosMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    renderPage()

    expect(await screen.findByRole('heading', { name: 'Holidays' })).toBeInTheDocument()
    await waitFor(() => {
      expect(fetchPhotosMock).toHaveBeenCalled()
    })
    expect(fetchPhotosMock.mock.calls[0][0].album).toBe('al_1')
  })

  it('offers a back link that names the album list it returns to', async () => {
    fetchAlbumMock.mockResolvedValue(album())
    fetchPhotosMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    renderPage()

    // An arrow alone said nothing; the label names the destination, and the
    // arrow itself stays decorative so the link's accessible name is the text.
    const back = await screen.findByRole('link', { name: 'Back to albums' })
    expect(back).toHaveAttribute('href', '/albums')
    expect(back.querySelector('.bi-arrow-left')).toHaveAttribute('aria-hidden', 'true')
  })

  it('names the album list in the back link of the error state too', async () => {
    fetchAlbumMock.mockRejectedValue(new Error('boom'))
    fetchPhotosMock.mockResolvedValue(page([]))
    renderPage()

    const back = await screen.findByRole('link', { name: 'Back to albums' })
    expect(back).toHaveAttribute('href', '/albums')
  })

  it('links each tile to the detail page carrying the album scope', async () => {
    fetchAlbumMock.mockResolvedValue(album())
    fetchPhotosMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    renderPage()

    // The tile's detail link carries ?album so pressing Esc/Back on the photo
    // (and prev/next) returns to this album, not the whole library.
    const link = await screen.findByRole('link', { name: 'a.jpg' })
    expect(link).toHaveAttribute('href', '/photos/a?album=al_1')
  })

  it('renders no sort selector and no manual reordering controls', async () => {
    fetchAlbumMock.mockResolvedValue(album())
    fetchPhotosMock.mockResolvedValue(page([photo('a', 'a.jpg'), photo('b', 'b.jpg')]))
    renderPage()

    await screen.findByRole('link', { name: 'a.jpg' })
    // An album is always chronological: the shared filter bar hides its sort
    // selector here (other photo lists keep theirs).
    expect(screen.queryByRole('combobox', { name: 'Sort' })).not.toBeInTheDocument()
    // Manual ordering is gone: no reorder mode, no per-tile drag handles.
    expect(screen.queryByRole('button', { name: 'Reorder' })).not.toBeInTheDocument()
    expect(
      screen.queryByRole('button', { name: /^Move .+ (earlier|later)$/ }),
    ).not.toBeInTheDocument()
  })

  it('hides mutation controls from viewers', async () => {
    fetchAlbumMock.mockResolvedValue(album())
    fetchPhotosMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    renderPage(false)

    await screen.findByRole('heading', { name: 'Holidays' })
    expect(screen.queryByRole('button', { name: 'Edit' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Delete' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Select a.jpg' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Bulk edit' })).not.toBeInTheDocument()
  })

  it('deletes the album through the styled confirm dialog, not a native prompt', async () => {
    fetchAlbumMock.mockResolvedValue(album())
    fetchPhotosMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    deleteAlbumMock.mockResolvedValue(undefined)
    const user = userEvent.setup()
    renderPage()

    await screen.findByRole('heading', { name: 'Holidays' })
    // The row control opens the dialog; nothing is deleted until it is confirmed.
    await user.click(screen.getByRole('button', { name: 'Delete' }))
    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByText(/Delete the album "Holidays"/)).toBeInTheDocument()
    expect(deleteAlbumMock).not.toHaveBeenCalled()

    // The confirm button carries the action itself, never "OK".
    await user.click(within(dialog).getByRole('button', { name: 'Delete album' }))
    await waitFor(() => {
      expect(deleteAlbumMock).toHaveBeenCalledWith('al_1')
    })
  })

  it('closes the confirm dialog without deleting when cancelled', async () => {
    fetchAlbumMock.mockResolvedValue(album())
    fetchPhotosMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    const user = userEvent.setup()
    renderPage()

    await screen.findByRole('heading', { name: 'Holidays' })
    await user.click(screen.getByRole('button', { name: 'Delete' }))
    const dialog = await screen.findByRole('dialog')
    await user.click(within(dialog).getByRole('button', { name: 'Cancel' }))

    await waitFor(() => {
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    })
    expect(deleteAlbumMock).not.toHaveBeenCalled()
  })

  it('offers a select checkmark on every tile, with no selection mode to enter', async () => {
    fetchAlbumMock.mockResolvedValue(album())
    fetchPhotosMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    const user = userEvent.setup()
    renderPage()

    // No "Select" step: the tile is a link that already carries its checkmark,
    // exactly as on the library, and the selection bar is still out of the way.
    expect(await screen.findByRole('link', { name: 'a.jpg' })).toBeInTheDocument()
    expect(screen.queryByRole('toolbar', { name: 'Selection actions' })).not.toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Select a.jpg' }))

    // Picking raises the selection bar with the album's selection actions.
    expect(screen.getByRole('button', { name: 'Set as cover' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Remove from album' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Bulk edit' })).toBeEnabled()
  })

  it('bulk-edits exactly the selected photos, then reloads the grid', async () => {
    fetchAlbumMock.mockResolvedValue(album())
    fetchPhotosMock.mockResolvedValue(
      page([photo('a', 'a.jpg'), photo('b', 'b.jpg'), photo('c', 'c.jpg')]),
    )
    bulkMock.mockResolvedValue({
      results: [],
      counts: { total: 2, updated: 2, skipped: 0, errored: 0 },
    })
    const user = userEvent.setup()
    renderPage()

    await screen.findByRole('link', { name: 'a.jpg' })
    await user.click(screen.getByRole('button', { name: 'Select a.jpg' }))
    await user.click(screen.getByRole('button', { name: 'Select c.jpg' }))

    const fetchesBefore = fetchPhotosMock.mock.calls.length
    await user.click(screen.getByRole('button', { name: 'Bulk edit' }))
    const dialog = await screen.findByRole('dialog')
    await user.selectOptions(within(dialog).getByLabelText('Favorite'), 'true')
    await user.click(within(dialog).getByRole('button', { name: 'Apply' }))

    // The two picked photos, not the three the album scope matches.
    await waitFor(() => {
      expect(bulkMock).toHaveBeenCalledWith(['a', 'c'], { set_favorite: true })
    })

    await user.click(await screen.findByRole('button', { name: 'Done' }))
    // The selection is cleared, so the bar steps back out of the way.
    expect(screen.queryByRole('toolbar', { name: 'Selection actions' })).not.toBeInTheDocument()
    await waitFor(() => {
      expect(fetchPhotosMock.mock.calls.length).toBeGreaterThan(fetchesBefore)
    })
  })

  it('drops the selection when the selected photos are removed from the album', async () => {
    fetchAlbumMock.mockResolvedValue(album())
    fetchPhotosMock.mockResolvedValue(page([photo('a', 'a.jpg'), photo('b', 'b.jpg')]))
    const user = userEvent.setup()
    renderPage()

    await screen.findByRole('link', { name: 'a.jpg' })
    await user.click(screen.getByRole('button', { name: 'Select a.jpg' }))
    await user.click(screen.getByRole('button', { name: 'Remove from album' }))

    await waitFor(() => {
      expect(removeMock).toHaveBeenCalledWith('al_1', ['a'])
    })
    // The selection is dropped, so no removed UID lingers in it — and with it
    // the bar, handing the header back to the album's own actions.
    await waitFor(() => {
      expect(screen.queryByRole('toolbar', { name: 'Selection actions' })).not.toBeInTheDocument()
    })
    expect(screen.getByRole('button', { name: 'Edit' })).toBeInTheDocument()
  })
})
