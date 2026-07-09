import { render, screen, waitFor } from '@testing-library/react'
import { type ReactNode } from 'react'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../i18n'
import { type Label } from '../services/organize'
import { type Photo, type PhotoListResponse } from '../services/photos'

import { LabelDetailPage } from './LabelDetailPage'

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
  return { ...actual, fetchLabel: vi.fn() }
})

const { fetchPhotos } = await import('../services/photos')
const { fetchLabel } = await import('../services/organize')
const fetchPhotosMock = vi.mocked(fetchPhotos)
const fetchLabelMock = vi.mocked(fetchLabel)

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

function label(): Label {
  return {
    uid: 'lb_1',
    slug: 'sunset',
    name: 'Sunset',
    priority: 0,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
  }
}

function renderPage(entry = '/labels/lb_1') {
  return render(
    <I18nextProvider i18n={i18n}>
      <MemoryRouter initialEntries={[entry]}>
        <Routes>
          <Route path="/labels/:uid" element={<LabelDetailPage />} />
        </Routes>
      </MemoryRouter>
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  fetchPhotosMock.mockReset()
  fetchLabelMock.mockReset()
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('LabelDetailPage', () => {
  it('scopes the grid to the label from the URL and shows its name', async () => {
    fetchLabelMock.mockResolvedValue(label())
    fetchPhotosMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    renderPage()

    expect(await screen.findByRole('heading', { name: 'Sunset' })).toBeInTheDocument()
    await waitFor(() => {
      expect(fetchPhotosMock).toHaveBeenCalled()
    })
    expect(fetchPhotosMock.mock.calls[0][0].label).toBe('lb_1')
  })

  it('honours filters from the URL in the scoped fetch', async () => {
    fetchLabelMock.mockResolvedValue(label())
    fetchPhotosMock.mockResolvedValue(page([]))
    renderPage('/labels/lb_1?sort=oldest')

    await waitFor(() => {
      expect(fetchPhotosMock).toHaveBeenCalled()
    })
    const first = fetchPhotosMock.mock.calls[0][0]
    expect(first.label).toBe('lb_1')
    expect(first.sort).toBe('oldest')
  })
})
