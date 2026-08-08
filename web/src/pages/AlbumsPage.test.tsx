import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { type ComponentType, type ReactNode } from 'react'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter, useLocation, useNavigate } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
import { type TileGridLayout } from '../components/TileGrid'
import i18n from '../i18n'
import { type Album, type AlbumSummary, type AlbumType } from '../services/organize'

import { AlbumsPage } from './AlbumsPage'

// Minimal stand-in for react-virtuoso's grid (jsdom has no layout, so the real
// one measures zero and mounts nothing). It renders every album through the real
// `List` component, which keeps the grid's own column template assertable.
interface MockGridProps {
  data: AlbumSummary[]
  context: TileGridLayout
  components: { List: ComponentType<{ context: TileGridLayout; children: ReactNode }> }
  itemContent: (index: number, item: AlbumSummary) => ReactNode
  computeItemKey: (index: number, item: AlbumSummary) => string
}
vi.mock('react-virtuoso', () => ({
  VirtuosoGrid: ({ components, context, data, itemContent, computeItemKey }: MockGridProps) => (
    <components.List context={context}>
      {data.map((item, index) => (
        <div key={computeItemKey(index, item)}>{itemContent(index, item)}</div>
      ))}
    </components.List>
  ),
}))

vi.mock('../services/organize', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/organize')>()
  return { ...actual, fetchAlbums: vi.fn(), createAlbum: vi.fn() }
})

const { fetchAlbums, createAlbum } = await import('../services/organize')
const fetchMock = vi.mocked(fetchAlbums)
const createMock = vi.mocked(createAlbum)

function album(
  uid: string,
  title: string,
  { type = 'album', photoCount = 3 }: { type?: AlbumType; photoCount?: number } = {},
): AlbumSummary {
  return {
    uid,
    slug: title.toLowerCase(),
    title,
    description: '',
    type,
    private: false,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    photo_count: photoCount,
  }
}

/**
 * A library like the real one: a couple of hand-made albums among the
 * machine-made month folders, moments and places, plus an empty leftover.
 */
function mixedLibrary(): AlbumSummary[] {
  return [
    album('al_1', 'Dovolená 2019'),
    album('al_2', 'Zebra', { photoCount: 7 }),
    album('al_3', 'Pets', { photoCount: 0 }),
    album('al_4', 'January 2026', { type: 'folder', photoCount: 15 }),
    album('al_5', 'May 2026', { type: 'folder', photoCount: 4 }),
    album('al_6', 'Trip to the lake', { type: 'moment', photoCount: 9 }),
    album('al_7', 'Czechia', { type: 'state', photoCount: 120 }),
  ]
}

/** Builds a minimal auth context value with the given write capability. */
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

function renderPage(canWrite = true, children?: ReactNode, entry = '/albums') {
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={auth(canWrite)}>
        <MemoryRouter initialEntries={[entry]}>
          <AlbumsPage />
          <LocationProbe />
          {children}
        </MemoryRouter>
      </AuthContext.Provider>
    </I18nextProvider>,
  )
}

/** The album titles currently in the grid, in render order. */
function gridTitles(): string[] {
  const grid = document.querySelector<HTMLElement>('.kk-tile-grid')
  if (grid === null) {
    return []
  }
  return within(grid)
    .getAllByRole('link')
    .map((link) => link.getAttribute('aria-label') ?? '')
}

/** The image sources of every tile in the grid, in render order. */
function gridCovers(): string[][] {
  const grid = document.querySelector<HTMLElement>('.kk-tile-grid')
  if (grid === null) {
    return []
  }
  return within(grid)
    .getAllByRole('link')
    .map((link) => [...link.querySelectorAll('img')].map((img) => img.getAttribute('src') ?? ''))
}

/** The section button carrying the given label. */
function section(name: string) {
  return screen.getByRole('button', { name: new RegExp(`^${name}`) })
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  fetchMock.mockReset()
  createMock.mockReset()
})

describe('AlbumsPage', () => {
  it('lists albums with their photo counts', async () => {
    fetchMock.mockResolvedValue([album('al_1', 'Holidays')])
    renderPage()
    expect(await screen.findByText('Holidays')).toBeInTheDocument()
    expect(screen.getByText('3 photos')).toBeInTheDocument()
  })

  it('renders the albums into a virtualized grid that reflows its columns', async () => {
    fetchMock.mockResolvedValue([album('al_1', 'Holidays'), album('al_2', 'Alps')])
    renderPage()

    await screen.findByText('Holidays')
    const grid = document.querySelector<HTMLElement>('.kk-tile-grid')
    expect(grid).not.toBeNull()
    // The virtualized grid keeps the geometry of the plain one it replaced (and
    // of the loading skeleton): 160px-floor `auto-fill` tracks, so the column
    // count still follows the container width — one at 320px, two at 360px.
    expect(grid?.style.gridTemplateColumns).toBe('repeat(auto-fill, minmax(160px, 1fr))')
    expect(grid?.style.gap).toBe('12px')
    expect(gridTitles()).toHaveLength(2)
  })

  it('draws albums built from the same photos with different covers', async () => {
    // The bug this fences off: the cover was each album's newest photo, so four
    // albums holding the same scanned title page all showed that page. A grid of
    // cards earns its extra room over a list only while the cards differ.
    const shared = ['p1', 'p2', 'p3', 'p4', 'p5', 'p6', 'p7', 'p8']
    fetchMock.mockResolvedValue([
      { ...album('al_1', 'Kronika I'), cover_uid: 'p1', cover_uids: shared },
      { ...album('al_2', 'Kronika II'), cover_uid: 'p1', cover_uids: shared },
    ])
    renderPage()

    await screen.findByText('Kronika I')
    const [first = [], second = []] = gridCovers()
    expect(first).toHaveLength(4)
    expect(second).toHaveLength(4)
    expect(first.filter((src) => second.includes(src))).toEqual([])
  })

  it('shows the empty state when there are no albums', async () => {
    fetchMock.mockResolvedValue([])
    renderPage()
    expect(await screen.findByText('No albums yet')).toBeInTheDocument()
  })

  it('renders albums in the order the server returns, without re-sorting', async () => {
    // The server ranks by newest photo, so "Zebra" leading is a valid order that
    // any client-side sort by title would destroy.
    fetchMock.mockResolvedValue([album('al_2', 'Zebra'), album('al_1', 'Alps')])
    renderPage()

    await screen.findByText('Zebra')
    expect(gridTitles()).toEqual(['Zebra', 'Alps'])
  })

  it('opens on the hand-made albums, leaving the machine-made ones to their sections', async () => {
    fetchMock.mockResolvedValue(mixedLibrary())
    renderPage()

    await screen.findByText('Dovolená 2019')
    // Two of seven: the month folders, the moment and the place are elsewhere,
    // and the empty leftover is hidden altogether.
    expect(gridTitles()).toEqual(['Dovolená 2019', 'Zebra'])
  })

  it('counts every section, so the strip says where the rest of the albums are', async () => {
    fetchMock.mockResolvedValue(mixedLibrary())
    renderPage()

    await screen.findByText('Dovolená 2019')
    expect(section('My albums')).toHaveTextContent('2')
    expect(section('By month')).toHaveTextContent('2')
    expect(section('Moments')).toHaveTextContent('1')
    expect(section('Places')).toHaveTextContent('1')
  })

  it('switches sections into the URL, so Back steps out of one', async () => {
    fetchMock.mockResolvedValue(mixedLibrary())
    const user = userEvent.setup()
    renderPage()

    await screen.findByText('Dovolená 2019')
    await user.click(section('By month'))

    expect(gridTitles()).toEqual(['January 2026', 'May 2026'])
    expect(screen.getByTestId('search')).toHaveTextContent('type=folder')

    await user.click(screen.getByRole('button', { name: '__back' }))
    await waitFor(() => {
      expect(gridTitles()).toEqual(['Dovolená 2019', 'Zebra'])
    })
  })

  it('restores the section from the URL, so a link carries the view', async () => {
    fetchMock.mockResolvedValue(mixedLibrary())
    renderPage(true, undefined, '/albums?type=moment')

    await screen.findByText('Trip to the lake')
    expect(gridTitles()).toEqual(['Trip to the lake'])
  })

  it('filters by name as it is typed, without pushing a history entry per keystroke', async () => {
    fetchMock.mockResolvedValue(mixedLibrary())
    const user = userEvent.setup()
    renderPage()

    await screen.findByText('Dovolená 2019')
    // A pushed entry to come back to, so what Back skips is visible.
    await user.click(section('By month'))
    await user.type(screen.getByRole('searchbox', { name: 'Search albums' }), 'may')

    expect(gridTitles()).toEqual(['May 2026'])
    expect(screen.getByTestId('search')).toHaveTextContent('q=may')

    // Live typing replaces the entry, so one Back steps out of the whole search
    // instead of deleting one letter at a time.
    await user.click(screen.getByRole('button', { name: '__back' }))
    await waitFor(() => {
      expect(screen.getByTestId('search').textContent).toBe('')
    })
    expect(gridTitles()).toEqual(['Dovolená 2019', 'Zebra'])
  })

  it('offers the section counts of a search, so a miss points at where the hits are', async () => {
    fetchMock.mockResolvedValue(mixedLibrary())
    const user = userEvent.setup()
    renderPage()

    await screen.findByText('Dovolená 2019')
    await user.type(screen.getByRole('searchbox', { name: 'Search albums' }), 'january')

    expect(await screen.findByText('Nothing matches here')).toBeInTheDocument()
    expect(section('By month')).toHaveTextContent('1')
  })

  it('sorts by name and by photo count', async () => {
    fetchMock.mockResolvedValue(mixedLibrary())
    const user = userEvent.setup()
    renderPage()

    await screen.findByText('Dovolená 2019')
    await user.selectOptions(screen.getByRole('combobox', { name: 'Sort' }), 'name')
    expect(gridTitles()).toEqual(['Dovolená 2019', 'Zebra'])
    expect(screen.getByTestId('search')).toHaveTextContent('sort=name')

    await user.click(section('By month'))
    // 15 photos in January against 4 in May: by name the order is alphabetical,
    // by count the fuller album leads.
    expect(gridTitles()).toEqual(['January 2026', 'May 2026'])
    await user.selectOptions(screen.getByRole('combobox', { name: 'Sort' }), 'count')
    expect(gridTitles()).toEqual(['January 2026', 'May 2026'])
  })

  it('hides albums with no photos until the switch asks for them', async () => {
    fetchMock.mockResolvedValue(mixedLibrary())
    const user = userEvent.setup()
    renderPage()

    await screen.findByText('Dovolená 2019')
    expect(gridTitles()).not.toContain('Pets')

    await user.click(screen.getByRole('checkbox', { name: 'Include empty' }))
    expect(gridTitles()).toContain('Pets')
    expect(screen.getByTestId('search')).toHaveTextContent('empty=1')
  })

  it('renders a machine-made month album in Czech, leaving the stored title alone', async () => {
    await i18n.changeLanguage('cs')
    fetchMock.mockResolvedValue(mixedLibrary())
    renderPage(true, undefined, '/albums?type=folder')

    expect(await screen.findByText('leden 2026')).toBeInTheDocument()
    expect(screen.queryByText('January 2026')).not.toBeInTheDocument()
    // Display only: nothing was sent back to the server.
    expect(createMock).not.toHaveBeenCalled()
  })

  it('creates an album: calls the API and shows the fresh, still empty album', async () => {
    const created: Album = {
      uid: 'al_new',
      slug: 'trip',
      title: 'Trip',
      description: '',
      type: 'album',
      private: false,
      created_at: '2026-01-01T00:00:00Z',
      updated_at: '2026-01-01T00:00:00Z',
    }
    // The fresh album only reaches the grid through a refetch: the page never
    // appends it locally, because only the server knows where it ranks.
    fetchMock.mockResolvedValueOnce([]).mockResolvedValueOnce([{ ...created, photo_count: 0 }])
    createMock.mockResolvedValue(created)
    const user = userEvent.setup()
    renderPage()

    await screen.findByText('No albums yet')
    await user.click(screen.getByRole('button', { name: 'New album' }))
    await user.type(screen.getByLabelText('Title'), 'Trip')
    await user.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => {
      expect(createMock).toHaveBeenCalledWith(expect.objectContaining({ title: 'Trip' }))
    })
    // An album with no photos would fall through the default filter, so saving
    // one turns the switch on rather than hiding what was just created.
    expect(await screen.findByText('Trip')).toBeInTheDocument()
    expect(screen.getByRole('checkbox', { name: 'Include empty' })).toBeChecked()
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it('hides the create control from viewers', async () => {
    fetchMock.mockResolvedValue([album('al_1', 'Holidays')])
    renderPage(false)
    await screen.findByText('Holidays')
    expect(screen.queryByRole('button', { name: 'New album' })).not.toBeInTheDocument()
  })
})
