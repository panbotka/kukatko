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

/** Props for {@link BulkEditModal}. */
export interface BulkEditModalProps {
  /** Whether the modal is visible. */
  show: boolean
  /** The selected photo UIDs the operations apply to. */
  photoUids: string[]
  /** Dismisses the modal without applying (also used to close the result view). */
  onHide: () => void
  /** Called after a successful apply, so the caller can clear the selection. */
  onDone: () => void
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
  locationMode: SetClearMode
  lat: string
  lng: string
  privateMode: BoolMode
  archiveMode: '' | 'archive' | 'unarchive'
  favoriteMode: BoolMode
}

const EMPTY_FORM: FormState = {
  addAlbums: [],
  removeAlbums: [],
  addLabels: [],
  removeLabels: [],
  descriptionMode: '',
  description: '',
  locationMode: '',
  lat: '',
  lng: '',
  privateMode: '',
  archiveMode: '',
  favoriteMode: '',
}

/**
 * Above this many selected photos an apply is not a slip of the mouse but a
 * catalog-wide edit, so it takes a second, explicit confirmation. Below it the
 * blast radius is small enough to undo by hand.
 */
const LARGE_SELECTION = 50

/**
 * An album or label picked via the multi-select's create entry is selected as
 * its name behind this prefix until Apply creates it — real UIDs are short
 * base32 strings and never carry a colon, so the two cannot collide. Deferring
 * creation to Apply means cancelling the dialog never leaves an empty album or
 * label behind: the bulk endpoint only accepts existing identifiers, so Apply
 * first creates the pending entries, swaps their fresh UIDs in, and only then
 * submits the batch.
 */
const CREATE_PREFIX = 'create:'

/** Encodes a not-yet-existing entry name as a multi-select value. */
function pendingValue(name: string): string {
  return CREATE_PREFIX + name
}

/** Decodes a pending-creation value back to its name; null for a real UID. */
function pendingName(value: string): string | null {
  return value.startsWith(CREATE_PREFIX) ? value.slice(CREATE_PREFIX.length) : null
}

/** Synthetic options for pending creations, so their chips read as the name. */
function pendingOptions(selected: string[]): MultiSelectOption[] {
  return selected.flatMap((value) => {
    const name = pendingName(value)
    return name === null ? [] : [{ value, label: name }]
  })
}

/**
 * Builds the {@link BulkOperations} payload from the form, or returns the
 * `'invalid-coords'` / `'empty'` sentinel when set-location coordinates do not
 * parse or no operation was chosen. Set/clear pairs map to the distinct wire
 * keys the backend expects; the whole batch stays a single `POST /photos/bulk`.
 */
function buildOperations(form: FormState): BulkOperations | 'invalid-coords' | 'empty' {
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
  if (form.privateMode !== '') {
    ops.set_private = form.privateMode === 'true'
  }
  if (form.archiveMode === 'archive') {
    ops.archive = true
  } else if (form.archiveMode === 'unarchive') {
    ops.unarchive = true
  }
  if (form.favoriteMode !== '') {
    ops.set_favorite = form.favoriteMode === 'true'
  }
  return Object.keys(ops).length === 0 ? 'empty' : ops
}

/**
 * A modal bulk-edit dialog: applies a set of metadata operations (add/remove
 * albums, add/remove labels, set/clear description, set/clear location, private,
 * archive, favorite) to a multi-photo grid selection in one `POST /photos/bulk`
 * call, applied by the backend in one transaction.
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
export function BulkEditModal({ show, photoUids, onHide, onDone }: BulkEditModalProps) {
  const { t } = useTranslation()
  const { canWrite } = useAuth()
  const [load, setLoad] = useState<LoadState>({ status: 'loading' })
  const [form, setForm] = useState<FormState>(EMPTY_FORM)
  const [busy, setBusy] = useState(false)
  const [confirming, setConfirming] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [result, setResult] = useState<BulkResult | null>(null)

  useEffect(() => {
    if (!show) {
      return
    }
    const controller = new AbortController()
    setLoad({ status: 'loading' })
    setForm(EMPTY_FORM)
    setConfirming(false)
    setError(null)
    setResult(null)
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
  }, [show])

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
      if (ops === 'empty' || ops === 'invalid-coords') {
        // Unreachable: apply() validated the same form, and resolving pending
        // entries only swaps values. The guard merely narrows the union.
        return
      }
      try {
        setResult(await bulkUpdatePhotos(photoUids, ops))
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
    if (!skipConfirm && photoUids.length > LARGE_SELECTION) {
      setError(null)
      setConfirming(true)
      return
    }
    void send(form)
  }

  return (
    <Modal show={show} onHide={onHide} centered scrollable fullscreen="sm-down">
      <Modal.Header closeButton>
        <Modal.Title>
          {result ? t('bulkEdit.result.title') : t('bulkEdit.title', { count: photoUids.length })}
        </Modal.Title>
      </Modal.Header>
      <Modal.Body>
        {result ? (
          <ResultSummary result={result} />
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
                      {t('bulkEdit.confirm.body', { count: photoUids.length })}
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
        {result ? (
          <Button
            variant="primary"
            onClick={() => {
              onDone()
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
        )}
      </Section>

      <Section title={t('bulkEdit.sections.flags')} className="mb-0">
        <Row className="g-3">
          <Col xs={12} md={4}>
            <Form.Group controlId="bulk-private">
              <Form.Label className="kk-text-caption mb-1">
                {t('bulkEdit.private.label')}
              </Form.Label>
              <Form.Select
                value={form.privateMode}
                disabled={busy}
                onChange={(e) => {
                  onChange({ privateMode: e.target.value as BoolMode })
                }}
              >
                <option value="">{t('bulkEdit.private.noChange')}</option>
                <option value="true">{t('bulkEdit.private.yes')}</option>
                <option value="false">{t('bulkEdit.private.no')}</option>
              </Form.Select>
            </Form.Group>
          </Col>
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
  const { t } = useTranslation()

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
  if (form.privateMode !== '') {
    lines.push({
      id: 'private',
      text:
        form.privateMode === 'true'
          ? t('bulkEdit.summary.private')
          : t('bulkEdit.summary.notPrivate'),
      destructive: false,
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
