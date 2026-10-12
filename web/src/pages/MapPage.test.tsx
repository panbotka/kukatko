import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter, useLocation } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../i18n'
import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
import { type LeafletMapProps } from '../components/map/LeafletMap'
import { type MapFeature, type MapFeatureCollection } from '../services/map'

import { MapPage } from './MapPage'

/** The tile URL the fake map reports as failed when the test clicks "fail tile". */
const FAILED_TILE_URL = '/api/v1/map/tiles/basic/7/70/44'

/** The visible box the fake map reports when the test clicks "report bounds". */
const VIEWPORT = { south: 49, west: 14, north: 51, east: 16 }

/** The box the fake map hands over when the test clicks "open cluster". */
const SPOT = { south: 50.08, west: 14.42, north: 50.08, east: 14.42 }

// Stand in for the imperative Leaflet map: render each feature as a button
// that activates its photo tile, plus buttons that report a settled viewport,
// open an unsplittable cluster and fire a tile-load failure the way Leaflet's
// tileerror does. What the page hands the map is mirrored onto data attributes.
vi.mock('../components/map/LeafletMap', () => ({
  LeafletMap: ({
    features,
    mapset,
    photoTiles,
    onBoundsChange,
    onTileError,
    highlightUid,
    initialBounds,
    fillsViewport = false,
  }: LeafletMapProps) => (
    <div
      data-testid="leaflet-map"
      data-mapset={mapset}
      data-fills={String(fillsViewport)}
      data-highlight={highlightUid ?? ''}
      data-area={initialBounds === null || initialBounds === undefined ? '' : 'set'}
    >
      {features.map((f) => (
        <button
          type="button"
          key={f.properties.uid}
          onClick={() => {
            photoTiles?.onOpenPhoto(f.properties.uid)
          }}
        >
          {f.properties.uid}
        </button>
      ))}
      <button
        type="button"
        onClick={() => {
          onBoundsChange?.(VIEWPORT)
        }}
      >
        report bounds
      </button>
      <button
        type="button"
        onClick={() => {
          photoTiles?.onOpenCluster(SPOT)
        }}
      >
        open cluster
      </button>
      <button
        type="button"
        onClick={() => {
          onTileError?.(FAILED_TILE_URL)
        }}
      >
        fail tile
      </button>
    </div>
  ),
}))

// Keep the real helpers; only the network call is faked.
vi.mock('../services/map', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/map')>()
  return { ...actual, fetchMapPhotos: vi.fn() }
})

const { fetchMapPhotos } = await import('../services/map')
const fetchMock = vi.mocked(fetchMapPhotos)

function feature(uid: string, lng = 14.42, lat = 50.08): MapFeature {
  return {
    type: 'Feature',
    geometry: { type: 'Point', coordinates: [lng, lat] },
    properties: {
      uid,
      title: uid,
      media_type: 'image',
      thumb: `/api/v1/photos/${uid}/thumb/tile_224`,
    },
  }
}

function collection(features: MapFeature[], total?: number): MapFeatureCollection {
  const fc: MapFeatureCollection = { type: 'FeatureCollection', features }
  if (total !== undefined) {
    fc.coverage = { located: features.length, total }
  }
  return fc
}

/** Builds a minimal auth context value with the given write capability. */
function auth(canWrite: boolean): AuthContextValue {
  return {
    status: 'authenticated',
    user: { uid: 'u1', username: 'u', display_name: 'U', role: canWrite ? 'editor' : 'viewer' },
    role: canWrite ? 'editor' : 'viewer',
    downloadToken: null,
    canCurate: canWrite,
    canWrite,
    isAdmin: false,
    login: vi.fn(),
    logout: vi.fn(),
    refresh: vi.fn(),
  } as unknown as AuthContextValue
}

/** Surfaces the current location for navigation assertions. */
function LocationProbe() {
  const location = useLocation()
  return <span data-testid="location">{location.pathname + location.search}</span>
}

function renderMap(initialEntry = '/p/v/map', canWrite = true) {
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={auth(canWrite)}>
        <MemoryRouter initialEntries={[initialEntry]}>
          <MapPage />
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

describe('MapPage', () => {
  it('loads the GeoJSON feed and plots the markers', async () => {
    fetchMock.mockResolvedValue(collection([feature('ph1'), feature('ph2')]))
    renderMap()

    expect(await screen.findByRole('button', { name: 'ph1' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'ph2' })).toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('shows the empty state when no photos are geotagged', async () => {
    fetchMock.mockResolvedValue(collection([]))
    renderMap()

    expect(await screen.findByText('No geotagged photos')).toBeInTheDocument()
  })

  it('shows an error with a retry that re-runs the fetch', async () => {
    fetchMock.mockRejectedValueOnce(new Error('boom'))
    const user = userEvent.setup()
    renderMap()

    expect(await screen.findByText('Could not load the map.')).toBeInTheDocument()

    fetchMock.mockResolvedValueOnce(collection([feature('ph1')]))
    await user.click(screen.getByRole('button', { name: 'Try again' }))

    expect(await screen.findByRole('button', { name: 'ph1' })).toBeInTheDocument()
  })

  it('refetches the feed when a filter changes', async () => {
    fetchMock.mockResolvedValue(collection([feature('ph1')]))
    const user = userEvent.setup()
    renderMap()

    await screen.findByRole('button', { name: 'ph1' })
    expect(fetchMock).toHaveBeenCalledTimes(1)

    await user.selectOptions(screen.getByLabelText('Archived'), 'true')

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledTimes(2)
    })
    const lastParams = fetchMock.mock.calls[fetchMock.mock.calls.length - 1][0]
    expect(lastParams.archived).toBe('true')
    expect(screen.getByTestId('location')).toHaveTextContent('archived=true')
  })

  it('switches the mapset via the URL without refetching the feed', async () => {
    fetchMock.mockResolvedValue(collection([feature('ph1')]))
    const user = userEvent.setup()
    renderMap()

    await screen.findByRole('button', { name: 'ph1' })
    expect(screen.getByTestId('leaflet-map')).toHaveAttribute('data-mapset', 'basic')

    await user.click(screen.getByRole('button', { name: 'Aerial' }))

    await waitFor(() => {
      expect(screen.getByTestId('leaflet-map')).toHaveAttribute('data-mapset', 'aerial')
    })
    expect(screen.getByTestId('location')).toHaveTextContent('mapset=aerial')
    // Changing the base map must not reload the markers.
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('opens a photo tile in the viewer, paging through the visible area', async () => {
    fetchMock.mockResolvedValue(collection([feature('ph1')]))
    const user = userEvent.setup()
    renderMap('/p/v/map?archived=only')

    await user.click(await screen.findByRole('button', { name: 'report bounds' }))
    // The page lists the area only once the map has rested (debounced).
    await screen.findByRole('heading', { name: '1 photo in this area' })
    await user.click(screen.getByRole('button', { name: 'ph1' }))
    const location = screen.getByTestId('location').textContent
    expect(location).toMatch(/^\/p\/v\/photos\/ph1\?/)
    expect(location).toContain('bbox=49.00000%2C14.00000%2C51.00000%2C16.00000')
    expect(location).toContain('archived=only')
  })

  it('reproduces the mapset and filters from a shared URL', async () => {
    fetchMock.mockResolvedValue(collection([feature('ph1')]))
    renderMap('/p/v/map?mapset=outdoor&archived=only')

    await screen.findByRole('button', { name: 'ph1' })
    expect(screen.getByTestId('leaflet-map')).toHaveAttribute('data-mapset', 'outdoor')
    expect(fetchMock.mock.calls[0][0].archived).toBe('only')
  })
})

describe('MapPage coverage', () => {
  it('says how much of the library the map speaks for', async () => {
    fetchMock.mockResolvedValue(collection([feature('ph1'), feature('ph2')], 20906))
    renderMap()

    expect(
      await screen.findByText(
        '2 of 20,906 photos are on the map — the rest have no location stored.',
      ),
    ).toBeInTheDocument()
  })

  it('offers an editor the photos that have no location', async () => {
    fetchMock.mockResolvedValue(collection([feature('ph1')], 10))
    const user = userEvent.setup()
    renderMap()

    await user.click(await screen.findByRole('link', { name: 'Fill in locations' }))
    // The library's own route, not the retired `/library` one: an in-app link
    // must not detour through the redirect kept for old bookmarks.
    expect(screen.getByTestId('location')).toHaveTextContent('/p/v?has_gps=false')
  })

  it('states the coverage to a viewer but sends them nowhere', async () => {
    fetchMock.mockResolvedValue(collection([feature('ph1')], 10))
    renderMap('/p/v/map', false)

    expect(await screen.findByText(/1 of 10 photos are on the map/)).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: 'Fill in locations' })).not.toBeInTheDocument()
  })

  it('offers nothing to fill in when every photo is already placed', async () => {
    fetchMock.mockResolvedValue(collection([feature('ph1')], 1))
    renderMap()

    await screen.findByText(/1 of 1 photos are on the map/)
    expect(screen.queryByRole('link', { name: 'Fill in locations' })).not.toBeInTheDocument()
  })

  it('falls back to the plain marker count when the feed reports no coverage', async () => {
    fetchMock.mockResolvedValue(collection([feature('ph1')]))
    renderMap()

    expect(await screen.findByText('Photos on the map: 1')).toBeInTheDocument()
  })
})

/**
 * Points `window.matchMedia` at a fixed phone/desktop answer. The shared test
 * setup stubs a non-matching (desktop) `matchMedia`, which is what every test
 * above runs on — they are therefore the guard that the desktop layout is
 * unchanged; a phone-width test overrides it so the page takes its narrow
 * branch.
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
 * Opens the phone drawer and returns its panel. An open Offcanvas is a portal
 * and the page's own controls are siblings in the same document (not
 * `aria-hidden`), so anything asserted *about the drawer* has to be scoped to
 * this element with `within()`.
 */
async function openMapFilterDrawer(user: ReturnType<typeof userEvent.setup>) {
  await user.click(screen.getByRole('button', { name: /Filters/ }))
  return screen.findByRole('dialog')
}

/**
 * The phone layout. The page used to spend 382 px of a 853 px screen — three
 * mapset tabs, two date pickers, an archive select and a two-line coverage
 * sentence — before the map began. These tests hold the fold to its two
 * promises: nothing but the title, one button and the short coverage stands
 * above the map, and every control that left is reachable in the drawer.
 */
describe('MapPage narrow viewport (phone)', () => {
  afterEach(() => {
    // Restore the shared desktop default so later tests never inherit a phone.
    mockViewport(false)
  })

  it('leaves the header with the title, one button and the coverage in short', async () => {
    mockViewport(true)
    fetchMock.mockResolvedValue(collection([feature('ph1'), feature('ph2')], 20906))
    renderMap()

    await screen.findByRole('button', { name: 'ph1' })

    // The map is not a tab-bar destination, so the heading is the only thing
    // that says where the reader is and stays visible…
    expect(screen.getByRole('heading', { level: 1, name: 'Map' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Filters/ })).toBeInTheDocument()
    // …with the coverage shortened to fit beside the button, still saying the
    // map holds a fraction of the library.
    expect(screen.getByText('2 of 20,906 on the map')).toBeInTheDocument()

    // What no longer costs the map a row: the three mapset tabs, the two date
    // pickers and the archive select.
    expect(screen.queryByRole('button', { name: 'Aerial' })).not.toBeInTheDocument()
    expect(screen.queryByLabelText('Taken from')).not.toBeInTheDocument()
    expect(screen.queryByLabelText('Taken until')).not.toBeInTheDocument()
    expect(screen.queryByLabelText('Archived')).not.toBeInTheDocument()
  })

  it('reveals every control inside the drawer, still writing through to the URL', async () => {
    mockViewport(true)
    fetchMock.mockResolvedValue(collection([feature('ph1')]))
    const user = userEvent.setup()
    renderMap()

    await screen.findByRole('button', { name: 'ph1' })
    const drawer = await openMapFilterDrawer(user)

    // The controls are not merely somewhere on the page — they are inside the
    // drawer, which is the only place a phone can reach them now.
    expect(within(drawer).getByLabelText('Taken from')).toBeInTheDocument()
    expect(within(drawer).getByLabelText('Taken until')).toBeInTheDocument()

    await user.selectOptions(within(drawer).getByLabelText('Archived'), 'only')
    await waitFor(() => {
      expect(screen.getByTestId('location')).toHaveTextContent('archived=only')
    })
    const lastParams = fetchMock.mock.calls[fetchMock.mock.calls.length - 1][0]
    expect(lastParams.archived).toBe('only')
  })

  it('badges the shut drawer with how many filters are narrowing the map', async () => {
    mockViewport(true)
    fetchMock.mockResolvedValue(collection([feature('ph1')]))
    renderMap('/p/v/map?taken_after=2019-06-01&archived=only')

    await screen.findByRole('button', { name: 'ph1' })
    // Two filters are on and every control is hidden: without the badge the
    // reader has no way to tell a sparse map from a filtered one.
    expect(screen.getByRole('button', { name: 'Filters 2' })).toBeInTheDocument()
  })

  it('carries the full coverage sentence and the editor link into the drawer', async () => {
    mockViewport(true)
    fetchMock.mockResolvedValue(collection([feature('ph1')], 10))
    const user = userEvent.setup()
    renderMap()

    await screen.findByRole('button', { name: 'ph1' })
    const drawer = await openMapFilterDrawer(user)

    expect(
      within(drawer).getByText('1 of 10 photos are on the map — the rest have no location stored.'),
    ).toBeInTheDocument()
    await user.click(within(drawer).getByRole('link', { name: 'Fill in locations' }))
    expect(screen.getByTestId('location')).toHaveTextContent('/p/v?has_gps=false')
  })

  it('closes the drawer on a footer that already says what the filters left', async () => {
    mockViewport(true)
    fetchMock.mockResolvedValue(collection([feature('ph1'), feature('ph2')]))
    const user = userEvent.setup()
    renderMap()

    await screen.findByRole('button', { name: 'ph1' })
    const drawer = await openMapFilterDrawer(user)

    // The count is read *inside* the drawer, before the trip back out.
    const apply = within(drawer).getByRole('button', { name: 'Show 2 photos on the map' })
    await user.click(apply)

    await waitFor(() => {
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    })
  })

  it('offers a way out even when the filters leave nothing on the map', async () => {
    mockViewport(true)
    fetchMock.mockResolvedValue(collection([], 10))
    const user = userEvent.setup()
    renderMap('/p/v/map?taken_after=2030-01-01')

    await screen.findByText('No geotagged photos')
    const drawer = await openMapFilterDrawer(user)

    // "Show 0 photos" would read as a broken promise; the way out stays, and
    // clearing the filters is offered beside it without closing the drawer.
    expect(
      within(drawer).getByRole('button', { name: 'No located photos — close' }),
    ).toBeInTheDocument()
    await user.click(within(drawer).getByRole('button', { name: 'Clear filters' }))
    expect(screen.getByRole('dialog')).toBeInTheDocument()
    expect(screen.getByTestId('location')).not.toHaveTextContent('taken_after')
  })
})

/**
 * Points `window.matchMedia` at a viewport `width` wide, answering the
 * `max-width` queries the page asks honestly — a tablet is narrow enough for
 * the tab bar but not for the phone header, which a single yes/no cannot say.
 */
function mockWidth(width: number): void {
  window.matchMedia = vi.fn().mockImplementation((query: string) => {
    const max = /max-width:\s*([\d.]+)px/.exec(query)
    return {
      matches: max !== null && width <= Number(max[1]),
      media: query,
      onchange: null,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      addListener: vi.fn(),
      removeListener: vi.fn(),
      dispatchEvent: vi.fn(),
    }
  })
}

/**
 * The map as the whole page, on every screen. On a phone the map used to end
 * around 600 px of an 844 px screen, with the footer and empty background under
 * it, and on a desktop a fixed 70dvh map pushed the photos below the fold; the
 * page now wears the class the shell's stylesheet answers by sizing the column
 * to the viewport, dropping the footer and flexing the map into the rest (the
 * CSS half is guarded by `styles/mapFill.test.ts`).
 */
describe('MapPage full-height layout', () => {
  afterEach(() => {
    mockViewport(false)
  })

  /** The page root, which carries the layout class the shell's `:has()` reads. */
  function pageRoot(): Element {
    const root = screen.getByTestId('leaflet-map').closest('.kk-map-page')
    if (root === null) {
      throw new Error('the map page root was not rendered')
    }
    return root
  }

  it.each([
    ['a phone', 390],
    ['a portrait tablet', 820],
    ['a desktop', 1440],
  ])('fills the viewport with the map on %s', async (_name, width) => {
    mockWidth(width)
    fetchMock.mockResolvedValue(collection([feature('ph1')]))
    renderMap()

    await screen.findByRole('button', { name: 'ph1' })
    expect(pageRoot()).toHaveClass('kk-map-fill')
    // The map sits in the canvas the stylesheet flexes down to the bottom edge…
    const canvas = screen.getByTestId('leaflet-map').parentElement
    expect(canvas).toHaveClass('kk-map-canvas')
    expect(canvas?.parentElement).toHaveClass('kk-map-stage')
    // …and, with no page left to scroll, takes a one-finger swipe itself.
    expect(screen.getByTestId('leaflet-map')).toHaveAttribute('data-fills', 'true')
  })
})

/**
 * The area panel: the photos inside the settled viewport, newest first, as a
 * grid — a column beside the map on a desktop, a bottom sheet on a phone.
 */
describe('MapPage area panel', () => {
  afterEach(() => {
    mockViewport(false)
  })

  it('lists only the photos inside the visible area', async () => {
    fetchMock.mockResolvedValue(
      collection([feature('ph1'), feature('ph2'), feature('far', -73.98, 40.75)]),
    )
    const user = userEvent.setup()
    renderMap()

    await user.click(await screen.findByRole('button', { name: 'report bounds' }))
    expect(
      await screen.findByRole('heading', { name: '2 photos in this area' }),
    ).toBeInTheDocument()
  })

  it('opens an unsplittable cluster as its own grid, and Back to the area', async () => {
    fetchMock.mockResolvedValue(collection([feature('ph1'), feature('ph2', 14.5, 50.1)]))
    const user = userEvent.setup()
    renderMap()

    await user.click(await screen.findByRole('button', { name: 'report bounds' }))
    await screen.findByRole('heading', { name: '2 photos in this area' })
    await user.click(screen.getByRole('button', { name: 'open cluster' }))

    expect(await screen.findByRole('heading', { name: '1 photo at this spot' })).toBeInTheDocument()
    expect(screen.getByTestId('location')).toHaveTextContent('sel=50.08000%2C14.42000')

    await user.click(screen.getByRole('button', { name: /Whole area/ }))
    expect(
      await screen.findByRole('heading', { name: '2 photos in this area' }),
    ).toBeInTheDocument()
    expect(screen.getByTestId('location')).not.toHaveTextContent('sel=')
  })

  it('collapses the desktop column into the URL and brings it back', async () => {
    fetchMock.mockResolvedValue(collection([feature('ph1')]))
    const user = userEvent.setup()
    renderMap()

    await user.click(await screen.findByRole('button', { name: 'Hide the photo panel' }))
    expect(screen.getByTestId('location')).toHaveTextContent('panel=closed')
    expect(screen.queryByRole('complementary')).not.toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: /Photos in this area/ }))
    expect(screen.getByTestId('location')).not.toHaveTextContent('panel=')
    expect(
      screen.getByRole('complementary', { name: 'Photos in the map area' }),
    ).toBeInTheDocument()
  })

  it('restores a collapsed column from a shared URL', async () => {
    fetchMock.mockResolvedValue(collection([feature('ph1')]))
    renderMap('/p/v/map?panel=closed')

    expect(await screen.findByRole('button', { name: /Photos in this area/ })).toBeInTheDocument()
    expect(screen.queryByRole('complementary')).not.toBeInTheDocument()
  })

  it('peeks on a phone with a strip of photos, and the handle raises the sheet', async () => {
    mockViewport(true)
    fetchMock.mockResolvedValue(collection([feature('ph1')]))
    const user = userEvent.setup()
    renderMap()

    await user.click(await screen.findByRole('button', { name: 'report bounds' }))
    const sheet = await screen.findByRole('region', { name: 'Photos in the map area' })
    expect(sheet).toHaveAttribute('data-state', 'peek')
    // The strip's photo links into the viewer with the area's query.
    const link = await within(sheet).findByRole('link', { name: 'Undated photo' })
    expect(link.getAttribute('href')).toContain('/p/v/photos/ph1?bbox=')

    await user.click(within(sheet).getByRole('button', { name: 'Enlarge the photo panel' }))
    expect(screen.getByTestId('location')).toHaveTextContent('panel=half')
    expect(sheet).toHaveAttribute('data-state', 'half')
  })

  it('raises a peeking sheet when a cluster opens as a grid', async () => {
    mockViewport(true)
    fetchMock.mockResolvedValue(collection([feature('ph1')]))
    const user = userEvent.setup()
    renderMap()

    await user.click(await screen.findByRole('button', { name: 'open cluster' }))
    expect(screen.getByTestId('location')).toHaveTextContent('panel=half')
  })

  it('switches the base map from the folded phone control', async () => {
    mockViewport(true)
    fetchMock.mockResolvedValue(collection([feature('ph1')]))
    const user = userEvent.setup()
    renderMap()

    await screen.findByRole('button', { name: 'ph1' })
    await user.click(screen.getByRole('button', { name: 'Change the base map' }))
    await user.click(screen.getByRole('button', { name: 'Aerial' }))
    expect(screen.getByTestId('leaflet-map')).toHaveAttribute('data-mapset', 'aerial')
    // Picking folds the choices away again.
    expect(screen.queryByRole('button', { name: 'Outdoor' })).not.toBeInTheDocument()
  })
})

describe('MapPage deep links', () => {
  it('highlights the photo "show on map" pointed at', async () => {
    fetchMock.mockResolvedValue(collection([feature('ph1')]))
    renderMap('/p/v/map?lat=50.08&lng=14.42&z=17&photo=ph1')

    await screen.findByRole('button', { name: 'ph1' })
    expect(screen.getByTestId('leaflet-map')).toHaveAttribute('data-highlight', 'ph1')
  })

  it('hands the area of a viewer Back link to the map to fit', async () => {
    fetchMock.mockResolvedValue(collection([feature('ph1')]))
    renderMap('/p/v/map?area=49%2C14%2C51%2C16')

    await screen.findByRole('button', { name: 'ph1' })
    expect(screen.getByTestId('leaflet-map')).toHaveAttribute('data-area', 'set')
  })
})

describe('MapPage tile failures', () => {
  /** Stubs the tile probe's fetch with the given tile-proxy status. */
  function stubTileProbe(status: number): ReturnType<typeof vi.fn> {
    const probe = vi.fn().mockResolvedValue(new Response(null, { status }))
    vi.stubGlobal('fetch', probe)
    return probe
  }

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('explains a rejected map key instead of leaving the tiles silently grey', async () => {
    fetchMock.mockResolvedValue(collection([feature('ph1')]))
    stubTileProbe(424)
    const user = userEvent.setup()
    renderMap()

    await user.click(await screen.findByRole('button', { name: 'fail tile' }))

    expect(
      await screen.findByText('Map tiles could not be loaded — the map key was rejected.'),
    ).toBeInTheDocument()
    // The map itself must stay usable: the markers still render on the empty
    // background.
    expect(screen.getByRole('button', { name: 'ph1' })).toBeInTheDocument()
  })

  it('dismisses the warning when the user closes it', async () => {
    fetchMock.mockResolvedValue(collection([feature('ph1')]))
    stubTileProbe(424)
    const user = userEvent.setup()
    renderMap()

    await user.click(await screen.findByRole('button', { name: 'fail tile' }))
    const warning = await screen.findByText(
      'Map tiles could not be loaded — the map key was rejected.',
    )
    expect(warning).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: /close/i }))

    await waitFor(() => {
      expect(
        screen.queryByText('Map tiles could not be loaded — the map key was rejected.'),
      ).not.toBeInTheDocument()
    })
  })

  it('probes only once for a whole burst of failing tiles', async () => {
    fetchMock.mockResolvedValue(collection([feature('ph1')]))
    const probe = stubTileProbe(424)
    const user = userEvent.setup()
    renderMap()

    const failTile = await screen.findByRole('button', { name: 'fail tile' })
    await user.click(failTile)
    await screen.findByText('Map tiles could not be loaded — the map key was rejected.')
    // A failing map fires one tileerror per tile in the viewport; the cause is
    // already known, so none of them may cost another request.
    await user.click(failTile)
    await user.click(failTile)

    expect(probe).toHaveBeenCalledTimes(1)
  })

  it('says nothing when the failing tile turns out to be fine', async () => {
    fetchMock.mockResolvedValue(collection([feature('ph1')]))
    stubTileProbe(200)
    const user = userEvent.setup()
    renderMap()

    await user.click(await screen.findByRole('button', { name: 'fail tile' }))

    await waitFor(() => {
      expect(screen.queryByText(/Map tiles could not be loaded/)).not.toBeInTheDocument()
    })
  })
})
