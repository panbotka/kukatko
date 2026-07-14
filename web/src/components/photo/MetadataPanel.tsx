import { type ReactNode, type SyntheticEvent, useMemo, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import Form from 'react-bootstrap/Form'
import { useTranslation } from 'react-i18next'

import { type Coordinates, formatCoordinates, parseCoordinates } from '../../lib/coordinates'
import { formatDateTime } from '../../lib/format'
import { type PhotoDetail, type PhotoMetadataUpdate, updatePhoto } from '../../services/photos'
import { Icon } from '../Icon'
import { LeafletMap } from '../map/LeafletMap'

import { PhotoLocation } from './PhotoLocation'

/**
 * The dating note's length cap, mirroring the backend's (`photoapi`'s
 * `takenAtNoteLimit`): the input simply stops accepting more, so a note that would
 * be answered with a 400 cannot be typed in the first place.
 */
const TAKEN_AT_NOTE_MAX = 500

/** Props for {@link MetadataPanel}. */
export interface MetadataPanelProps {
  /** The photo whose caption & place is shown and (for editors) edited. */
  photo: PhotoDetail
  /** Whether the current user may edit metadata (editor/admin). */
  canWrite: boolean
  /** Called with the refreshed photo after a successful save. */
  onUpdated: (photo: PhotoDetail) => void
}

/**
 * Formats an ISO timestamp for the value of a `datetime-local` input, keeping the
 * seconds — the field carries `step="1"` — so a capture time of `00:33:39` is not
 * silently rewritten to `00:33:00` the first time the form is saved.
 */
function toLocalInput(iso: string | undefined): string {
  if (iso === undefined || iso === '') {
    return ''
  }
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) {
    return ''
  }
  // Shift to local time then trim to seconds (YYYY-MM-DDTHH:mm:ss).
  const offset = date.getTimezoneOffset() * 60_000
  return new Date(date.getTime() - offset).toISOString().slice(0, 19)
}

/** The canonical coordinate text for a photo, or empty when it has no location. */
function initialCoordText(photo: PhotoDetail): string {
  if (photo.lat !== undefined && photo.lng !== undefined) {
    return formatCoordinates({ lat: photo.lat, lng: photo.lng })
  }
  return ''
}

/** Props for {@link EditableField}. */
interface EditableFieldProps {
  /** The (already translated) field label. */
  label: string
  /** The current value, or empty/undefined for an unset field. */
  value: string | undefined
  /**
   * A richer rendering of the same value (the estimated capture date's `cca`
   * marker and its dating note). It replaces the plain text when given; `value`
   * still decides whether the field counts as filled, and stays the field's plain
   * text form.
   */
  display?: ReactNode
  /** Whether the current user may edit (editor/admin). */
  canWrite: boolean
  /** Opens the caption edit form (shared by every field's affordance). */
  onEdit: () => void
}

/**
 * One caption field in read-only form. For an editor the whole row is a button —
 * so the field itself is the edit affordance, with a pencil cue and an accessible
 * "Edit «label»" name — that opens the shared caption form; a muted "add…"
 * placeholder invites filling an empty field. A viewer sees the value alone (and
 * an empty field renders nothing, so read-only panels stay free of blank rows).
 */
function EditableField({ label, value, display, canWrite, onEdit }: EditableFieldProps) {
  const { t } = useTranslation()
  const hasValue = value !== undefined && value.trim() !== ''
  const shown = display ?? value

  if (canWrite) {
    return (
      <button
        type="button"
        className="btn btn-link text-reset text-decoration-none d-block w-100 text-start p-0 mb-2"
        aria-label={t('photo.metadata.editField', { field: label })}
        onClick={onEdit}
      >
        <span className="small text-secondary d-block">{label}</span>
        <span className="d-flex justify-content-between align-items-start gap-2">
          <span className={hasValue ? 'text-break' : 'text-secondary fst-italic'}>
            {hasValue ? shown : t('photo.metadata.addPlaceholder')}
          </span>
          <Icon name="pencil" className="text-secondary flex-shrink-0" />
        </span>
      </button>
    )
  }

  if (!hasValue) {
    return null
  }
  return (
    <div className="mb-2">
      <div className="small text-secondary">{label}</div>
      <div className="text-break">{shown}</div>
    </div>
  )
}

/** Props for {@link CaptureDate}. */
interface CaptureDateProps {
  /** The formatted capture date, empty when the photo carries none. */
  date: string
  /** The dating note, empty when there is none. */
  note: string
}

/**
 * An estimated capture date: the date itself behind a `cca` (cs) / `c.` (en)
 * marker, with the dating note beside it. The marker is a badge rather than a
 * colour or a glyph, and it carries a title with the note, so "this date is a
 * guess" survives both a glance and a screen reader — an estimate must never be
 * mistakable for a known date.
 *
 * A photo with no `taken_at` at all can still be an estimate ("někdy ve 40.
 * letech"); the marker and the note then stand alone.
 */
function CaptureDate({ date, note }: CaptureDateProps) {
  const { t } = useTranslation()
  const title =
    note !== ''
      ? t('photo.metadata.estimatedTitleNote', { note })
      : t('photo.metadata.estimatedTitle')

  return (
    <span className="d-inline-flex flex-wrap align-items-baseline gap-1">
      <span className="badge text-bg-secondary flex-shrink-0" title={title}>
        {t('photo.metadata.estimatedMarker')}
      </span>
      {date !== '' && <span>{date}</span>}
      {note !== '' && <span className="fst-italic text-secondary text-break">{note}</span>}
    </span>
  )
}

/**
 * The caption & place panel: the photo's title, description, AI description,
 * capture notes, capture time and location shown read-only, each an inline edit
 * affordance for editors that opens one shared form PATCHing the catalogue. This
 * replaces the old single hidden "Edit" button so every caption field is
 * discoverably editable in place. Location reuses {@link PhotoLocation} read-only
 * (mini-map + on-demand reverse-geocode) and is set/changed/cleared through the
 * form's coordinate field and Leaflet map picker. Camera/lens/EXIF and the
 * uploader are deliberately absent — they live in the collapsed technical
 * details. All text is i18n.
 */
export function MetadataPanel({ photo, canWrite, onUpdated }: MetadataPanelProps) {
  const { t, i18n } = useTranslation()
  const [editing, setEditing] = useState(false)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState(false)

  // The photo's own values, as the form renders them. A field still equal to its
  // pristine value is left out of the PATCH entirely — see buildPatch.
  const pristineTakenAt = toLocalInput(photo.taken_at)
  const pristineEstimated = photo.taken_at_estimated === true
  const pristineNote = photo.taken_at_note ?? ''
  const pristineCoords = initialCoordText(photo)

  const [title, setTitle] = useState(photo.title)
  const [description, setDescription] = useState(photo.description)
  const [notes, setNotes] = useState(photo.notes ?? '')
  const [aiNote, setAiNote] = useState(photo.ai_note ?? '')
  const [takenAt, setTakenAt] = useState(pristineTakenAt)
  const [takenAtEstimated, setTakenAtEstimated] = useState(pristineEstimated)
  const [takenAtNote, setTakenAtNote] = useState(pristineNote)
  // The location lives as a single free-form coordinate string; it is parsed to
  // drive the map marker and, on save, the PATCH lat/lng.
  const [coordText, setCoordText] = useState(pristineCoords)

  // The capture date as the read-only view shows it. captureDateText is its plain
  // text form — it decides whether the field counts as filled and is what a copy of
  // the row reads like — while an estimated date is *rendered* by CaptureDate, with
  // the marker as a badge and the note beside it.
  const takenAtText =
    photo.taken_at !== undefined ? formatDateTime(photo.taken_at, i18n.language) : ''
  const isEstimated = pristineEstimated
  const captureDateText = isEstimated
    ? [t('photo.metadata.estimatedMarker'), takenAtText, pristineNote]
        .filter((part) => part !== '')
        .join(' ')
    : takenAtText

  const parsedCoords = useMemo(() => parseCoordinates(coordText), [coordText])
  const hasCoordText = coordText.trim() !== ''
  const coordsInvalid = hasCoordText && !parsedCoords.ok
  // The controlled marker position: the parsed coordinate, or none while the
  // text is empty or not yet valid.
  const markerPosition: Coordinates | null = parsedCoords.ok ? parsedCoords.value : null

  function startEditing() {
    setTitle(photo.title)
    setDescription(photo.description)
    setNotes(photo.notes ?? '')
    setAiNote(photo.ai_note ?? '')
    setTakenAt(pristineTakenAt)
    setTakenAtEstimated(pristineEstimated)
    setTakenAtNote(pristineNote)
    setCoordText(pristineCoords)
    setError(false)
    setEditing(true)
  }

  /** Rewrites the coordinate text in canonical decimal degrees after a map move. */
  function pickLocation(lat: number, lng: number) {
    setCoordText(formatCoordinates({ lat, lng }))
  }

  /**
   * Builds the PATCH payload. The capture time and the location are only sent when
   * the user actually changed them: both fields are lossy round-trips, so resending
   * an untouched value would quietly rewrite the catalogue — the capture time would
   * flip `taken_at_source` from `exif` to `manual`, and the coordinate would be
   * rounded to the six decimals the text field shows (`16.7083583333333` →
   * `16.708358`). Unparseable coordinate text is left out too (the field reports it
   * inline), so the rest of the form still saves.
   *
   * The dating note rides along only while the photo is (or has just become) an
   * estimate: unchecking the flag is enough on its own, since the backend drops the
   * note with it — a date presented as a fact never keeps a stale remark.
   */
  function buildPatch(): PhotoMetadataUpdate {
    const patch: PhotoMetadataUpdate = {
      title: title.trim(),
      description,
      notes,
      ai_note: aiNote,
    }
    if (takenAt !== pristineTakenAt) {
      patch.taken_at = takenAt === '' ? null : new Date(takenAt).toISOString()
    }
    if (takenAtEstimated !== pristineEstimated) {
      patch.taken_at_estimated = takenAtEstimated
    }
    if (takenAtEstimated && takenAtNote.trim() !== pristineNote) {
      patch.taken_at_note = takenAtNote.trim()
    }
    if (coordText.trim() !== pristineCoords) {
      if (!hasCoordText) {
        patch.lat = null
        patch.lng = null
      } else if (parsedCoords.ok) {
        patch.lat = parsedCoords.value.lat
        patch.lng = parsedCoords.value.lng
      }
    }
    return patch
  }

  async function save(event: SyntheticEvent) {
    event.preventDefault()
    setSaving(true)
    setError(false)
    try {
      const updated = await updatePhoto(photo.uid, buildPatch())
      onUpdated(updated)
      // An unparseable coordinate was not part of the patch: keep the form open so
      // its inline error stays visible and the user can see the location did not
      // save — the other fields did.
      if (!coordsInvalid) {
        setEditing(false)
      }
    } catch {
      setError(true)
    } finally {
      setSaving(false)
    }
  }

  if (editing) {
    return (
      <Form onSubmit={(event) => void save(event)} aria-label={t('photo.metadata.formLabel')}>
        {error && (
          <Alert variant="danger" className="py-2 small">
            {t('photo.metadata.saveError')}
          </Alert>
        )}
        <Form.Group className="mb-2" controlId="photo-title">
          <Form.Label className="small text-secondary mb-1">{t('photo.metadata.title')}</Form.Label>
          <Form.Control
            value={title}
            onChange={(event) => {
              setTitle(event.target.value)
            }}
          />
        </Form.Group>
        <Form.Group className="mb-2" controlId="photo-description">
          <Form.Label className="small text-secondary mb-1">
            {t('photo.metadata.description')}
          </Form.Label>
          <Form.Control
            as="textarea"
            rows={2}
            value={description}
            onChange={(event) => {
              setDescription(event.target.value)
            }}
          />
        </Form.Group>
        <Form.Group className="mb-2" controlId="photo-ai-note">
          <Form.Label className="small text-secondary mb-1">
            {t('photo.metadata.aiNote')}
          </Form.Label>
          <Form.Control
            as="textarea"
            rows={2}
            value={aiNote}
            onChange={(event) => {
              setAiNote(event.target.value)
            }}
          />
        </Form.Group>
        <Form.Group className="mb-2" controlId="photo-notes">
          <Form.Label className="small text-secondary mb-1">{t('photo.metadata.notes')}</Form.Label>
          <Form.Control
            as="textarea"
            rows={2}
            value={notes}
            onChange={(event) => {
              setNotes(event.target.value)
            }}
          />
        </Form.Group>
        <Form.Group className="mb-2" controlId="photo-taken-at">
          <Form.Label className="small text-secondary mb-1">
            {t('photo.metadata.takenAt')}
          </Form.Label>
          <Form.Control
            type="datetime-local"
            step={1}
            value={takenAt}
            onChange={(event) => {
              setTakenAt(event.target.value)
            }}
          />
        </Form.Group>
        {/* The approximate date: the flag, and — only while it is set — the note
            that says what the estimate rests on. An empty note on a photo whose
            date is a fact means nothing, so the field is not offered there. */}
        <Form.Group className="mb-2" controlId="photo-taken-at-estimated">
          <Form.Check
            type="checkbox"
            label={t('photo.metadata.takenAtEstimated')}
            checked={takenAtEstimated}
            onChange={(event) => {
              setTakenAtEstimated(event.target.checked)
            }}
          />
        </Form.Group>
        {takenAtEstimated && (
          <Form.Group className="mb-2" controlId="photo-taken-at-note">
            <Form.Label className="small text-secondary mb-1">
              {t('photo.metadata.takenAtNote')}
            </Form.Label>
            <Form.Control
              value={takenAtNote}
              maxLength={TAKEN_AT_NOTE_MAX}
              placeholder={t('photo.metadata.takenAtNotePlaceholder')}
              onChange={(event) => {
                setTakenAtNote(event.target.value)
              }}
            />
          </Form.Group>
        )}
        <Form.Group className="mb-2" controlId="photo-coordinates">
          <Form.Label className="small text-secondary mb-1">
            {t('photo.metadata.coordinates')}
          </Form.Label>
          <div className="d-flex gap-2 align-items-start">
            <Form.Control
              value={coordText}
              onChange={(event) => {
                setCoordText(event.target.value)
              }}
              placeholder={t('photo.metadata.coordinatesPlaceholder')}
              isInvalid={coordsInvalid}
              inputMode="text"
              aria-describedby="photo-coordinates-help"
            />
            <Button
              type="button"
              variant="outline-secondary"
              size="sm"
              className="flex-shrink-0 kukatko-tap-target"
              disabled={!hasCoordText}
              onClick={() => {
                setCoordText('')
              }}
            >
              {t('photo.metadata.clearLocation')}
            </Button>
          </div>
          {coordsInvalid && (
            <Form.Text className="text-danger d-block">
              {t('photo.metadata.coordinatesInvalid')}
            </Form.Text>
          )}
          <Form.Text id="photo-coordinates-help" className="text-secondary d-block">
            {t('photo.metadata.coordinatesHelp')}
          </Form.Text>
        </Form.Group>
        <div className="mb-2 rounded overflow-hidden">
          <LeafletMap
            features={[]}
            mapset="basic"
            viewport={
              markerPosition !== null
                ? { lat: markerPosition.lat, lng: markerPosition.lng, zoom: 13 }
                : null
            }
            onViewportChange={() => undefined}
            onSelectPhoto={() => undefined}
            thumbAlt={t('map.thumbAlt')}
            height="260px"
            picker={{ position: markerPosition, onPick: pickLocation }}
          />
        </div>
        <div className="d-flex gap-2">
          {/* Saving stays available with an invalid coordinate: the location is
              then left untouched (the field says so) rather than holding the
              caption fields hostage to it. */}
          <Button type="submit" variant="primary" size="sm" disabled={saving}>
            {t('photo.metadata.save')}
          </Button>
          <Button
            type="button"
            variant="outline-secondary"
            size="sm"
            disabled={saving}
            onClick={() => {
              setEditing(false)
            }}
          >
            {t('photo.metadata.cancel')}
          </Button>
        </div>
      </Form>
    )
  }

  return (
    <div>
      <EditableField
        label={t('photo.metadata.title')}
        value={photo.title}
        canWrite={canWrite}
        onEdit={startEditing}
      />
      <EditableField
        label={t('photo.metadata.description')}
        value={photo.description}
        canWrite={canWrite}
        onEdit={startEditing}
      />
      <EditableField
        label={t('photo.metadata.aiNote')}
        value={photo.ai_note}
        canWrite={canWrite}
        onEdit={startEditing}
      />
      <EditableField
        label={t('photo.metadata.takenAt')}
        value={captureDateText}
        display={isEstimated ? <CaptureDate date={takenAtText} note={pristineNote} /> : undefined}
        canWrite={canWrite}
        onEdit={startEditing}
      />
      <EditableField
        label={t('photo.metadata.notes')}
        value={photo.notes}
        canWrite={canWrite}
        onEdit={startEditing}
      />

      {/* Location: the read-only mini-map + place lookup (editing coordinates is
          done in the shared form, so the embedded view stays read-only). */}
      <div className="mb-1">
        <div className="small text-secondary d-flex justify-content-between align-items-center">
          <span>{t('photo.metadata.location')}</span>
          {canWrite && (
            <Button
              variant="link"
              size="sm"
              className="p-0 text-decoration-none"
              aria-label={t('photo.metadata.editField', { field: t('photo.metadata.location') })}
              onClick={startEditing}
            >
              <Icon name="pencil" />
            </Button>
          )}
        </div>
        <PhotoLocation photo={photo} canWrite={false} onUpdated={onUpdated} />
      </div>
    </div>
  )
}
