import { render, screen } from '@testing-library/react'
import { type ReactNode } from 'react'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

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

const { fetchPhotos } = await import('../services/photos')
const fetchMock = vi.mocked(fetchPhotos)

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

function renderFavorites() {
  return render(
    <I18nextProvider i18n={i18n}>
      <MemoryRouter initialEntries={['/favorites']}>
        <FavoritesPage />
      </MemoryRouter>
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  fetchMock.mockReset()
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
})
