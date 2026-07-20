import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { type ReactNode } from 'react'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
import i18n from '../i18n'
import { type Label } from '../services/organize'
import { type Photo, type PhotoListResponse } from '../services/photos'

import { albumOption, BATCH_ACTIONS } from '../test/batchBar'

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
    expect(screen.queryByRole('button', { name: 'Select a.jpg' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'More edits' })).not.toBeInTheDocument()
  })

  it('offers a select checkmark on every tile, with no selection mode to enter', async () => {
    fetchLabelMock.mockResolvedValue(label())
    fetchPhotosMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    const user = userEvent.setup()
    renderPage()

    // No "Select" step: the tile is a link that already carries its checkmark,
    // exactly as on the library.
    expect(await screen.findByRole('link', { name: 'a.jpg' })).toBeInTheDocument()
    expect(screen.queryByRole('toolbar', { name: 'Batch actions' })).not.toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Select a.jpg' }))
    expect(screen.getByText('1 selected')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'More edits' })).toBeEnabled()
  })

  it('raises the library’s full batch bar, and only that one bar', async () => {
    fetchLabelMock.mockResolvedValue(label())
    fetchPhotosMock.mockResolvedValue(page([photo('a', 'a.jpg'), photo('b', 'b.jpg')]))
    const user = userEvent.setup()
    renderPage()

    await screen.findByRole('link', { name: 'a.jpg' })
    await user.click(screen.getByRole('button', { name: 'Select a.jpg' }))

    const bars = screen.getAllByRole('toolbar', { name: 'Batch actions' })
    expect(bars).toHaveLength(1)
    const [bar] = bars
    for (const name of BATCH_ACTIONS) {
      expect(within(bar).getByRole('button', { name })).toBeInTheDocument()
    }

    // Select-all reaches the rest of the loaded grid, as on the library.
    await user.click(within(bar).getByRole('button', { name: 'Select all' }))
    expect(screen.getByText('2 selected')).toBeInTheDocument()
  })

  it('adds the selection to an album straight from the bar, then reloads', async () => {
    fetchLabelMock.mockResolvedValue(label())
    fetchPhotosMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    albumsMock.mockResolvedValue([albumOption('al_2', 'Trips')])
    labelsMock.mockResolvedValue([])
    bulkMock.mockResolvedValue({
      results: [],
      counts: { total: 1, updated: 1, skipped: 0, errored: 0 },
    })
    const user = userEvent.setup()
    renderPage()

    await screen.findByRole('link', { name: 'a.jpg' })
    await user.click(screen.getByRole('button', { name: 'Select a.jpg' }))

    const fetchesBefore = fetchPhotosMock.mock.calls.length
    await user.click(screen.getByRole('button', { name: 'Add to album' }))
    await user.click(await screen.findByLabelText('Add to albums'))
    await user.click(await screen.findByRole('option', { name: /Trips/ }))
    await user.click(screen.getByRole('button', { name: 'Apply' }))

    await waitFor(() => {
      expect(bulkMock).toHaveBeenCalledWith(['a'], { add_to_albums: ['al_2'] })
    })
    // The label's own grid refetches: the edit may have changed what it matches.
    await waitFor(() => {
      expect(fetchPhotosMock.mock.calls.length).toBeGreaterThan(fetchesBefore)
    })
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
    await user.click(screen.getByRole('button', { name: 'Select b.jpg' }))
    const fetchesBefore = fetchPhotosMock.mock.calls.length
    await user.click(screen.getByRole('button', { name: 'More edits' }))
    await user.selectOptions(await screen.findByLabelText('Archive'), 'archive')
    await user.click(screen.getByRole('button', { name: 'Apply' }))

    await waitFor(() => {
      expect(bulkMock).toHaveBeenCalledWith(['b'], { archive: true })
    })

    await user.click(await screen.findByRole('button', { name: 'Done' }))
    // The selection is cleared, so the bar steps back out of the way.
    expect(screen.queryByRole('toolbar', { name: 'Batch actions' })).not.toBeInTheDocument()
    await waitFor(() => {
      expect(fetchPhotosMock.mock.calls.length).toBeGreaterThan(fetchesBefore)
    })
  })
})
