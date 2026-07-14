import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { type ReactNode } from 'react'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
import i18n from '../i18n'
import { type Photo, type PhotoListResponse } from '../services/photos'

import { LibraryPage } from './LibraryPage'
import { SearchPage } from './SearchPage'

// Stand-in for react-virtuoso's grid (jsdom has no layout): render every item.
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
  return { ...actual, fetchPhotos: vi.fn(), searchPhotos: vi.fn() }
})

vi.mock('../services/savedSearches', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/savedSearches')>()
  return { ...actual, createSavedSearch: vi.fn() }
})

const { fetchPhotos, searchPhotos } = await import('../services/photos')
const { createSavedSearch } = await import('../services/savedSearches')
const fetchPhotosMock = vi.mocked(fetchPhotos)
const searchPhotosMock = vi.mocked(searchPhotos)
const createMock = vi.mocked(createSavedSearch)

function photo(uid: string): Photo {
  return {
    uid,
    file_hash: uid,
    file_name: `${uid}.jpg`,
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

const viewerAuth = {
  status: 'authenticated',
  user: { uid: 'u1', username: 'u', display_name: 'U', role: 'viewer' },
  role: 'viewer',
  downloadToken: null,
  canWrite: false,
  isAdmin: false,
  login: vi.fn(),
  logout: vi.fn(),
  refresh: vi.fn(),
} as unknown as AuthContextValue

beforeEach(async () => {
  await i18n.changeLanguage('en')
  fetchPhotosMock.mockReset()
  searchPhotosMock.mockReset()
  createMock.mockReset()
  createMock.mockResolvedValue({
    uid: 'ss_new',
    name: 'My view',
    params: {},
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
  })
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('saving a view from the library', () => {
  it('captures the current filters/sort as the saved-search params', async () => {
    fetchPhotosMock.mockResolvedValue(page([photo('a')]))
    const user = userEvent.setup()
    render(
      <I18nextProvider i18n={i18n}>
        <AuthContext.Provider value={viewerAuth}>
          <MemoryRouter initialEntries={['/?sort=oldest&camera=Canon']}>
            <LibraryPage />
          </MemoryRouter>
        </AuthContext.Provider>
      </I18nextProvider>,
    )

    await screen.findByRole('link', { name: 'a.jpg' })
    await user.click(screen.getByRole('button', { name: 'Save view' }))
    await user.type(screen.getByLabelText('Name'), 'My view')
    await user.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => {
      expect(createMock).toHaveBeenCalledTimes(1)
    })
    const [name, params] = createMock.mock.calls[0]
    expect(name).toBe('My view')
    expect(params).toMatchObject({ sort: 'oldest', camera: 'Canon' })
    // A library view must not carry a mode (so it restores to the homepage).
    expect(params.mode).toBeUndefined()
  })
})

describe('saving a view from search', () => {
  it('captures the current query and mode as the saved-search params', async () => {
    searchPhotosMock.mockResolvedValue({ ...page([photo('a')]), mode: 'semantic', degraded: false })
    const user = userEvent.setup()
    render(
      <I18nextProvider i18n={i18n}>
        <AuthContext.Provider value={viewerAuth}>
          <MemoryRouter initialEntries={['/search?q=cat&mode=semantic']}>
            <SearchPage />
          </MemoryRouter>
        </AuthContext.Provider>
      </I18nextProvider>,
    )

    await screen.findByRole('link', { name: 'a.jpg' })
    await user.click(screen.getByRole('button', { name: 'Save view' }))
    await user.type(screen.getByLabelText('Name'), 'Cats')
    await user.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => {
      expect(createMock).toHaveBeenCalledTimes(1)
    })
    const [name, params] = createMock.mock.calls[0]
    expect(name).toBe('Cats')
    expect(params).toMatchObject({ q: 'cat', mode: 'semantic' })
  })
})
