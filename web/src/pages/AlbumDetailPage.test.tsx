import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { forwardRef, type ReactNode, useEffect, useImperativeHandle, useRef, useState } from 'react'
import { type ListRange, type StateSnapshot, type VirtuosoHandle } from 'react-virtuoso'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
import i18n from '../i18n'
import { writeGridScroll } from '../lib/gridScroll'
import { ApiError } from '../services/auth'
import { type Album } from '../services/organize'
import { type Photo, type PhotoListResponse, type Timeline } from '../services/photos'

import { AlbumDetailPage } from './AlbumDetailPage'

// Shared spies captured across renders, so a test can assert the timeline
// scrolled the grid. Hoisted so the (hoisted) vi.mock factory can reference them.
const grid = vi.hoisted(() => ({
  /** Every jump the page asked for, in photo indices (see the mock below). */
  scrollToIndex: vi.fn(),
  /** The position the grid was mounted with, if the page restored one. */
  restoredFrom: null as StateSnapshot | null,
}))

/**
 * How many photos the mock list keeps "on screen". The album hands the grid an
 * array as long as the whole album, so a mock rendering all of it would mount
 * every tile of the very albums these tests make big on purpose.
 */
const MOCK_WINDOW = 100

// Stand-in for react-virtuoso's list (jsdom lays nothing out, so the real one
// renders nothing). The photo wall virtualizes by justified *row*, so this is
// handed rows: it renders about MOCK_WINDOW photos' worth of them from wherever
// the last `scrollToIndex` landed — which is what makes it a faithful stand-in
// for a windowed list — reports that window through `rangeChanged`, and forwards
// a `scrollToIndex` handle so the timeline can drive it. The handle records the
// jump in *photo* indices (the row's first and last), because that is the only
// thing about a row a test has any business knowing.
interface MockRow {
  start: number
  tiles: { index: number }[]
}
interface MockListProps {
  data: readonly MockRow[]
  itemContent: (index: number, row: MockRow) => ReactNode
  computeItemKey?: (index: number, row: MockRow) => string
  rangeChanged?: (range: ListRange) => void
  restoreStateFrom?: StateSnapshot
}
vi.mock('react-virtuoso', () => ({
  Virtuoso: forwardRef<VirtuosoHandle, MockListProps>(function MockList(
    { data, itemContent, computeItemKey, rangeChanged, restoreStateFrom },
    ref,
  ) {
    const [start, setStart] = useState(0)
    // Real virtuoso reads `restoreStateFrom` once, as it mounts; recording it is
    // as much as jsdom can say about the restore.
    grid.restoredFrom = restoreStateFrom ?? null
    const rangeRef = useRef(rangeChanged)
    rangeRef.current = rangeChanged
    const dataRef = useRef(data)
    dataRef.current = data
    useImperativeHandle(ref, () => ({
      scrollToIndex: (location: number | { index?: number | 'LAST'; align?: string }) => {
        const index = typeof location === 'number' ? location : location.index
        const align = typeof location === 'number' ? undefined : location.align
        const row = typeof index === 'number' ? dataRef.current[index] : undefined
        grid.scrollToIndex({
          align,
          first: row?.start ?? -1,
          last: row === undefined ? -1 : row.start + row.tiles.length - 1,
        })
        if (typeof index === 'number') {
          setStart(index)
        }
      },
      scrollTo: vi.fn(),
      scrollBy: vi.fn(),
      scrollIntoView: vi.fn(),
      autoscrollToBottom: vi.fn(),
      getState: vi.fn(),
    }))
    let end = start
    for (let shown = 0; end < data.length; end++) {
      const size = data[end]?.tiles.length ?? 1
      if (shown + size > MOCK_WINDOW && end > start) {
        break
      }
      shown += size
    }
    end = Math.max(start, end - 1)
    useEffect(() => {
      if (data.length > 0) {
        rangeRef.current?.({ startIndex: start, endIndex: end })
      }
    }, [start, end, data.length])
    const window = []
    for (let index = start; index <= end && index < data.length; index++) {
      const row = data[index]
      window.push(<div key={computeItemKey?.(index, row) ?? index}>{itemContent(index, row)}</div>)
    }
    return <div data-testid="grid">{window}</div>
  }),
}))

vi.mock('../services/photos', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/photos')>()
  return { ...actual, fetchPhotos: vi.fn(), fetchTimeline: vi.fn() }
})

vi.mock('../services/organize', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/organize')>()
  return {
    ...actual,
    fetchAlbum: vi.fn(),
    deleteAlbum: vi.fn(),
    removeAlbumPhotos: vi.fn(),
    updateAlbum: vi.fn(),
    fetchAlbums: vi.fn(),
    fetchLabels: vi.fn(),
  }
})

vi.mock('../services/bulk', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/bulk')>()
  return { ...actual, bulkUpdatePhotos: vi.fn() }
})

const { fetchPhotos, fetchTimeline } = await import('../services/photos')
const { bulkUpdatePhotos } = await import('../services/bulk')
const { fetchAlbum, deleteAlbum, removeAlbumPhotos, fetchAlbums, fetchLabels } =
  await import('../services/organize')
const fetchPhotosMock = vi.mocked(fetchPhotos)
const timelineMock = vi.mocked(fetchTimeline)
const fetchAlbumMock = vi.mocked(fetchAlbum)
const deleteAlbumMock = vi.mocked(deleteAlbum)
const removeMock = vi.mocked(removeAlbumPhotos)
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

/** An album with no months to scrub: the default for tests not about the rail. */
const EMPTY_TIMELINE: Timeline = { buckets: [], total: 0 }

/**
 * A histogram of one month per year from `from` to `to`, oldest first — the order
 * an album's grid runs in — with each bucket holding one photo.
 */
function spanningTimeline(from: number, to: number): Timeline {
  const buckets = []
  for (let year = from; year <= to; year++) {
    buckets.push({ year, month: 6, count: 1, cumulative: year - from })
  }
  return { buckets, total: buckets.length }
}

function album(): Album {
  return {
    uid: 'al_1',
    slug: 'holidays',
    title: 'Holidays',
    description: '',
    type: 'album',
    private: false,
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

/** Surfaces the current URL query, which MemoryRouter keeps to itself. */
function LocationProbe() {
  const location = useLocation()
  return <span data-testid="search">{location.search}</span>
}

/** The query string the page has written, as the router sees it. */
function currentSearch(): string {
  return screen.getByTestId('search').textContent
}

/** A page of `count` photos starting at `from`, in an album of 250. */
function albumPage(from: number, count: number): PhotoListResponse {
  const photos = Array.from({ length: count }, (_, i) =>
    photo(`p${String(from + i)}`, `p${String(from + i)}.jpg`),
  )
  return {
    photos,
    total: 250,
    limit: 100,
    offset: from,
    next_offset: from + count < 250 ? from + count : null,
  }
}

function renderPage(canWrite = true, entry = '/albums/al_1') {
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={auth(canWrite)}>
        <MemoryRouter initialEntries={[entry]}>
          <LocationProbe />
          <Routes>
            <Route path="/albums/:uid" element={<AlbumDetailPage />} />
          </Routes>
        </MemoryRouter>
      </AuthContext.Provider>
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  window.sessionStorage.clear()
  grid.restoredFrom = null
  fetchPhotosMock.mockReset()
  fetchAlbumMock.mockReset()
  deleteAlbumMock.mockReset()
  removeMock.mockReset()
  bulkMock.mockReset()
  albumsMock.mockReset()
  labelsMock.mockReset()
  timelineMock.mockReset()
  grid.scrollToIndex.mockReset()
  timelineMock.mockResolvedValue(EMPTY_TIMELINE)
  removeMock.mockResolvedValue([])
  albumsMock.mockResolvedValue([])
  labelsMock.mockResolvedValue([])
})

describe('AlbumDetailPage', () => {
  it('names the browser tab after the album, so a bookmark says which one', async () => {
    fetchAlbumMock.mockResolvedValue(album())
    fetchPhotosMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    renderPage()

    await screen.findByRole('heading', { name: 'Holidays' })
    expect(document.title).toBe('Album Holidays · Kukátko')
  })

  it('scopes the photo grid to the album from the URL', async () => {
    fetchAlbumMock.mockResolvedValue(album())
    fetchPhotosMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    renderPage()

    expect(await screen.findByRole('heading', { name: 'Holidays' })).toBeInTheDocument()
    await waitFor(() => {
      expect(fetchPhotosMock).toHaveBeenCalled()
    })
    expect(fetchPhotosMock.mock.calls[0][0].album).toBe('al_1')
  })

  it('lets a long unbroken album title wrap inside the header', async () => {
    // A title with no spaces used to hold the header open past a phone's
    // viewport: `.kk-page-title` now breaks anywhere, and its group is allowed
    // to shrink so the break can happen.
    const long = 'Dovolena'.repeat(20)
    fetchAlbumMock.mockResolvedValue({ ...album(), title: long })
    fetchPhotosMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    renderPage()

    const heading = await screen.findByRole('heading', { name: long })
    expect(heading).toHaveClass('kk-page-title')
    expect(heading.parentElement).toHaveClass('kk-min-w-0')
  })

  it('offers a back link that names the album list it returns to', async () => {
    fetchAlbumMock.mockResolvedValue(album())
    fetchPhotosMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    renderPage()

    // An arrow alone said nothing; the label names the destination, and the
    // arrow itself stays decorative so the link's accessible name is the text.
    const back = await screen.findByRole('link', { name: 'Back to albums' })
    expect(back).toHaveAttribute('href', '/albums')
    expect(back.querySelector('.bi-arrow-left')).toHaveAttribute('aria-hidden', 'true')
  })

  it('names the album list in the back link of the error state too', async () => {
    fetchAlbumMock.mockRejectedValue(new Error('boom'))
    fetchPhotosMock.mockResolvedValue(page([]))
    renderPage()

    const back = await screen.findByRole('link', { name: 'Back to albums' })
    expect(back).toHaveAttribute('href', '/albums')
  })

  it('says a deleted album is gone rather than failing blankly', async () => {
    // A link out of the audit log is a link to what an entry recorded a change
    // of — including its deletion — so a 404 is normal here, not a failure.
    fetchAlbumMock.mockRejectedValue(new ApiError(404, 'album not found'))
    fetchPhotosMock.mockResolvedValue(page([]))
    renderPage()

    expect(await screen.findByText('This album no longer exists.')).toBeInTheDocument()
    expect(screen.getByText(/It was most likely deleted/)).toBeInTheDocument()
    expect(screen.queryByText('Could not load this album.')).toBeNull()
    expect(screen.getByRole('link', { name: 'Back to albums' })).toHaveAttribute('href', '/albums')
  })

  it('links each tile to the detail page carrying the album scope', async () => {
    fetchAlbumMock.mockResolvedValue(album())
    fetchPhotosMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    renderPage()

    // The tile's detail link carries ?album so pressing Esc/Back on the photo
    // (and prev/next) returns to this album, not the whole library — and the
    // album's order with it, so prev/next steps the way the grid reads.
    const link = await screen.findByRole('link', { name: 'a.jpg' })
    expect(link).toHaveAttribute('href', '/photos/a?sort=oldest&album=al_1')
  })

  it('renders no sort selector and no manual reordering controls', async () => {
    fetchAlbumMock.mockResolvedValue(album())
    fetchPhotosMock.mockResolvedValue(page([photo('a', 'a.jpg'), photo('b', 'b.jpg')]))
    renderPage()

    await screen.findByRole('link', { name: 'a.jpg' })
    // An album is always chronological — its sort *key* is pinned server-side —
    // so the selector offers the two directions and nothing else.
    const sort = screen.getByRole('combobox', { name: 'Sort' })
    expect([...sort.querySelectorAll('option')].map((o) => o.value)).toEqual(['oldest', 'newest'])
    // Manual ordering is gone: no reorder mode, no per-tile drag handles.
    expect(screen.queryByRole('button', { name: 'Reorder' })).not.toBeInTheDocument()
    expect(
      screen.queryByRole('button', { name: /^Move .+ (earlier|later)$/ }),
    ).not.toBeInTheDocument()
  })

  it('hides mutation controls from viewers', async () => {
    fetchAlbumMock.mockResolvedValue(album())
    fetchPhotosMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    renderPage(false)

    await screen.findByRole('heading', { name: 'Holidays' })
    expect(screen.queryByRole('button', { name: 'Edit' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Delete' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Select a.jpg' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'More edits' })).not.toBeInTheDocument()
  })

  it('deletes the album through the styled confirm dialog, not a native prompt', async () => {
    fetchAlbumMock.mockResolvedValue(album())
    fetchPhotosMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    deleteAlbumMock.mockResolvedValue(undefined)
    const user = userEvent.setup()
    renderPage()

    await screen.findByRole('heading', { name: 'Holidays' })
    // The row control opens the dialog; nothing is deleted until it is confirmed.
    await user.click(screen.getByRole('button', { name: 'Delete' }))
    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByText(/Delete the album "Holidays"/)).toBeInTheDocument()
    expect(deleteAlbumMock).not.toHaveBeenCalled()

    // The confirm button carries the action itself, never "OK".
    await user.click(within(dialog).getByRole('button', { name: 'Delete album' }))
    await waitFor(() => {
      expect(deleteAlbumMock).toHaveBeenCalledWith('al_1')
    })
  })

  it('closes the confirm dialog without deleting when cancelled', async () => {
    fetchAlbumMock.mockResolvedValue(album())
    fetchPhotosMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    const user = userEvent.setup()
    renderPage()

    await screen.findByRole('heading', { name: 'Holidays' })
    await user.click(screen.getByRole('button', { name: 'Delete' }))
    const dialog = await screen.findByRole('dialog')
    await user.click(within(dialog).getByRole('button', { name: 'Cancel' }))

    await waitFor(() => {
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    })
    expect(deleteAlbumMock).not.toHaveBeenCalled()
  })

  it('offers a select checkmark on every tile, with no selection mode to enter', async () => {
    fetchAlbumMock.mockResolvedValue(album())
    fetchPhotosMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    const user = userEvent.setup()
    renderPage()

    // No "Select" step: the tile is a link that already carries its checkmark,
    // exactly as on the library, and the selection bar is still out of the way.
    expect(await screen.findByRole('link', { name: 'a.jpg' })).toBeInTheDocument()
    expect(screen.queryByRole('toolbar', { name: 'Batch actions' })).not.toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Select a.jpg' }))

    // Picking raises the selection bar with the album's selection actions.
    expect(screen.getByRole('button', { name: 'Set as cover' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Remove from album' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'More edits' })).toBeEnabled()
  })

  it('raises the library’s full batch bar, with the album’s own actions merged in', async () => {
    fetchAlbumMock.mockResolvedValue(album())
    fetchPhotosMock.mockResolvedValue(page([photo('a', 'a.jpg'), photo('b', 'b.jpg')]))
    const user = userEvent.setup()
    renderPage()

    await screen.findByRole('link', { name: 'a.jpg' })
    await user.click(screen.getByRole('button', { name: 'Select a.jpg' }))

    // One toolbar, not two: the album's actions live on the shared bar next to
    // the full batch vocabulary the library offers.
    const bars = screen.getAllByRole('toolbar', { name: 'Batch actions' })
    expect(bars).toHaveLength(1)
    const [bar] = bars
    for (const name of [
      'Clear selection',
      'Select all',
      'Add to album',
      'Labels',
      'Favorite',
      'Archive',
      'Download ZIP',
      'Stack selected',
      'More edits',
      'Set as cover',
      'Remove from album',
    ]) {
      expect(within(bar).getByRole('button', { name })).toBeInTheDocument()
    }

    // Select-all picks up the rest of the loaded grid, so the count follows.
    await user.click(within(bar).getByRole('button', { name: 'Select all' }))
    expect(screen.getByText('2 selected')).toBeInTheDocument()

    // A cover is a single photo: two picked leaves that one action inapplicable.
    expect(within(bar).getByRole('button', { name: 'Set as cover' })).toBeDisabled()
  })

  it('adds the selection to another album straight from the bar, then reloads', async () => {
    fetchAlbumMock.mockResolvedValue(album())
    fetchPhotosMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    albumsMock.mockResolvedValue([
      {
        uid: 'al_2',
        slug: 'trips',
        title: 'Trips',
        description: '',
        type: 'album',
        private: false,
        created_at: '2026-01-01T00:00:00Z',
        updated_at: '2026-01-01T00:00:00Z',
        photo_count: 0,
      },
    ])
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

    // Exactly the picked photo, and the album's own grid refetches afterwards.
    await waitFor(() => {
      expect(bulkMock).toHaveBeenCalledWith(['a'], { add_to_albums: ['al_2'] })
    })
    await waitFor(() => {
      expect(fetchPhotosMock.mock.calls.length).toBeGreaterThan(fetchesBefore)
    })
  })

  it('bulk-edits exactly the selected photos, then reloads the grid', async () => {
    fetchAlbumMock.mockResolvedValue(album())
    fetchPhotosMock.mockResolvedValue(
      page([photo('a', 'a.jpg'), photo('b', 'b.jpg'), photo('c', 'c.jpg')]),
    )
    bulkMock.mockResolvedValue({
      results: [],
      counts: { total: 2, updated: 2, skipped: 0, errored: 0 },
    })
    const user = userEvent.setup()
    renderPage()

    await screen.findByRole('link', { name: 'a.jpg' })
    await user.click(screen.getByRole('button', { name: 'Select a.jpg' }))
    await user.click(screen.getByRole('button', { name: 'Select c.jpg' }))

    const fetchesBefore = fetchPhotosMock.mock.calls.length
    await user.click(screen.getByRole('button', { name: 'More edits' }))
    const dialog = await screen.findByRole('dialog')
    await user.selectOptions(within(dialog).getByLabelText('Favorite'), 'true')
    await user.click(within(dialog).getByRole('button', { name: 'Apply' }))

    // The two picked photos, not the three the album scope matches.
    await waitFor(() => {
      expect(bulkMock).toHaveBeenCalledWith(['a', 'c'], { set_favorite: true })
    })

    await user.click(await screen.findByRole('button', { name: 'Done' }))
    // The selection is cleared, so the bar steps back out of the way.
    expect(screen.queryByRole('toolbar', { name: 'Batch actions' })).not.toBeInTheDocument()
    await waitFor(() => {
      expect(fetchPhotosMock.mock.calls.length).toBeGreaterThan(fetchesBefore)
    })
  })

  it('keeps every header action inline on a wide screen', async () => {
    fetchAlbumMock.mockResolvedValue(album())
    fetchPhotosMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    renderPage()

    await screen.findByRole('heading', { name: 'Holidays' })
    // Desktop is unchanged by the phone collapse: no overflow toggle, every
    // action directly on the header row.
    expect(screen.queryByRole('button', { name: 'More actions' })).not.toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Slideshow' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Download ZIP' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Edit' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Delete' })).toBeInTheDocument()
  })

  it('drops the selection when the selected photos are removed from the album', async () => {
    fetchAlbumMock.mockResolvedValue(album())
    fetchPhotosMock.mockResolvedValue(page([photo('a', 'a.jpg'), photo('b', 'b.jpg')]))
    const user = userEvent.setup()
    renderPage()

    await screen.findByRole('link', { name: 'a.jpg' })
    await user.click(screen.getByRole('button', { name: 'Select a.jpg' }))
    await user.click(screen.getByRole('button', { name: 'Remove from album' }))

    await waitFor(() => {
      expect(removeMock).toHaveBeenCalledWith('al_1', ['a'])
    })
    // The selection is dropped, so no removed UID lingers in it — and with it
    // the bar, handing the header back to the album's own actions.
    await waitFor(() => {
      expect(screen.queryByRole('toolbar', { name: 'Batch actions' })).not.toBeInTheDocument()
    })
    expect(screen.getByRole('button', { name: 'Edit' })).toBeInTheDocument()
  })
})

/**
 * Points `window.matchMedia` at a fixed phone/desktop answer. The shared test
 * setup stubs a non-matching (desktop) `matchMedia`; a phone-width test
 * overrides it so the header takes its collapsed branch.
 */
function mockViewport(narrow: boolean): void {
  window.matchMedia = vi.fn().mockImplementation((query: string) => ({
    matches: narrow,
    media: query,
    onchange: null,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    addListener: vi.fn(),
    removeListener: vi.fn(),
    dispatchEvent: vi.fn(),
  }))
}

/**
 * The header's overflow menu, once opened. react-bootstrap mounts it on the
 * first open and then only toggles the `show` class; jsdom loads no Bootstrap
 * CSS, so a closed menu is that missing class, not a missing node.
 */
function overflowMenu(): HTMLElement {
  const menu = document.querySelector<HTMLElement>('.dropdown-menu')
  if (menu === null) {
    throw new Error('the overflow menu has not been rendered')
  }
  return menu
}

describe('AlbumDetailPage on a narrow (phone) screen', () => {
  afterEach(() => {
    // Restore the shared desktop default so later tests never inherit a phone.
    mockViewport(false)
  })

  it('keeps the slideshow inline and folds the rest into an overflow menu', async () => {
    mockViewport(true)
    fetchAlbumMock.mockResolvedValue(album())
    fetchPhotosMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    const user = userEvent.setup()
    renderPage()

    await screen.findByRole('heading', { name: 'Holidays' })
    // One compact row: the primary action plus the "…" toggle, instead of five
    // buttons wrapping into two or three rows beside the title.
    expect(screen.getByRole('link', { name: 'Slideshow' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Download ZIP' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Edit' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Delete' })).not.toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'More actions' }))
    await waitFor(() => {
      expect(overflowMenu()).toHaveClass('show')
    })

    // Every folded action is reachable from the menu, and the destructive one
    // sits behind a divider in danger styling rather than next to "Edit".
    const menu = overflowMenu()
    expect(within(menu).getByRole('button', { name: 'Download ZIP' })).toBeInTheDocument()
    expect(within(menu).getByRole('button', { name: 'Edit' })).toBeInTheDocument()
    const remove = within(menu).getByRole('button', { name: 'Delete' })
    expect(remove).toHaveClass('btn-outline-danger')
    const divider = menu.querySelector('.dropdown-divider')
    expect(divider).not.toBeNull()
    expect(divider?.compareDocumentPosition(remove)).toBe(Node.DOCUMENT_POSITION_FOLLOWING)
  })

  it('deletes the album from the overflow menu, still through the confirm dialog', async () => {
    mockViewport(true)
    fetchAlbumMock.mockResolvedValue(album())
    fetchPhotosMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    deleteAlbumMock.mockResolvedValue(undefined)
    const user = userEvent.setup()
    renderPage()

    await screen.findByRole('heading', { name: 'Holidays' })
    await user.click(screen.getByRole('button', { name: 'More actions' }))
    await waitFor(() => {
      expect(overflowMenu()).toHaveClass('show')
    })
    await user.click(within(overflowMenu()).getByRole('button', { name: 'Delete' }))

    // Collapsing the action changed nothing about it: the dialog still guards
    // the deletion, and the menu steps out of the way behind it.
    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByText(/Delete the album "Holidays"/)).toBeInTheDocument()
    expect(deleteAlbumMock).not.toHaveBeenCalled()
    await waitFor(() => {
      expect(overflowMenu()).not.toHaveClass('show')
    })

    await user.click(within(dialog).getByRole('button', { name: 'Delete album' }))
    await waitFor(() => {
      expect(deleteAlbumMock).toHaveBeenCalledWith('al_1')
    })
  })

  it('edits the album from the overflow menu', async () => {
    mockViewport(true)
    fetchAlbumMock.mockResolvedValue(album())
    fetchPhotosMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    const user = userEvent.setup()
    renderPage()

    await screen.findByRole('heading', { name: 'Holidays' })
    await user.click(screen.getByRole('button', { name: 'More actions' }))
    await waitFor(() => {
      expect(overflowMenu()).toHaveClass('show')
    })
    await user.click(within(overflowMenu()).getByRole('button', { name: 'Edit' }))

    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByLabelText('Title')).toHaveValue('Holidays')
  })

  it('offers a viewer only the actions their role allows', async () => {
    mockViewport(true)
    fetchAlbumMock.mockResolvedValue(album())
    fetchPhotosMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    const user = userEvent.setup()
    renderPage(false)

    await screen.findByRole('heading', { name: 'Holidays' })
    await user.click(screen.getByRole('button', { name: 'More actions' }))
    await waitFor(() => {
      expect(overflowMenu()).toHaveClass('show')
    })

    // The menu is a different place to put the actions, not a way around RBAC:
    // a viewer's overflow holds the download alone.
    const menu = overflowMenu()
    expect(within(menu).getByRole('button', { name: 'Download ZIP' })).toBeInTheDocument()
    expect(within(menu).queryByRole('button', { name: 'Edit' })).not.toBeInTheDocument()
    expect(within(menu).queryByRole('button', { name: 'Delete' })).not.toBeInTheDocument()
    expect(menu.querySelector('.dropdown-divider')).toBeNull()
  })
})

describe('AlbumDetailPage scroll position', () => {
  /** A virtuoso state at the given offset, as the grid would report it. */
  function gridState(scrollTop: number): StateSnapshot {
    return { ranges: [{ startIndex: 0, endIndex: 20, size: 220 }], scrollTop }
  }

  it('restores the position without paging its way back to it', async () => {
    // The grid is a window over the album: it is 250 photos tall from the first
    // response, so the remembered offset has somewhere to land straight away and
    // nothing has to be walked back through to get there.
    writeGridScroll('/albums/al_1', {
      count: 250,
      scrollY: 6000,
      snapshot: gridState(6000),
    })
    fetchAlbumMock.mockResolvedValue(album())
    fetchPhotosMock.mockImplementation((params) =>
      Promise.resolve(albumPage(params.offset ?? 0, 100)),
    )

    renderPage()

    await screen.findByRole('link', { name: 'p0.jpg' })
    expect(grid.restoredFrom).toEqual(gridState(6000))
    // Only the page under the reported range (plus its prefetch neighbour) is
    // fetched — never the whole album.
    expect(fetchPhotosMock.mock.calls.map((c) => c[0].offset)).toEqual([0, 100])
  })

  it('opens at the top of an album it has not shown before', async () => {
    fetchAlbumMock.mockResolvedValue(album())
    fetchPhotosMock.mockResolvedValue(page([photo('a', 'a.jpg')]))

    renderPage()

    await screen.findByRole('link', { name: 'a.jpg' })
    expect(fetchPhotosMock).toHaveBeenCalledTimes(1)
    expect(grid.restoredFrom).toBeNull()
  })
})

describe('AlbumDetailPage description', () => {
  it('shows what the album is, in the words of whoever made it', async () => {
    // The field was stored, editable and returned by the API — and rendered
    // nowhere. It is the answer to "what am I looking at", so it sits under the
    // heading, above the controls.
    fetchAlbumMock.mockResolvedValue({
      ...album(),
      description: 'Sjezd rodáků 2016.\nDva dny, 780 let obce.',
    })
    fetchPhotosMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    renderPage()

    const heading = await screen.findByRole('heading', { name: 'Holidays' })
    const note = screen.getByText(/Sjezd rodáků 2016/)
    // The line breaks the writer typed are kept: a description is often a list.
    expect(note).toHaveClass('kk-prose-note')
    expect(note.textContent).toContain('\n')
    expect(heading.compareDocumentPosition(note) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
  })

  it('spends no line on an album without one', async () => {
    fetchAlbumMock.mockResolvedValue(album())
    fetchPhotosMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    const { container } = renderPage()

    await screen.findByRole('heading', { name: 'Holidays' })
    expect(container.querySelector('.kk-prose-note')).toBeNull()
  })
})

describe('AlbumDetailPage order', () => {
  it('opens oldest first, the way an album is meant to be read', async () => {
    fetchAlbumMock.mockResolvedValue(album())
    fetchPhotosMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    renderPage()

    await screen.findByRole('link', { name: 'a.jpg' })
    expect(fetchPhotosMock.mock.calls[0][0].sort).toBe('oldest')
    expect(screen.getByRole('combobox', { name: 'Sort' })).toHaveValue('oldest')
  })

  it('turns the album round and writes the choice into the URL', async () => {
    fetchAlbumMock.mockResolvedValue(album())
    fetchPhotosMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    const user = userEvent.setup()
    renderPage()

    await screen.findByRole('link', { name: 'a.jpg' })
    await user.selectOptions(screen.getByRole('combobox', { name: 'Sort' }), 'newest')

    // The order reaches the backend — which pins an album to capture time and
    // takes only the direction from here…
    await waitFor(() => {
      expect(fetchPhotosMock.mock.calls.at(-1)?.[0].sort).toBe('newest')
    })
    // …and it lives in the URL, so Back, a reload and a shared link all agree.
    expect(currentSearch()).toContain('sort=newest')
  })

  it('reads a sort the album cannot offer as its own default', async () => {
    // A stale link or a hand-typed URL must not leave the selector showing an
    // order that is not in the list — the grid and the control have to agree.
    fetchAlbumMock.mockResolvedValue(album())
    fetchPhotosMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    renderPage(true, '/albums/al_1?sort=title')

    await screen.findByRole('link', { name: 'a.jpg' })
    expect(screen.getByRole('combobox', { name: 'Sort' })).toHaveValue('oldest')
    expect(fetchPhotosMock.mock.calls[0][0].sort).toBe('oldest')
  })
})

describe('AlbumDetailPage timeline', () => {
  it('gives an album spanning a lifetime the library’s own timeline rail', async () => {
    fetchAlbumMock.mockResolvedValue(album())
    fetchPhotosMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    timelineMock.mockResolvedValue(spanningTimeline(1910, 2026))
    renderPage()

    await screen.findByRole('link', { name: 'a.jpg' })
    expect(await screen.findByRole('navigation', { name: 'Timeline' })).toBeInTheDocument()
    // The histogram is asked for with the album's own scope and order, so its
    // cumulative indexes are indexes into this grid.
    const params = timelineMock.mock.calls[0][0]
    expect(params.album).toBe('al_1')
    expect(params.sort).toBe('oldest')
  })

  it('spares a short album a scale of months it has nothing to put on', async () => {
    fetchAlbumMock.mockResolvedValue(album())
    fetchPhotosMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    // One summer: a rail here is a control offering nothing, and it costs a
    // strip of the screen and the taps under it.
    timelineMock.mockResolvedValue({
      buckets: [
        { year: 2026, month: 6, count: 1, cumulative: 0 },
        { year: 2026, month: 7, count: 1, cumulative: 1 },
      ],
      total: 2,
    })
    renderPage()

    await screen.findByRole('link', { name: 'a.jpg' })
    await waitFor(() => {
      expect(timelineMock).toHaveBeenCalled()
    })
    expect(screen.queryByRole('navigation', { name: 'Timeline' })).toBeNull()
  })

  it('jumps the grid straight to a month and remembers it in the URL', async () => {
    // The whole point of the rail on a 781-photo album: reaching 1936 must cost
    // one scroll and one page, not eight sequential ones.
    fetchAlbumMock.mockResolvedValue(album())
    fetchPhotosMock.mockImplementation((params) =>
      Promise.resolve(albumPage(params.offset ?? 0, 100)),
    )
    timelineMock.mockResolvedValue({
      buckets: [
        { year: 1910, month: 6, count: 200, cumulative: 0 },
        { year: 2026, month: 6, count: 50, cumulative: 200 },
      ],
      total: 250,
    })
    const user = userEvent.setup()
    renderPage()

    await screen.findByRole('link', { name: 'p0.jpg' })
    const rail = await screen.findByRole('navigation', { name: 'Timeline' })
    await user.click(within(rail).getByRole('button', { name: 'Jump to Jun 2026' }))

    // The wall scrolls by justified row, so the jump lands on the row holding
    // photo 200 — reported here as that row's first and last photo.
    expect(grid.scrollToIndex).toHaveBeenCalledWith(expect.objectContaining({ align: 'start' }))
    const jump = grid.scrollToIndex.mock.calls.at(-1)?.[0] as {
      first: number
      last: number
    }
    expect(jump.first).toBeLessThanOrEqual(200)
    expect(jump.last).toBeGreaterThanOrEqual(200)
    await waitFor(() => {
      expect(fetchPhotosMock.mock.calls.map((c) => c[0].offset)).toContain(200)
    })
    expect(currentSearch()).toContain('at=2026-06')
  })

  it('gets out of the way while photos are being picked', async () => {
    fetchAlbumMock.mockResolvedValue(album())
    fetchPhotosMock.mockResolvedValue(page([photo('a', 'a.jpg')]))
    timelineMock.mockResolvedValue(spanningTimeline(1910, 2026))
    const user = userEvent.setup()
    renderPage()

    await screen.findByRole('navigation', { name: 'Timeline' })
    // The rail overlays the right edge, where the tiles' own controls are.
    await user.click(screen.getByRole('button', { name: 'Select a.jpg' }))
    await waitFor(() => {
      expect(screen.queryByRole('navigation', { name: 'Timeline' })).toBeNull()
    })
  })
})
