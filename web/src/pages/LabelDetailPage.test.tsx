import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { type ReactNode } from 'react'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
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
  return { ...actual, fetchLabel: vi.fn(), fetchAlbums: vi.fn(), fetchLabels: vi.fn() }
})

vi.mock('../services/bulk', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/bulk')>()
  return { ...actual, bulkUpdatePhotos: vi.fn() }
})

const { fetchPhotos } = await import('../services/photos')
const { bulkUpdatePhotos } = await import('../services/bulk')
const { fetchLabel, fetchAlbums, fetchLabels } = await import('../services/organize')
const fetchPhotosMock = vi.mocked(fetchPhotos)
const fetchLabelMock = vi.mocked(fetchLabel)
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

function renderPage(entry = '/labels/lb_1', canWrite = true) {
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={auth(canWrite)}>
        <MemoryRouter initialEntries={[entry]}>
          <Routes>
            <Route path="/labels/:uid" element={<LabelDetailPage />} />
          </Routes>
        </MemoryRouter>
      </AuthContext.Provider>
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  fetchPhotosMock.mockReset()
  fetchLabelMock.mockReset()
  bulkMock.mockReset()
  albumsMock.mockReset()
  labelsMock.mockReset()
  albumsMock.mockResolvedValue([])
  labelsMock.mockResolvedValue([])
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

  it('offers a back link that names the label list it returns to', async () => {
    fetchLabelMock.mockResolvedValue(label())
    fetchPhotosMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    renderPage()

    // An arrow alone said nothing; the label names the destination, and the
    // arrow itself stays decorative so the link's accessible name is the text.
    const back = await screen.findByRole('link', { name: 'Back to labels' })
    expect(back).toHaveAttribute('href', '/labels')
    expect(back.querySelector('.bi-arrow-left')).toHaveAttribute('aria-hidden', 'true')
  })

  it('names the label list in the back link of the error state too', async () => {
    fetchLabelMock.mockRejectedValue(new Error('boom'))
    fetchPhotosMock.mockResolvedValue(page([]))
    renderPage()

    const back = await screen.findByRole('link', { name: 'Back to labels' })
    expect(back).toHaveAttribute('href', '/labels')
  })

  it('links each tile to the detail page carrying the label scope', async () => {
    fetchLabelMock.mockResolvedValue(label())
    fetchPhotosMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    renderPage()

    // The tile's detail link carries ?label so Esc/Back and prev/next stay in
    // this label rather than falling through to the library.
    const link = await screen.findByRole('link', { name: 'a.jpg' })
    expect(link).toHaveAttribute('href', '/photos/a?label=lb_1')
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

  it('keeps selection and bulk edit away from viewers', async () => {
    fetchLabelMock.mockResolvedValue(label())
    fetchPhotosMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    renderPage('/labels/lb_1', false)

    await screen.findByRole('heading', { name: 'Sunset' })
    expect(screen.queryByRole('button', { name: 'Select' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Bulk edit' })).not.toBeInTheDocument()
  })

  it('bulk-edits exactly the selected photos, then reloads the grid', async () => {
    fetchLabelMock.mockResolvedValue(label())
    fetchPhotosMock.mockResolvedValue(page([photo('a', 'a.jpg'), photo('b', 'b.jpg')]))
    bulkMock.mockResolvedValue({
      results: [],
      counts: { total: 1, updated: 1, skipped: 0, errored: 0 },
    })
    const user = userEvent.setup()
    renderPage()

    await screen.findByRole('link', { name: 'a.jpg' })
    await user.click(screen.getByRole('button', { name: 'Select' }))
    expect(screen.getByRole('button', { name: 'Bulk edit' })).toBeDisabled()

    await user.click(screen.getByRole('button', { name: 'b.jpg' }))
    const fetchesBefore = fetchPhotosMock.mock.calls.length
    await user.click(screen.getByRole('button', { name: 'Bulk edit' }))
    await user.selectOptions(await screen.findByLabelText('Archive'), 'archive')
    await user.click(screen.getByRole('button', { name: 'Apply' }))

    await waitFor(() => {
      expect(bulkMock).toHaveBeenCalledWith(['b'], { archive: true })
    })

    await user.click(await screen.findByRole('button', { name: 'Done' }))
    expect(screen.getByText('0 selected')).toBeInTheDocument()
    await waitFor(() => {
      expect(fetchPhotosMock.mock.calls.length).toBeGreaterThan(fetchesBefore)
    })
  })
})
