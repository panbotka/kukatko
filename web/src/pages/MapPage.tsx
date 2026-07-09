import { useCallback, useMemo } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router-dom'

import { EmptyState } from '../components/EmptyState'
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

/**
 * The map view: geotagged photos plotted as clustered markers over mapy.com
 * tiles (served through the backend proxy, so the API key stays server-side).
 * The mapset, viewport and photo filters all live in the URL, so Back/Forward
 * restore the exact map and a shared link reproduces it. Panning/zooming updates
 * the URL without refetching; changing a filter refetches the GeoJSON feed.
 */
export function MapPage() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const [view, setView] = useUrlState<MapView>(MAP_DEFAULTS)

  // Only the feed filters drive a refetch — memoise on those fields (not the
  // whole view) so panning (which writes lat/lng/zoom into the URL) does not
  // reload every marker. Reconstructing from the primitive fields keeps the memo
  // callback free of the `view` object, so its dependency list is exact.
  const { taken_after, taken_before, archived, private: privateFilter, album, label } = view
  const params = useMemo(
    () =>
      mapViewToParams({
        ...MAP_DEFAULTS,
        taken_after,
        taken_before,
        archived,
        private: privateFilter,
        album,
        label,
      }),
    [taken_after, taken_before, archived, privateFilter, album, label],
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

  return (
    <>
      <h1 className="kk-page-title mb-3">{t('map.title')}</h1>

      <MapFilterBar view={view} onChange={setView} mapset={mapset} count={features.length} />

      {status === 'error' ? (
        <Alert variant="danger" className="d-flex align-items-center justify-content-between">
          <span>{t('map.error.load')}</span>
          <Button variant="outline-light" size="sm" onClick={retry}>
            {t('map.error.retry')}
          </Button>
        </Alert>
      ) : (
        <div className="position-relative">
          <LeafletMap
            features={features}
            mapset={mapset}
            viewport={viewport}
            onViewportChange={handleViewportChange}
            onSelectPhoto={handleSelectPhoto}
            thumbAlt={t('map.thumbAlt')}
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
