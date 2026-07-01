import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { type ReactNode } from 'react'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../i18n'
import { type PlaceCountry } from '../services/places'
import { type Photo, type PhotoListResponse } from '../services/photos'

import { PlacesPage } from './PlacesPage'

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

vi.mock('../services/places', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/places')>()
  return { ...actual, fetchPlaces: vi.fn() }
})

const { fetchPhotos } = await import('../services/photos')
const { fetchPlaces } = await import('../services/places')
const fetchPhotosMock = vi.mocked(fetchPhotos)
const fetchPlacesMock = vi.mocked(fetchPlaces)

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

const HIERARCHY: PlaceCountry[] = [
  {
    country: 'Czechia',
    count: 12,
    cities: [
      { city: 'Prague', count: 8 },
      { city: 'Brno', count: 4 },
    ],
  },
  { country: 'Italy', count: 3, cities: [{ city: 'Rome', count: 3 }] },
]

function renderPage(entry = '/places') {
  return render(
    <I18nextProvider i18n={i18n}>
      <MemoryRouter initialEntries={[entry]}>
        <Routes>
          <Route path="/places" element={<PlacesPage />} />
        </Routes>
      </MemoryRouter>
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  fetchPhotosMock.mockReset()
  fetchPlacesMock.mockReset()
})

afterEach(() => {
  vi.restoreAllMocks()
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
})
