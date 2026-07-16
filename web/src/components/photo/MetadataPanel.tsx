import { type ReactNode, type SyntheticEvent, useMemo, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import Form from 'react-bootstrap/Form'
import { useTranslation } from 'react-i18next'

import { type Coordinates, formatCoordinates, parseCoordinates } from '../../lib/coordinates'
import { formatDateTime } from '../../lib/format'
import { joinKeywords, sameKeywords, splitKeywords } from '../../lib/photoFacts'
import { type PhotoDetail, type PhotoMetadataUpdate, updatePhoto } from '../../services/photos'
import { Icon } from '../Icon'
import { LeafletMap } from '../map/LeafletMap'

import { KeywordsInput } from './KeywordsInput'
import { PhotoLocation } from './PhotoLocation'

/**
 * The dating note's length cap, mirroring the backend's (`photoapi`'s
 * `takenAtNoteLimit`): the input simply stops accepting more, so a note that would
 * be answered with a 400 cannot be typed in the first place.
 */
const TAKEN_AT_NOTE_MAX = 500

/**
 * The IPTC/XMP credit fields' length caps, mirroring the backend's `creditLimits`
 * (`internal/photoapi/update.go`) field for field: the inputs stop accepting more,
 * so a value the PATCH would answer with a 400 cannot be typed in the first place.
 */
const SUBJECT_MAX = 1000
const COPYRIGHT_MAX = 1000
const LICENSE_MAX = 1000
const ARTIST_MAX = 255
/** The cap on the joined, comma-separated keyword string — not on one keyword. */
const KEYWORDS_MAX = 2000

/** The DOM id of the collapsible credit sub-section, referenced by `aria-controls`. */
const CREDITS_REGION_ID = 'photo-credit-fields'

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

/** Props for {@link EstimatedLocation}. */
interface EstimatedLocationProps {
  /** Whether the current user may accept or discard the estimate. */
  canWrite: boolean
  /** Whether an accept/discard request is in flight. */
  busy: boolean
  /** Whether the last accept/discard failed. */
  failed: boolean
  /** Promotes the estimate to the user's own decision. */
  onAccept: () => void
  /** Throws the estimate away for good. */
  onDiscard: () => void
}

/**
 * The banner shown above an estimated location: a badge naming it an estimate, a
 * one-line explanation of where it came from, and — for an editor — the two ways
 * out of it.
 *
 * An estimated location that looks identical to a real one is a lie the app tells
 * the user, so this is a labelled badge and a sentence rather than a subtler
 * shade: colour alone says nothing to a screen reader, and a pin that merely
 * looks a bit different is not a claim anyone would read as "we guessed this".
 *
 * The two actions are deliberately not symmetric in tone. Accepting is a normal
 * confirmation. Discarding is permanent in a way worth knowing about — the
 * backfill will not offer the estimate again — so its help text says so rather
 * than leaving the user to discover it by it never coming back.
 */
function EstimatedLocation({
  canWrite,
  busy,
  failed,
  onAccept,
  onDiscard,
}: EstimatedLocationProps) {
  const { t } = useTranslation()

  return (
    <div className="mb-1">
      <span className="d-inline-flex flex-wrap align-items-baseline gap-1">
        <span
          className="badge text-bg-secondary flex-shrink-0"
          title={t('photo.metadata.locationEstimatedTitle')}
        >
          {t('photo.metadata.locationEstimatedMarker')}
        </span>
        <span className="small text-secondary">{t('photo.metadata.locationEstimatedHelp')}</span>
      </span>
      {canWrite && (
        <div className="d-flex flex-wrap gap-2 mt-1">
          <Button
            type="button"
            variant="outline-success"
            size="sm"
            className="kukatko-tap-target"
            disabled={busy}
            title={t('photo.metadata.acceptLocationHelp')}
            onClick={onAccept}
          >
            {t('photo.metadata.acceptLocation')}
          </Button>
          <Button
            type="button"
            variant="outline-secondary"
            size="sm"
            className="kukatko-tap-target"
            disabled={busy}
            title={t('photo.metadata.discardLocationHelp')}
            onClick={onDiscard}
          >
            {t('photo.metadata.discardLocation')}
          </Button>
        </div>
      )}
      {failed && (
        <div className="text-danger small mt-1">{t('photo.metadata.locationActionFailed')}</div>
      )}
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
 * form's coordinate field and Leaflet map picker.
 *
 * The form also carries the IPTC/XMP credit block — subject, artist, copyright,
 * licence, keywords (as chips, see {@link KeywordsInput}) and the "this is a scan"
 * flag — in a sub-section that starts collapsed, saved by the very same PATCH. It
 * is where a scanned or inherited photo is credited at all: EXIF and the importers
 * know nothing about who took a print from 1950.
 *
 * Camera/lens/EXIF and the uploader are deliberately absent — they live in the
 * collapsed technical details. All text is i18n.
 */
export function MetadataPanel({ photo, canWrite, onUpdated }: MetadataPanelProps) {
  const { t, i18n } = useTranslation()
  const [editing, setEditing] = useState(false)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState(false)
  // Accepting or discarding an estimated location is its own one-click request,
  // separate from the edit form's save: both are answers to a question the app
  // asked, not an edit the user came here to make.
  const [locationBusy, setLocationBusy] = useState(false)
  const [locationFailed, setLocationFailed] = useState(false)

  // The photo's own values, as the form renders them. A field still equal to its
  // pristine value is left out of the PATCH entirely — see buildPatch.
  const pristineTakenAt = toLocalInput(photo.taken_at)
  const pristineEstimated = photo.taken_at_estimated === true
  const pristineNote = photo.taken_at_note ?? ''
  const pristineCoords = initialCoordText(photo)
  const pristineSubject = photo.subject ?? ''
  const pristineArtist = photo.artist ?? ''
  const pristineCopyright = photo.copyright ?? ''
  const pristineLicense = photo.license ?? ''
  const pristineScan = photo.scan === true
  // Read from the photo rather than form state: the estimate is a fact about the
  // stored row, and accepting or discarding it round-trips through the API, so it
  // is never something the open form is holding an unsaved opinion about.
  const isLocationEstimated = photo.location_source === 'estimate'
  const pristineKeywords = useMemo(() => splitKeywords(photo.keywords), [photo.keywords])

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
  // The IPTC/XMP credit block. It is secondary to the caption, so it lives behind a
  // disclosure that starts closed — typing a title must not mean scrolling past six
  // more inputs first. Keywords are held as the chip list the field edits and only
  // joined back into the stored comma-separated string on save.
  const [creditsOpen, setCreditsOpen] = useState(false)
  const [subject, setSubject] = useState(pristineSubject)
  const [artist, setArtist] = useState(pristineArtist)
  const [copyright, setCopyright] = useState(pristineCopyright)
  const [license, setLicense] = useState(pristineLicense)
  const [scan, setScan] = useState(pristineScan)
  const [keywords, setKeywords] = useState<string[]>(pristineKeywords)

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
    setSubject(pristineSubject)
    setArtist(pristineArtist)
    setCopyright(pristineCopyright)
    setLicense(pristineLicense)
    setScan(pristineScan)
    setKeywords(pristineKeywords)
    setCreditsOpen(false)
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
   *
   * The credit fields follow the same only-when-changed rule, for the same reason:
   * the form normalises what it holds (each value trimmed, the keywords rejoined as
   * "beach, sunset"), so resending an untouched field would rewrite the source
   * file's own wording on a save the user made for something else. A field the user
   * did empty is sent as "", which clears it.
   */
  function buildPatch(): PhotoMetadataUpdate {
    const patch: PhotoMetadataUpdate = {
      title: title.trim(),
      description,
      notes,
      ai_note: aiNote,
    }
    if (subject.trim() !== pristineSubject) {
      patch.subject = subject.trim()
    }
    if (artist.trim() !== pristineArtist) {
      patch.artist = artist.trim()
    }
    if (copyright.trim() !== pristineCopyright) {
      patch.copyright = copyright.trim()
    }
    if (license.trim() !== pristineLicense) {
      patch.license = license.trim()
    }
    if (!sameKeywords(keywords, pristineKeywords)) {
      patch.keywords = joinKeywords(keywords)
    }
    if (scan !== pristineScan) {
      patch.scan = scan
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

  /**
   * Answers the estimated-location question with `patch` and feeds the refreshed
   * photo back, so the badge and its buttons disappear on their own once the
   * estimate is gone.
   */
  async function resolveEstimate(patch: PhotoMetadataUpdate) {
    setLocationBusy(true)
    setLocationFailed(false)
    try {
      onUpdated(await updatePhoto(photo.uid, patch))
    } catch {
      setLocationFailed(true)
    } finally {
      setLocationBusy(false)
    }
  }

  /**
   * Accepts the estimate: `location_source` alone, never the coordinates. Echoing
   * lat/lng back would round them to the six decimals the form renders, quietly
   * moving the pin a few centimetres as the price of agreeing with it.
   */
  function acceptEstimate() {
    void resolveEstimate({ location_source: 'manual' })
  }

  /**
   * Discards the estimate. Clearing the coordinates is what the backend records as
   * a decision ("manual" with no location), which is what keeps the backfill from
   * handing the same guess back tomorrow.
   */
  function discardEstimate() {
    void resolveEstimate({ lat: null, lng: null })
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
        {/* The IPTC/XMP credit block, behind a disclosure that starts closed: it
            matters most on scans and inherited photos, where the automatic sources
            know nothing — but it must not bury the common case of typing a title
            and a description under six more inputs. */}
        <div className="mb-2">
          <Button
            type="button"
            variant="link"
            size="sm"
            className="px-0 text-decoration-none"
            aria-expanded={creditsOpen}
            aria-controls={CREDITS_REGION_ID}
            onClick={() => {
              setCreditsOpen(!creditsOpen)
            }}
          >
            <Icon name={creditsOpen ? 'chevron-down' : 'chevron-right'} className="me-1" />
            {t('photo.metadata.credits')}
          </Button>
          {creditsOpen && (
            <div id={CREDITS_REGION_ID} className="mt-2">
              <Form.Group className="mb-2" controlId="photo-subject">
                <Form.Label className="small text-secondary mb-1">
                  {t('photo.metadata.subject')}
                </Form.Label>
                <Form.Control
                  value={subject}
                  maxLength={SUBJECT_MAX}
                  onChange={(event) => {
                    setSubject(event.target.value)
                  }}
                />
              </Form.Group>
              <Form.Group className="mb-2" controlId="photo-artist">
                <Form.Label className="small text-secondary mb-1">
                  {t('photo.metadata.artist')}
                </Form.Label>
                <Form.Control
                  value={artist}
                  maxLength={ARTIST_MAX}
                  placeholder={t('photo.metadata.artistPlaceholder')}
                  onChange={(event) => {
                    setArtist(event.target.value)
                  }}
                />
              </Form.Group>
              <Form.Group className="mb-2" controlId="photo-copyright">
                <Form.Label className="small text-secondary mb-1">
                  {t('photo.metadata.copyright')}
                </Form.Label>
                <Form.Control
                  value={copyright}
                  maxLength={COPYRIGHT_MAX}
                  onChange={(event) => {
                    setCopyright(event.target.value)
                  }}
                />
              </Form.Group>
              <Form.Group className="mb-2" controlId="photo-license">
                <Form.Label className="small text-secondary mb-1">
                  {t('photo.metadata.license')}
                </Form.Label>
                <Form.Control
                  value={license}
                  maxLength={LICENSE_MAX}
                  onChange={(event) => {
                    setLicense(event.target.value)
                  }}
                />
              </Form.Group>
              <KeywordsInput
                id="photo-keywords"
                label={t('photo.metadata.keywords')}
                value={keywords}
                maxRunes={KEYWORDS_MAX}
                onChange={setKeywords}
              />
              <Form.Group className="mb-2" controlId="photo-scan">
                <Form.Check
                  type="checkbox"
                  label={t('photo.metadata.scan')}
                  checked={scan}
                  onChange={(event) => {
                    setScan(event.target.checked)
                  }}
                />
                <Form.Text className="text-secondary d-block">
                  {t('photo.metadata.scanHelp')}
                </Form.Text>
              </Form.Group>
            </div>
          )}
        </div>
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
        {isLocationEstimated && (
          <EstimatedLocation
            canWrite={canWrite}
            busy={locationBusy}
            failed={locationFailed}
            onAccept={acceptEstimate}
            onDiscard={discardEstimate}
          />
        )}
        <PhotoLocation photo={photo} canWrite={false} onUpdated={onUpdated} />
      </div>
    </div>
  )
}
