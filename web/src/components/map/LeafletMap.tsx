import L from 'leaflet'
import { useEffect, useRef } from 'react'

import 'leaflet/dist/leaflet.css'
import 'leaflet.markercluster'
import 'leaflet.markercluster/dist/MarkerCluster.css'
import 'leaflet.markercluster/dist/MarkerCluster.Default.css'

import { buildPopupElement } from '../../lib/mapPopup'
import { type MapViewport } from '../../lib/mapView'
import { type MapFeature, type Mapset, tileLayerUrl } from '../../services/map'

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

/** A small CSS-only marker pin, avoiding bundler issues with Leaflet's default image icon. */
const markerIcon = L.divIcon({
  className: 'kukatko-map-pin',
  html: '<span style="display:block;width:14px;height:14px;border-radius:50%;background:#0d6efd;border:2px solid #fff;box-shadow:0 0 2px rgba(0,0,0,.6)"></span>',
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
    'background:transparent;border:2px dashed #0d6efd;box-shadow:0 0 2px rgba(0,0,0,.6)"></span>',
  iconSize: [18, 18],
  iconAnchor: [9, 9],
})

/**
 * A larger, high-contrast pin for the location picker's draggable marker: big
 * enough to be an easy touch drag target on a phone.
 */
const pickerIcon = L.divIcon({
  className: 'kukatko-map-picker-pin',
  html: '<span style="display:block;width:24px;height:24px;border-radius:50%;background:#dc3545;border:3px solid #fff;box-shadow:0 0 4px rgba(0,0,0,.7)"></span>',
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
  /** Called when a marker's popup link is clicked, with the photo UID. */
  onSelectPhoto: (uid: string) => void
  /** Alt text for a thumbnail without its own title. */
  thumbAlt: string
  /**
   * Tooltip for a pin whose position is an estimate, naming it as one in words.
   * The dashed pin says it at a glance; this says it to a screen reader and on
   * hover. Optional: a map with no estimated pins never needs it.
   */
  estimatedTitle?: string
  /** CSS height of the map container. Defaults to `70vh`; a detail mini-map
   * passes a smaller fixed height. */
  height?: string
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
  thumbAlt,
  estimatedTitle,
  height = '70vh',
  picker,
  onTileError,
}: LeafletMapProps) {
  const containerRef = useRef<HTMLDivElement | null>(null)
  const mapRef = useRef<L.Map | null>(null)
  const tileLayerRef = useRef<L.TileLayer | null>(null)
  const clusterRef = useRef<L.MarkerClusterGroup | null>(null)
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
  const thumbAltRef = useRef(thumbAlt)
  thumbAltRef.current = thumbAlt
  const estimatedTitleRef = useRef(estimatedTitle)
  estimatedTitleRef.current = estimatedTitle
  const onPickRef = useRef(picker?.onPick)
  onPickRef.current = picker?.onPick
  const onTileErrorRef = useRef(onTileError)
  onTileErrorRef.current = onTileError
  // Whether this map instance is a picker is fixed at mount (a page renders it in
  // one mode or the other), captured so the one-time setup effect can read it.
  const pickerEnabledRef = useRef(picker !== undefined)
  // Captured once: the initial viewport and mapset used to build the map.
  const initialViewportRef = useRef(viewport)
  const initialMapsetRef = useRef(mapset)

  // One-time setup: create the map, the proxied tile layer, the required mapy.com
  // controls and the cluster layer. Cleans up fully on unmount.
  useEffect(() => {
    const container = containerRef.current
    if (container === null) {
      return
    }

    const initial = initialViewportRef.current
    const map = L.map(container, {
      center: initial !== null ? [initial.lat, initial.lng] : DEFAULT_CENTER,
      zoom: initial !== null ? initial.zoom : DEFAULT_ZOOM,
      maxZoom: MAX_ZOOM,
    })
    mapRef.current = map
    // An explicit viewport means the user chose this position; do not override it
    // by fitting to the markers later.
    didFitRef.current = initial !== null

    const tileLayer = L.tileLayer(tileLayerUrl(initialMapsetRef.current), {
      attribution: MAPY_ATTRIBUTION,
      detectRetina: true,
      maxZoom: MAX_ZOOM,
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

    const cluster = L.markerClusterGroup({ chunkedLoading: true })
    map.addLayer(cluster)
    clusterRef.current = cluster

    map.on('moveend', () => {
      const center = map.getCenter()
      onViewportChangeRef.current({ lat: center.lat, lng: center.lng, zoom: map.getZoom() })
    })

    // In picker mode a click on the map places the marker at that point.
    if (pickerEnabledRef.current) {
      map.on('click', (event: L.LeafletMouseEvent) => {
        skipPanRef.current = true
        onPickRef.current?.(event.latlng.lat, event.latlng.lng)
      })
    }

    return () => {
      map.remove()
      mapRef.current = null
      tileLayerRef.current = null
      clusterRef.current = null
    }
  }, [])

  // Swap the tile layer's URL when the mapset changes (no map rebuild needed).
  useEffect(() => {
    tileLayerRef.current?.setUrl(tileLayerUrl(mapset))
  }, [mapset])

  // Rebuild the clustered markers whenever the features change.
  useEffect(() => {
    const cluster = clusterRef.current
    const map = mapRef.current
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
      marker.bindPopup(() => buildPopupElement(feature, onSelectRef.current, thumbAltRef.current))
      cluster.addLayer(marker)
      points.push([lat, lng])
    }

    if (!didFitRef.current && points.length > 0) {
      map.fitBounds(points, { padding: [40, 40], maxZoom: 16 })
      didFitRef.current = true
    }
  }, [features])

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
