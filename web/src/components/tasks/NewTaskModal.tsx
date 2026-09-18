import { useState } from 'react'
import { Alert, Button, Form, Modal } from 'react-bootstrap'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router-dom'

import { createTask } from '../../services/tasks'

/** Props for {@link NewTaskModal}. */
export interface NewTaskModalProps {
  show: boolean
  onClose: () => void
  /**
   * The photographs the question is about. A task may be opened over none — the
   * group can be filled in afterwards — which is what the bare "new task" button
   * does.
   */
  photoUids?: string[]
}

/**
 * Opens a task. Two fields, because a question that takes a form to ask does not
 * get asked: the question itself, and whatever context makes it answerable.
 *
 * On success it navigates straight to the new task, which is both the
 * confirmation that it exists and the page whose link gets sent to whoever knows
 * the answer.
 */
export function NewTaskModal({ show, onClose, photoUids = [] }: NewTaskModalProps) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const [title, setTitle] = useState('')
  const [body, setBody] = useState('')
  const [busy, setBusy] = useState(false)
  const [failed, setFailed] = useState(false)

  function submit() {
    setBusy(true)
    setFailed(false)
    createTask({ title, body, photo_uids: photoUids })
      .then((task) => {
        void navigate(`/tasks/${task.uid}`)
      })
      .catch(() => {
        setFailed(true)
        setBusy(false)
      })
  }

  return (
    <Modal show={show} onHide={onClose} centered>
      <Modal.Header closeButton>
        <Modal.Title as="h2" className="h5">
          {t('tasks.create')}
        </Modal.Title>
      </Modal.Header>
      <Modal.Body>
        {failed && (
          <Alert variant="danger" className="py-2">
            {t('taskDetail.controls.failed')}
          </Alert>
        )}
        <Form.Group className="mb-3">
          <Form.Label htmlFor="new-task-title">{t('taskDetail.controls.question')}</Form.Label>
          <Form.Control
            id="new-task-title"
            value={title}
            autoFocus
            disabled={busy}
            onChange={(event) => {
              setTitle(event.target.value)
            }}
          />
        </Form.Group>
        <Form.Group>
          <Form.Label htmlFor="new-task-body">{t('taskDetail.controls.body')}</Form.Label>
          <Form.Control
            id="new-task-body"
            as="textarea"
            rows={4}
            value={body}
            disabled={busy}
            onChange={(event) => {
              setBody(event.target.value)
            }}
          />
        </Form.Group>
        {photoUids.length > 0 && (
          <p className="text-body-secondary small mb-0 mt-2">
            {t('tasks.row.photos', { count: photoUids.length })}
          </p>
        )}
      </Modal.Body>
      <Modal.Footer>
        <Button variant="secondary" onClick={onClose} disabled={busy}>
          {t('confirmModal.cancel')}
        </Button>
        <Button variant="primary" onClick={submit} disabled={busy || title.trim() === ''}>
          {busy ? t('taskDetail.controls.saving') : t('tasks.create')}
        </Button>
      </Modal.Footer>
    </Modal>
  )
}
