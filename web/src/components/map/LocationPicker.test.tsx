import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { I18nextProvider } from 'react-i18next'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'

// A fake Leaflet recording the calls the picker makes through LeafletMap, so the
// wiring — where the map opens, the draggable pin, the click that drops it — can
// be asserted without a real map (jsdom has no layout).
const leaflet = vi.hoisted(() => {
  interface RecordedMarker {
    latlng: [number, number]
    options: Record<string, unknown>
    handlers: Record<string, (() => void) | undefined>
    setLatLng: ReturnType<typeof vi.fn>
    getLatLng: () => { lat: number; lng: number }
  }
  const calls = {
    /** Options every created map was opened with (centre + zoom). */
    mapOptions: [] as Record<string, unknown>[],
    /** Handlers subscribed on the map, by event name. */
    mapHandlers: {} as Record<string, ((event: unknown) => void) | undefined>,
    markers: [] as RecordedMarker[],
    removedLayers: 0,
  }

  const map = {
    on: vi.fn((event: string, handler: (event: unknown) => void) => {
      calls.mapHandlers[event] = handler
    }),
    remove: vi.fn(),
    addLayer: vi.fn(),
    removeLayer: vi.fn(() => {
      calls.removedLayers += 1
    }),
    getCenter: () => ({ lat: 49.8, lng: 15.5 }),
    getZoom: () => 7,
    fitBounds: vi.fn(),
    panTo: vi.fn(),
    dragging: { enable: vi.fn(), disable: vi.fn() },
  }

  class Control {
    options: { position?: string }
    onAdd: ((map: unknown) => HTMLElement) | undefined
    constructor(options: { position?: string } = {}) {
      this.options = options
      this.onAdd = undefined
    }
    addTo(m: unknown): this {
      this.onAdd?.(m)
      return this
    }
  }

  const L = {
    map: vi.fn((_container: HTMLElement, options: Record<string, unknown>) => {
      calls.mapOptions.push(options)
      return map
    }),
    tileLayer: vi.fn(() => {
      const layer = { on: vi.fn(() => layer), addTo: vi.fn(() => layer), setUrl: vi.fn() }
      return layer
    }),
    markerClusterGroup: vi.fn(() => ({
      addLayer: vi.fn(),
      clearLayers: vi.fn(),
      on: vi.fn(),
    })),
    marker: vi.fn((latlng: [number, number], options: Record<string, unknown>) => {
      const marker: RecordedMarker & {
        on: (event: string, handler: () => void) => unknown
        addTo: (m: unknown) => unknown
      } = {
        latlng,
        options,
        handlers: {},
        setLatLng: vi.fn((next: [number, number]) => {
          marker.latlng = next
        }),
        getLatLng: () => ({ lat: marker.latlng[0], lng: marker.latlng[1] }),
        on: vi.fn((event: string, handler: () => void) => {
          marker.handlers[event] = handler
          return marker
        }),
        addTo: vi.fn(() => marker),
      }
      calls.markers.push(marker)
      return marker
    }),
    divIcon: vi.fn(() => ({ icon: true })),
    Control,
    DomEvent: { disableClickPropagation: vi.fn() },
  }

  return { calls, map, L }
})

vi.mock('leaflet', () => ({ default: leaflet.L }))
vi.mock('leaflet.markercluster', () => ({}))

const { LocationPicker } = await import('./LocationPicker')

/** Brno's main square, the coordinate the fixtures are picked around. */
const BRNO = { lat: 49.19522, lng: 16.60796 }

/**
 * A controlled host for the picker, mirroring how the metadata form uses it: the
 * coordinate text is the caller's state and every way in writes it.
 */
function Host({ initial = '' }: { initial?: string }) {
  const [value, setValue] = useState(initial)
  return (
    <I18nextProvider i18n={i18n}>
      <LocationPicker value={value} onChange={setValue} />
      <output data-testid="value">{value}</output>
    </I18nextProvider>
  )
}

beforeEach(() => {
  window.sessionStorage.clear()
})

afterEach(() => {
  vi.clearAllMocks()
  leaflet.calls.mapOptions.length = 0
  leaflet.calls.markers.length = 0
  leaflet.calls.removedLayers = 0
  for (const key of Object.keys(leaflet.calls.mapHandlers)) {
    // eslint-disable-next-line @typescript-eslint/no-dynamic-delete
    delete leaflet.calls.mapHandlers[key]
  }
})

describe('LocationPicker ways in', () => {
  it('offers all three ways to set a location', () => {
    render(<Host />)
    expect(screen.getByLabelText('Najít místo podle názvu')).toBeInTheDocument()
    expect(screen.getByLabelText('Souřadnice')).toBeInTheDocument()
    expect(leaflet.L.map).toHaveBeenCalledTimes(1)
  })

  it('writes a canonical coordinate when the map is clicked', async () => {
    render(<Host />)
    leaflet.calls.mapHandlers.click?.({ latlng: BRNO })
    await waitFor(() => {
      expect(screen.getByTestId('value')).toHaveTextContent('49.195220, 16.607960')
    })
  })

  it('drops a draggable pin at the typed coordinate', async () => {
    const user = userEvent.setup()
    render(<Host />)
    // Typed a character at a time, so the pin appears, follows and disappears as
    // the half-written text goes in and out of being a coordinate; what matters
    // is where it ends up.
    await user.type(screen.getByLabelText('Souřadnice'), '49.19522, 16.60796')
    await waitFor(() => {
      expect(leaflet.calls.markers.at(-1)?.latlng).toEqual([BRNO.lat, BRNO.lng])
    })
    expect(leaflet.calls.markers.at(-1)?.options.draggable).toBe(true)
  })

  it('takes the coordinate from a dragged pin', async () => {
    render(<Host initial="49.19522, 16.60796" />)
    await waitFor(() => {
      expect(leaflet.calls.markers).toHaveLength(1)
    })
    const marker = leaflet.calls.markers[0]
    marker.latlng = [50.08804, 14.42076]
    marker.handlers.dragend?.()
    await waitFor(() => {
      expect(screen.getByTestId('value')).toHaveTextContent('50.088040, 14.420760')
    })
  })

  it('reports an unrecognised coordinate and shows no pin', async () => {
    const user = userEvent.setup()
    render(<Host />)
    await user.type(screen.getByLabelText('Souřadnice'), 'někde u lesa')
    expect(await screen.findByText(/Nerozpoznané souřadnice/)).toBeInTheDocument()
    expect(leaflet.calls.markers).toHaveLength(0)
  })

  it('removes the location on demand', async () => {
    const user = userEvent.setup()
    render(<Host initial="49.19522, 16.60796" />)
    await user.click(screen.getAllByRole('button', { name: 'Vymazat polohu' })[0])
    expect(screen.getByTestId('value')).toHaveTextContent('')
  })
})

describe('LocationPicker starting viewport', () => {
  it('opens on the photo location when it has one', () => {
    render(<Host initial="49.19522, 16.60796" />)
    expect(leaflet.calls.mapOptions[0].center).toEqual([BRNO.lat, BRNO.lng])
    expect(leaflet.calls.mapOptions[0].zoom).toBe(13)
  })

  it('opens near the place picked last, not in the ocean', async () => {
    // A first photo geotagged in this session…
    const first = render(<Host />)
    leaflet.calls.mapHandlers.click?.({ latlng: BRNO })
    await waitFor(() => {
      expect(screen.getByTestId('value')).toHaveTextContent('49.195220')
    })
    first.unmount()
    leaflet.calls.mapOptions.length = 0

    // …and the next one with no location of its own starts where that one was.
    render(<Host />)
    expect(leaflet.calls.mapOptions[0].center).toEqual([BRNO.lat, BRNO.lng])
    expect(leaflet.calls.mapOptions[0].zoom).toBe(11)
  })

  it('falls back to the library region for the first photo of a session', () => {
    render(<Host />)
    // LeafletMap's own default view — the Czech lands, never 0,0.
    expect(leaflet.calls.mapOptions[0].center).toEqual([49.8, 15.5])
  })
})

describe('LocationPicker full screen', () => {
  it('moves the one map into a dialog and back', async () => {
    const user = userEvent.setup()
    render(<Host initial="49.19522, 16.60796" />)
    expect(leaflet.L.map).toHaveBeenCalledTimes(1)

    await user.click(screen.getByRole('button', { name: /Mapa na celou obrazovku/ }))
    const dialog = await screen.findByRole('dialog')
    // A second map, not two at once: the inline one is unmounted while the
    // dialog holds the map, so Leaflet never owns two containers for one pin.
    expect(leaflet.L.map).toHaveBeenCalledTimes(2)
    expect(leaflet.map.remove).toHaveBeenCalledTimes(1)
    // The dialog reads the picked coordinate back: the field is behind it.
    expect(within(dialog).getByText('49.19522, 16.60796')).toBeInTheDocument()

    await user.click(within(dialog).getByRole('button', { name: 'Hotovo' }))
    await waitFor(() => {
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    })
  })

  it('picks on the full-screen map', async () => {
    const user = userEvent.setup()
    render(<Host />)
    await user.click(screen.getByRole('button', { name: /Mapa na celou obrazovku/ }))
    await screen.findByRole('dialog')

    leaflet.calls.mapHandlers.click?.({ latlng: BRNO })
    await waitFor(() => {
      expect(screen.getByTestId('value')).toHaveTextContent('49.195220, 16.607960')
    })
  })
})
