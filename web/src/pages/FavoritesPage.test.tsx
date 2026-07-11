import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { type ReactNode } from 'react'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
import i18n from '../i18n'
import { type Photo, type PhotoListResponse } from '../services/photos'

import { FavoritesPage } from './FavoritesPage'

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
  return { ...actual, fetchAlbums: vi.fn(), fetchLabels: vi.fn() }
})

vi.mock('../services/bulk', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/bulk')>()
  return { ...actual, bulkUpdatePhotos: vi.fn() }
})

const { fetchPhotos } = await import('../services/photos')
const { bulkUpdatePhotos } = await import('../services/bulk')
const { fetchAlbums, fetchLabels } = await import('../services/organize')
const fetchMock = vi.mocked(fetchPhotos)
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
    is_favorite: true,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
  }
}

function page(photos: Photo[]): PhotoListResponse {
  return { photos, total: photos.length, limit: 100, offset: 0, next_offset: null }
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

function renderFavorites(canWrite = true) {
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={auth(canWrite)}>
        <MemoryRouter initialEntries={['/favorites']}>
          <FavoritesPage />
        </MemoryRouter>
      </AuthContext.Provider>
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  fetchMock.mockReset()
  bulkMock.mockReset()
  albumsMock.mockReset()
  labelsMock.mockReset()
  albumsMock.mockResolvedValue([])
  labelsMock.mockResolvedValue([])
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('FavoritesPage', () => {
  it('scopes the listing to the favorite=true filter', async () => {
    fetchMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    renderFavorites()

    await screen.findByRole('link', { name: 'a.jpg' })
    expect(fetchMock.mock.calls[0][0].favorite).toBe('true')
  })

  it('links each tile to the detail page carrying the favorites scope', async () => {
    fetchMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    renderFavorites()

    // The tile's detail link carries ?favorite=true so Esc/Back and prev/next
    // return to Favorites rather than the whole library.
    const link = await screen.findByRole('link', { name: 'a.jpg' })
    expect(link).toHaveAttribute('href', '/photos/a?favorite=true')
  })

  it('renders an empty state when the user has no favorites', async () => {
    fetchMock.mockResolvedValue(page([]))
    renderFavorites()

    expect(await screen.findByText('No favorites yet')).toBeInTheDocument()
  })

  it('shows a favorite heart on each tile so a photo can be unfavorited in place', async () => {
    fetchMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    renderFavorites()

    await screen.findByRole('link', { name: 'a.jpg' })
    // is_favorite is true, so the heart offers "remove".
    expect(screen.getByRole('button', { name: 'Remove from favorites' })).toBeInTheDocument()
  })

  it('keeps selection and bulk edit away from viewers', async () => {
    fetchMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    renderFavorites(false)

    await screen.findByRole('link', { name: 'a.jpg' })
    expect(screen.queryByRole('button', { name: 'Select' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Bulk edit' })).not.toBeInTheDocument()
  })

  it('disables the bulk-edit trigger until a photo is picked', async () => {
    fetchMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    const user = userEvent.setup()
    renderFavorites()

    await screen.findByRole('link', { name: 'a.jpg' })
    await user.click(screen.getByRole('button', { name: 'Select' }))

    expect(screen.getByRole('button', { name: 'Bulk edit' })).toBeDisabled()
    await user.click(screen.getByRole('button', { name: 'a.jpg' }))
    expect(screen.getByRole('button', { name: 'Bulk edit' })).toBeEnabled()
  })

  it('un-favoriting a selection drops those photos from the list and clears the selection', async () => {
    // The refetch after the edit no longer matches `b`: it is no longer a favorite.
    fetchMock.mockResolvedValueOnce(page([photo('a', 'a.jpg'), photo('b', 'b.jpg')]))
    fetchMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    bulkMock.mockResolvedValue({
      results: [],
      counts: { total: 1, updated: 1, skipped: 0, errored: 0 },
    })
    const user = userEvent.setup()
    renderFavorites()

    await screen.findByRole('link', { name: 'b.jpg' })
    await user.click(screen.getByRole('button', { name: 'Select' }))
    await user.click(screen.getByRole('button', { name: 'b.jpg' }))

    await user.click(screen.getByRole('button', { name: 'Bulk edit' }))
    await user.selectOptions(await screen.findByLabelText('Favorite'), 'false')
    await user.click(screen.getByRole('button', { name: 'Apply' }))

    // Exactly the picked photo, never the whole filtered result set.
    await waitFor(() => {
      expect(bulkMock).toHaveBeenCalledWith(['b'], { set_favorite: false })
    })

    await user.click(await screen.findByRole('button', { name: 'Done' }))

    // The list refreshed without `b`, and nothing is left selected — least of all
    // the photo that just left the view.
    await waitFor(() => {
      expect(screen.queryByRole('button', { name: 'b.jpg' })).not.toBeInTheDocument()
    })
    expect(screen.getByRole('button', { name: 'a.jpg' })).toBeInTheDocument()
    expect(screen.getByText('0 selected')).toBeInTheDocument()
  })
})
