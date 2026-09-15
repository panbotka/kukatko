import { useEffect, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import Form from 'react-bootstrap/Form'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'

import Modal from '../Modal'

import { MAX_RENDITION_DPR, squareRenditionName } from '../../lib/rendition'
import { type Photo, searchPhotos, thumbUrl } from '../../services/photos'

/** Props for {@link PhotoPickerModal}. */
export interface PhotoPickerModalProps {
  /** Whether the dialog is open. */
  show: boolean
  /** Closes the dialog without choosing anything. */
  onClose: () => void
  /** Receives the chosen photo's uid; the dialog does not close itself. */
  onPick: (photoUid: string) => void
  /** True while the caller is saving the pick, so the grid stands down. */
  busy?: boolean
}

/** How many photos one page of the grid offers — enough to recognise oneself in. */
const PAGE_SIZE = 24

/** The thumbnail rung each tile paints: a ~120 px square on the sharpest screen. */
const TILE_SIZE = squareRenditionName(120, MAX_RENDITION_DPR)

/**
 * "Pick a photo from the library" — a small search box over a grid of square
 * thumbnails, for choosing the photograph that stands for one's account.
 *
 * It is deliberately not the library grid. That grid is a virtualised,
 * URL-driven, selection-aware surface built for browsing tens of thousands of
 * photographs; what this needs is one photo, found by typing a word, in a dialog
 * that closes afterwards. Reusing the big one would drag its whole state model
 * into a modal on the account page.
 *
 * It opens on the newest photos rather than on an empty state, because the
 * picture somebody wants is usually recent and because an empty grid gives no
 * hint that typing is what one does here.
 *
 * What it offers is not what the server accepts: a photo flagged private or
 * hidden may not become a profile picture, and the search this reads already
 * leaves hidden photos out — but the refusal is the server's (a 400), and the
 * card above shows its message. The grid does not try to predict it.
 */
export function PhotoPickerModal({ show, onClose, onPick, busy = false }: PhotoPickerModalProps) {
  const { t } = useTranslation()
  const [query, setQuery] = useState('')
  const [photos, setPhotos] = useState<Photo[]>([])
  const [loading, setLoading] = useState(false)
  const [failed, setFailed] = useState(false)

  useEffect(() => {
    if (!show) {
      return undefined
    }
    const controller = new AbortController()
    setLoading(true)
    setFailed(false)
    searchPhotos({ q: query, limit: PAGE_SIZE, sort: 'newest' }, undefined, controller.signal)
      .then((response) => {
        setPhotos(response.photos)
        setLoading(false)
      })
      .catch((error: unknown) => {
        if (error instanceof DOMException && error.name === 'AbortError') {
          return
        }
        setFailed(true)
        setLoading(false)
      })
    return () => {
      controller.abort()
    }
  }, [show, query])

  return (
    <Modal show={show} onHide={onClose} size="lg" centered scrollable>
      <Modal.Header closeButton closeVariant="white">
        <Modal.Title as="h2" className="kk-section-title mb-0">
          {t('account.picture.pickTitle')}
        </Modal.Title>
      </Modal.Header>
      <Modal.Body>
        <Form
          onSubmit={(event) => {
            // The grid already reloads as the field changes; submitting must not
            // navigate the page out from under the dialog.
            event.preventDefault()
          }}
        >
          <Form.Group className="mb-3" controlId="account-picture-search">
            <Form.Label>{t('account.picture.searchLabel')}</Form.Label>
            <Form.Control
              type="search"
              value={query}
              placeholder={t('account.picture.searchPlaceholder')}
              onChange={(event) => {
                setQuery(event.target.value)
              }}
            />
          </Form.Group>
        </Form>

        {failed && (
          <Alert variant="danger" role="alert">
            {t('account.picture.searchError')}
          </Alert>
        )}
        {loading && (
          <div className="text-center py-3">
            <Spinner animation="border" role="status" size="sm" className="me-2" />
            {t('account.picture.searching')}
          </div>
        )}
        {!loading && !failed && photos.length === 0 && (
          <p className="text-secondary mb-0">{t('account.picture.noMatches')}</p>
        )}

        <div className="kk-picture-picker">
          {photos.map((photo) => (
            <button
              key={photo.uid}
              type="button"
              className="kk-picture-picker__tile"
              disabled={busy}
              title={photo.title === '' ? photo.file_name : photo.title}
              onClick={() => {
                onPick(photo.uid)
              }}
            >
              <img
                src={thumbUrl(photo.uid, TILE_SIZE)}
                alt={photo.title === '' ? photo.file_name : photo.title}
                loading="lazy"
                decoding="async"
              />
            </button>
          ))}
        </div>
      </Modal.Body>
      <Modal.Footer>
        <Button variant="outline-light" onClick={onClose}>
          {t('confirmModal.cancel')}
        </Button>
      </Modal.Footer>
    </Modal>
  )
}
