import { type SyntheticEvent, useEffect, useState } from 'react'
import Button from 'react-bootstrap/Button'
import Form from 'react-bootstrap/Form'
import Modal from 'react-bootstrap/Modal'
import { useTranslation } from 'react-i18next'

import { type Album, type AlbumInput, createAlbum, updateAlbum } from '../../services/organize'

/** Props for {@link AlbumEditModal}. */
export interface AlbumEditModalProps {
  /**
   * The album being edited; omit (or pass `null`) to create a new album. The
   * structural `type` is fixed on create and not editable afterwards.
   */
  album?: Album | null
  /** Whether the modal is visible. */
  show: boolean
  /** Dismisses the modal without saving. */
  onHide: () => void
  /** Called with the created/updated album after a successful save. */
  onSaved: (album: Album) => void
}

/**
 * A modal form for creating or renaming an album. Create mode also captures the
 * description and the private flag; edit mode rewrites the same editable fields
 * (the cover is set from the album page, and the structural type is preserved by
 * the backend). A validation or save error is surfaced inline.
 */
export function AlbumEditModal({ album, show, onHide, onSaved }: AlbumEditModalProps) {
  const { t } = useTranslation()
  const editing = album != null
  const [title, setTitle] = useState('')
  const [description, setDescription] = useState('')
  const [isPrivate, setIsPrivate] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState(false)

  // Reset the form to the album's values (or blanks) whenever it opens, so a
  // reused modal never shows the previous subject's data.
  useEffect(() => {
    if (show) {
      setTitle(album?.title ?? '')
      setDescription(album?.description ?? '')
      setIsPrivate(album?.private ?? false)
      setError(false)
    }
  }, [show, album])

  async function save(event: SyntheticEvent) {
    event.preventDefault()
    const trimmed = title.trim()
    if (trimmed === '') {
      setError(true)
      return
    }
    const input: AlbumInput = {
      title: trimmed,
      description,
      private: isPrivate,
      cover_photo_uid: album?.cover_photo_uid ?? null,
    }
    setBusy(true)
    setError(false)
    try {
      const saved = editing ? await updateAlbum(album.uid, input) : await createAlbum(input)
      onSaved(saved)
    } catch {
      setError(true)
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal show={show} onHide={onHide} centered fullscreen="sm-down">
      <Form
        onSubmit={(event) => {
          void save(event)
        }}
      >
        <Modal.Header closeButton>
          <Modal.Title>
            {editing ? t('albums.edit.titleEdit') : t('albums.edit.titleNew')}
          </Modal.Title>
        </Modal.Header>
        <Modal.Body>
          {error && <p className="text-danger small">{t('albums.edit.error')}</p>}
          <Form.Group className="mb-3" controlId="album-title">
            <Form.Label>{t('albums.edit.name')}</Form.Label>
            <Form.Control
              type="text"
              value={title}
              autoFocus
              disabled={busy}
              onChange={(event) => {
                setTitle(event.target.value)
              }}
            />
          </Form.Group>
          <Form.Group className="mb-3" controlId="album-description">
            <Form.Label>{t('albums.edit.description')}</Form.Label>
            <Form.Control
              as="textarea"
              rows={2}
              value={description}
              disabled={busy}
              onChange={(event) => {
                setDescription(event.target.value)
              }}
            />
          </Form.Group>
          <Form.Check
            type="checkbox"
            id="album-private"
            label={t('albums.edit.private')}
            checked={isPrivate}
            disabled={busy}
            onChange={(event) => {
              setIsPrivate(event.target.checked)
            }}
          />
        </Modal.Body>
        <Modal.Footer>
          <Button variant="secondary" onClick={onHide} disabled={busy}>
            {t('albums.edit.cancel')}
          </Button>
          <Button type="submit" variant="primary" disabled={busy}>
            {t('albums.edit.save')}
          </Button>
        </Modal.Footer>
      </Form>
    </Modal>
  )
}
