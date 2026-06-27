import { useEffect, useState } from 'react'
import Button from 'react-bootstrap/Button'
import Form from 'react-bootstrap/Form'
import Modal from 'react-bootstrap/Modal'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'

import { bulkUpdatePhotos } from '../../services/bulk'
import { type AlbumCount, fetchAlbums, fetchLabels, type LabelCount } from '../../services/organize'

/** Props for {@link AddToCollectionModal}. */
export interface AddToCollectionModalProps {
  /** Whether the modal is visible. */
  show: boolean
  /** The selected photo UIDs to add to the chosen album/label. */
  photoUids: string[]
  /** Dismisses the modal. */
  onHide: () => void
  /** Called after a successful add, so the caller can clear the selection. */
  onDone: () => void
}

/** Fetch lifecycle of the album/label option lists. */
type State =
  | { status: 'loading' }
  | { status: 'error' }
  | { status: 'ready'; albums: AlbumCount[]; labels: LabelCount[] }

/**
 * A modal that adds a multi-photo grid selection to an album and/or a label in a
 * single bulk-metadata call. It loads the available albums and labels, lets the
 * user pick one of each (either optional), and applies them via
 * `POST /photos/bulk`. A load or save error is surfaced inline.
 */
export function AddToCollectionModal({
  show,
  photoUids,
  onHide,
  onDone,
}: AddToCollectionModalProps) {
  const { t } = useTranslation()
  const [state, setState] = useState<State>({ status: 'loading' })
  const [albumUid, setAlbumUid] = useState('')
  const [labelUid, setLabelUid] = useState('')
  const [busy, setBusy] = useState(false)
  const [saveError, setSaveError] = useState(false)

  useEffect(() => {
    if (!show) {
      return
    }
    const controller = new AbortController()
    setState({ status: 'loading' })
    setAlbumUid('')
    setLabelUid('')
    setSaveError(false)
    Promise.all([fetchAlbums(controller.signal), fetchLabels(controller.signal)])
      .then(([albums, labels]) => {
        setState({ status: 'ready', albums, labels })
      })
      .catch((err: unknown) => {
        if (err instanceof DOMException && err.name === 'AbortError') {
          return
        }
        setState({ status: 'error' })
      })
    return () => {
      controller.abort()
    }
  }, [show])

  async function apply() {
    if (albumUid === '' && labelUid === '') {
      onHide()
      return
    }
    setBusy(true)
    setSaveError(false)
    try {
      await bulkUpdatePhotos(photoUids, {
        ...(albumUid !== '' ? { add_to_albums: [albumUid] } : {}),
        ...(labelUid !== '' ? { add_labels: [labelUid] } : {}),
      })
      onDone()
    } catch {
      setSaveError(true)
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal show={show} onHide={onHide} centered>
      <Modal.Header closeButton>
        <Modal.Title>{t('collection.add.title', { count: photoUids.length })}</Modal.Title>
      </Modal.Header>
      <Modal.Body>
        {state.status === 'loading' && (
          <div className="d-flex justify-content-center py-3">
            <Spinner animation="border" role="status" size="sm">
              <span className="visually-hidden">{t('collection.add.loading')}</span>
            </Spinner>
          </div>
        )}
        {state.status === 'error' && (
          <p className="text-danger small mb-0">{t('collection.add.loadError')}</p>
        )}
        {state.status === 'ready' && (
          <>
            {saveError && <p className="text-danger small">{t('collection.add.saveError')}</p>}
            <Form.Group className="mb-3" controlId="collection-album">
              <Form.Label>{t('collection.add.album')}</Form.Label>
              <Form.Select
                value={albumUid}
                disabled={busy}
                onChange={(event) => {
                  setAlbumUid(event.target.value)
                }}
              >
                <option value="">{t('collection.add.none')}</option>
                {state.albums.map((album) => (
                  <option key={album.uid} value={album.uid}>
                    {album.title}
                  </option>
                ))}
              </Form.Select>
            </Form.Group>
            <Form.Group controlId="collection-label">
              <Form.Label>{t('collection.add.label')}</Form.Label>
              <Form.Select
                value={labelUid}
                disabled={busy}
                onChange={(event) => {
                  setLabelUid(event.target.value)
                }}
              >
                <option value="">{t('collection.add.none')}</option>
                {state.labels.map((label) => (
                  <option key={label.uid} value={label.uid}>
                    {label.name}
                  </option>
                ))}
              </Form.Select>
            </Form.Group>
          </>
        )}
      </Modal.Body>
      <Modal.Footer>
        <Button variant="secondary" onClick={onHide} disabled={busy}>
          {t('collection.add.cancel')}
        </Button>
        <Button
          variant="primary"
          disabled={busy || state.status !== 'ready'}
          onClick={() => {
            void apply()
          }}
        >
          {t('collection.add.confirm')}
        </Button>
      </Modal.Footer>
    </Modal>
  )
}
