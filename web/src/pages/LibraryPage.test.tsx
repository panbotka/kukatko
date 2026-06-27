import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { type ReactNode } from 'react'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter, useLocation, useNavigate } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
import i18n from '../i18n'
import { type Photo, type PhotoListResponse } from '../services/photos'

import { LibraryPage } from './LibraryPage'

// Minimal stand-in for react-virtuoso's grid: jsdom has no layout, so the real
// virtualized grid would render nothing. This renders every item and exposes a
// button to fire `endReached` (the infinite-scroll trigger).
interface MockGridProps {
  data: Photo[]
  itemContent: (index: number, item: Photo) => ReactNode
  endReached?: () => void
}
vi.mock('react-virtuoso', () => ({
  VirtuosoGrid: ({ data, itemContent, endReached }: MockGridProps) => (
    <div data-testid="grid">
      {data.map((item, index) => (
        <div key={item.uid}>{itemContent(index, item)}</div>
      ))}
      <button
        type="button"
        onClick={() => {
          endReached?.()
        }}
      >
        __endReached
      </button>
    </div>
  ),
}))

// Keep the real thumbUrl/GRID_THUMB_SIZE; only the network call is faked.
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

function page(photos: Photo[], total: number, nextOffset: number | null): PhotoListResponse {
  return { photos, total, limit: 100, offset: 0, next_offset: nextOffset }
}

/** Surfaces the current URL query and a Back control for navigation tests. */
function LocationProbe() {
  const location = useLocation()
  const navigate = useNavigate()
  return (
    <>
      <span data-testid="search">{location.search}</span>
      <button
        type="button"
        onClick={() => {
          void navigate(-1)
        }}
      >
        __back
      </button>
    </>
  )
}

/** Minimal viewer auth context: enough for LibraryPage's role-gated controls. */
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

function renderLibrary(initialEntry = '/library') {
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={viewerAuth}>
        <MemoryRouter initialEntries={[initialEntry]}>
          <LibraryPage />
          <LocationProbe />
        </MemoryRouter>
      </AuthContext.Provider>
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

describe('LibraryPage', () => {
  it('shows a loading skeleton during the first-page load', () => {
    fetchMock.mockReturnValue(new Promise<PhotoListResponse>(() => undefined))
    renderLibrary()
    expect(screen.getByRole('status', { name: 'Loading photos…' })).toBeInTheDocument()
  })

  it('renders the empty state when no photos match', async () => {
    fetchMock.mockResolvedValue(page([], 0, null))
    renderLibrary()
    expect(await screen.findByText('No photos found')).toBeInTheDocument()
  })

  it('renders an error with a retry that reloads the photos', async () => {
    fetchMock.mockRejectedValueOnce(new Error('boom'))
    const user = userEvent.setup()
    renderLibrary()

    expect(await screen.findByText('Could not load photos.')).toBeInTheDocument()

    fetchMock.mockResolvedValueOnce(page([photo('a', 'a.jpg')], 1, null))
    await user.click(screen.getByRole('button', { name: 'Try again' }))

    expect(await screen.findByRole('link', { name: 'a.jpg' })).toBeInTheDocument()
  })

  it('changing the sort updates the URL and refetches with the new sort', async () => {
    fetchMock.mockResolvedValue(page([photo('a', 'a.jpg')], 1, null))
    const user = userEvent.setup()
    renderLibrary()

    await screen.findByRole('link', { name: 'a.jpg' })
    expect(fetchMock.mock.calls[0][0].sort).toBe('newest')

    await user.selectOptions(screen.getByLabelText('Sort'), 'oldest')

    await waitFor(() => {
      const calls = fetchMock.mock.calls
      expect(calls[calls.length - 1][0].sort).toBe('oldest')
    })
    expect(screen.getByTestId('search')).toHaveTextContent('sort=oldest')
  })

  it('reproduces the view from a shared URL (filters drive the first fetch)', async () => {
    fetchMock.mockResolvedValue(page([], 0, null))
    renderLibrary('/library?sort=oldest&has_gps=true&camera=Canon')

    await screen.findByText('No photos found')
    const first = fetchMock.mock.calls[0][0]
    expect(first.sort).toBe('oldest')
    expect(first.has_gps).toBe('true')
    expect(first.camera).toBe('Canon')
  })

  it('Back restores the previous view and refetches it (Zpět vždy funguje)', async () => {
    fetchMock.mockResolvedValue(page([photo('a', 'a.jpg')], 1, null))
    const user = userEvent.setup()
    renderLibrary()

    await screen.findByRole('link', { name: 'a.jpg' })

    // Push a new view (sort=oldest), creating a history entry.
    await user.selectOptions(screen.getByLabelText('Sort'), 'oldest')
    await waitFor(() => {
      expect(screen.getByTestId('search')).toHaveTextContent('sort=oldest')
    })

    expect(screen.getByLabelText('Sort')).toHaveValue('oldest')

    // Back must restore the default view: the control and a refetch with newest.
    await user.click(screen.getByRole('button', { name: '__back' }))

    await waitFor(() => {
      expect(screen.getByLabelText('Sort')).toHaveValue('newest')
    })
    const calls = fetchMock.mock.calls
    expect(calls[calls.length - 1][0].sort).toBe('newest')
  })

  it('requests the next page when the grid reaches its end', async () => {
    fetchMock.mockResolvedValueOnce(page([photo('a', 'a.jpg')], 3, 1))
    const user = userEvent.setup()
    renderLibrary()

    await screen.findByRole('link', { name: 'a.jpg' })

    fetchMock.mockResolvedValueOnce(page([photo('b', 'b.jpg')], 3, null))
    await user.click(screen.getByRole('button', { name: '__endReached' }))

    expect(await screen.findByRole('link', { name: 'b.jpg' })).toBeInTheDocument()
    const second = fetchMock.mock.calls[1][0]
    expect(second.offset).toBe(1)
  })
})
