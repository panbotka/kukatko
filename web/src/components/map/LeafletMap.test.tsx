import { render } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { type MapFeature } from '../../services/map'

// A fake Leaflet that records the calls the component makes, so we can assert the
// tile layer points at the proxy (no key), the mandatory controls are added, and
// markers/popups are built — all without a real DOM map (jsdom has no layout).
const leaflet = vi.hoisted(() => {
  interface RecordedTile {
    url: string
    options: Record<string, unknown>
    setUrl: ReturnType<typeof vi.fn>
    /** Handlers the component subscribed to the layer with, by event name. */
    handlers: Record<string, (event: unknown) => void>
  }
  interface RecordedMarker {
    latlng: [number, number]
    options: Record<string, unknown>
    popup: unknown
  }
  const calls = {
    mapOptions: [] as Record<string, unknown>[],
    tiles: [] as RecordedTile[],
    markers: [] as RecordedMarker[],
    clusterAdded: [] as RecordedMarker[],
    clusterCleared: 0,
    controlPositions: [] as (string | undefined)[],
    logoElement: null as HTMLElement | null,
    fitBounds: [] as unknown[],
  }

  const map = {
    on: vi.fn(),
    remove: vi.fn(),
    addLayer: vi.fn(),
    getCenter: () => ({ lat: 50, lng: 14 }),
    getZoom: () => 7,
    fitBounds: vi.fn((latlngs: unknown) => {
      calls.fitBounds.push(latlngs)
    }),
    dragging: { enable: vi.fn(), disable: vi.fn() },
  }

  const cluster = {
    addLayer: vi.fn((m: RecordedMarker) => {
      calls.clusterAdded.push(m)
    }),
    clearLayers: vi.fn(() => {
      calls.clusterCleared += 1
    }),
    on: vi.fn(),
  }

  class Control {
    options: { position?: string }
    onAdd: ((map: unknown) => HTMLElement) | undefined
    constructor(options: { position?: string } = {}) {
      this.options = options
      this.onAdd = undefined
    }
    addTo(m: unknown): this {
      calls.controlPositions.push(this.options.position)
      if (this.onAdd) {
        calls.logoElement = this.onAdd(m)
      }
      return this
    }
  }

  const L = {
    map: vi.fn((_container: HTMLElement, options: Record<string, unknown>) => {
      calls.mapOptions.push(options)
      return map
    }),
    tileLayer: vi.fn((url: string, options: Record<string, unknown>) => {
      const layer: RecordedTile & {
        addTo: (m: unknown) => unknown
        on: (event: string, handler: (event: unknown) => void) => unknown
      } = {
        url,
        options,
        setUrl: vi.fn(),
        handlers: {},
        on: vi.fn((event: string, handler: (event: unknown) => void) => {
          layer.handlers[event] = handler
          return layer
        }),
        addTo: vi.fn(() => layer),
      }
      calls.tiles.push(layer)
      return layer
    }),
    markerClusterGroup: vi.fn(() => cluster),
    marker: vi.fn((latlng: [number, number], options: Record<string, unknown>) => {
      const m: RecordedMarker & { bindPopup: (c: unknown) => unknown } = {
        latlng,
        options,
        popup: undefined,
        bindPopup: vi.fn((content: unknown) => {
          m.popup = content
          return m
        }),
      }
      calls.markers.push(m)
      return m
    }),
    divIcon: vi.fn(() => ({ icon: true })),
    Control,
    DomEvent: { disableClickPropagation: vi.fn() },
  }

  return { calls, map, cluster, L }
})

vi.mock('leaflet', () => ({ default: leaflet.L }))
vi.mock('leaflet.markercluster', () => ({}))

const { DEFAULT_MAP_HEIGHT, LeafletMap } = await import('./LeafletMap')
const { buildPopupElement } = await import('../../lib/mapPopup')

/**
 * Dispatches a touch event carrying the given active touches. jsdom has no
 * `TouchEvent` constructor, so build an `Event` and hang the touch list on it —
 * that is all the gesture handlers read.
 */
function fireTouch(
  target: EventTarget,
  type: string,
  touches: { clientX: number; clientY: number }[],
): void {
  const event = new Event(type, { bubbles: true })
  Object.defineProperty(event, 'touches', { value: touches })
  target.dispatchEvent(event)
}

function feature(uid: string, title: string, lng: number, lat: number): MapFeature {
  return {
    type: 'Feature',
    geometry: { type: 'Point', coordinates: [lng, lat] },
    properties: { uid, title, media_type: 'image', thumb: `/api/v1/photos/${uid}/thumb/tile_224` },
  }
}

const FEATURES: MapFeature[] = [
  feature('ph1', 'Prague', 14.42, 50.08),
  feature('ph2', '', 16.61, 49.19),
]

afterEach(() => {
  vi.clearAllMocks()
  leaflet.calls.mapOptions.length = 0
  leaflet.calls.tiles.length = 0
  leaflet.calls.markers.length = 0
  leaflet.calls.clusterAdded.length = 0
  leaflet.calls.clusterCleared = 0
  leaflet.calls.controlPositions.length = 0
  leaflet.calls.logoElement = null
  leaflet.calls.fitBounds.length = 0
})

function renderMap(features = FEATURES) {
  return render(
    <LeafletMap
      features={features}
      mapset="basic"
      viewport={null}
      onViewportChange={vi.fn()}
      onSelectPhoto={vi.fn()}
      thumbAlt="Photo on the map"
    />,
  )
}

describe('LeafletMap tile layer', () => {
  it('points the tile layer at the backend proxy and carries no API key', () => {
    renderMap()
    const tile = leaflet.calls.tiles[0]
    expect(tile.url).toBe('/api/v1/map/tiles/basic/{z}/{x}/{y}{r}')
    expect(tile.url).not.toMatch(/api[_-]?key/i)
    expect(tile.url).not.toContain('mapy.com')
    expect(tile.options.detectRetina).toBe(true)
  })

  it('swaps the tile URL when the mapset changes', () => {
    const { rerender } = renderMap()
    const tile = leaflet.calls.tiles[0]
    rerender(
      <LeafletMap
        features={FEATURES}
        mapset="aerial"
        viewport={null}
        onViewportChange={vi.fn()}
        onSelectPhoto={vi.fn()}
        thumbAlt="Photo on the map"
      />,
    )
    expect(tile.setUrl).toHaveBeenCalledWith('/api/v1/map/tiles/aerial/{z}/{x}/{y}{r}')
  })
})

describe('LeafletMap tile failures', () => {
  it('reports the URL of a tile that failed to load, so the page can explain why', () => {
    const onTileError = vi.fn()
    render(
      <LeafletMap
        features={FEATURES}
        mapset="basic"
        viewport={null}
        onViewportChange={vi.fn()}
        onSelectPhoto={vi.fn()}
        thumbAlt="Photo on the map"
        onTileError={onTileError}
      />,
    )

    const tile = leaflet.calls.tiles[0]
    const img = document.createElement('img')
    img.src = '/api/v1/map/tiles/basic/7/70/44'
    tile.handlers.tileerror({ tile: img })

    expect(onTileError).toHaveBeenCalledTimes(1)
    expect(String(onTileError.mock.calls[0][0])).toContain('/api/v1/map/tiles/basic/7/70/44')
  })

  it('stays silent when no tile-error handler is given', () => {
    renderMap()
    const tile = leaflet.calls.tiles[0]
    const img = document.createElement('img')
    img.src = '/api/v1/map/tiles/basic/7/70/44'

    // The map is also used without an error handler (the photo-detail mini-map);
    // a failing tile there must not blow up.
    expect(() => {
      tile.handlers.tileerror({ tile: img })
    }).not.toThrow()
  })
})

describe('LeafletMap mandatory mapy.com controls', () => {
  it('renders the attribution with the Seznam copyright link', () => {
    renderMap()
    const attribution = String(leaflet.calls.tiles[0].options.attribution)
    expect(attribution).toContain('Seznam.cz')
    expect(attribution).toContain('mapy.com/copyright')
  })

  it('renders a bottom-left clickable logo control linking to mapy.com', () => {
    renderMap()
    expect(leaflet.calls.controlPositions).toContain('bottomleft')
    const logo = leaflet.calls.logoElement
    expect(logo).not.toBeNull()
    expect(logo?.tagName).toBe('A')
    expect(logo?.getAttribute('href')).toBe('https://mapy.com')
    const img = logo?.querySelector('img')
    expect(img?.getAttribute('src')).toContain('mapy.com')
    expect(img?.getAttribute('alt')).toBe('mapy.com')
  })
})

describe('LeafletMap markers and clustering', () => {
  it('adds one clustered marker per feature and fits the bounds', () => {
    renderMap()
    expect(leaflet.calls.markers).toHaveLength(2)
    expect(leaflet.calls.clusterAdded).toHaveLength(2)
    // Coordinates are passed to Leaflet as [lat, lng] (GeoJSON is [lng, lat]).
    expect(leaflet.calls.markers[0].latlng).toEqual([50.08, 14.42])
    expect(leaflet.calls.fitBounds[0]).toEqual([
      [50.08, 14.42],
      [49.19, 16.61],
    ])
  })

  it('builds a popup whose thumbnail links to the photo detail', () => {
    const onSelect = vi.fn()
    render(
      <LeafletMap
        features={FEATURES}
        mapset="basic"
        viewport={null}
        onViewportChange={vi.fn()}
        onSelectPhoto={onSelect}
        thumbAlt="Photo on the map"
      />,
    )
    // The popup content is a lazy function; invoke it to get the element.
    const popupFn = leaflet.calls.markers[0].popup as () => HTMLElement
    const el = popupFn()
    const link = el.tagName === 'A' ? (el as HTMLAnchorElement) : el.querySelector('a')
    expect(link?.getAttribute('href')).toBe('/photos/ph1')
    expect(el.querySelector('img')?.getAttribute('src')).toBe('/api/v1/photos/ph1/thumb/tile_224')

    // A plain click navigates within the SPA instead of following the href.
    link?.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true, button: 0 }))
    expect(onSelect).toHaveBeenCalledWith('ph1')
  })
})

describe('buildPopupElement', () => {
  it('falls back to the alt text when the photo has no title', () => {
    const el = buildPopupElement(FEATURES[1], vi.fn(), 'fallback alt')
    expect(el.querySelector('img')?.getAttribute('alt')).toBe('fallback alt')
  })
})

/**
 * Touch gesture handling: a tall map inside a scrolling page must not eat a
 * one-finger swipe, or the user is stuck on it and cannot reach what is below.
 * The wiring is what these assert — the gesture logic itself lives in
 * `lib/mapGestures` and is tested there.
 */
describe('LeafletMap touch gestures', () => {
  const originalMatchMedia = window.matchMedia

  afterEach(() => {
    // Reassigning `window.matchMedia` outlives `restoreMocks`, so put the shared
    // desktop stub back for the rest of the suite.
    window.matchMedia = originalMatchMedia
  })

  /** Points `window.matchMedia` at a fixed coarse/fine answer. */
  function pinPointer(coarse: boolean): void {
    window.matchMedia = vi.fn().mockImplementation((query: string) => ({
      matches: coarse && query.includes('pointer: coarse'),
      media: query,
      onchange: null,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      addListener: vi.fn(),
      removeListener: vi.fn(),
      dispatchEvent: vi.fn(),
    }))
  }

  it('keeps drag-to-pan on a fine pointer, so the mouse behaves as before', () => {
    pinPointer(false)
    renderMap()
    expect(leaflet.calls.mapOptions[0].dragging).toBe(true)
  })

  it('starts undraggable on a coarse pointer, leaving pinch-zoom on', () => {
    pinPointer(true)
    renderMap()
    // Without the drag handler Leaflet's own stylesheet leaves the container at
    // `touch-action: pan-x pan-y`, i.e. the page — not the map — takes the swipe.
    expect(leaflet.calls.mapOptions[0].dragging).toBe(false)
    expect(leaflet.calls.mapOptions[0].touchZoom).toBe(true)
  })

  it('shows the two-finger hint once a one-finger drag scrolls the page instead', () => {
    pinPointer(true)
    const { container } = render(
      <LeafletMap
        features={FEATURES}
        mapset="basic"
        viewport={null}
        onViewportChange={vi.fn()}
        onSelectPhoto={vi.fn()}
        thumbAlt="Photo on the map"
        twoFingerHint="Use two fingers to move the map"
      />,
    )
    const surface = container.querySelector('.kukatko-map')
    if (surface === null) {
      throw new Error('the map surface was not rendered')
    }
    const hint = surface.querySelector('.kukatko-map-gesture-hint')
    if (hint === null) {
      throw new Error('the gesture hint was not attached')
    }
    // Nothing is said until the user actually tries to pan.
    expect(hint.classList.contains('is-visible')).toBe(false)
    // Decorative: it answers a gesture no screen-reader user makes.
    expect(hint.getAttribute('aria-hidden')).toBe('true')

    fireTouch(surface, 'touchstart', [{ clientX: 100, clientY: 100 }])
    fireTouch(surface, 'touchmove', [{ clientX: 100, clientY: 200 }])

    expect(hint.classList.contains('is-visible')).toBe(true)
    // The pill renders the label through `content: attr(data-label)`.
    expect(hint.getAttribute('data-label')).toBe('Use two fingers to move the map')
  })

  it('adds no hint element on a fine pointer', () => {
    pinPointer(false)
    const { container } = render(
      <LeafletMap
        features={FEATURES}
        mapset="basic"
        viewport={null}
        onViewportChange={vi.fn()}
        onSelectPhoto={vi.fn()}
        thumbAlt="Photo on the map"
        twoFingerHint="Use two fingers to move the map"
      />,
    )
    expect(container.querySelector('.kukatko-map-gesture-hint')).toBeNull()
  })
})

describe('LeafletMap sizing', () => {
  it('asks for its height in dynamic viewport units', () => {
    // `dvh` is the space actually left under a phone browser's collapsing
    // chrome; jsdom drops the unit, so read the prop's default rather than the
    // computed style.
    expect(DEFAULT_MAP_HEIGHT).toBe('70dvh')
  })
})
