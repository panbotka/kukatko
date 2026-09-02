import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
import i18n from '../i18n'
import { type PlaceCountry } from '../services/places'
import { type Photo, type PhotoListResponse } from '../services/photos'

import { PlacesPage } from './PlacesPage'

// jsdom lays nothing out, so the real virtualizer mounts nothing: render it
// all instead (see `test/virtuoso`).
vi.mock('react-virtuoso', async () => (await import('../test/virtuoso')).virtuosoMock())

vi.mock('../services/photos', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/photos')>()
  return { ...actual, fetchPhotos: vi.fn() }
})

vi.mock('../services/places', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/places')>()
  return { ...actual, fetchPlaces: vi.fn() }
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
const { fetchPlaces } = await import('../services/places')
const { bulkUpdatePhotos } = await import('../services/bulk')
const { fetchAlbums, fetchLabels } = await import('../services/organize')
const fetchPhotosMock = vi.mocked(fetchPhotos)
const fetchPlacesMock = vi.mocked(fetchPlaces)
const bulkMock = vi.mocked(bulkUpdatePhotos)
const albumsMock = vi.mocked(fetchAlbums)
const labelsMock = vi.mocked(fetchLabels)

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

const HIERARCHY: PlaceCountry[] = [
  {
    country: 'Czechia',
    count: 12,
    cover_uid: 'cz',
    cities: [
      { city: 'Prague', count: 8, cover_uid: 'pr' },
      { city: 'Brno', count: 4, cover_uid: 'bo' },
    ],
  },
  // Italy's cities carry no cover, so a row with nothing to draw is covered too.
  {
    country: 'Italy',
    count: 5,
    cover_uid: 'it',
    cities: [
      { city: 'Rome', count: 3 },
      { city: 'Milan', count: 2 },
    ],
  },
]

/** A library entirely inside one country — this instance's actual shape. */
const ONE_COUNTRY: PlaceCountry[] = [
  {
    country: 'Czechia',
    count: 12,
    cover_uid: 'cz',
    cities: [
      { city: 'Prague', count: 8, cover_uid: 'pr' },
      { city: 'Brno', count: 4, cover_uid: 'bo' },
    ],
  },
]

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

function renderPage(entry = '/places', canWrite = true) {
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={auth(canWrite)}>
        <MemoryRouter initialEntries={[entry]}>
          <Routes>
            <Route path="/places" element={<PlacesPage />} />
          </Routes>
        </MemoryRouter>
      </AuthContext.Provider>
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  fetchPhotosMock.mockReset()
  fetchPlacesMock.mockReset()
  bulkMock.mockReset()
  albumsMock.mockReset()
  labelsMock.mockReset()
  albumsMock.mockResolvedValue([])
  labelsMock.mockResolvedValue([])
})

describe('PlacesPage', () => {
  it('lists countries with their photo counts', async () => {
    fetchPlacesMock.mockResolvedValue(HIERARCHY)
    renderPage()

    expect(await screen.findByRole('button', { name: /Czechia/ })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Italy/ })).toBeInTheDocument()
    // The country count is shown as a photo-count badge.
    expect(screen.getByRole('button', { name: /Czechia/ })).toHaveTextContent('12 photos')
    // No place selected yet: the scoped grid must not fetch photos.
    expect(fetchPhotosMock).not.toHaveBeenCalled()
  })

  it('drilling into a country reveals its cities', async () => {
    fetchPlacesMock.mockResolvedValue(HIERARCHY)
    const user = userEvent.setup()
    renderPage()

    await user.click(await screen.findByRole('button', { name: /Czechia/ }))

    expect(await screen.findByRole('button', { name: /Prague/ })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Brno/ })).toBeInTheDocument()
    // Cities of the other country are not shown.
    expect(screen.queryByRole('button', { name: /Rome/ })).not.toBeInTheDocument()
    // Still no grid fetch — only a city selection scopes the grid.
    expect(fetchPhotosMock).not.toHaveBeenCalled()
  })

  it('selecting a city scopes the grid to that place', async () => {
    fetchPlacesMock.mockResolvedValue(HIERARCHY)
    fetchPhotosMock.mockResolvedValue(page([photo('a')]))
    const user = userEvent.setup()
    renderPage()

    await user.click(await screen.findByRole('button', { name: /Czechia/ }))
    await user.click(await screen.findByRole('button', { name: /Prague/ }))

    await waitFor(() => {
      expect(fetchPhotosMock).toHaveBeenCalled()
    })
    const params = fetchPhotosMock.mock.calls[0][0]
    expect(params.country).toBe('Czechia')
    expect(params.city).toBe('Prague')
    expect(await screen.findByTestId('grid')).toBeInTheDocument()
  })

  it('honours the place drill from the URL', async () => {
    fetchPlacesMock.mockResolvedValue(HIERARCHY)
    fetchPhotosMock.mockResolvedValue(page([]))
    renderPage('/places?country=Italy&city=Rome')

    await waitFor(() => {
      expect(fetchPhotosMock).toHaveBeenCalled()
    })
    const params = fetchPhotosMock.mock.calls[0][0]
    expect(params.country).toBe('Italy')
    expect(params.city).toBe('Rome')
  })

  it('shows an error state with a retry when the hierarchy fails to load', async () => {
    fetchPlacesMock.mockRejectedValue(new Error('boom'))
    renderPage()

    expect(await screen.findByText('Could not load places.')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Try again' })).toBeInTheDocument()
  })

  it('draws a preview photo for every row', async () => {
    fetchPlacesMock.mockResolvedValue(HIERARCHY)
    const user = userEvent.setup()
    const { container } = renderPage()

    await screen.findByRole('button', { name: /Czechia/ })
    const covers = container.querySelectorAll('img')
    expect([...covers].map((img) => img.getAttribute('src'))).toEqual([
      '/api/v1/photos/cz/thumb/tile_224',
      '/api/v1/photos/it/thumb/tile_224',
    ])

    await user.click(screen.getByRole('button', { name: /Czechia/ }))

    await screen.findByRole('button', { name: /Prague/ })
    expect([...container.querySelectorAll('img')].map((img) => img.getAttribute('src'))).toEqual([
      '/api/v1/photos/pr/thumb/tile_224',
      '/api/v1/photos/bo/thumb/tile_224',
    ])
  })

  it('shows no broken image for a place with no cover', async () => {
    fetchPlacesMock.mockResolvedValue(HIERARCHY)
    const user = userEvent.setup()
    const { container } = renderPage()

    await user.click(await screen.findByRole('button', { name: /Italy/ }))

    // Rome carries no cover_uid: the well stays empty rather than requesting a
    // thumbnail for nothing.
    expect(await screen.findByRole('button', { name: /Rome/ })).toBeInTheDocument()
    expect(container.querySelectorAll('img')).toHaveLength(0)
  })
})

describe('PlacesPage single-entry levels', () => {
  it('opens on the cities when the library holds one country', async () => {
    fetchPlacesMock.mockResolvedValue(ONE_COUNTRY)
    renderPage()

    // The country list would have been one row; its cities are shown instead.
    expect(await screen.findByRole('button', { name: /Prague/ })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Brno/ })).toBeInTheDocument()
    // The breadcrumb still names the country, but as text: there is nowhere to
    // step back to.
    expect(screen.queryByRole('button', { name: 'Places' })).not.toBeInTheDocument()
    expect(screen.getByText('Czechia')).toBeInTheDocument()
  })

  it('opens on the photos when that one country holds one city', async () => {
    fetchPlacesMock.mockResolvedValue([
      { country: 'Czechia', count: 5, cover_uid: 'cz', cities: [{ city: 'Brno', count: 5 }] },
    ])
    fetchPhotosMock.mockResolvedValue(page([photo('a')]))
    renderPage()

    await waitFor(() => {
      expect(fetchPhotosMock).toHaveBeenCalled()
    })
    const params = fetchPhotosMock.mock.calls[0][0]
    expect(params.country).toBe('Czechia')
    expect(params.city).toBe('Brno')
  })

  it('keeps the city list when the one city leaves photos behind', async () => {
    // 5 of the country's 40 photos are in the only named town; the rest were
    // never resolved to one and have no row, so skipping would hide them.
    fetchPlacesMock.mockResolvedValue([
      { country: 'Czechia', count: 40, cover_uid: 'cz', cities: [{ city: 'Brno', count: 5 }] },
    ])
    renderPage()

    expect(await screen.findByRole('button', { name: /Brno/ })).toBeInTheDocument()
    expect(fetchPhotosMock).not.toHaveBeenCalled()
  })

  it('puts the implied country into the URL when a city is picked', async () => {
    fetchPlacesMock.mockResolvedValue(ONE_COUNTRY)
    fetchPhotosMock.mockResolvedValue(page([]))
    const user = userEvent.setup()
    renderPage()

    await user.click(await screen.findByRole('button', { name: /Prague/ }))

    await waitFor(() => {
      expect(fetchPhotosMock).toHaveBeenCalled()
    })
    // A link to a city without its country is not an address anyone can open.
    const params = fetchPhotosMock.mock.calls[0][0]
    expect(params.country).toBe('Czechia')
    expect(params.city).toBe('Prague')
  })
})

describe('PlacesPage bulk edit', () => {
  it('offers no selection before a city is drilled into', async () => {
    fetchPlacesMock.mockResolvedValue(HIERARCHY)
    renderPage()

    await screen.findByRole('button', { name: /Czechia/ })
    // Only the place grid can be selected, and it is not on screen yet.
    expect(screen.queryByRole('button', { name: 'Select a.jpg' })).not.toBeInTheDocument()
  })

  it('keeps selection and bulk edit away from viewers', async () => {
    fetchPlacesMock.mockResolvedValue(HIERARCHY)
    fetchPhotosMock.mockResolvedValue(page([photo('a')]))
    renderPage('/places?country=Italy&city=Rome', false)

    await screen.findByTestId('grid')
    expect(screen.queryByRole('button', { name: 'Select a.jpg' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Bulk edit' })).not.toBeInTheDocument()
  })

  it('offers a select checkmark on every tile, with no selection mode to enter', async () => {
    fetchPlacesMock.mockResolvedValue(HIERARCHY)
    fetchPhotosMock.mockResolvedValue(page([photo('a')]))
    const user = userEvent.setup()
    renderPage('/places?country=Italy&city=Rome')

    // No "Select" step: the tile is a link that already carries its checkmark,
    // exactly as on the library.
    expect(await screen.findByRole('link', { name: 'a.jpg' })).toBeInTheDocument()
    expect(screen.queryByRole('toolbar', { name: 'Selection actions' })).not.toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Select a.jpg' }))
    expect(screen.getByText('1 selected')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Bulk edit' })).toBeEnabled()
  })

  it('bulk-edits exactly the picked photos, then reloads the scoped grid', async () => {
    fetchPlacesMock.mockResolvedValue(HIERARCHY)
    fetchPhotosMock.mockResolvedValue(page([photo('a'), photo('b')]))
    bulkMock.mockResolvedValue({
      results: [],
      counts: { total: 1, updated: 1, skipped: 0, errored: 0 },
    })
    const user = userEvent.setup()
    renderPage('/places?country=Czechia&city=Prague')

    await screen.findByRole('link', { name: 'a.jpg' })
    await user.click(screen.getByRole('button', { name: 'Select b.jpg' }))

    const fetchesBefore = fetchPhotosMock.mock.calls.length
    await user.click(screen.getByRole('button', { name: 'Bulk edit' }))
    await user.selectOptions(await screen.findByLabelText('Archive'), 'archive')
    await user.click(screen.getByRole('button', { name: 'Apply' }))

    await waitFor(() => {
      expect(bulkMock).toHaveBeenCalledWith(['b'], { archive: true })
    })

    await user.click(await screen.findByRole('button', { name: 'Done' }))

    // The selection is cleared, so the bar steps back out of the way.
    expect(screen.queryByRole('toolbar', { name: 'Selection actions' })).not.toBeInTheDocument()
    await waitFor(() => {
      expect(fetchPhotosMock.mock.calls.length).toBeGreaterThan(fetchesBefore)
    })
  })

  it('drops the selection when the drill moves to another place', async () => {
    fetchPlacesMock.mockResolvedValue(HIERARCHY)
    fetchPhotosMock.mockResolvedValue(page([photo('a')]))
    const user = userEvent.setup()
    renderPage('/places?country=Czechia&city=Prague')

    await screen.findByRole('link', { name: 'a.jpg' })
    await user.click(screen.getByRole('button', { name: 'Select a.jpg' }))
    expect(screen.getByText('1 selected')).toBeInTheDocument()

    // Step back up to the country: a Prague photo must not stay selected.
    await user.click(screen.getByRole('button', { name: 'Czechia' }))

    await waitFor(() => {
      expect(screen.queryByText('1 selected')).not.toBeInTheDocument()
    })
  })
})
