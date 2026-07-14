import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { forwardRef, type ReactNode, useImperativeHandle } from 'react'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter, useLocation, useNavigate } from 'react-router-dom'
import { type ListRange, type VirtuosoGridHandle } from 'react-virtuoso'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
import i18n from '../i18n'
import { type AlbumCount, type LabelCount } from '../services/organize'
import { type SubjectCount } from '../services/people'
import { type Photo, type PhotoListResponse, type Timeline } from '../services/photos'

import { LibraryPage } from './LibraryPage'

// Shared spy captured across renders so tests can assert the scrubber scrolled
// the grid. Hoisted so the (hoisted) vi.mock factory can reference it.
const grid = vi.hoisted(() => ({ scrollToIndex: vi.fn() }))

// Minimal stand-in for react-virtuoso's grid: jsdom has no layout, so the real
// virtualized grid would render nothing. This renders every item, exposes a
// button to fire `endReached` (the infinite-scroll trigger), and forwards a
// `scrollToIndex` handle so the timeline scrubber can drive it.
interface MockGridProps {
  data: Photo[]
  itemContent: (index: number, item: Photo) => ReactNode
  endReached?: () => void
  rangeChanged?: (range: ListRange) => void
}
vi.mock('react-virtuoso', () => ({
  VirtuosoGrid: forwardRef<VirtuosoGridHandle, MockGridProps>(function MockGrid(
    { data, itemContent, endReached },
    ref,
  ) {
    useImperativeHandle(ref, () => ({
      scrollToIndex: grid.scrollToIndex,
      scrollTo: vi.fn(),
      scrollBy: vi.fn(),
    }))
    return (
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
    )
  }),
}))

// Keep the real thumbUrl/GRID_THUMB_SIZE; only the network calls are faked.
vi.mock('../services/photos', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/photos')>()
  return { ...actual, fetchPhotos: vi.fn(), fetchTimeline: vi.fn(), fetchPhotoYears: vi.fn() }
})

// The filter bar's album/label facets load their options on mount.
vi.mock('../services/organize', () => ({ fetchAlbums: vi.fn(), fetchLabels: vi.fn() }))

// The filter bar's person facet loads its subjects on mount.
vi.mock('../services/people', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/people')>()
  return { ...actual, fetchSubjects: vi.fn() }
})

const { fetchPhotos, fetchTimeline, fetchPhotoYears } = await import('../services/photos')
const { fetchAlbums, fetchLabels } = await import('../services/organize')
const { fetchSubjects } = await import('../services/people')
const fetchMock = vi.mocked(fetchPhotos)
const timelineMock = vi.mocked(fetchTimeline)
const yearsMock = vi.mocked(fetchPhotoYears)
const albumsMock = vi.mocked(fetchAlbums)
const labelsMock = vi.mocked(fetchLabels)
const subjectsMock = vi.mocked(fetchSubjects)

const EMPTY_TIMELINE: Timeline = { buckets: [], total: 0 }

/** An album the facet select offers, trimmed to the fields the bar reads. */
function album(uid: string, title: string, photoCount: number): AlbumCount {
  return {
    uid,
    slug: uid,
    title,
    description: '',
    type: 'album',
    private: false,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    photo_count: photoCount,
  }
}

/** A label the facet select offers, trimmed to the fields the bar reads. */
function label(uid: string, name: string, photoCount: number): LabelCount {
  return {
    uid,
    slug: uid,
    name,
    priority: 0,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    photo_count: photoCount,
  }
}

/** A subject the person facet offers, trimmed to the fields the bar reads. */
function subject(uid: string, name: string, markerCount: number): SubjectCount {
  return {
    uid,
    slug: uid,
    name,
    type: 'person',
    favorite: false,
    notes: '',
    private: false,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    marker_count: markerCount,
  }
}

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
      <span data-testid="pathname">{location.pathname}</span>
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

/** Editor auth context: write actions (selection, bulk edit) are enabled. */
const editorAuth = {
  ...viewerAuth,
  user: { uid: 'u2', username: 'e', display_name: 'E', role: 'editor' },
  role: 'editor',
  canWrite: true,
} as unknown as AuthContextValue

function renderLibraryAs(auth: AuthContextValue, initialEntry = '/') {
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={auth}>
        <MemoryRouter initialEntries={[initialEntry]}>
          <LibraryPage />
          <LocationProbe />
        </MemoryRouter>
      </AuthContext.Provider>
    </I18nextProvider>,
  )
}

function renderLibrary(initialEntry = '/') {
  return renderLibraryAs(viewerAuth, initialEntry)
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  fetchMock.mockReset()
  timelineMock.mockReset()
  // Default: no timeline, so the scrubber renders nothing unless a test opts in.
  timelineMock.mockResolvedValue(EMPTY_TIMELINE)
  // Default facet options; a test that cares about the facet row overrides them.
  yearsMock.mockReset()
  yearsMock.mockResolvedValue({ years: [], total: 0 })
  albumsMock.mockReset()
  albumsMock.mockResolvedValue([])
  labelsMock.mockReset()
  labelsMock.mockResolvedValue([])
  subjectsMock.mockReset()
  subjectsMock.mockResolvedValue([])
  grid.scrollToIndex.mockReset()
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

  it('renders the empty state when no photos match the active filters', async () => {
    fetchMock.mockResolvedValue(page([], 0, null))
    renderLibrary('/?camera=Canon')

    expect(await screen.findByText('No photos found')).toBeInTheDocument()
    expect(screen.getByText('Active filters: Camera: Canon')).toBeInTheDocument()
  })

  it('names every active filter in the empty state, the quick filter included', async () => {
    fetchMock.mockResolvedValue(page([], 0, null))
    yearsMock.mockResolvedValue({ years: [{ year: 2023, count: 4 }], total: 4 })
    albumsMock.mockResolvedValue([album('al_1', 'Holidays', 4)])
    labelsMock.mockResolvedValue([label('lb_1', 'Beach', 2)])
    renderLibrary('/?q=sunset&year=2023&album=al_1&label=lb_1')

    // Albums and labels are named by their title, not the UID the URL carries.
    expect(
      await screen.findByText(
        'Active filters: Filter the library: sunset · Year: 2023 · Album: Holidays · Label: Beach',
      ),
    ).toBeInTheDocument()
  })

  it('clears every filter from the empty state, keeping the sort', async () => {
    fetchMock.mockResolvedValue(page([], 0, null))
    renderLibrary('/?camera=Canon&year=2023&sort=oldest')

    await screen.findByText('No photos found')
    await userEvent.click(screen.getByRole('button', { name: 'Clear all filters' }))

    expect(screen.getByTestId('search')).toHaveTextContent('sort=oldest')
    expect(screen.getByTestId('search').textContent).not.toContain('camera')
    expect(screen.getByTestId('search').textContent).not.toContain('year')
  })

  it('invites an editor to upload when the catalog itself is empty', async () => {
    fetchMock.mockResolvedValue(page([], 0, null))
    renderLibraryAs(editorAuth)

    // Unfiltered and empty means there is nothing to un-filter: point at upload.
    expect(await screen.findByText('There are no photos yet')).toBeInTheDocument()
    expect(screen.queryByText('No photos found')).not.toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Upload photos' })).toHaveAttribute('href', '/upload')
  })

  it('tells a viewer to wait rather than offering an upload they cannot do', async () => {
    fetchMock.mockResolvedValue(page([], 0, null))
    renderLibraryAs(viewerAuth)

    expect(await screen.findByText('There are no photos yet')).toBeInTheDocument()
    expect(
      screen.getByText('Once someone uploads the first photos, they will show up here.'),
    ).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: 'Upload photos' })).not.toBeInTheDocument()
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
    renderLibrary('/?sort=oldest&has_gps=true&camera=Canon')

    await screen.findByText('No photos found')
    const first = fetchMock.mock.calls[0][0]
    expect(first.sort).toBe('oldest')
    expect(first.has_gps).toBe('true')
    expect(first.camera).toBe('Canon')
  })

  it('writes a selected year facet to the query string and refetches with it', async () => {
    fetchMock.mockResolvedValue(page([photo('a', 'a.jpg')], 1, null))
    yearsMock.mockResolvedValue({
      years: [
        { year: 2023, count: 12 },
        { year: 2021, count: 3 },
      ],
      total: 15,
    })
    const user = userEvent.setup()
    renderLibrary()

    await screen.findByRole('link', { name: 'a.jpg' })
    await user.selectOptions(await screen.findByLabelText('Year'), '2023')

    expect(screen.getByTestId('search')).toHaveTextContent('year=2023')
    await waitFor(() => {
      const calls = fetchMock.mock.calls
      expect(calls[calls.length - 1][0].year).toBe('2023')
    })
  })

  it('writes a selected album facet to the query string, and Back removes it', async () => {
    fetchMock.mockResolvedValue(page([photo('a', 'a.jpg')], 1, null))
    albumsMock.mockResolvedValue([album('al_1', 'Holidays', 12)])
    const user = userEvent.setup()
    renderLibrary()

    await screen.findByRole('link', { name: 'a.jpg' })
    await user.click(await screen.findByLabelText('Album'))
    await user.click(await screen.findByRole('option', { name: /Holidays/ }))

    expect(screen.getByTestId('search')).toHaveTextContent('album=al_1')
    await waitFor(() => {
      const calls = fetchMock.mock.calls
      expect(calls[calls.length - 1][0].album).toBe('al_1')
    })

    // Facets push history, so Back returns to the unfiltered grid.
    await user.click(screen.getByRole('button', { name: '__back' }))
    await waitFor(() => {
      expect(screen.getByTestId('search').textContent).not.toContain('album')
    })
  })

  it('writes the picked person to the URL, refetches with it, and Back removes it', async () => {
    fetchMock.mockResolvedValue(page([photo('a', 'a.jpg')], 1, null))
    subjectsMock.mockResolvedValue([subject('su_1', 'Alice', 9)])
    const user = userEvent.setup()
    renderLibrary()

    await screen.findByRole('link', { name: 'a.jpg' })
    await user.click(await screen.findByLabelText('Person'))
    await user.click(await screen.findByRole('option', { name: /Alice/ }))

    expect(screen.getByTestId('search')).toHaveTextContent('person=su_1')
    await waitFor(() => {
      const calls = fetchMock.mock.calls
      expect(calls[calls.length - 1][0].person).toBe('su_1')
    })

    // The person facet pushes history, so Back drops it — "Zpět vždy funguje".
    await user.click(screen.getByRole('button', { name: '__back' }))
    await waitFor(() => {
      expect(screen.getByTestId('search').textContent).not.toContain('person')
    })
    expect(fetchMock.mock.calls[fetchMock.mock.calls.length - 1][0].person).toBe('')
  })

  it('sets and clears the favorites scope from the filter bar, refetching each time', async () => {
    fetchMock.mockResolvedValue(page([photo('a', 'a.jpg')], 1, null))
    const user = userEvent.setup()
    renderLibrary()

    await screen.findByRole('link', { name: 'a.jpg' })
    // The favorites toggle lives in the advanced panel.
    await user.click(screen.getByRole('button', { name: /Filters/ }))
    await user.selectOptions(await screen.findByLabelText('Favorites'), 'true')

    expect(screen.getByTestId('search')).toHaveTextContent('favorite=true')
    await waitFor(() => {
      const calls = fetchMock.mock.calls
      expect(calls[calls.length - 1][0].favorite).toBe('true')
    })

    await user.selectOptions(screen.getByLabelText('Favorites'), '')
    await waitFor(() => {
      expect(screen.getByTestId('search').textContent).not.toContain('favorite')
    })
  })

  it('writes a selected label facet to the query string', async () => {
    fetchMock.mockResolvedValue(page([photo('a', 'a.jpg')], 1, null))
    labelsMock.mockResolvedValue([label('lb_1', 'Beach', 7)])
    const user = userEvent.setup()
    renderLibrary()

    await screen.findByRole('link', { name: 'a.jpg' })
    await user.click(await screen.findByLabelText('Label'))
    await user.click(await screen.findByRole('option', { name: /Beach/ }))

    expect(screen.getByTestId('search')).toHaveTextContent('label=lb_1')
  })

  it('combines the facets with the filters the library already had', async () => {
    fetchMock.mockResolvedValue(page([], 0, null))
    renderLibrary('/?year=2023&album=al_1&label=lb_1&camera=Canon&sort=oldest')

    await screen.findByText('No photos found')
    const first = fetchMock.mock.calls[0][0]
    expect(first.year).toBe('2023')
    expect(first.album).toBe('al_1')
    expect(first.label).toBe('lb_1')
    expect(first.camera).toBe('Canon')
    expect(first.sort).toBe('oldest')
  })

  it('never asks the years endpoint to narrow its own facet', async () => {
    fetchMock.mockResolvedValue(page([], 0, null))
    renderLibrary('/?year=2023&camera=Canon')

    await screen.findByText('No photos found')
    await waitFor(() => {
      expect(yearsMock).toHaveBeenCalled()
    })
    // The other filters still scope the counts; the selected year does not.
    const asked = yearsMock.mock.calls[0][0]
    expect(asked.year).toBe('')
    expect(asked.camera).toBe('Canon')
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

  it('shows favorite hearts to a viewer but no selection / bulk-edit controls', async () => {
    fetchMock.mockResolvedValue(page([photo('a', 'a.jpg')], 1, null))
    renderLibraryAs(viewerAuth)

    // A favorite heart overlay is present (personal action, allowed for all).
    expect(await screen.findByRole('button', { name: 'Add to favorites' })).toBeInTheDocument()
    // But the write-only selection trigger is not.
    expect(screen.queryByRole('button', { name: 'Select' })).not.toBeInTheDocument()
  })

  it('lets an editor enter selection mode and multi-select photos', async () => {
    fetchMock.mockResolvedValue(page([photo('a', 'a.jpg'), photo('b', 'b.jpg')], 2, null))
    const user = userEvent.setup()
    renderLibraryAs(editorAuth)

    await user.click(await screen.findByRole('button', { name: 'Select' }))

    // Tiles become selection targets (buttons) rather than links.
    const tileA = screen.getByRole('button', { name: 'a.jpg' })
    expect(tileA).toHaveAttribute('aria-pressed', 'false')

    // Bulk edit is disabled until something is selected.
    expect(screen.getByRole('button', { name: 'Bulk edit' })).toBeDisabled()

    await user.click(tileA)
    expect(screen.getByRole('button', { name: 'a.jpg' })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByText('1 selected')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Bulk edit' })).toBeEnabled()
  })

  it('select-all selects every photo in view', async () => {
    fetchMock.mockResolvedValue(page([photo('a', 'a.jpg'), photo('b', 'b.jpg')], 2, null))
    const user = userEvent.setup()
    renderLibraryAs(editorAuth)

    await user.click(await screen.findByRole('button', { name: 'Select' }))
    await user.click(screen.getByRole('button', { name: 'Select all' }))

    expect(screen.getByText('2 selected')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'a.jpg' })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByRole('button', { name: 'b.jpg' })).toHaveAttribute('aria-pressed', 'true')
  })

  it('arrow keys move a visible focus highlight between tiles', async () => {
    fetchMock.mockResolvedValue(page([photo('a', 'a.jpg'), photo('b', 'b.jpg')], 2, null))
    renderLibrary()

    await screen.findByRole('link', { name: 'a.jpg' })

    // First arrow focuses the first tile (its wrapper gets the highlight marker).
    fireEvent.keyDown(document, { key: 'ArrowRight' })
    expect(
      screen.getByRole('link', { name: 'a.jpg' }).closest('[data-focused="true"]'),
    ).not.toBeNull()

    // The next arrow moves the highlight to the second tile.
    fireEvent.keyDown(document, { key: 'ArrowRight' })
    expect(
      screen.getByRole('link', { name: 'b.jpg' }).closest('[data-focused="true"]'),
    ).not.toBeNull()
    expect(screen.getByRole('link', { name: 'a.jpg' }).closest('[data-focused="true"]')).toBeNull()

    // Left moves it back to the first tile.
    fireEvent.keyDown(document, { key: 'ArrowLeft' })
    expect(
      screen.getByRole('link', { name: 'a.jpg' }).closest('[data-focused="true"]'),
    ).not.toBeNull()
  })

  it('Enter opens the focused photo detail', async () => {
    fetchMock.mockResolvedValue(page([photo('a', 'a.jpg'), photo('b', 'b.jpg')], 2, null))
    renderLibrary()

    await screen.findByRole('link', { name: 'a.jpg' })
    fireEvent.keyDown(document, { key: 'ArrowRight' }) // focus tile a
    fireEvent.keyDown(document, { key: 'Enter' })

    await waitFor(() => {
      expect(screen.getByTestId('pathname')).toHaveTextContent('/photos/a')
    })
  })

  it('x selects the focused tile and enters selection mode', async () => {
    fetchMock.mockResolvedValue(page([photo('a', 'a.jpg'), photo('b', 'b.jpg')], 2, null))
    renderLibraryAs(editorAuth)

    await screen.findByRole('link', { name: 'a.jpg' })
    fireEvent.keyDown(document, { key: 'ArrowRight' }) // focus tile a
    fireEvent.keyDown(document, { key: 'x' })

    // The tile is now a selection target and is selected; the selection bar shows.
    expect(await screen.findByText('1 selected')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'a.jpg' })).toHaveAttribute('aria-pressed', 'true')
  })

  it('does not move focus while typing in the filter search input', async () => {
    fetchMock.mockResolvedValue(page([photo('a', 'a.jpg'), photo('b', 'b.jpg')], 2, null))
    renderLibrary()

    await screen.findByRole('link', { name: 'a.jpg' })
    const search = screen.getByLabelText('Filter the library')
    search.focus()
    fireEvent.keyDown(search, { key: 'ArrowRight' })

    expect(document.querySelector('[data-focused="true"]')).toBeNull()
  })

  it('clicking a timeline month scrolls the grid to that month’s index', async () => {
    // Three loaded photos spanning two months; the scrubber's January bucket
    // starts at grid index 2 (its cumulative), which is already loaded.
    fetchMock.mockResolvedValue(
      page([photo('a', 'a.jpg'), photo('b', 'b.jpg'), photo('c', 'c.jpg')], 3, null),
    )
    timelineMock.mockResolvedValue({
      buckets: [
        { year: 2026, month: 2, count: 2, cumulative: 0 },
        { year: 2026, month: 1, count: 1, cumulative: 2 },
      ],
      total: 3,
    })
    const user = userEvent.setup()
    renderLibrary()

    const jan = await screen.findByRole('button', { name: 'Jump to Jan 2026' })
    await user.click(jan)

    await waitFor(() => {
      expect(grid.scrollToIndex).toHaveBeenCalledWith({ index: 2, align: 'start' })
    })
  })
})
