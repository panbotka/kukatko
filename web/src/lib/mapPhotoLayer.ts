import L from 'leaflet'
import 'leaflet.markercluster'

import { type MapFeature } from '../services/map'

import { type GeoBounds } from './mapArea'
import {
  buildMapTile,
  MAP_CLUSTER_RADIUS,
  MAP_TILE_PX,
  summarizeTile,
  type TileSummary,
} from './mapTile'

/** What the photo layer asks of its owner. Read on every event, so it may change. */
export interface PhotoLayerHandlers {
  /** A single photo's tile was activated (click, Enter or Space). */
  onOpenPhoto: (uid: string) => void
  /**
   * A cluster that cannot split any further was activated — its photos share
   * one spot (or so nearly that no zoom separates them) — so its photos should
   * open as a grid. Called with the box around them.
   */
  onOpenCluster: (bounds: GeoBounds) => void
  /** The accessible name of a tile (see `tileLabel`). */
  describe: (summary: TileSummary, estimated: boolean) => string
  /** The thumbnail size tiles load (see `mapTileThumbSize`). */
  thumbSize: string
}

/** The imperative handle {@link createPhotoLayer} returns. */
export interface PhotoLayer {
  /** The cluster group, already added to the map. */
  group: L.MarkerClusterGroup
  /**
   * Replaces the plotted photos (in the feed's newest-first order, which is
   * also the representative ranking) and returns the bounds they cover, or
   * `null` for none.
   */
  setFeatures: (features: readonly MapFeature[]) => L.LatLngBounds | null
  /** Marks the tile currently showing `uid` (its cluster, when it is in one). */
  setHighlight: (uid: string | null) => void
  /** Detaches every listener and removes the layer from the map. */
  destroy: () => void
}

/**
 * The parts of markercluster's private cluster state the "can it split?"
 * decision reads — the same three fields its own `_zoomOrSpiderfy` uses to
 * decide between zooming in and spiderfying.
 */
interface ClusterInternals {
  _childClusters: ClusterInternals[]
  _childCount: number
  _zoom: number
}

/** Padding around a cluster's photos when zooming to them (px). */
const ZOOM_PADDING: L.PointExpression = [MAP_TILE_PX, MAP_TILE_PX]

/** The keys that activate a focused tile, as a button's would. */
const ACTIVATING_KEYS = new Set(['Enter', ' ', 'Spacebar'])

/**
 * A `L.DivIcon` whose content is built only when the icon is first drawn, then
 * reused across redraws. Markers outside the viewport are never drawn, so their
 * thumbnails are never requested — the whole point of deferring it: 2 500
 * photos must not mean 2 500 image requests. (An instance override rather than
 * a subclass, so the module still loads against a mocked Leaflet in tests.)
 */
export function lazyTileIcon(build: () => HTMLElement): L.DivIcon {
  const icon = L.divIcon({
    className: 'kk-map-tile-icon',
    iconSize: [MAP_TILE_PX, MAP_TILE_PX],
    iconAnchor: [MAP_TILE_PX / 2, MAP_TILE_PX / 2],
    html: '',
  })
  const draw = icon.createIcon.bind(icon)
  let element: HTMLElement | null = null
  icon.createIcon = (oldIcon?: HTMLElement) => {
    element ??= build()
    icon.options.html = element
    return draw(oldIcon)
  }
  return icon
}

/**
 * The layer an event bubbled up from — the marker or cluster whose icon was
 * activated (Leaflet's `propagatedFrom`, typed `any` upstream).
 */
function eventSource(event: L.LeafletEvent): unknown {
  return (event as { propagatedFrom?: unknown }).propagatedFrom
}

/** Whether a keyboard event on a tile should activate it. */
function isActivation(event: L.LeafletEvent): boolean {
  const original = (event as L.LeafletKeyboardEvent).originalEvent as KeyboardEvent | undefined
  if (original === undefined) {
    return true
  }
  if (!ACTIVATING_KEYS.has(original.key)) {
    return false
  }
  // Space would otherwise scroll whatever scrolls behind the map.
  original.preventDefault()
  return true
}

/**
 * Whether zooming in would split a cluster. Mirrors markercluster's own test:
 * follow the chain of single-child clusters down; if it ends at the deepest
 * zoom still holding every photo, no zoom will ever separate them.
 */
export function clusterCanSplit(cluster: L.MarkerCluster, maxZoom: number): boolean {
  const top = cluster as unknown as ClusterInternals
  let bottom = top
  while (bottom._childClusters.length === 1) {
    bottom = bottom._childClusters[0]
  }
  return !(bottom._zoom === maxZoom && bottom._childCount === top._childCount)
}

/** Converts Leaflet bounds into a plain box. */
export function toGeoBounds(bounds: L.LatLngBounds): GeoBounds {
  return {
    south: bounds.getSouth(),
    west: bounds.getWest(),
    north: bounds.getNorth(),
    east: bounds.getEast(),
  }
}

/**
 * The map page's photo layer: every geotagged photo as a marker, clustered by
 * markercluster into **photo tiles** — a cluster is the thumbnail of its newest
 * photo with a count badge, a single photo the same tile without one (see
 * `buildMapTile`). Activating a cluster zooms to its photos, or — when no zoom
 * can split it — hands its box to `onOpenCluster`; activating a photo hands its
 * uid to `onOpenPhoto`. Markercluster's own click handling (zoom, spiderfy,
 * coverage polygon) is switched off: the decisions are made here.
 *
 * Performance: markers are added with `chunkedLoading`, icons are built only
 * when markercluster draws them (visible clusters only, with
 * `removeOutsideVisibleBounds`), and each tile's summary is computed once per
 * cluster (markercluster caches the icon until the cluster's children change).
 */
export function createPhotoLayer(
  map: L.Map,
  handlers: () => PhotoLayerHandlers,
  options: { animate: boolean },
): PhotoLayer {
  const featureOf = new WeakMap<L.Marker, MapFeature>()
  const rankOf = new Map<string, number>()
  const markerByUid = new Map<string, L.Marker>()
  let highlightUid: string | null = null
  let highlighted: HTMLElement | null = null

  const rank = (feature: MapFeature): number =>
    rankOf.get(feature.properties.uid) ?? Number.POSITIVE_INFINITY

  const group = L.markerClusterGroup({
    chunkedLoading: true,
    maxClusterRadius: MAP_CLUSTER_RADIUS,
    showCoverageOnHover: false,
    zoomToBoundsOnClick: false,
    spiderfyOnMaxZoom: false,
    removeOutsideVisibleBounds: true,
    animate: options.animate,
    chunkProgress: (processed: number, total: number) => {
      if (processed >= total) applyHighlight()
    },
    iconCreateFunction: (cluster) =>
      lazyTileIcon(() => {
        const features = cluster
          .getAllChildMarkers()
          .map((marker) => featureOf.get(marker))
          .filter((feature): feature is MapFeature => feature !== undefined)
        const summary = summarizeTile(features, rank) ?? {
          representative: features[0],
          count: features.length,
        }
        const current = handlers()
        return buildMapTile(summary, {
          thumbSize: current.thumbSize,
          label: current.describe(summary, false),
        })
      }),
  })

  /** Moves the highlight onto whatever currently shows the highlighted photo. */
  function applyHighlight() {
    highlighted?.classList.remove('is-highlighted')
    highlighted = null
    if (highlightUid === null) return
    const marker = markerByUid.get(highlightUid)
    if (marker === undefined) return
    const parent = group.getVisibleParent(marker) as L.Marker | null
    const element = parent?.getElement()
    if (element === undefined) return
    element.classList.add('is-highlighted')
    highlighted = element
  }

  const onPhoto = (event: L.LeafletEvent) => {
    const source = eventSource(event)
    const feature = source instanceof L.Marker ? featureOf.get(source) : undefined
    if (feature === undefined || (event.type === 'keypress' && !isActivation(event))) return
    handlers().onOpenPhoto(feature.properties.uid)
  }

  const onCluster = (event: L.LeafletEvent) => {
    const cluster = eventSource(event)
    if (!(cluster instanceof L.MarkerCluster)) return
    if (event.type === 'clusterkeypress' && !isActivation(event)) return
    if (clusterCanSplit(cluster, map.getMaxZoom())) {
      cluster.zoomToBounds({ padding: ZOOM_PADDING })
      return
    }
    handlers().onOpenCluster(toGeoBounds(cluster.getBounds()))
  }

  group.on('click keypress', onPhoto)
  group.on('clusterclick clusterkeypress', onCluster)
  group.on('animationend', applyHighlight)
  map.on('moveend zoomend', applyHighlight)
  map.addLayer(group)

  function setFeatures(features: readonly MapFeature[]): L.LatLngBounds | null {
    group.clearLayers()
    rankOf.clear()
    markerByUid.clear()
    highlighted = null
    const markers: L.Marker[] = []
    const points: L.LatLngTuple[] = []
    features.forEach((feature, index) => {
      const [lng, lat] = feature.geometry.coordinates
      const estimated = feature.properties.location_estimated === true
      rankOf.set(feature.properties.uid, index)
      const marker = L.marker([lat, lng], {
        keyboard: true,
        riseOnHover: true,
        icon: lazyTileIcon(() => {
          const summary: TileSummary = { representative: feature, count: 1 }
          const current = handlers()
          return buildMapTile(summary, {
            thumbSize: current.thumbSize,
            label: current.describe(summary, estimated),
            estimated,
          })
        }),
      })
      featureOf.set(marker, feature)
      markerByUid.set(feature.properties.uid, marker)
      markers.push(marker)
      points.push([lat, lng])
    })
    group.addLayers(markers)
    return points.length === 0 ? null : L.latLngBounds(points)
  }

  function setHighlight(uid: string | null) {
    highlightUid = uid
    applyHighlight()
  }

  function destroy() {
    map.off('moveend zoomend', applyHighlight)
    group.off()
    map.removeLayer(group)
  }

  return { group, setFeatures, setHighlight, destroy }
}
