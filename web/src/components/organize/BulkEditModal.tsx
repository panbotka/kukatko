import { useEffect, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import Col from 'react-bootstrap/Col'
import Form from 'react-bootstrap/Form'
import Modal from 'react-bootstrap/Modal'
import Row from 'react-bootstrap/Row'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'

import { type BulkOperations, type BulkResult, bulkUpdatePhotos } from '../../services/bulk'
import { type AlbumCount, fetchAlbums, fetchLabels, type LabelCount } from '../../services/organize'

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
  addAlbum: string
  removeAlbum: string
  addLabel: string
  removeLabel: string
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
  addAlbum: '',
  removeAlbum: '',
  addLabel: '',
  removeLabel: '',
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
 * Builds the {@link BulkOperations} payload from the form, or returns the
 * `'invalid-coords'` / `'empty'` sentinel when set-location coordinates do not
 * parse or no operation was chosen. Set/clear pairs map to the distinct wire
 * keys the backend expects.
 */
function buildOperations(form: FormState): BulkOperations | 'invalid-coords' | 'empty' {
  const ops: BulkOperations = {}
  if (form.addAlbum !== '') {
    ops.add_to_albums = [form.addAlbum]
  }
  if (form.removeAlbum !== '') {
    ops.remove_from_albums = [form.removeAlbum]
  }
  if (form.addLabel !== '') {
    ops.add_labels = [form.addLabel]
  }
  if (form.removeLabel !== '') {
    ops.remove_labels = [form.removeLabel]
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
 * A modal bulk-edit toolbar dialog: applies a set of metadata operations
 * (add/remove album, add/remove label, set/clear description, set/clear location,
 * private, archive, favorite) to a multi-photo grid selection in one
 * `POST /photos/bulk` call. It loads the album/label option lists, validates the
 * form (coordinates, at least one change) client-side, then renders the per-photo
 * result summary the endpoint returns. Only editors/admins reach it (the caller
 * gates the trigger), except the favorite operation which is itself per-user.
 */
export function BulkEditModal({ show, photoUids, onHide, onDone }: BulkEditModalProps) {
  const { t } = useTranslation()
  const [load, setLoad] = useState<LoadState>({ status: 'loading' })
  const [form, setForm] = useState<FormState>(EMPTY_FORM)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [result, setResult] = useState<BulkResult | null>(null)

  useEffect(() => {
    if (!show) {
      return
    }
    const controller = new AbortController()
    setLoad({ status: 'loading' })
    setForm(EMPTY_FORM)
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
  }

  async function apply() {
    const ops = buildOperations(form)
    if (ops === 'empty') {
      setError(t('bulkEdit.noOps'))
      return
    }
    if (ops === 'invalid-coords') {
      setError(t('bulkEdit.location.invalid'))
      return
    }
    setBusy(true)
    setError(null)
    try {
      setResult(await bulkUpdatePhotos(photoUids, ops))
    } catch {
      setError(t('bulkEdit.applyError'))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal show={show} onHide={onHide} centered scrollable>
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
              <Alert variant="danger" className="py-2 small">
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
              <p className="text-danger small mb-0">{t('bulkEdit.loadError')}</p>
            )}
            {load.status === 'ready' && (
              <BulkEditForm
                form={form}
                albums={load.albums}
                labels={load.labels}
                busy={busy}
                onChange={update}
              />
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
                void apply()
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

/** The editable operation form (album/label/description/location/flags). */
function BulkEditForm({
  form,
  albums,
  labels,
  busy,
  onChange,
}: {
  form: FormState
  albums: AlbumCount[]
  labels: LabelCount[]
  busy: boolean
  onChange: (patch: Partial<FormState>) => void
}) {
  const { t } = useTranslation()
  return (
    <Form>
      <Row className="g-3">
        <Col xs={12} md={6}>
          <Form.Group controlId="bulk-add-album">
            <Form.Label className="small mb-1">{t('bulkEdit.addAlbum')}</Form.Label>
            <Form.Select
              value={form.addAlbum}
              disabled={busy}
              onChange={(e) => {
                onChange({ addAlbum: e.target.value })
              }}
            >
              <option value="">{t('bulkEdit.none')}</option>
              {albums.map((album) => (
                <option key={album.uid} value={album.uid}>
                  {album.title}
                </option>
              ))}
            </Form.Select>
          </Form.Group>
        </Col>
        <Col xs={12} md={6}>
          <Form.Group controlId="bulk-remove-album">
            <Form.Label className="small mb-1">{t('bulkEdit.removeAlbum')}</Form.Label>
            <Form.Select
              value={form.removeAlbum}
              disabled={busy}
              onChange={(e) => {
                onChange({ removeAlbum: e.target.value })
              }}
            >
              <option value="">{t('bulkEdit.none')}</option>
              {albums.map((album) => (
                <option key={album.uid} value={album.uid}>
                  {album.title}
                </option>
              ))}
            </Form.Select>
          </Form.Group>
        </Col>
        <Col xs={12} md={6}>
          <Form.Group controlId="bulk-add-label">
            <Form.Label className="small mb-1">{t('bulkEdit.addLabel')}</Form.Label>
            <Form.Select
              value={form.addLabel}
              disabled={busy}
              onChange={(e) => {
                onChange({ addLabel: e.target.value })
              }}
            >
              <option value="">{t('bulkEdit.none')}</option>
              {labels.map((label) => (
                <option key={label.uid} value={label.uid}>
                  {label.name}
                </option>
              ))}
            </Form.Select>
          </Form.Group>
        </Col>
        <Col xs={12} md={6}>
          <Form.Group controlId="bulk-remove-label">
            <Form.Label className="small mb-1">{t('bulkEdit.removeLabel')}</Form.Label>
            <Form.Select
              value={form.removeLabel}
              disabled={busy}
              onChange={(e) => {
                onChange({ removeLabel: e.target.value })
              }}
            >
              <option value="">{t('bulkEdit.none')}</option>
              {labels.map((label) => (
                <option key={label.uid} value={label.uid}>
                  {label.name}
                </option>
              ))}
            </Form.Select>
          </Form.Group>
        </Col>

        <Col xs={12}>
          <Form.Group controlId="bulk-description-mode">
            <Form.Label className="small mb-1">{t('bulkEdit.description.label')}</Form.Label>
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
        </Col>

        <Col xs={12}>
          <Form.Group controlId="bulk-location-mode">
            <Form.Label className="small mb-1">{t('bulkEdit.location.label')}</Form.Label>
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
        </Col>

        <Col xs={12} md={4}>
          <Form.Group controlId="bulk-private">
            <Form.Label className="small mb-1">{t('bulkEdit.private.label')}</Form.Label>
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
            <Form.Label className="small mb-1">{t('bulkEdit.archive.label')}</Form.Label>
            <Form.Select
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
            <Form.Label className="small mb-1">{t('bulkEdit.favorite.label')}</Form.Label>
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
    </Form>
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
          <p className="small text-secondary mb-1">{t('bulkEdit.result.errorsTitle')}</p>
          <ul className="small mb-0">
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
