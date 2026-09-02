import { useMemo, useState } from 'react'
import Button from 'react-bootstrap/Button'
import Form from 'react-bootstrap/Form'
import Modal from 'react-bootstrap/Modal'
import { useTranslation } from 'react-i18next'

import { type Coordinates, formatCoordinates, parseCoordinates } from '../../lib/coordinates'
import { NEARBY_ZOOM, lastPickedLocation, rememberPickedLocation } from '../../lib/pickedLocation'
import { type MapViewport } from '../../lib/mapView'
import { type Place } from '../../services/map'
import { Icon } from '../Icon'

import { LeafletMap } from './LeafletMap'
import { PlaceSearch } from './PlaceSearch'

/**
 * Zoom the map opens at when the photo already has a location: close enough to
 * see which house it is, far enough to recognise the village around it.
 */
const PICKED_ZOOM = 13

/** Height of the inline map when the caller names none. */
const DEFAULT_HEIGHT = '260px'

/** Props for {@link LocationPicker}. */
export interface LocationPickerProps {
  /**
   * The coordinate being edited, as the free-form text the field holds — the
   * canonical `49.19522, 16.60796` this component writes, or whatever notation
   * the user pasted (see `lib/coordinates`). Empty means "no location".
   *
   * Text rather than a parsed pair on purpose: the caller decides what an
   * unparseable value means for its save, and a half-typed coordinate must not
   * be silently rewritten under the cursor.
   */
  value: string
  /** Called with the new coordinate text after any of the three ways in. */
  onChange: (coordText: string) => void
  /** Disables every control while the caller's save is in flight. */
  disabled?: boolean
  /**
   * Prefix for the ids tying labels to fields. Give two pickers on one page
   * different prefixes.
   */
  idPrefix?: string
  /** CSS height of the inline map. The full-screen one always fills the dialog. */
  height?: string
}

/**
 * The three ways to put a photo on the map, in the order they are reached for:
 * name the place, type or paste the numbers, or click the map and drag the pin.
 *
 * It is one reusable control rather than a block of the metadata form because the
 * same question — "where was this taken?" — is asked of a single photo today and
 * of a selection tomorrow; the bulk editor gets the picker, not a copy of it. It
 * is fully controlled: the coordinate text is the caller's state and every way in
 * writes the same field, so a caller that already knows how to save one text
 * field needs to learn nothing new.
 *
 * The map starts somewhere useful even for a photo that has no location. First
 * choice is the photo's own coordinate, second the last place picked in this
 * session (a box of scans is almost always one village, see `lib/pickedLocation`)
 * and last the library's own region — never the Atlantic at 0,0, which is where a
 * map with no opinion puts you and where nobody's photographs were taken.
 *
 * On a phone the inline map is a postage stamp, so the picker can be opened
 * full-screen: the same map, the same pin, the whole viewport. The controls stay
 * behind the dialog and the dialog carries a read-out of what is picked, so there
 * is never a second copy of the coordinate field competing with the first.
 */
export function LocationPicker({
  value,
  onChange,
  disabled = false,
  idPrefix = 'location-picker',
  height = DEFAULT_HEIGHT,
}: LocationPickerProps) {
  const { t } = useTranslation()
  const [expanded, setExpanded] = useState(false)

  const parsed = useMemo(() => parseCoordinates(value), [value])
  const hasText = value.trim() !== ''
  const invalid = hasText && !parsed.ok
  // The controlled pin position: the parsed coordinate, or none while the text is
  // empty or not yet valid.
  const position: Coordinates | null = parsed.ok ? parsed.value : null

  /** Writes a picked point back as canonical decimal degrees. */
  function pick(lat: number, lng: number) {
    rememberPickedLocation({ lat, lng })
    onChange(formatCoordinates({ lat, lng }))
  }

  /**
   * Takes a searched-for place's coordinates. It writes the same field a map
   * click does, so the map recentres on the point and the caller's save path
   * stays the one it already had — a place search is a third way to fill the
   * coordinate in, not a third kind of location.
   */
  function pickPlace(place: Place) {
    pick(place.lat, place.lng)
  }

  const map = (mapHeight: string) => (
    <LeafletMap
      features={[]}
      mapset="basic"
      viewport={startViewport(position)}
      onViewportChange={() => undefined}
      onSelectPhoto={() => undefined}
      thumbAlt={t('map.thumbAlt')}
      twoFingerHint={t('map.gesture.twoFingers')}
      height={mapHeight}
      picker={{ position, onPick: pick }}
    />
  )

  return (
    <div>
      <PlaceSearch id={`${idPrefix}-place`} onPick={pickPlace} disabled={disabled} />
      <Form.Group className="mb-2" controlId={`${idPrefix}-coordinates`}>
        <Form.Label className="small text-secondary mb-1">
          {t('photo.metadata.coordinates')}
        </Form.Label>
        <div className="d-flex gap-2 align-items-start">
          <Form.Control
            value={value}
            onChange={(event) => {
              onChange(event.target.value)
            }}
            placeholder={t('photo.metadata.coordinatesPlaceholder')}
            isInvalid={invalid}
            disabled={disabled}
            inputMode="text"
            aria-describedby={`${idPrefix}-coordinates-help`}
          />
          <Button
            type="button"
            variant="outline-secondary"
            size="sm"
            className="flex-shrink-0 kukatko-tap-target"
            disabled={disabled || !hasText}
            onClick={() => {
              onChange('')
            }}
          >
            {t('photo.metadata.clearLocation')}
          </Button>
        </div>
        {invalid && (
          <Form.Text className="text-danger d-block">
            {t('photo.metadata.coordinatesInvalid')}
          </Form.Text>
        )}
        <Form.Text id={`${idPrefix}-coordinates-help`} className="text-secondary d-block">
          {t('photo.metadata.coordinatesHelp')}
        </Form.Text>
      </Form.Group>

      <div className="d-flex justify-content-end mb-1">
        <Button
          type="button"
          variant="outline-secondary"
          size="sm"
          className="kukatko-tap-target"
          disabled={disabled}
          onClick={() => {
            setExpanded(true)
          }}
        >
          <Icon name="arrows-fullscreen" className="me-1" />
          {t('map.picker.expand')}
        </Button>
      </div>

      {/* Only one map is mounted at a time: Leaflet owns the DOM under its
          container, and the picker is controlled, so moving it into the dialog
          and back loses nothing but the viewport. */}
      {!expanded && <div className="mb-2 rounded overflow-hidden">{map(height)}</div>}

      {/* Full screen at every width, not only on a phone: a map you are aiming a
          pin on wants the whole viewport wherever it is opened, and it makes the
          height the map has to fill the one thing it cannot be — ambiguous. */}
      <Modal
        show={expanded}
        onHide={() => {
          setExpanded(false)
        }}
        fullscreen
        aria-label={t('map.picker.title')}
      >
        <Modal.Header closeButton>
          <Modal.Title as="h2" className="h5 mb-0">
            {t('map.picker.title')}
          </Modal.Title>
        </Modal.Header>
        {/* The map is the content: it takes everything left between the header
            and the footer (see `.kukatko-picker-body`). */}
        <Modal.Body className="p-0 kukatko-picker-body">{expanded && map('100%')}</Modal.Body>
        <Modal.Footer className="justify-content-between gap-2">
          {/* What is picked, in numbers — the coordinate field itself stays behind
              the dialog, and a pin with nothing to read back is a guess. */}
          <span className="small text-secondary text-truncate">
            {position !== null ? formatCoordinates(position, 5) : t('map.picker.noLocation')}
          </span>
          <span className="d-flex gap-2">
            <Button
              type="button"
              variant="outline-secondary"
              size="sm"
              disabled={disabled || !hasText}
              onClick={() => {
                onChange('')
              }}
            >
              {t('photo.metadata.clearLocation')}
            </Button>
            <Button
              type="button"
              variant="primary"
              size="sm"
              onClick={() => {
                setExpanded(false)
              }}
            >
              {t('map.picker.done')}
            </Button>
          </span>
        </Modal.Footer>
      </Modal>
    </div>
  )
}

/**
 * Where the map opens: on the photo's own coordinate when it has one, else on the
 * last place picked this session, else nowhere in particular — `null`, which
 * leaves LeafletMap on its own default view of the library's region rather than
 * on 0,0 in the ocean.
 */
function startViewport(position: Coordinates | null): MapViewport | null {
  if (position !== null) {
    return { lat: position.lat, lng: position.lng, zoom: PICKED_ZOOM }
  }
  const recent = lastPickedLocation()
  if (recent !== null) {
    return { lat: recent.lat, lng: recent.lng, zoom: NEARBY_ZOOM }
  }
  return null
}
