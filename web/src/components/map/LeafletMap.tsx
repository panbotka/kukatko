import L from 'leaflet'
import { useEffect, useRef } from 'react'

import 'leaflet/dist/leaflet.css'
import 'leaflet.markercluster'
import 'leaflet.markercluster/dist/MarkerCluster.css'
import 'leaflet.markercluster/dist/MarkerCluster.Default.css'

import { type GeoBounds } from '../../lib/mapArea'
import { enableTwoFingerPan, prefersTouchGestures } from '../../lib/mapGestures'
import { createPhotoLayer, type PhotoLayer, toGeoBounds } from '../../lib/mapPhotoLayer'
import { buildPopupElement } from '../../lib/mapPopup'
import { mapTileThumbSize, type TileSummary } from '../../lib/mapTile'
import { type MapViewport } from '../../lib/mapView'
import { type MapFeature, type Mapset, mapsetNeedsDimming, tileLayerUrl } from '../../services/map'

/**
 * mapy.com requires both an attribution link to the Seznam copyright page and a
 * clickable logo over the map. This is the attribution markup handed to
 * Leaflet's attribution control; it must always be present.
 */
const MAPY_ATTRIBUTION =
  '<a href="https://mapy.com/copyright" target="_blank" rel="noreferrer noopener">© Seznam.cz a.s. a&nbsp;další</a>'

/** The mapy.com logo served by their public API, linked from the logo control. */
const MAPY_LOGO_SRC = 'https://api.mapy.com/img/api/logo.svg'

/** Where the mapy.com logo links to (the licensing requirement). */
const MAPY_LOGO_HREF = 'https://mapy.com'

/** Fallback map centre/zoom when no viewport is given and there are no markers. */
const DEFAULT_CENTER: [number, number] = [49.8, 15.5]
const DEFAULT_ZOOM = 7
const MAX_ZOOM = 20
/**
 * The deepest zoom mapy.com draws for every mapset: `basic` and `outdoor` stop
 * at 19 (z20 answers 404, measured over the proxy), so beyond it Leaflet scales
 * the z19 tiles up rather than showing an empty well — which is exactly where a
 * cluster of photos taken on one spot is finally opened as a grid.
 */
const MAX_NATIVE_ZOOM = 19

/**
 * Default map height. `dvh` — the *dynamic* viewport height — is the share of
 * the space actually left under a mobile browser's collapsing chrome, where
 * `vh` measures a viewport the user never fully sees. `.kukatko-map` carries a
 * `vh` fallback for engines that reject the unit; exported so a test can hold
 * the two in step.
 */
export const DEFAULT_MAP_HEIGHT = '70dvh'

/** How long the two-finger hint stays up after a one-finger drag (ms). */
const GESTURE_HINT_MS = 2000

/** A small CSS-only marker pin, avoiding bundler issues with Leaflet's default image icon. */
const markerIcon = L.divIcon({
  className: 'kukatko-map-pin',
  html: '<span style="display:block;width:14px;height:14px;border-radius:50%;background:var(--kk-accent);border:2px solid var(--kk-on-photo);box-shadow:0 0 2px color-mix(in srgb,var(--kk-shade) 60%,transparent)"></span>',
  iconSize: [18, 18],
  iconAnchor: [9, 9],
})

/**
 * The pin for a photo whose location is an estimate rather than a measurement.
 *
 * It is deliberately a different SHAPE, not just a different colour: hollow and
 * dashed rather than solid. Estimated pins belong on the map, but one that looks
 * identical to a measured pin makes the map claim a precision it does not have,
 * and colour alone would not survive a colour-blind glance or a greyscale print.
 * The title carries the same fact in words for a screen reader.
 */
const estimatedMarkerIcon = L.divIcon({
  className: 'kukatko-map-pin kukatko-map-pin-estimated',
  html:
    '<span style="display:block;width:14px;height:14px;border-radius:50%;' +
    'background:transparent;border:2px dashed var(--kk-accent);' +
    'box-shadow:0 0 2px color-mix(in srgb,var(--kk-shade) 60%,transparent)"></span>',
  iconSize: [18, 18],
  iconAnchor: [9, 9],
})

/**
 * A larger, high-contrast pin for the location picker's draggable marker: big
 * enough to be an easy touch drag target on a phone.
 */
const pickerIcon = L.divIcon({
  className: 'kukatko-map-picker-pin',
  html: '<span style="display:block;width:24px;height:24px;border-radius:50%;background:var(--kk-danger);border:3px solid var(--kk-on-photo);box-shadow:0 0 4px color-mix(in srgb,var(--kk-shade) 70%,transparent)"></span>',
  iconSize: [30, 30],
  iconAnchor: [15, 15],
})

/**
 * Turns the map into an interactive location picker: a single draggable marker
 * at `position` (or none when `null`). Clicking the map or dragging the marker
 * calls `onPick` with the new coordinate; the parent moves `position` in
 * response, so the picker is fully controlled.
 */
export interface LeafletPicker {
  /** Current picked coordinate, or `null` when no location is set. */
  position: { lat: number; lng: number } | null
  /** Called when the user clicks the map or drops the marker. */
  onPick: (lat: number, lng: number) => void
}

/**
 * Turns the plotted features into **photo tiles** (the map page): clusters show
 * the thumbnail of their newest photo with a count badge, single photos the same
 * tile without one (see `lib/mapPhotoLayer`). Omitted, features are plain pins
 * with a popup (the detail page's mini-map). Fixed for the map's lifetime; the
 * callbacks may change.
 */
export interface LeafletPhotoTiles {
  /** A single photo's tile was activated. */
  onOpenPhoto: (uid: string) => void
  /** A cluster no zoom can split was activated: open its photos as a grid. */
  onOpenCluster: (bounds: GeoBounds) => void
  /** The accessible name of a tile (see `lib/mapTile`'s `tileLabel`). */
  describe: (summary: TileSummary, estimated: boolean) => string
}

/** Room left around fitted markers (px), so no tile sits on the map's edge. */
const FIT_PADDING_PX = 48

/** Fit padding keeping `bottomInset` pixels of the map's bottom edge clear too. */
function fitPadding(bottomInset: number): L.FitBoundsOptions {
  return {
    paddingTopLeft: [FIT_PADDING_PX, FIT_PADDING_PX],
    paddingBottomRight: [FIT_PADDING_PX, FIT_PADDING_PX + bottomInset],
  }
}

/** Props for {@link LeafletMap}. */
export interface LeafletMapProps {
  /** Geotagged photos to plot as clustered markers. */
  features: MapFeature[]
  /** Active tile mapset (selects the proxied tile layer). */
  mapset: Mapset
  /** Initial viewport from the URL, or `null` to fit the markers on first load. */
  viewport: MapViewport | null
  /** Called when the user pans/zooms, so the URL can track the viewport. */
  onViewportChange: (viewport: MapViewport) => void
  /** Called when a marker's popup link is clicked, with the photo UID (pin mode). */
  onSelectPhoto?: (uid: string) => void
  /** Photo-tile mode; see {@link LeafletPhotoTiles}. */
  photoTiles?: LeafletPhotoTiles
  /** The photo whose tile (or the cluster holding it) is marked; photo-tile mode. */
  highlightUid?: string | null
  /**
   * Called with the visible bounds whenever the view settles (after a pan, a
   * zoom, a resize and once at start), so the page can list the photos in
   * the area.
   */
  onBoundsChange?: (bounds: GeoBounds) => void
  /**
   * A box to fit on load when no `viewport` is given (the viewer's Back link
   * knows the area a photo came from, not its zoom). Without either, the map
   * fits all its markers once they arrive.
   */
  initialBounds?: GeoBounds | null
  /** Whether zooms and cluster splits animate (off for reduced motion). */
  animate?: boolean
  /**
   * Pixels along the map's bottom edge that something floats over (the phone's
   * peeking sheet), kept clear when the map fits its markers or an area.
   */
  bottomInset?: number
  /** Alt text for a thumbnail without its own title. */
  thumbAlt: string
  /**
   * Tooltip for a pin whose position is an estimate, naming it as one in words.
   * The dashed pin says it at a glance; this says it to a screen reader and on
   * hover. Optional: a map with no estimated pins never needs it.
   */
  estimatedTitle?: string
  /** CSS height of the map container. Defaults to `70dvh`; a detail mini-map
   * passes a smaller fixed height. */
  height?: string
  /**
   * Text of the "use two fingers to move the map" hint, shown over the map when
   * a one-finger swipe scrolls the page instead of panning (touch devices only,
   * see `lib/mapGestures`). Optional: without it the gesture handling still
   * works, it just says nothing — but then nothing tells the user how to pan.
   */
  twoFingerHint?: string
  /**
   * Whether the map fills the viewport on a page that never scrolls (the map
   * page on a phone or tablet). The two-finger convention exists so a map does
   * not trap a swipe meant for the page around it; with no page to scroll there
   * is nothing to trap, and a one-finger swipe that only flashed the hint would
   * be a map that cannot be panned the obvious way. So a filling map keeps
   * Leaflet's own one-finger drag on every pointer. May change while the map is
   * mounted (a rotation across the breakpoint); the gesture handling follows.
   */
  fillsViewport?: boolean
  /**
   * Called with the URL of a tile the layer failed to load. An `<img>` never
   * exposes its response status to JavaScript, so the parent has to re-request
   * the URL to learn *why* the tiles are missing (see `probeTileFailure`) and can
   * then explain the empty map instead of leaving a silent grey grid. Fires once
   * per failed tile, so the parent must debounce.
   */
  onTileError?: (tileUrl: string) => void
  /**
   * When set, turns the map into an interactive location picker with a draggable
   * marker (used by the metadata editor to geotag a photo). Omitted for the
   * read-only cluster map. Whether picking is enabled is fixed for the map's
   * lifetime; only the position and callback may change.
   */
  picker?: LeafletPicker
}

/**
 * The imperative Leaflet map: a tile layer pointing at the **backend proxy**
 * (so the mapy.com key stays server-side), the mandatory mapy.com attribution
 * and logo controls, and a marker-cluster layer over the photo features.
 * Clicking a cluster zooms in (markercluster's default), clicking a marker opens
 * a popup whose thumbnail links to the photo detail.
 *
 * Leaflet owns the DOM under the container, so this component bridges React
 * props to imperative Leaflet calls via effects: one-time setup on mount, a tile
 * URL swap on mapset change, and a marker rebuild on feature change.
 */
export function LeafletMap({
  features,
  mapset,
  viewport,
  onViewportChange,
  onSelectPhoto,
  photoTiles,
  highlightUid = null,
  onBoundsChange,
  initialBounds = null,
  animate = true,
  bottomInset = 0,
  thumbAlt,
  estimatedTitle,
  height = DEFAULT_MAP_HEIGHT,
  picker,
  onTileError,
  twoFingerHint,
  fillsViewport = false,
}: LeafletMapProps) {
  const containerRef = useRef<HTMLDivElement | null>(null)
  const mapRef = useRef<L.Map | null>(null)
  const tileLayerRef = useRef<L.TileLayer | null>(null)
  const clusterRef = useRef<L.MarkerClusterGroup | null>(null)
  // The photo-tile layer, in photo-tile mode (replaces the pin cluster).
  const photoLayerRef = useRef<PhotoLayer | null>(null)
  // The picker's single draggable marker, created lazily once a position exists.
  const pickerMarkerRef = useRef<L.Marker | null>(null)
  // When true, the next position change came from a click/drag (already where the
  // user wants it), so the map should not re-pan; a change from parsed text does.
  const skipPanRef = useRef(false)
  // Whether the map has fitted its bounds to the markers yet (only auto-fit once,
  // and never when an explicit viewport was supplied).
  const didFitRef = useRef(false)

  // Latest callbacks/values referenced by long-lived Leaflet handlers and the
  // one-time setup effect, kept in refs so neither re-subscribes on every render.
  const onViewportChangeRef = useRef(onViewportChange)
  onViewportChangeRef.current = onViewportChange
  const onSelectRef = useRef(onSelectPhoto)
  onSelectRef.current = onSelectPhoto
  const photoTilesRef = useRef(photoTiles)
  photoTilesRef.current = photoTiles
  const onBoundsChangeRef = useRef(onBoundsChange)
  onBoundsChangeRef.current = onBoundsChange
  const initialBoundsRef = useRef(initialBounds)
  const animateRef = useRef(animate)
  const bottomInsetRef = useRef(bottomInset)
  bottomInsetRef.current = bottomInset
  const thumbAltRef = useRef(thumbAlt)
  thumbAltRef.current = thumbAlt
  const estimatedTitleRef = useRef(estimatedTitle)
  estimatedTitleRef.current = estimatedTitle
  const onPickRef = useRef(picker?.onPick)
  onPickRef.current = picker?.onPick
  const onTileErrorRef = useRef(onTileError)
  onTileErrorRef.current = onTileError
  const twoFingerHintRef = useRef(twoFingerHint)
  twoFingerHintRef.current = twoFingerHint
  // Whether this map instance is a picker is fixed at mount (a page renders it in
  // one mode or the other), captured so the one-time setup effect can read it.
  const pickerEnabledRef = useRef(picker !== undefined)
  // Captured once: the initial viewport and mapset used to build the map.
  const initialViewportRef = useRef(viewport)
  const initialMapsetRef = useRef(mapset)
  // Captured for the one-time setup effect, so the map is built with the drag
  // handler the gesture effect below would otherwise switch on a frame later.
  const fillsViewportRef = useRef(fillsViewport)
  fillsViewportRef.current = fillsViewport

  // One-time setup: create the map, the proxied tile layer, the required mapy.com
  // controls and the cluster layer. Cleans up fully on unmount.
  useEffect(() => {
    const container = containerRef.current
    if (container === null) {
      return
    }

    const initial = initialViewportRef.current
    // Whether this map starts out handing one-finger swipes to the page
    // scroller; the gesture effect below owns the decision from here on.
    const gestureHandling = !fillsViewportRef.current && prefersTouchGestures()
    const map = L.map(container, {
      center: initial !== null ? [initial.lat, initial.lng] : DEFAULT_CENTER,
      zoom: initial !== null ? initial.zoom : DEFAULT_ZOOM,
      maxZoom: MAX_ZOOM,
      // On a touch device the map does not claim a one-finger swipe: without the
      // drag handler Leaflet's own stylesheet leaves the container at
      // `touch-action: pan-x pan-y`, so the swipe scrolls the PAGE and the user
      // is never trapped on a tall map. Two fingers still pan and zoom (see
      // `lib/mapGestures`). A mouse keeps drag-to-pan exactly as before.
      dragging: !gestureHandling,
      touchZoom: true,
      zoomAnimation: animateRef.current,
      markerZoomAnimation: animateRef.current,
    })
    mapRef.current = map
    // An explicit viewport means the user chose this position; do not override it
    // by fitting to the markers later.
    didFitRef.current = initial !== null
    // The viewer's Back link brings an area instead of a centre/zoom: fit it,
    // and let it stand in for the markers fit as well.
    const area = initialBoundsRef.current
    if (initial === null && area !== null) {
      map.fitBounds(
        [
          [area.south, area.west],
          [area.north, area.east],
        ],
        { animate: false, ...fitPadding(bottomInsetRef.current) },
      )
      didFitRef.current = true
    }

    const tileLayer = L.tileLayer(tileLayerUrl(initialMapsetRef.current), {
      attribution: MAPY_ATTRIBUTION,
      detectRetina: true,
      maxZoom: MAX_ZOOM,
      // `detectRetina` asks a retina screen's tiles one zoom deeper than the
      // grid's, so its deepest grid zoom is one shallower (Leaflet's own
      // retina test is this same device-pixel ratio check).
      maxNativeZoom: window.devicePixelRatio > 1 ? MAX_NATIVE_ZOOM - 1 : MAX_NATIVE_ZOOM,
    })
    // A tile that fails to load leaves nothing but a grey square, and the <img>
    // keeps its response status to itself — so hand the URL to the parent, which
    // can ask the proxy what went wrong (a rejected mapy.com key, most likely).
    tileLayer.on('tileerror', (event: L.TileErrorEvent) => {
      const { tile } = event
      if (tile instanceof HTMLImageElement && tile.src !== '') {
        onTileErrorRef.current?.(tile.src)
      }
    })
    tileLayer.addTo(map)
    tileLayerRef.current = tileLayer

    // The mandatory, always-present clickable mapy.com logo (bottom-left).
    const logoControl = new L.Control({ position: 'bottomleft' })
    logoControl.onAdd = () => {
      const anchor = document.createElement('a')
      anchor.href = MAPY_LOGO_HREF
      anchor.target = '_blank'
      anchor.rel = 'noreferrer noopener'
      anchor.className = 'kukatko-map-logo'
      const img = document.createElement('img')
      img.src = MAPY_LOGO_SRC
      img.alt = 'mapy.com'
      img.style.height = '18px'
      img.style.display = 'block'
      anchor.appendChild(img)
      // Stop map drag/click from starting on the logo.
      L.DomEvent.disableClickPropagation(anchor)
      return anchor
    }
    logoControl.addTo(map)

    if (photoTilesRef.current !== undefined) {
      const thumbSize = mapTileThumbSize(window.devicePixelRatio)
      photoLayerRef.current = createPhotoLayer(
        map,
        () => {
          const tiles = photoTilesRef.current
          return {
            onOpenPhoto: (uid) => tiles?.onOpenPhoto(uid),
            onOpenCluster: (bounds) => tiles?.onOpenCluster(bounds),
            describe: (summary, estimated) => tiles?.describe(summary, estimated) ?? '',
            thumbSize,
          }
        },
        { animate: animateRef.current },
      )
    } else {
      const cluster = L.markerClusterGroup({ chunkedLoading: true })
      map.addLayer(cluster)
      clusterRef.current = cluster
    }

    const reportBounds = () => {
      onBoundsChangeRef.current?.(toGeoBounds(map.getBounds()))
    }
    map.on('moveend', () => {
      const center = map.getCenter()
      onViewportChangeRef.current({ lat: center.lat, lng: center.lng, zoom: map.getZoom() })
      reportBounds()
    })
    // The view the map was built with never fired a moveend this listener saw.
    reportBounds()

    // In picker mode a click on the map places the marker at that point.
    if (pickerEnabledRef.current) {
      map.on('click', (event: L.LeafletMouseEvent) => {
        skipPanRef.current = true
        onPickRef.current?.(event.latlng.lat, event.latlng.lng)
      })
    }

    // Leaflet measures its container once, at creation, and again only on a
    // window resize — but this container also changes size on its own: the map
    // page flexes it into whatever the header row leaves, a tile warning above
    // it takes a slice away, and a phone's collapsing URL bar moves the dynamic
    // viewport it is sized from. Re-measure whenever it does, or the tiles and
    // the pan bounds keep the stale size (a grey band, a centre that is not).
    const resizeObserver =
      typeof ResizeObserver === 'function'
        ? new ResizeObserver(() => {
            map.invalidateSize()
          })
        : null
    resizeObserver?.observe(container)

    return () => {
      resizeObserver?.disconnect()
      photoLayerRef.current?.destroy()
      map.remove()
      mapRef.current = null
      tileLayerRef.current = null
      clusterRef.current = null
      photoLayerRef.current = null
    }
  }, [])

  // Touch only, and only on a map with a page around it: two-finger gesture
  // handling, plus the hint that says so. Its own effect (not part of the
  // one-time setup) so a filling map can hand the one-finger drag back and forth
  // as a rotation crosses the breakpoint. The pointer is asked here rather than
  // during render, once per change of `fillsViewport`. The hint is plain DOM
  // rather than React state so a gesture never re-renders the map; it sits
  // inside the (positioned) Leaflet container and is inert to hit-testing, so
  // it can never swallow a tap.
  useEffect(() => {
    const map = mapRef.current
    const container = containerRef.current
    if (map === null || container === null) {
      return
    }
    if (fillsViewport || !prefersTouchGestures()) {
      map.dragging.enable()
      return
    }
    map.dragging.disable()

    const hint = document.createElement('div')
    hint.className = 'kukatko-map-gesture-hint'
    // Decorative: the hint answers a gesture nobody makes with a keyboard or a
    // screen reader, and it would otherwise be announced out of nowhere.
    hint.setAttribute('aria-hidden', 'true')
    container.appendChild(hint)
    let hintTimer: number | undefined
    const detachGestures = enableTwoFingerPan(map, container, () => {
      const text = twoFingerHintRef.current
      if (text === undefined || text === '') {
        return
      }
      // The pill draws its label from this attribute (`content: attr(...)`), so
      // an empty hint leaves nothing visible at all.
      hint.dataset.label = text
      hint.classList.add('is-visible')
      window.clearTimeout(hintTimer)
      hintTimer = window.setTimeout(() => {
        hint.classList.remove('is-visible')
      }, GESTURE_HINT_MS)
    })

    return () => {
      window.clearTimeout(hintTimer)
      detachGestures()
      hint.remove()
    }
  }, [fillsViewport])

  // Swap the tile layer's URL when the mapset changes (no map rebuild needed).
  useEffect(() => {
    tileLayerRef.current?.setUrl(tileLayerUrl(mapset))
  }, [mapset])

  // Tone the drawn mapsets down to match the dark page (the filter itself is in
  // `styles/app.css`, scoped to the tile pane so pins, clusters and popups keep
  // their colours). The class is toggled imperatively rather than rendered into
  // `className`: Leaflet stamps its own classes onto this same element, and a
  // React-owned className rewrites the whole attribute when it changes, which
  // would take `leaflet-container` with it.
  const dimTiles = mapsetNeedsDimming(mapset)
  useEffect(() => {
    containerRef.current?.classList.toggle('kukatko-map--dim-tiles', dimTiles)
  }, [dimTiles])

  // Rebuild the clustered markers whenever the features change.
  useEffect(() => {
    const map = mapRef.current
    const photoLayer = photoLayerRef.current
    if (map !== null && photoLayer !== null) {
      const bounds = photoLayer.setFeatures(features)
      if (!didFitRef.current && bounds !== null) {
        map.fitBounds(bounds, {
          ...fitPadding(bottomInsetRef.current),
          maxZoom: 16,
          animate: false,
        })
        didFitRef.current = true
      }
      return
    }
    const cluster = clusterRef.current
    if (cluster === null || map === null) {
      return
    }
    cluster.clearLayers()

    const points: [number, number][] = []
    for (const feature of features) {
      const [lng, lat] = feature.geometry.coordinates
      const estimated = feature.properties.location_estimated === true
      const marker = L.marker([lat, lng], {
        icon: estimated ? estimatedMarkerIcon : markerIcon,
        title: estimated ? estimatedTitleRef.current : undefined,
      })
      // Lazy popup content: built only when the marker is opened.
      marker.bindPopup(() =>
        buildPopupElement(
          feature,
          (uid) => {
            onSelectRef.current?.(uid)
          },
          thumbAltRef.current,
        ),
      )
      cluster.addLayer(marker)
      points.push([lat, lng])
    }

    if (!didFitRef.current && points.length > 0) {
      map.fitBounds(points, { padding: [40, 40], maxZoom: 16 })
      didFitRef.current = true
    }
  }, [features])

  // Photo-tile mode: move the highlight with the prop (the deep-linked photo,
  // or the one under the pointer in the area panel).
  useEffect(() => {
    photoLayerRef.current?.setHighlight(highlightUid)
  }, [highlightUid, features])

  // Picker mode: keep the draggable marker in sync with the controlled position.
  // Depends on the primitive lat/lng so it re-runs whenever the parent moves the
  // marker (via parsed text) or clears it, but not on unrelated re-renders.
  const pickerLat = picker?.position?.lat
  const pickerLng = picker?.position?.lng
  useEffect(() => {
    if (!pickerEnabledRef.current) {
      return
    }
    const map = mapRef.current
    if (map === null) {
      return
    }

    if (pickerLat === undefined || pickerLng === undefined) {
      if (pickerMarkerRef.current !== null) {
        map.removeLayer(pickerMarkerRef.current)
        pickerMarkerRef.current = null
      }
      return
    }

    if (pickerMarkerRef.current === null) {
      const marker = L.marker([pickerLat, pickerLng], {
        icon: pickerIcon,
        draggable: true,
        autoPan: true,
      })
      marker.on('dragend', () => {
        const latlng = marker.getLatLng()
        skipPanRef.current = true
        onPickRef.current?.(latlng.lat, latlng.lng)
      })
      marker.addTo(map)
      pickerMarkerRef.current = marker
    } else {
      pickerMarkerRef.current.setLatLng([pickerLat, pickerLng])
    }

    // Recentre only for programmatic moves (parsed text); a click/drag already
    // sits where the user wants it, so leave the viewport put.
    if (skipPanRef.current) {
      skipPanRef.current = false
    } else {
      map.panTo([pickerLat, pickerLng])
    }
  }, [pickerLat, pickerLng])

  return <div ref={containerRef} className="kukatko-map" style={{ height, width: '100%' }} />
}
