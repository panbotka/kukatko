import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router-dom'

import { EmptyState } from '../components/EmptyState'
import { ErrorState } from '../components/ErrorState'
import { LeafletMap } from '../components/map/LeafletMap'
import { MapFilterBar } from '../components/map/MapFilterBar'
import { useMapPhotos } from '../hooks/useMapPhotos'
import {
  MAP_DEFAULTS,
  type MapView,
  mapsetFromView,
  mapViewToParams,
  type MapViewport,
  viewportFromView,
} from '../lib/mapView'
import { useUrlState } from '../lib/urlState'
import { probeTileFailure, type TileFailure } from '../services/map'

/** The i18n key explaining each tile failure. */
const TILE_FAILURE_MESSAGES = {
  key_rejected: 'map.tiles.keyRejected',
  rate_limited: 'map.tiles.rateLimited',
  unavailable: 'map.tiles.unavailable',
  error: 'map.tiles.error',
} as const satisfies Record<TileFailure, string>

/**
 * The map view: geotagged photos plotted as clustered markers over mapy.com
 * tiles (served through the backend proxy, so the API key stays server-side).
 * The mapset, viewport and photo filters all live in the URL, so Back/Forward
 * restore the exact map and a shared link reproduces it. Panning/zooming updates
 * the URL without refetching; changing a filter refetches the GeoJSON feed.
 *
 * When the tiles fail to load — most likely because mapy.com is rejecting the
 * server's API key — the page says so in a dismissible warning instead of showing
 * an unexplained grey grid. The map itself stays up: the markers, clusters and
 * popups all keep working over the empty background.
 */
export function MapPage() {
  const { t } = useTranslation()
  const navigate = useNavigate()
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
  // reload every marker. Reconstructing from the primitive fields keeps the memo
  // callback free of the `view` object, so its dependency list is exact.
  const { taken_after, taken_before, archived, album, label } = view
  const params = useMemo(
    () =>
      mapViewToParams({
        ...MAP_DEFAULTS,
        taken_after,
        taken_before,
        archived,
        album,
        label,
      }),
    [taken_after, taken_before, archived, album, label],
  )
  const { features, status, retry } = useMapPhotos(params)

  const mapset = mapsetFromView(view)
  const viewport = viewportFromView(view)

  const handleViewportChange = useCallback(
    (next: MapViewport) => {
      // Replace (not push) so live panning does not flood the history stack.
      setView(
        { lat: next.lat.toFixed(5), lng: next.lng.toFixed(5), z: String(next.zoom) },
        { replace: true },
      )
    },
    [setView],
  )

  const handleSelectPhoto = useCallback(
    (uid: string) => {
      void navigate(`/photos/${uid}`)
    },
    [navigate],
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
    <>
      <h1 className="kk-page-title mb-3">{t('map.title')}</h1>

      <MapFilterBar view={view} onChange={setView} mapset={mapset} count={features.length} />

      {status === 'error' ? (
        <ErrorState title={t('map.error.load')} onRetry={retry} />
      ) : (
        <div className="position-relative">
          {tileFailure !== null && !warningDismissed && (
            <Alert
              variant="warning"
              dismissible
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
            onViewportChange={handleViewportChange}
            onSelectPhoto={handleSelectPhoto}
            thumbAlt={t('map.thumbAlt')}
            estimatedTitle={t('map.estimatedTitle')}
            onTileError={handleTileError}
          />

          {status === 'loading' && (
            <div
              className="position-absolute top-0 start-50 translate-middle-x mt-2 px-3 py-1 rounded bg-dark text-light small d-flex align-items-center gap-2"
              style={{ zIndex: 1000 }}
              aria-live="polite"
            >
              <Spinner animation="border" size="sm" role="status" />
              {t('map.loading')}
            </div>
          )}

          {status === 'ready' && features.length === 0 && (
            <div
              className="position-absolute top-0 start-50 translate-middle-x mt-2 px-3 rounded bg-dark text-light"
              style={{ zIndex: 1000, maxWidth: '90%' }}
              aria-live="polite"
            >
              {/* Compact variant: this floats over the map rather than filling
                  the space a collection would occupy. */}
              <EmptyState size="sm" title={t('map.empty.title')} hint={t('map.empty.hint')} />
            </div>
          )}
        </div>
      )}
    </>
  )
}
