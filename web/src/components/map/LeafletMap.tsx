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
}: LeafletMapProps) {
  const containerRef = useRef<HTMLDivElement | null>(null)
  const mapRef = useRef<L.Map | null>(null)
  const tileLayerRef = useRef<L.TileLayer | null>(null)
  const clusterRef = useRef<L.MarkerClusterGroup | null>(null)
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
      const marker = L.marker([lat, lng], { icon: markerIcon })
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

  return (
    <div ref={containerRef} className="kukatko-map" style={{ height: '70vh', width: '100%' }} />
  )
}
