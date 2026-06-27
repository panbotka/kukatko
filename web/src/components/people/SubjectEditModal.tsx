import { type SyntheticEvent, useState } from 'react'
import Button from 'react-bootstrap/Button'
import Form from 'react-bootstrap/Form'
import Modal from 'react-bootstrap/Modal'
import { useTranslation } from 'react-i18next'

import {
  type Subject,
  SUBJECT_TYPES,
  type SubjectInput,
  type SubjectType,
  updateSubject,
} from '../../services/people'

/** Props for {@link SubjectEditModal}. */
export interface SubjectEditModalProps {
  /** The subject being edited (provides the initial field values). */
  subject: Subject
  /** Whether the modal is visible. */
  show: boolean
  /** Dismisses the modal without saving. */
  onHide: () => void
  /** Called with the refreshed subject after a successful save. */
  onSaved: (subject: Subject) => void
}

/**
 * A modal form for editing a subject's name, type and visibility flags. It
 * preserves the existing cover (set elsewhere) and submits the full editable set
 * to `PATCH /subjects/{uid}`, surfacing a validation error inline.
 */
export function SubjectEditModal({ subject, show, onHide, onSaved }: SubjectEditModalProps) {
  const { t } = useTranslation()
  const [name, setName] = useState(subject.name)
  const [type, setType] = useState<SubjectType>(subject.type)
  const [favorite, setFavorite] = useState(subject.favorite)
  const [isPrivate, setIsPrivate] = useState(subject.private)
  const [notes, setNotes] = useState(subject.notes)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState(false)

  async function save(event: SyntheticEvent) {
    event.preventDefault()
    const trimmed = name.trim()
    if (trimmed === '') {
      setError(true)
      return
    }
    const input: SubjectInput = {
      name: trimmed,
      type,
      favorite,
      private: isPrivate,
      notes,
      cover_photo_uid: subject.cover_photo_uid ?? null,
    }
    setBusy(true)
    setError(false)
    try {
      const updated = await updateSubject(subject.uid, input)
      onSaved(updated)
    } catch {
      setError(true)
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal show={show} onHide={onHide} centered>
      <Form
        onSubmit={(event) => {
          void save(event)
        }}
      >
        <Modal.Header closeButton>
          <Modal.Title>{t('subject.edit.title')}</Modal.Title>
        </Modal.Header>
        <Modal.Body>
          {error && <p className="text-danger small">{t('subject.edit.error')}</p>}
          <Form.Group className="mb-3" controlId="subject-name">
            <Form.Label>{t('subject.edit.name')}</Form.Label>
            <Form.Control
              type="text"
              value={name}
              disabled={busy}
              onChange={(event) => {
                setName(event.target.value)
              }}
            />
          </Form.Group>
          <Form.Group className="mb-3" controlId="subject-type">
            <Form.Label>{t('subject.edit.type')}</Form.Label>
            <Form.Select
              value={type}
              disabled={busy}
              onChange={(event) => {
                setType(event.target.value as SubjectType)
              }}
            >
              {SUBJECT_TYPES.map((value) => (
                <option key={value} value={value}>
                  {t(`subject.type.${value}`)}
                </option>
              ))}
            </Form.Select>
          </Form.Group>
          <Form.Check
            type="checkbox"
            id="subject-favorite"
            className="mb-2"
            label={t('subject.edit.favorite')}
            checked={favorite}
            disabled={busy}
            onChange={(event) => {
              setFavorite(event.target.checked)
            }}
          />
          <Form.Check
            type="checkbox"
            id="subject-private"
            className="mb-3"
            label={t('subject.edit.private')}
            checked={isPrivate}
            disabled={busy}
            onChange={(event) => {
              setIsPrivate(event.target.checked)
            }}
          />
          <Form.Group controlId="subject-notes">
            <Form.Label>{t('subject.edit.notes')}</Form.Label>
            <Form.Control
              as="textarea"
              rows={2}
              value={notes}
              disabled={busy}
              onChange={(event) => {
                setNotes(event.target.value)
              }}
            />
          </Form.Group>
        </Modal.Body>
        <Modal.Footer>
          <Button variant="secondary" onClick={onHide} disabled={busy}>
            {t('subject.edit.cancel')}
          </Button>
          <Button type="submit" variant="primary" disabled={busy}>
            {t('subject.edit.save')}
          </Button>
        </Modal.Footer>
      </Form>
    </Modal>
  )
}
