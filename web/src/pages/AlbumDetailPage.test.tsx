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
    reorderAlbumPhotos: vi.fn(),
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
const { fetchAlbum, reorderAlbumPhotos, removeAlbumPhotos, fetchAlbums, fetchLabels } =
  await import('../services/organize')
const fetchPhotosMock = vi.mocked(fetchPhotos)
const fetchAlbumMock = vi.mocked(fetchAlbum)
const reorderMock = vi.mocked(reorderAlbumPhotos)
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
    private: false,
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
    order_by: 'added',
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
  reorderMock.mockReset()
  removeMock.mockReset()
  bulkMock.mockReset()
  albumsMock.mockReset()
  labelsMock.mockReset()
  reorderMock.mockResolvedValue([])
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

  it('persists a reorder via the API', async () => {
    fetchAlbumMock.mockResolvedValue(album())
    fetchPhotosMock.mockResolvedValue(page([photo('a', 'a.jpg'), photo('b', 'b.jpg')]))
    const user = userEvent.setup()
    renderPage()

    await screen.findByRole('link', { name: 'a.jpg' })
    await user.click(screen.getByRole('button', { name: 'Reorder' }))

    // Move the first photo later — the new order is [b, a].
    await user.click(screen.getByRole('button', { name: 'Move a.jpg later' }))

    await waitFor(() => {
      expect(reorderMock).toHaveBeenCalledWith('al_1', ['b', 'a'])
    })
  })

  it('hides mutation controls from viewers', async () => {
    fetchAlbumMock.mockResolvedValue(album())
    fetchPhotosMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    renderPage(false)

    await screen.findByRole('heading', { name: 'Holidays' })
    expect(screen.queryByRole('button', { name: 'Reorder' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Edit' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Delete' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Select' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Bulk edit' })).not.toBeInTheDocument()
  })

  it('offers bulk edit alongside the album actions in selection mode', async () => {
    fetchAlbumMock.mockResolvedValue(album())
    fetchPhotosMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    const user = userEvent.setup()
    renderPage()

    await screen.findByRole('link', { name: 'a.jpg' })
    await user.click(screen.getByRole('button', { name: 'Select' }))

    // Bulk edit is added to the album's own actions, it does not replace them.
    expect(screen.getByRole('button', { name: 'Set as cover' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Remove from album' })).toBeInTheDocument()
    // Nothing is selected yet, so nothing can be applied to anything.
    expect(screen.getByRole('button', { name: 'Bulk edit' })).toBeDisabled()

    await user.click(screen.getByRole('button', { name: 'a.jpg' }))
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
    await user.click(screen.getByRole('button', { name: 'Select' }))
    await user.click(screen.getByRole('button', { name: 'a.jpg' }))
    await user.click(screen.getByRole('button', { name: 'c.jpg' }))

    const fetchesBefore = fetchPhotosMock.mock.calls.length
    await user.click(screen.getByRole('button', { name: 'Bulk edit' }))
    // The filter bar carries a "Private" select of its own; take the dialog's.
    const dialog = await screen.findByRole('dialog')
    await user.selectOptions(within(dialog).getByLabelText('Private'), 'true')
    await user.click(within(dialog).getByRole('button', { name: 'Apply' }))

    // The two picked photos, not the three the album scope matches.
    await waitFor(() => {
      expect(bulkMock).toHaveBeenCalledWith(['a', 'c'], { set_private: true })
    })

    await user.click(await screen.findByRole('button', { name: 'Done' }))
    expect(screen.getByText('0 selected')).toBeInTheDocument()
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
    await user.click(screen.getByRole('button', { name: 'Select' }))
    await user.click(screen.getByRole('button', { name: 'a.jpg' }))
    await user.click(screen.getByRole('button', { name: 'Remove from album' }))

    await waitFor(() => {
      expect(removeMock).toHaveBeenCalledWith('al_1', ['a'])
    })
    // Selection mode is left, so no removed UID lingers in the selection.
    await waitFor(() => {
      expect(screen.queryByRole('toolbar', { name: 'Selection actions' })).not.toBeInTheDocument()
    })
    expect(screen.getByRole('button', { name: 'Select' })).toBeInTheDocument()
  })
})
