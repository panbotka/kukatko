import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router-dom'

import { useAuth } from '../auth/AuthContext'
import { EmptyState } from '../components/EmptyState'
import { ErrorState } from '../components/ErrorState'
import { LeafletMap, type LeafletPhotoTiles } from '../components/map/LeafletMap'
import { MapAreaPanel } from '../components/map/MapAreaPanel'
import { MapFilterBar } from '../components/map/MapFilterBar'
import { MapsetControl } from '../components/map/MapsetControl'
import { useDocumentTitle } from '../hooks/useDocumentTitle'
import { useIsNarrowViewport } from '../hooks/useIsNarrowViewport'
import { useMapPhotos } from '../hooks/useMapPhotos'
import { usePrefersReducedMotion } from '../hooks/usePrefersReducedMotion'
import { SHEET_PEEK_PX } from '../lib/bottomSheet'
import { formatBBox, type GeoBounds, parseBBox, photosInBounds, sameBBox } from '../lib/mapArea'
import { mapDetailQuery } from '../lib/mapDetail'
import { tileLabel } from '../lib/mapTile'
import {
  MAP_DEFAULTS,
  MAP_PANEL_CLOSED,
  type MapView,
  mapsetFromView,
  mapViewToParams,
  type MapViewport,
  panelOpenFromView,
  panelValueForSheet,
  type SheetState,
  sheetStateFromView,
  viewportFromView,
} from '../lib/mapView'
import { useUrlState } from '../lib/urlState'
import { useProjectPath } from '../project/ProjectContext'
import { type Mapset, probeTileFailure, type TileFailure } from '../services/map'

/** The i18n key explaining each tile failure. */
const TILE_FAILURE_MESSAGES = {
  key_rejected: 'map.tiles.keyRejected',
  rate_limited: 'map.tiles.rateLimited',
  unavailable: 'map.tiles.unavailable',
  error: 'map.tiles.error',
} as const satisfies Record<TileFailure, string>

/**
 * How long the map must rest before the area panel re-lists (ms). Leaflet
 * already reports only settled views (`moveend`), but a fling or a run of wheel
 * zooms settles several times in a row; re-filtering and re-rendering the grid
 * for each would make the panel flicker through states nobody looked at.
 */
export const AREA_DEBOUNCE_MS = 200

/**
 * The map: a way to **browse the photos by place**. Every geotagged photo is on
 * it as a photo tile — clusters are the thumbnail of their newest photo with a
 * count badge, single photos the same tile without one — over mapy.com tiles
 * served through the backend proxy (the key stays server-side). Tapping a
 * cluster zooms into it; one that no zoom can split (its photos share a spot)
 * opens its photos as a grid instead; tapping a photo opens it in the viewer.
 *
 * Beside the map (desktop) or over it as a bottom sheet (phone), the **area
 * panel** lists the photos inside the current viewport, newest first, re-listed
 * as the map settles; opening one from there lets the viewer page through
 * exactly that list (`bbox` in the detail URL, see `lib/mapDetail`).
 *
 * Everything that shapes the view lives in the URL — the mapset, centre and
 * zoom, the filters, the panel's state, an opened cluster and a highlighted
 * photo (where "show on map" from a photo lands) — so Back/Forward restore the
 * exact map and a shared link reproduces it. Panning only writes the URL; a
 * filter change refetches the feed.
 *
 * The map **is** the page on every screen: `.kk-map-fill` sizes the column to
 * the dynamic viewport and drops the footer (see `app.css`), the header is one
 * row (the title and the quiet filter bar; on a phone the bar folds into a
 * drawer behind one button), and the base-map switch floats over the map's
 * corner. With nothing around the map to scroll, a one-finger swipe belongs to
 * the map ({@link LeafletMap}'s `fillsViewport`).
 *
 * When the tiles fail to load — most likely because mapy.com is rejecting the
 * server's API key — the page says so in a dismissible warning instead of an
 * unexplained grey grid; the photo tiles keep working over the empty background.
 */
export function MapPage() {
  const { t, i18n } = useTranslation()
  useDocumentTitle(t('map.title'))
  const navigate = useNavigate()
  const projectPath = useProjectPath()
  const { canWrite } = useAuth()
  const narrow = useIsNarrowViewport()
  const reducedMotion = usePrefersReducedMotion()
  const [view, setView] = useUrlState<MapView>(MAP_DEFAULTS)
  const [tileFailure, setTileFailure] = useState<TileFailure | null>(null)
  const [warningDismissed, setWarningDismissed] = useState(false)
  // A failing map fires one tileerror per tile in the viewport, so keep the
  // diagnosis to a single in-flight probe (and none at all once we know why).
  const probeRef = useRef(false)
  const tileFailureRef = useRef<TileFailure | null>(null)
  tileFailureRef.current = tileFailure

  // Only the feed filters drive a refetch — memoise on those fields (not the
  // whole view) so panning (which writes lat/lng/zoom into the URL) does not
  // reload every marker.
  const { taken_after, taken_before, archived, album, label } = view
  const filterView = useMemo(
    () => ({ ...MAP_DEFAULTS, taken_after, taken_before, archived, album, label }),
    [taken_after, taken_before, archived, album, label],
  )
  const params = useMemo(() => mapViewToParams(filterView), [filterView])
  const { features, coverage, status, retry } = useMapPhotos(params)

  const mapset = mapsetFromView(view)
  const viewport = viewportFromView(view)
  // Read once: the area to fit on load (the viewer's Back link), never re-fit.
  const [initialArea] = useState(() => parseBBox(view.area))

  // The visible box, debounced; null until the map has reported one.
  const [bounds, setBounds] = useState<GeoBounds | null>(null)
  const boundsTimer = useRef<number | undefined>(undefined)
  const handleBoundsChange = useCallback((next: GeoBounds) => {
    window.clearTimeout(boundsTimer.current)
    boundsTimer.current = window.setTimeout(() => {
      setBounds((prev) => (sameBBox(prev, next) ? prev : next))
    }, AREA_DEBOUNCE_MS)
  }, [])
  useEffect(
    () => () => {
      window.clearTimeout(boundsTimer.current)
    },
    [],
  )

  // A cluster opened as a grid replaces the viewport as the panel's subject.
  const spot = useMemo(() => parseBBox(view.sel), [view.sel])
  const listBounds = spot ?? bounds
  const areaPhotos = useMemo(() => photosInBounds(features, listBounds), [features, listBounds])
  const detailQuery = useMemo(
    () => (listBounds === null ? '' : mapDetailQuery(filterView, listBounds)),
    [filterView, listBounds],
  )

  const [hoverUid, setHoverUid] = useState<string | null>(null)
  const highlightUid = hoverUid ?? (view.photo === '' ? null : view.photo)

  const handleViewportChange = useCallback(
    (next: MapViewport) => {
      // Replace (not push) so live panning does not flood the history stack.
      // The load-time area has served its purpose once the map has a view.
      setView(
        { lat: next.lat.toFixed(5), lng: next.lng.toFixed(5), z: String(next.zoom), area: '' },
        { replace: true },
      )
    },
    [setView],
  )

  // Latest values for the map's long-lived tile callbacks.
  const latest = useRef({ bounds, filterView, narrow, view })
  latest.current = { bounds, filterView, narrow, view }

  const photoTiles = useMemo<LeafletPhotoTiles>(
    () => ({
      onOpenPhoto: (uid) => {
        const { bounds: area, filterView: filters } = latest.current
        const query = area === null ? '' : `?${mapDetailQuery(filters, area)}`
        void navigate(projectPath(`/photos/${encodeURIComponent(uid)}${query}`))
      },
      onOpenCluster: (box) => {
        const { narrow: phone, view: current } = latest.current
        // Pushed, so Back closes the cluster's grid again. A peeking sheet
        // rises to show it; a collapsed desktop column opens.
        const peeking = sheetStateFromView(current) === 'peek'
        setView({
          sel: formatBBox(box),
          panel: phone ? (peeking ? panelValueForSheet('half') : current.panel) : '',
        })
      },
      describe: (summary, estimated) => tileLabel(t, i18n.language, summary, estimated),
    }),
    [navigate, projectPath, setView, t, i18n.language],
  )

  const handleBackToArea = useCallback(() => {
    setView({ sel: '' })
  }, [setView])

  const handlePanelOpen = useCallback(
    (open: boolean) => {
      setView({ panel: open ? '' : MAP_PANEL_CLOSED }, { replace: true })
    },
    [setView],
  )

  const handleSheetState = useCallback(
    (state: SheetState) => {
      setView({ panel: panelValueForSheet(state) }, { replace: true })
    },
    [setView],
  )

  const handleMapset = useCallback(
    (next: Mapset) => {
      setView({ mapset: next })
    },
    [setView],
  )

  // Leaflet only reports *that* a tile failed; ask the proxy why, so the warning
  // can name the cause (a rejected key is the one an operator must act on).
  const handleTileError = useCallback((tileUrl: string) => {
    if (probeRef.current || tileFailureRef.current !== null) {
      return
    }
    probeRef.current = true
    void probeTileFailure(tileUrl)
      .then((failure) => {
        if (failure !== null) {
          setTileFailure(failure)
        }
      })
      .catch(() => undefined)
      .finally(() => {
        probeRef.current = false
      })
  }, [])

  // A mapset switch re-arms the warning: the new layer may well load, and a stale
  // warning over a working map would be worse than none.
  useEffect(() => {
    setTileFailure(null)
    setWarningDismissed(false)
  }, [mapset])

  return (
    <div className="kk-map-page kk-map-fill">
      {/* One row for the whole page header: the title, and the bar — on a phone
          a single Filters button plus the short coverage, on a desktop the quiet
          filter row — wrapping under the title only when there is no room. */}
      <div className="kk-map-header">
        <h1 className="kk-page-title mb-0">{t('map.title')}</h1>
        <MapFilterBar
          view={view}
          onChange={setView}
          count={features.length}
          coverage={coverage}
          canWrite={canWrite}
        />
      </div>

      {status === 'error' ? (
        <ErrorState title={t('map.error.load')} onRetry={retry} />
      ) : (
        <div className={narrow ? 'kk-map-stage kk-map-stage--sheet' : 'kk-map-stage'}>
          <div className="kk-map-canvas">
            {tileFailure !== null && !warningDismissed && (
              <Alert
                variant="warning"
                dismissible
                className="kk-map-tile-warning"
                onClose={() => {
                  setWarningDismissed(true)
                }}
              >
                <div>{t(TILE_FAILURE_MESSAGES[tileFailure])}</div>
                <div className="small mb-0">{t('map.tiles.hint')}</div>
              </Alert>
            )}

            <LeafletMap
              features={features}
              mapset={mapset}
              viewport={viewport}
              initialBounds={initialArea}
              onViewportChange={handleViewportChange}
              onBoundsChange={handleBoundsChange}
              photoTiles={photoTiles}
              highlightUid={highlightUid}
              animate={!reducedMotion}
              bottomInset={narrow ? SHEET_PEEK_PX : 0}
              thumbAlt={t('map.thumbAlt')}
              estimatedTitle={t('map.estimatedTitle')}
              twoFingerHint={t('map.gesture.twoFingers')}
              onTileError={handleTileError}
              fillsViewport
            />

            <MapsetControl mapset={mapset} onChange={handleMapset} compact={narrow} />

            {status === 'loading' && (
              <div className="kk-map-status" aria-live="polite">
                <Spinner animation="border" size="sm" role="status" />
                {t('map.loading')}
              </div>
            )}

            {status === 'ready' && features.length === 0 && (
              <div className="kk-map-status kk-map-status--empty" aria-live="polite">
                {/* Compact variant: this floats over the map rather than filling
                    the space a collection would occupy. */}
                <EmptyState size="sm" title={t('map.empty.title')} hint={t('map.empty.hint')} />
              </div>
            )}
          </div>

          {status === 'ready' && features.length > 0 && (
            <MapAreaPanel
              layout={narrow ? 'sheet' : 'side'}
              photos={areaPhotos}
              mode={spot === null ? 'area' : 'spot'}
              detailQuery={detailQuery}
              onBackToArea={handleBackToArea}
              onHoverPhoto={setHoverUid}
              highlightUid={highlightUid}
              open={panelOpenFromView(view)}
              onOpenChange={handlePanelOpen}
              sheetState={sheetStateFromView(view)}
              onSheetStateChange={handleSheetState}
            />
          )}
        </div>
      )}
    </div>
  )
}
