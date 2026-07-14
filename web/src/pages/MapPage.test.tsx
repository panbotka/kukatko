import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter, useLocation } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../i18n'
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

function collection(features: MapFeature[]): MapFeatureCollection {
  return { type: 'FeatureCollection', features }
}

/** Surfaces the current location for navigation assertions. */
function LocationProbe() {
  const location = useLocation()
  return <span data-testid="location">{location.pathname + location.search}</span>
}

function renderMap(initialEntry = '/map') {
  return render(
    <I18nextProvider i18n={i18n}>
      <MemoryRouter initialEntries={[initialEntry]}>
        <MapPage />
        <LocationProbe />
      </MemoryRouter>
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
