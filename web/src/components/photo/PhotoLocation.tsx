import { useState } from 'react'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'

import { ReasonedButton } from '../ReasonedButton'
import { LeafletMap } from '../map/LeafletMap'
import { formatCoordinates } from '../../lib/coordinates'
import { placeLabel } from '../../lib/photoPlace'
import { type GeocodeResult, type MapFeature, reverseGeocode } from '../../services/map'
import { type PhotoDetail, updatePhoto } from '../../services/photos'

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
      thumb: photo.thumb_url,
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
 * What the line under the map says: the place a lookup just resolved, else the
 * one the `places` job cached on the photo, else nothing — for which the caller
 * has words of its own.
 *
 * A freshly looked-up name wins over the cached hierarchy because it is what the
 * user just asked for; answering a click with the string that was already on
 * screen would read as the button having done nothing.
 */
function shownPlace(photo: PhotoDetail, geocode: GeocodeState): string {
  if (geocode.status === 'ready') {
    const { name, location } = geocode.place
    return name !== '' ? name : location
  }
  return placeLabel(photo.place)
}

/**
 * The GPS panel of the detail page: a Leaflet mini-map (over the mapy.com backend
 * proxy, so the key stays server-side) centred on the photo's coordinate, a
 * button to reverse-geocode the place name on demand (saving mapy.com credits by
 * only looking it up when asked), and — for editors — a button to clear the
 * location. When the photo has no coordinate it shows a hint; geotagging is done
 * via the metadata edit form.
 *
 * Under the map the panel names the **place**, not the coordinate: the cached
 * hierarchy the `places` job resolved, replaced by a fresh lookup once one is
 * made. The numbers stay in the line's `title` — a hover for whoever wants to
 * paste them into a map — and in full in the technical details.
 *
 * Clearing is an editor's power, so for a viewer that button is **absent** rather
 * than greyed out (the app-wide rule, see `ReasonedButton`); the lookup button
 * stays live for everyone and only ever goes off while its own request is in
 * flight — with the reason attached.
 */
export function PhotoLocation({ photo, canWrite, onUpdated }: PhotoLocationProps) {
  const { t } = useTranslation()
  const [geocode, setGeocode] = useState<GeocodeState>({ status: 'idle' })
  const [clearing, setClearing] = useState(false)

  const lat = photo.lat
  const lng = photo.lng
  const place = shownPlace(photo, geocode)
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
          twoFingerHint={t('map.gesture.twoFingers')}
          height="240px"
        />
      </div>

      {/* Where the photo was taken, in words. The coordinate itself is still one
          hover away — and spelled out in full in the technical details — but
          "49.39322, 16.70869" is not an answer to "where is this?" for anyone
          who is not holding a map. */}
      <p
        className={place !== '' ? 'mb-2' : 'small text-secondary mb-2'}
        title={formatCoordinates({ lat, lng }, 5)}
      >
        {place !== '' ? place : t('photo.location.unknownPlace')}
      </p>

      <div className="d-flex gap-2 flex-wrap align-items-center">
        {/* Looking a place up is a read: it costs mapy.com credits, not write
            access, so a viewer gets the same live button an editor does. While
            the lookup runs the button says so — the spinner beside it is a
            symbol, and a greyed control with no words next to it reads as broken
            rather than busy. */}
        <ReasonedButton
          variant="outline-secondary"
          size="sm"
          disabledReason={geocode.status === 'loading' ? t('photo.location.lookupBusy') : undefined}
          onClick={() => void lookup()}
        >
          {t('photo.location.lookup')}
        </ReasonedButton>
        {canWrite && (
          <ReasonedButton
            variant="outline-danger"
            size="sm"
            disabledReason={clearing ? t('photo.location.clearBusy') : undefined}
            onClick={() => void clearLocation()}
          >
            {t('photo.location.clear')}
          </ReasonedButton>
        )}
        {geocode.status === 'loading' && (
          <Spinner animation="border" role="status" size="sm">
            <span className="visually-hidden">{t('photo.location.lookup')}</span>
          </Spinner>
        )}
      </div>

      {geocode.status === 'error' && (
        <p className="mt-2 mb-0 text-secondary small">{t('photo.location.lookupError')}</p>
      )}
    </div>
  )
}
