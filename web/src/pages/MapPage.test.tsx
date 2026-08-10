import { render, screen, waitFor } from '@testing-library/react'
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

// Stand in for the imperative Leaflet map: render each feature as a link so we
// can assert markers reach the map and a marker click navigates to the detail,
// plus a button that fires a tile-load failure the way Leaflet's tileerror does.
vi.mock('../components/map/LeafletMap', () => ({
  LeafletMap: ({ features, mapset, onSelectPhoto, onTileError }: LeafletMapProps) => (
    <div data-testid="leaflet-map" data-mapset={mapset}>
      {features.map((f) => (
        <a
          key={f.properties.uid}
          href={`/photos/${f.properties.uid}`}
          onClick={(e) => {
            e.preventDefault()
            onSelectPhoto(f.properties.uid)
          }}
        >
          {f.properties.uid}
        </a>
      ))}
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

function feature(uid: string): MapFeature {
  return {
    type: 'Feature',
    geometry: { type: 'Point', coordinates: [14.42, 50.08] },
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

function renderMap(initialEntry = '/map', canWrite = true) {
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

    expect(await screen.findByRole('link', { name: 'ph1' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'ph2' })).toBeInTheDocument()
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

    expect(await screen.findByRole('link', { name: 'ph1' })).toBeInTheDocument()
  })

  it('refetches the feed when a filter changes', async () => {
    fetchMock.mockResolvedValue(collection([feature('ph1')]))
    const user = userEvent.setup()
    renderMap()

    await screen.findByRole('link', { name: 'ph1' })
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

    await screen.findByRole('link', { name: 'ph1' })
    expect(screen.getByTestId('leaflet-map')).toHaveAttribute('data-mapset', 'basic')

    await user.click(screen.getByRole('button', { name: 'Aerial' }))

    await waitFor(() => {
      expect(screen.getByTestId('leaflet-map')).toHaveAttribute('data-mapset', 'aerial')
    })
    expect(screen.getByTestId('location')).toHaveTextContent('mapset=aerial')
    // Changing the base map must not reload the markers.
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('navigates to the photo detail when a marker is clicked', async () => {
    fetchMock.mockResolvedValue(collection([feature('ph1')]))
    const user = userEvent.setup()
    renderMap()

    await user.click(await screen.findByRole('link', { name: 'ph1' }))
    expect(screen.getByTestId('location')).toHaveTextContent('/photos/ph1')
  })

  it('reproduces the mapset and filters from a shared URL', async () => {
    fetchMock.mockResolvedValue(collection([feature('ph1')]))
    renderMap('/map?mapset=outdoor&archived=only')

    await screen.findByRole('link', { name: 'ph1' })
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
    expect(screen.getByTestId('location')).toHaveTextContent('/?has_gps=false')
  })

  it('states the coverage to a viewer but sends them nowhere', async () => {
    fetchMock.mockResolvedValue(collection([feature('ph1')], 10))
    renderMap('/map', false)

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
    expect(screen.getByRole('link', { name: 'ph1' })).toBeInTheDocument()
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
