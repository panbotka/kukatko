import type { TFunction } from 'i18next'
import { useEffect, useMemo, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import Col from 'react-bootstrap/Col'
import Form from 'react-bootstrap/Form'
import Modal from 'react-bootstrap/Modal'
import Row from 'react-bootstrap/Row'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'

import { useAuth } from '../../auth/AuthContext'
import { formatDate } from '../../lib/format'
import { pendingName, pendingOptions, pendingValue } from '../../lib/pendingCreate'
import { DECADE_YEARS, decadeOf } from '../../lib/period'
import {
  formatDecade,
  formatTakenPeriod,
  TAKEN_PRECISIONS,
  type TakenPrecision,
} from '../../lib/takenDate'
import { ApiError } from '../../services/auth'
import { type BulkOperations, type BulkResult, bulkUpdatePhotos } from '../../services/bulk'
import {
  type AlbumCount,
  createAlbum,
  createLabel,
  fetchAlbums,
  fetchLabels,
  type LabelCount,
} from '../../services/organize'
import { MultiSelect, type MultiSelectOption } from '../MultiSelect'
import { PlaceSearch } from '../photo/PlaceSearch'

/**
 * What a successful apply actually did: the operations that were sent (after
 * pending creations resolved to real UIDs) and the backend's per-photo results.
 * Handed to {@link BulkEditModalProps.onDone} so a page can update its list in
 * place — e.g. /expand removes just the photos that joined the collection.
 */
export interface BulkEditOutcome {
  /** The operation set the batch was applied with. */
  operations: BulkOperations
  /** The per-photo results and aggregate counts the endpoint returned. */
  result: BulkResult
}

/**
 * Form values pre-selected when the dialog opens, for a page where one target
 * is the obvious default — /expand pre-fills the collection being expanded, so
 * "add these to it" is one click while everything else stays editable.
 */
export interface BulkEditPrefill {
  /** Album UIDs pre-selected in the add-to-albums field. */
  addAlbums?: string[]
  /** Label UIDs pre-selected in the add-labels field. */
  addLabels?: string[]
}

/** Props for {@link BulkEditModal}. */
export interface BulkEditModalProps {
  /** Whether the modal is visible. */
  show: boolean
  /** The selected photo UIDs the operations apply to. */
  photoUids: string[]
  /** Dismisses the modal without applying (also used to close the result view). */
  onHide: () => void
  /** Called after a successful apply, so the caller can clear the selection. */
  onDone: (outcome?: BulkEditOutcome) => void
  /**
   * Pre-selected form values applied every time the dialog opens. Keep the
   * object referentially stable (memoised) — a new object per render would
   * reset the form mid-edit.
   */
  prefill?: BulkEditPrefill
}

/** Fetch lifecycle of the album/label option lists. */
type LoadState =
  | { status: 'loading' }
  | { status: 'error' }
  | { status: 'ready'; albums: AlbumCount[]; labels: LabelCount[] }

/** A no-change / set / clear selector for an editable field. */
type SetClearMode = '' | 'set' | 'clear'
/** A no-change / yes / no selector for a boolean field. */
type BoolMode = '' | 'true' | 'false'

/** Mutable form state, reset every time the modal opens. */
interface FormState {
  addAlbums: string[]
  removeAlbums: string[]
  addLabels: string[]
  removeLabels: string[]
  descriptionMode: SetClearMode
  description: string
  /**
   * The grain a new capture date is stated at, `''` for no change. It is the
   * field's mode *and* the precision sent, because how much of a date you can
   * state and how much of it may be shown are the same question.
   */
  takenAtMode: '' | TakenPrecision
  /**
   * The date itself, shaped by {@link FormState.takenAtMode}: `1974-06-14`,
   * `1974-06`, `1974`, or a decade's first year `1970`. Reset whenever the mode
   * changes — a day left over in a year field is not a year.
   */
  takenAt: string
  locationMode: SetClearMode
  lat: string
  lng: string
  archiveMode: '' | 'archive' | 'unarchive'
  /**
   * Library visibility: hide the photos from the grid/timeline/search or bring
   * them back. It is the operation this dialog exists for on the feature's own
   * terms — the real use is fifty document scans at once.
   */
  hiddenMode: '' | 'hide' | 'unhide'
  favoriteMode: BoolMode
}

const EMPTY_FORM: FormState = {
  addAlbums: [],
  removeAlbums: [],
  addLabels: [],
  removeLabels: [],
  descriptionMode: '',
  description: '',
  takenAtMode: '',
  takenAt: '',
  locationMode: '',
  lat: '',
  lng: '',
  archiveMode: '',
  hiddenMode: '',
  favoriteMode: '',
}

/**
 * Above this many selected photos an apply is not a slip of the mouse but a
 * catalog-wide edit, so it takes a second, explicit confirmation. Below it the
 * blast radius is small enough to undo by hand.
 */
const LARGE_SELECTION = 50

/** The shape a date value must have at each grain, mirroring the backend's. */
const TAKEN_AT_PATTERNS: Record<TakenPrecision, RegExp> = {
  day: /^\d{4}-\d{2}-\d{2}$/,
  month: /^\d{4}-\d{2}$/,
  year: /^\d{4}$/,
  decade: /^\d{4}$/,
}

/** The earliest year the date field offers: photography is not older than this. */
const FIRST_PHOTO_YEAR = 1826

/**
 * The first instant of the period the date field states, as the ISO stamp the
 * period formatters read, or `''` when the field does not hold a value of the
 * shape its grain requires. It is both the validity check and the value used to
 * render the period back to the reader, so the two can never disagree.
 */
function takenAtInstant(mode: TakenPrecision, value: string): string {
  if (!TAKEN_AT_PATTERNS[mode].test(value)) {
    return ''
  }
  switch (mode) {
    case 'day':
      return `${value}T00:00:00Z`
    case 'month':
      return `${value}-01T00:00:00Z`
    default:
      return `${value}-01-01T00:00:00Z`
  }
}

/**
 * The date the form states, in the words the summary and the confirmation use:
 * "14. 6. 1974", "červen 1974", "rok 1974", "léta 1970–1979". The grain is named
 * — a bare "1974" beside "Set the capture date" would leave the reader guessing
 * whether they are about to stamp a year or a New Year's Day on fifty photos.
 * Returns `''` for an unusable value, so a caller can treat it as "nothing to
 * say yet".
 */
function takenAtPhrase(mode: TakenPrecision, value: string, t: TFunction, locale: string): string {
  const instant = takenAtInstant(mode, value)
  if (instant === '') {
    return ''
  }
  switch (mode) {
    case 'day':
      return t('bulkEdit.takenAt.phrase.day', { value: formatDate(instant, locale) })
    case 'month':
      return t('bulkEdit.takenAt.phrase.month', {
        value: formatTakenPeriod(instant, 'month', t, locale),
      })
    case 'year':
      return t('bulkEdit.takenAt.phrase.year', { value })
    default:
      return t('bulkEdit.takenAt.phrase.decade', { value: formatDecade(Number(value), t) })
  }
}

/**
 * Builds the {@link BulkOperations} payload from the form, or returns the
 * `'invalid-coords'` / `'invalid-taken-at'` / `'empty'` sentinel when
 * set-location coordinates do not parse, the capture date does not match the
 * grain it was stated at, or no operation was chosen. Set/clear pairs map to the
 * distinct wire keys the backend expects; the whole batch stays a single
 * `POST /photos/bulk`.
 */
function buildOperations(
  form: FormState,
): BulkOperations | 'invalid-coords' | 'invalid-taken-at' | 'empty' {
  const ops: BulkOperations = {}
  if (form.addAlbums.length > 0) {
    ops.add_to_albums = form.addAlbums
  }
  if (form.removeAlbums.length > 0) {
    ops.remove_from_albums = form.removeAlbums
  }
  if (form.addLabels.length > 0) {
    ops.add_labels = form.addLabels
  }
  if (form.removeLabels.length > 0) {
    ops.remove_labels = form.removeLabels
  }
  if (form.descriptionMode === 'set') {
    ops.set_description = form.description
  } else if (form.descriptionMode === 'clear') {
    ops.clear_description = true
  }
  if (form.takenAtMode !== '') {
    if (takenAtInstant(form.takenAtMode, form.takenAt) === '') {
      return 'invalid-taken-at'
    }
    ops.set_taken_at = { precision: form.takenAtMode, value: form.takenAt }
  }
  if (form.locationMode === 'set') {
    const lat = Number(form.lat)
    const lng = Number(form.lng)
    if (
      form.lat.trim() === '' ||
      form.lng.trim() === '' ||
      Number.isNaN(lat) ||
      Number.isNaN(lng)
    ) {
      return 'invalid-coords'
    }
    ops.set_location = { lat, lng }
  } else if (form.locationMode === 'clear') {
    ops.clear_location = true
  }
  if (form.archiveMode === 'archive') {
    ops.archive = true
  } else if (form.archiveMode === 'unarchive') {
    ops.unarchive = true
  }
  if (form.hiddenMode === 'hide') {
    ops.hide = true
  } else if (form.hiddenMode === 'unhide') {
    ops.unhide = true
  }
  if (form.favoriteMode !== '') {
    ops.set_favorite = form.favoriteMode === 'true'
  }
  return Object.keys(ops).length === 0 ? 'empty' : ops
}

/**
 * A modal bulk-edit dialog: applies a set of metadata operations (add/remove
 * albums, add/remove labels, set/clear description, set/clear location,
 * archive, hide from the library, favorite) to a multi-photo grid selection in
 * one `POST /photos/bulk` call, applied by the backend in one transaction.
 *
 * The form is grouped into four sections — Organize, Metadata, Location, Flags —
 * and each album/label field is a searchable {@link MultiSelect}, so a single
 * apply can add several albums and drop several labels at once. Destructive
 * choices (removing from an album or a label, archiving) are painted in the danger
 * key, a running summary states exactly what will happen and to how many photos,
 * and a selection larger than {@link LARGE_SELECTION} must be confirmed before it
 * is sent. Afterwards the per-photo result summary the endpoint returns replaces
 * the form.
 *
 * Only editors/admins reach it (the caller gates the trigger), except the favorite
 * operation which is itself per-user.
 */
export function BulkEditModal({ show, photoUids, onHide, onDone, prefill }: BulkEditModalProps) {
  const { t, i18n } = useTranslation()
  const { canWrite } = useAuth()
  const [load, setLoad] = useState<LoadState>({ status: 'loading' })
  const [form, setForm] = useState<FormState>(EMPTY_FORM)
  const [busy, setBusy] = useState(false)
  const [confirming, setConfirming] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [outcome, setOutcome] = useState<BulkEditOutcome | null>(null)

  useEffect(() => {
    if (!show) {
      return
    }
    const controller = new AbortController()
    setLoad({ status: 'loading' })
    setForm({
      ...EMPTY_FORM,
      addAlbums: prefill?.addAlbums ?? [],
      addLabels: prefill?.addLabels ?? [],
    })
    setConfirming(false)
    setError(null)
    setOutcome(null)
    Promise.all([fetchAlbums(controller.signal), fetchLabels(controller.signal)])
      .then(([albums, labels]) => {
        setLoad({ status: 'ready', albums, labels })
      })
      .catch((err: unknown) => {
        if (err instanceof DOMException && err.name === 'AbortError') {
          return
        }
        setLoad({ status: 'error' })
      })
    return () => {
      controller.abort()
    }
  }, [show, prefill])

  function update(patch: Partial<FormState>) {
    setForm((prev) => ({ ...prev, ...patch }))
    // A confirmation is granted for the operations the reader just read back to
    // themselves; editing the form withdraws it.
    setConfirming(false)
  }

  /**
   * Creates one pending entry and swaps its fresh UID into the form (and the
   * option list), so a later failure retries the batch without re-creating it.
   * Returns the resolved add list.
   */
  async function resolveOne(
    kind: 'album' | 'label',
    values: string[],
    value: string,
    name: string,
  ): Promise<string[]> {
    let uid: string
    if (kind === 'album') {
      const album = await createAlbum({ title: name, description: '', private: false })
      uid = album.uid
      setLoad((prev) =>
        prev.status === 'ready'
          ? { ...prev, albums: [...prev.albums, { ...album, photo_count: 0 }] }
          : prev,
      )
    } else {
      const label = await createLabel({ name, priority: 0 })
      uid = label.uid
      setLoad((prev) =>
        prev.status === 'ready'
          ? { ...prev, labels: [...prev.labels, { ...label, photo_count: 0 }] }
          : prev,
      )
    }
    const resolved = values.map((current) => (current === value ? uid : current))
    setForm((prev) => ({
      ...prev,
      [kind === 'album' ? 'addAlbums' : 'addLabels']: resolved,
    }))
    return resolved
  }

  /**
   * Applies the batch: first creates the albums/labels picked via the create
   * entry (the bulk endpoint only accepts existing identifiers), then submits
   * one `POST /photos/bulk` with the resolved UIDs. A creation failure surfaces
   * the server's message and stops before the batch, with every selection —
   * including the entries created so far, now under their real UIDs — intact
   * for a retry. A batch failure after a creation says so explicitly, so the
   * reader knows the new entries exist and only the assignment is missing.
   */
  async function send(current: FormState) {
    setBusy(true)
    setError(null)
    let created = false
    try {
      let resolved = current
      for (const kind of ['album', 'label'] as const) {
        const key = kind === 'album' ? 'addAlbums' : 'addLabels'
        for (const value of current[key]) {
          const name = pendingName(value)
          if (name === null) {
            continue
          }
          try {
            resolved = { ...resolved, [key]: await resolveOne(kind, resolved[key], value, name) }
            created = true
          } catch (err: unknown) {
            // Duplicate name, permission denied: the server names the problem.
            setError(
              err instanceof ApiError && err.message !== ''
                ? t('bulkEdit.createError', { name, message: err.message })
                : t('bulkEdit.applyError'),
            )
            return
          }
        }
      }
      const ops = buildOperations(resolved)
      if (ops === 'empty' || ops === 'invalid-coords' || ops === 'invalid-taken-at') {
        // Unreachable: apply() validated the same form, and resolving pending
        // entries only swaps values. The guard merely narrows the union.
        return
      }
      try {
        setOutcome({ operations: ops, result: await bulkUpdatePhotos(photoUids, ops) })
      } catch (err: unknown) {
        // The backend rejects a bad batch with a reason the reader can act on
        // (conflicting operations, too many photos); a generic failure would
        // hide it. Anything else — a network drop, a 5xx with no body — falls
        // back.
        const message =
          err instanceof ApiError && err.message !== '' ? err.message : t('bulkEdit.applyError')
        setError(created ? t('bulkEdit.createdButApplyFailed', { message }) : message)
      }
    } finally {
      setBusy(false)
    }
  }

  /** Validates the form, then either asks for confirmation or sends the batch. */
  function apply(skipConfirm: boolean) {
    const ops = buildOperations(form)
    if (ops === 'empty') {
      setError(t('bulkEdit.noOps'))
      return
    }
    if (ops === 'invalid-coords') {
      setError(t('bulkEdit.location.invalid'))
      return
    }
    if (ops === 'invalid-taken-at') {
      setError(t('bulkEdit.takenAt.invalid'))
      return
    }
    // Two things ask for a second look. A batch larger than LARGE_SELECTION is no
    // longer a slip of the mouse. And a capture date is confirmed at any size:
    // it overwrites the one fact the whole library is ordered by, on photos whose
    // old date is not shown in this dialog, so "which date, on how many photos"
    // has to be said out loud before it happens rather than discovered afterwards.
    if (!skipConfirm && (photoUids.length > LARGE_SELECTION || ops.set_taken_at !== undefined)) {
      setError(null)
      setConfirming(true)
      return
    }
    void send(form)
  }

  const takenAtText =
    form.takenAtMode === '' ? '' : takenAtPhrase(form.takenAtMode, form.takenAt, t, i18n.language)

  return (
    <Modal show={show} onHide={onHide} centered scrollable fullscreen="sm-down">
      <Modal.Header closeButton>
        <Modal.Title>
          {outcome ? t('bulkEdit.result.title') : t('bulkEdit.title', { count: photoUids.length })}
        </Modal.Title>
      </Modal.Header>
      <Modal.Body>
        {outcome ? (
          <ResultSummary result={outcome.result} />
        ) : (
          <>
            {error !== null && (
              <Alert variant="danger" className="py-2 kk-text-caption">
                {error}
              </Alert>
            )}
            {load.status === 'loading' && (
              <div className="d-flex justify-content-center py-3">
                <Spinner animation="border" role="status" size="sm">
                  <span className="visually-hidden">{t('bulkEdit.loading')}</span>
                </Spinner>
              </div>
            )}
            {load.status === 'error' && (
              <p className="text-danger kk-text-caption mb-0">{t('bulkEdit.loadError')}</p>
            )}
            {load.status === 'ready' && (
              <>
                <BulkEditForm
                  form={form}
                  albums={load.albums}
                  labels={load.labels}
                  busy={busy}
                  allowCreate={canWrite}
                  onChange={update}
                />
                <PendingChanges
                  form={form}
                  albums={load.albums}
                  labels={load.labels}
                  photoCount={photoUids.length}
                />
                {confirming && (
                  <Alert variant="danger" className="mt-3 mb-0">
                    <p className="mb-2">
                      {takenAtText !== ''
                        ? t('bulkEdit.confirm.takenAt', {
                            date: takenAtText,
                            count: photoUids.length,
                          })
                        : t('bulkEdit.confirm.body', { count: photoUids.length })}
                    </p>
                    <div className="d-flex flex-wrap gap-2">
                      <Button
                        variant="danger"
                        size="sm"
                        disabled={busy}
                        onClick={() => {
                          apply(true)
                        }}
                      >
                        {t('bulkEdit.confirm.apply', { count: photoUids.length })}
                      </Button>
                      <Button
                        variant="outline-light"
                        size="sm"
                        disabled={busy}
                        onClick={() => {
                          setConfirming(false)
                        }}
                      >
                        {t('bulkEdit.confirm.back')}
                      </Button>
                    </div>
                  </Alert>
                )}
              </>
            )}
          </>
        )}
      </Modal.Body>
      <Modal.Footer>
        {outcome ? (
          <Button
            variant="primary"
            onClick={() => {
              onDone(outcome)
            }}
          >
            {t('bulkEdit.result.done')}
          </Button>
        ) : (
          <>
            <Button variant="secondary" onClick={onHide} disabled={busy}>
              {t('bulkEdit.cancel')}
            </Button>
            <Button
              variant="primary"
              disabled={busy || load.status !== 'ready'}
              onClick={() => {
                apply(false)
              }}
            >
              {busy && <Spinner animation="border" size="sm" className="me-2" />}
              {t('bulkEdit.apply')}
            </Button>
          </>
        )}
      </Modal.Footer>
    </Modal>
  )
}

/** A titled group of related fields inside the form. */
function Section({
  title,
  children,
  className,
}: {
  title: string
  children: React.ReactNode
  className?: string
}) {
  return (
    <section className={className ?? 'mb-4'}>
      <h2 className="kk-text-eyebrow text-secondary mb-2">{title}</h2>
      {children}
    </section>
  )
}

/** Maps an album to a {@link MultiSelect} option, counted by its photo total. */
function albumOption(album: AlbumCount): MultiSelectOption {
  return { value: album.uid, label: album.title, count: album.photo_count }
}

/** Maps a label to a {@link MultiSelect} option, counted by its photo total. */
function labelOption(label: LabelCount): MultiSelectOption {
  return { value: label.uid, label: label.name, count: label.photo_count }
}

/** The editable operation form (albums/labels, description, location, flags). */
function BulkEditForm({
  form,
  albums,
  labels,
  busy,
  allowCreate,
  onChange,
}: {
  form: FormState
  albums: AlbumCount[]
  labels: LabelCount[]
  busy: boolean
  /** Whether the add fields may create entries (the acting user may write). */
  allowCreate: boolean
  onChange: (patch: Partial<FormState>) => void
}) {
  const { t } = useTranslation()
  const albumOptions = useMemo(() => albums.map(albumOption), [albums])
  const labelOptions = useMemo(() => labels.map(labelOption), [labels])
  // Only the add fields see the pending creations: a chip needs its option to
  // read as the name, and offering to create in the same field again would
  // duplicate it — while a remove field must never offer an album or label
  // that does not exist yet.
  const addAlbumOptions = useMemo(
    () => [...albumOptions, ...pendingOptions(form.addAlbums)],
    [albumOptions, form.addAlbums],
  )
  const addLabelOptions = useMemo(
    () => [...labelOptions, ...pendingOptions(form.addLabels)],
    [labelOptions, form.addLabels],
  )
  return (
    <Form>
      <Section title={t('bulkEdit.sections.organize')}>
        <Row className="g-3">
          <Col xs={12} md={6}>
            <MultiSelect
              id="bulk-add-albums"
              label={t('bulkEdit.addAlbums')}
              placeholder={t('bulkEdit.filterAlbums')}
              options={addAlbumOptions}
              selected={form.addAlbums}
              disabled={busy}
              onChange={(addAlbums) => {
                onChange({ addAlbums })
              }}
              onCreate={
                allowCreate
                  ? (name) => {
                      onChange({ addAlbums: [...form.addAlbums, pendingValue(name)] })
                    }
                  : undefined
              }
            />
          </Col>
          <Col xs={12} md={6}>
            <MultiSelect
              id="bulk-remove-albums"
              label={t('bulkEdit.removeAlbums')}
              placeholder={t('bulkEdit.filterAlbums')}
              options={albumOptions}
              selected={form.removeAlbums}
              disabled={busy}
              destructive
              onChange={(removeAlbums) => {
                onChange({ removeAlbums })
              }}
            />
          </Col>
          <Col xs={12} md={6}>
            <MultiSelect
              id="bulk-add-labels"
              label={t('bulkEdit.addLabels')}
              placeholder={t('bulkEdit.filterLabels')}
              options={addLabelOptions}
              selected={form.addLabels}
              disabled={busy}
              onChange={(addLabels) => {
                onChange({ addLabels })
              }}
              onCreate={
                allowCreate
                  ? (name) => {
                      onChange({ addLabels: [...form.addLabels, pendingValue(name)] })
                    }
                  : undefined
              }
            />
          </Col>
          <Col xs={12} md={6}>
            <MultiSelect
              id="bulk-remove-labels"
              label={t('bulkEdit.removeLabels')}
              placeholder={t('bulkEdit.filterLabels')}
              options={labelOptions}
              selected={form.removeLabels}
              disabled={busy}
              destructive
              onChange={(removeLabels) => {
                onChange({ removeLabels })
              }}
            />
          </Col>
        </Row>
      </Section>

      <Section title={t('bulkEdit.sections.metadata')}>
        <Form.Group controlId="bulk-description-mode">
          <Form.Label className="kk-text-caption mb-1">
            {t('bulkEdit.description.label')}
          </Form.Label>
          <Form.Select
            value={form.descriptionMode}
            disabled={busy}
            onChange={(e) => {
              onChange({ descriptionMode: e.target.value as SetClearMode })
            }}
          >
            <option value="">{t('bulkEdit.description.noChange')}</option>
            <option value="set">{t('bulkEdit.description.set')}</option>
            <option value="clear">{t('bulkEdit.description.clear')}</option>
          </Form.Select>
        </Form.Group>
        {form.descriptionMode === 'set' && (
          <Form.Control
            className="mt-2"
            as="textarea"
            rows={2}
            value={form.description}
            disabled={busy}
            aria-label={t('bulkEdit.description.placeholder')}
            placeholder={t('bulkEdit.description.placeholder')}
            onChange={(e) => {
              onChange({ description: e.target.value })
            }}
          />
        )}
        <TakenAtField form={form} busy={busy} onChange={onChange} />
      </Section>

      <Section title={t('bulkEdit.sections.location')}>
        <Form.Group controlId="bulk-location-mode">
          <Form.Label className="kk-text-caption mb-1">{t('bulkEdit.location.label')}</Form.Label>
          <Form.Select
            value={form.locationMode}
            disabled={busy}
            onChange={(e) => {
              onChange({ locationMode: e.target.value as SetClearMode })
            }}
          >
            <option value="">{t('bulkEdit.location.noChange')}</option>
            <option value="set">{t('bulkEdit.location.set')}</option>
            <option value="clear">{t('bulkEdit.location.clear')}</option>
          </Form.Select>
        </Form.Group>
        {form.locationMode === 'set' && (
          <>
            {/* The same picker as the photo detail's location editor: naming the
                place is usually easier than knowing its numbers, and it just
                fills the two fields below. */}
            <PlaceSearch
              id="bulk-place-search"
              disabled={busy}
              onPick={(place) => {
                onChange({ lat: String(place.lat), lng: String(place.lng) })
              }}
            />
            <Row className="g-2 mt-1">
              <Col xs={6}>
                <Form.Control
                  type="number"
                  step="any"
                  value={form.lat}
                  disabled={busy}
                  aria-label={t('bulkEdit.location.lat')}
                  placeholder={t('bulkEdit.location.lat')}
                  onChange={(e) => {
                    onChange({ lat: e.target.value })
                  }}
                />
              </Col>
              <Col xs={6}>
                <Form.Control
                  type="number"
                  step="any"
                  value={form.lng}
                  disabled={busy}
                  aria-label={t('bulkEdit.location.lng')}
                  placeholder={t('bulkEdit.location.lng')}
                  onChange={(e) => {
                    onChange({ lng: e.target.value })
                  }}
                />
              </Col>
            </Row>
          </>
        )}
      </Section>

      <Section title={t('bulkEdit.sections.flags')} className="mb-0">
        <Row className="g-3">
          <Col xs={12} md={4}>
            <Form.Group controlId="bulk-archive">
              {/* Archiving is the one destructive flag: it takes the photos out of
                  the library. Only the archive choice — not unarchive — is toned. */}
              <Form.Label
                className={`kk-text-caption mb-1 ${
                  form.archiveMode === 'archive' ? 'text-danger' : ''
                }`}
              >
                {t('bulkEdit.archive.label')}
              </Form.Label>
              <Form.Select
                className={form.archiveMode === 'archive' ? 'border-danger' : ''}
                value={form.archiveMode}
                disabled={busy}
                onChange={(e) => {
                  onChange({ archiveMode: e.target.value as FormState['archiveMode'] })
                }}
              >
                <option value="">{t('bulkEdit.archive.noChange')}</option>
                <option value="archive">{t('bulkEdit.archive.archive')}</option>
                <option value="unarchive">{t('bulkEdit.archive.unarchive')}</option>
              </Form.Select>
            </Form.Group>
          </Col>
          <Col xs={12} md={4}>
            <Form.Group controlId="bulk-hidden">
              {/* Hiding is not archiving and is deliberately not toned danger:
                  nothing is deleted, the photos stay in their albums and labels,
                  and the hint says how to list them again. */}
              <Form.Label className="kk-text-caption mb-1">{t('bulkEdit.hidden.label')}</Form.Label>
              <Form.Select
                value={form.hiddenMode}
                disabled={busy}
                onChange={(e) => {
                  onChange({ hiddenMode: e.target.value as FormState['hiddenMode'] })
                }}
              >
                <option value="">{t('bulkEdit.hidden.noChange')}</option>
                <option value="hide">{t('bulkEdit.hidden.hide')}</option>
                <option value="unhide">{t('bulkEdit.hidden.unhide')}</option>
              </Form.Select>
              <Form.Text className="kk-text-caption">{t('bulkEdit.hidden.hint')}</Form.Text>
            </Form.Group>
          </Col>
          <Col xs={12} md={4}>
            <Form.Group controlId="bulk-favorite">
              <Form.Label className="kk-text-caption mb-1">
                {t('bulkEdit.favorite.label')}
              </Form.Label>
              <Form.Select
                value={form.favoriteMode}
                disabled={busy}
                onChange={(e) => {
                  onChange({ favoriteMode: e.target.value as BoolMode })
                }}
              >
                <option value="">{t('bulkEdit.favorite.noChange')}</option>
                <option value="true">{t('bulkEdit.favorite.yes')}</option>
                <option value="false">{t('bulkEdit.favorite.no')}</option>
              </Form.Select>
            </Form.Group>
          </Col>
        </Row>
      </Section>
    </Form>
  )
}

/**
 * The capture-date field: the grain first, then a control shaped for that grain.
 *
 * The grain leads because it is the actual decision. A box of scans is dated
 * "1974" or "the seventies" far more often than to the day, and a form that
 * opened with a date picker would make the honest answer the awkward one — the
 * reason those photos carry the scanner's own date in the first place. Each
 * grain then gets the narrowest control that can state it, so an unknowable day
 * cannot be typed in by accident: a decade is a list, a year a bounded number, a
 * month and a day their native pickers.
 *
 * Nothing here writes to the files. The originals and their EXIF are untouched;
 * this is the catalogue's own date.
 */
function TakenAtField({
  form,
  busy,
  onChange,
}: {
  form: FormState
  busy: boolean
  onChange: (patch: Partial<FormState>) => void
}) {
  const { t } = useTranslation()
  // Newest first: the library's own decades are far likelier than the 1830s.
  const decades = useMemo(() => {
    const newest = decadeOf(new Date().getUTCFullYear())
    const out: number[] = []
    for (let decade = newest; decade >= FIRST_PHOTO_YEAR; decade -= DECADE_YEARS) {
      out.push(decade)
    }
    return out
  }, [])
  const thisYear = new Date().getUTCFullYear()

  return (
    <>
      <Form.Group controlId="bulk-taken-at-mode" className="mt-3">
        <Form.Label className="kk-text-caption mb-1">{t('bulkEdit.takenAt.label')}</Form.Label>
        <Form.Select
          value={form.takenAtMode}
          disabled={busy}
          onChange={(e) => {
            // The value's shape follows the grain, so a leftover from the previous
            // grain ("1974-06-14" in a year field) is cleared rather than reshaped.
            onChange({ takenAtMode: e.target.value as FormState['takenAtMode'], takenAt: '' })
          }}
        >
          <option value="">{t('bulkEdit.takenAt.noChange')}</option>
          {TAKEN_PRECISIONS.map((precision) => (
            <option key={precision} value={precision}>
              {t(`bulkEdit.takenAt.grain.${precision}` as const)}
            </option>
          ))}
        </Form.Select>
        <Form.Text className="kk-text-caption">{t('bulkEdit.takenAt.hint')}</Form.Text>
      </Form.Group>
      {form.takenAtMode === 'day' && (
        <Form.Control
          className="mt-2"
          type="date"
          value={form.takenAt}
          disabled={busy}
          aria-label={t('bulkEdit.takenAt.grain.day')}
          onChange={(e) => {
            onChange({ takenAt: e.target.value })
          }}
        />
      )}
      {form.takenAtMode === 'month' && (
        <Form.Control
          className="mt-2"
          type="month"
          value={form.takenAt}
          disabled={busy}
          aria-label={t('bulkEdit.takenAt.grain.month')}
          placeholder="1974-06"
          onChange={(e) => {
            onChange({ takenAt: e.target.value })
          }}
        />
      )}
      {form.takenAtMode === 'year' && (
        <Form.Control
          className="mt-2"
          type="number"
          min={FIRST_PHOTO_YEAR}
          max={thisYear}
          value={form.takenAt}
          disabled={busy}
          aria-label={t('bulkEdit.takenAt.grain.year')}
          placeholder="1974"
          onChange={(e) => {
            onChange({ takenAt: e.target.value })
          }}
        />
      )}
      {form.takenAtMode === 'decade' && (
        <Form.Select
          className="mt-2"
          value={form.takenAt}
          disabled={busy}
          aria-label={t('bulkEdit.takenAt.grain.decade')}
          onChange={(e) => {
            onChange({ takenAt: e.target.value })
          }}
        >
          <option value="">{t('bulkEdit.takenAt.pickDecade')}</option>
          {decades.map((decade) => (
            <option key={decade} value={String(decade)}>
              {formatDecade(decade, t)}
            </option>
          ))}
        </Form.Select>
      )}
    </>
  )
}

/** One line of the pending-change summary. */
interface ChangeLine {
  /** Stable React key. */
  id: string
  /** The already-translated sentence shown to the reader. */
  text: string
  /** Whether the change destroys something (a membership, a label, visibility). */
  destructive: boolean
}

/**
 * The running summary of what Apply will do, and to how many photos. It is the
 * one place the whole batch is stated in prose — the fields above each show only
 * their own slice — so nobody has to reconstruct the effect from eight controls.
 */
function PendingChanges({
  form,
  albums,
  labels,
  photoCount,
}: {
  form: FormState
  albums: AlbumCount[]
  labels: LabelCount[]
  photoCount: number
}) {
  const { t, i18n } = useTranslation()

  const lines: ChangeLine[] = []
  // A pending creation reads as its name — the entry will exist by the time
  // the batch runs, so the prose can already state it plainly.
  const albumNames = (uids: string[]) =>
    uids
      .map((uid) => pendingName(uid) ?? albums.find((album) => album.uid === uid)?.title ?? uid)
      .join(', ')
  const labelNames = (uids: string[]) =>
    uids
      .map((uid) => pendingName(uid) ?? labels.find((label) => label.uid === uid)?.name ?? uid)
      .join(', ')

  if (form.addAlbums.length > 0) {
    lines.push({
      id: 'addAlbums',
      text: t('bulkEdit.summary.addAlbums', { names: albumNames(form.addAlbums) }),
      destructive: false,
    })
  }
  if (form.removeAlbums.length > 0) {
    lines.push({
      id: 'removeAlbums',
      text: t('bulkEdit.summary.removeAlbums', { names: albumNames(form.removeAlbums) }),
      destructive: true,
    })
  }
  if (form.addLabels.length > 0) {
    lines.push({
      id: 'addLabels',
      text: t('bulkEdit.summary.addLabels', { names: labelNames(form.addLabels) }),
      destructive: false,
    })
  }
  if (form.removeLabels.length > 0) {
    lines.push({
      id: 'removeLabels',
      text: t('bulkEdit.summary.removeLabels', { names: labelNames(form.removeLabels) }),
      destructive: true,
    })
  }
  if (form.descriptionMode === 'set') {
    lines.push({
      id: 'description',
      text: t('bulkEdit.summary.setDescription', { value: form.description }),
      destructive: false,
    })
  } else if (form.descriptionMode === 'clear') {
    lines.push({
      id: 'description',
      text: t('bulkEdit.summary.clearDescription'),
      destructive: true,
    })
  }
  if (form.takenAtMode !== '') {
    const phrase = takenAtPhrase(form.takenAtMode, form.takenAt, t, i18n.language)
    lines.push({
      id: 'takenAt',
      text:
        phrase !== ''
          ? t('bulkEdit.summary.setTakenAt', { date: phrase })
          : t('bulkEdit.summary.setTakenAtPending'),
      // Overwriting a date loses the old one, and the old one may have been the
      // real thing on some of the selection — the one line here that warrants the
      // danger key without deleting anything.
      destructive: true,
    })
  }
  if (form.locationMode === 'set') {
    lines.push({
      id: 'location',
      text: t('bulkEdit.summary.setLocation', { lat: form.lat, lng: form.lng }),
      destructive: false,
    })
  } else if (form.locationMode === 'clear') {
    lines.push({
      id: 'location',
      text: t('bulkEdit.summary.clearLocation'),
      destructive: true,
    })
  }
  if (form.archiveMode !== '') {
    lines.push({
      id: 'archive',
      text:
        form.archiveMode === 'archive'
          ? t('bulkEdit.summary.archive')
          : t('bulkEdit.summary.unarchive'),
      destructive: form.archiveMode === 'archive',
    })
  }
  if (form.hiddenMode !== '') {
    lines.push({
      id: 'hidden',
      text: form.hiddenMode === 'hide' ? t('bulkEdit.summary.hide') : t('bulkEdit.summary.unhide'),
      // Not destructive: nothing is deleted and the photos stay in their albums
      // and labels — the toning is reserved for changes that lose something.
      destructive: false,
    })
  }
  if (form.favoriteMode !== '') {
    lines.push({
      id: 'favorite',
      text:
        form.favoriteMode === 'true'
          ? t('bulkEdit.summary.favorite')
          : t('bulkEdit.summary.unfavorite'),
      destructive: false,
    })
  }

  return (
    <div className="kk-surface p-3 mt-4">
      <h2 className="kk-text-eyebrow text-secondary mb-1">{t('bulkEdit.summary.title')}</h2>
      <p className="kk-text-caption text-secondary mb-2">
        {t('bulkEdit.summary.applies', { count: photoCount })}
      </p>
      <div aria-live="polite">
        {lines.length === 0 ? (
          <p className="kk-text-caption text-secondary mb-0">{t('bulkEdit.summary.none')}</p>
        ) : (
          <ul className="kk-text-caption mb-0 ps-3">
            {lines.map((line) => (
              <li key={line.id} className={line.destructive ? 'text-danger' : ''}>
                {line.text}
                {line.destructive && (
                  <span className="visually-hidden"> {t('bulkEdit.summary.destructive')}</span>
                )}
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  )
}

/** The per-photo result summary shown after a successful apply. */
function ResultSummary({ result }: { result: BulkResult }) {
  const { t } = useTranslation()
  const errored = result.results.filter((r) => r.status === 'error')
  return (
    <>
      <p className="mb-2" aria-live="polite">
        {t('bulkEdit.result.summary', {
          updated: result.counts.updated,
          skipped: result.counts.skipped,
          errored: result.counts.errored,
        })}
      </p>
      {errored.length > 0 && (
        <>
          <p className="kk-text-caption text-secondary mb-1">{t('bulkEdit.result.errorsTitle')}</p>
          <ul className="kk-text-caption mb-0">
            {errored.map((r) => (
              <li key={r.photo_uid}>
                <code>{r.photo_uid}</code>
                {r.error !== undefined && r.error !== '' ? ` — ${r.error}` : ''}
              </li>
            ))}
          </ul>
        </>
      )}
    </>
  )
}
