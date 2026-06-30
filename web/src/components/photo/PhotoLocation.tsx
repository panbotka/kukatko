import { useState } from 'react'
import Button from 'react-bootstrap/Button'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'

import { LeafletMap } from '../map/LeafletMap'
import { type GeocodeResult, type MapFeature, reverseGeocode } from '../../services/map'
import { GRID_THUMB_SIZE, type PhotoDetail, thumbUrl, updatePhoto } from '../../services/photos'

/** Props for {@link PhotoLocation}. */
export interface PhotoLocationProps {
  /** The photo whose location is shown. */
  photo: PhotoDetail
  /** Whether the current user may clear the location (editor/admin). */
  canWrite: boolean
  /** Called with the refreshed photo after the location is cleared. */
  onUpdated: (photo: PhotoDetail) => void
}

/** Builds a single-point map feature for the photo's location. */
function locationFeature(photo: PhotoDetail, lat: number, lng: number): MapFeature {
  return {
    type: 'Feature',
    geometry: { type: 'Point', coordinates: [lng, lat] },
    properties: {
      uid: photo.uid,
      title: photo.title,
      taken_at: photo.taken_at,
      media_type: photo.media_type ?? 'image',
      thumb: thumbUrl(photo.uid, GRID_THUMB_SIZE),
    },
  }
}

/** The reverse-geocode lookup lifecycle. */
type GeocodeState =
  | { status: 'idle' }
  | { status: 'loading' }
  | { status: 'error' }
  | { status: 'ready'; place: GeocodeResult }

/**
 * The GPS panel of the detail page: a Leaflet mini-map (over the mapy.com backend
 * proxy, so the key stays server-side) centred on the photo's coordinate, a
 * button to reverse-geocode the place name on demand (saving mapy.com credits by
 * only looking it up when asked), and — for editors — a button to clear the
 * location. When the photo has no coordinate it shows a hint; geotagging is done
 * via the metadata edit form.
 */
export function PhotoLocation({ photo, canWrite, onUpdated }: PhotoLocationProps) {
  const { t } = useTranslation()
  const [geocode, setGeocode] = useState<GeocodeState>({ status: 'idle' })
  const [clearing, setClearing] = useState(false)

  const lat = photo.lat
  const lng = photo.lng
  if (lat === undefined || lng === undefined) {
    return <p className="text-secondary small mb-0">{t('photo.location.none')}</p>
  }

  async function lookup() {
    if (lat === undefined || lng === undefined) {
      return
    }
    setGeocode({ status: 'loading' })
    try {
      const place = await reverseGeocode(lat, lng)
      setGeocode({ status: 'ready', place })
    } catch {
      setGeocode({ status: 'error' })
    }
  }

  async function clearLocation() {
    setClearing(true)
    try {
      const updated = await updatePhoto(photo.uid, { lat: null, lng: null })
      onUpdated(updated)
    } catch {
      // Leave the location in place; the panel simply stays as-is.
    } finally {
      setClearing(false)
    }
  }

  return (
    <div>
      <div className="rounded overflow-hidden mb-2">
        <LeafletMap
          features={[locationFeature(photo, lat, lng)]}
          mapset="basic"
          viewport={{ lat, lng, zoom: 13 }}
          onViewportChange={() => undefined}
          onSelectPhoto={() => undefined}
          thumbAlt={t('map.thumbAlt')}
          height="240px"
        />
      </div>

      <div className="small text-secondary mb-2">
        {lat.toFixed(5)}, {lng.toFixed(5)}
      </div>

      <div className="d-flex gap-2 flex-wrap align-items-center">
        <Button
          variant="outline-secondary"
          size="sm"
          disabled={geocode.status === 'loading'}
          onClick={() => void lookup()}
        >
          {t('photo.location.lookup')}
        </Button>
        {canWrite && (
          <Button
            variant="outline-danger"
            size="sm"
            disabled={clearing}
            onClick={() => void clearLocation()}
          >
            {t('photo.location.clear')}
          </Button>
        )}
        {geocode.status === 'loading' && (
          <Spinner animation="border" role="status" size="sm">
            <span className="visually-hidden">{t('photo.location.lookup')}</span>
          </Spinner>
        )}
      </div>

      {geocode.status === 'ready' && (
        <p className="mt-2 mb-0">
          {geocode.place.name !== '' ? geocode.place.name : geocode.place.location}
        </p>
      )}
      {geocode.status === 'error' && (
        <p className="mt-2 mb-0 text-secondary small">{t('photo.location.lookupError')}</p>
      )}
    </div>
  )
}
