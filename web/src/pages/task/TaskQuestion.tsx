import { useState } from 'react'
import { Alert, Button, Card, Form } from 'react-bootstrap'
import { useTranslation } from 'react-i18next'

import { Icon } from '../../components/Icon'
import { Markdown } from '../../components/Markdown'
import { type Task, type TaskEdit } from '../../services/tasks'

/** Props for {@link TaskQuestion}. */
export interface TaskQuestionProps {
  task: Task
  /** Whether the reader may reword the question (a writer). */
  canEdit: boolean
  /** True while a save is in flight, so every control stands down together. */
  busy: boolean
  /** Applies a partial change; resolves false when it did not land. */
  onSave: (edit: TaskEdit) => Promise<boolean>
}

/**
 * The question itself: who asked it and how big the batch is, the question as
 * the page's heading, and the context below it.
 *
 * The byline comes *before* the heading, as the eyebrow of a newspaper article
 * does. It is the smaller fact — it says where the question came from, which is
 * worth knowing before reading it but never worth reading first.
 *
 * Rewording happens in place: the pencil sits against the heading and turns the
 * two blocks into the two fields that produced them, so an editor fixing a typo
 * edits the thing they are looking at. Editing used to live in the curation card
 * at the foot of the page, which meant scrolling past the whole conversation to
 * fix a word and scrolling back to see whether it had taken.
 */
export function TaskQuestion({ task, canEdit, busy, onSave }: TaskQuestionProps) {
  const { t } = useTranslation()
  const [editing, setEditing] = useState(false)
  const [title, setTitle] = useState(task.title)
  const [body, setBody] = useState(task.body)
  const [failed, setFailed] = useState(false)

  function open() {
    // Start from what is on screen now, not from whatever was last typed into a
    // form that was abandoned — or that somebody else has since overwritten.
    setTitle(task.title)
    setBody(task.body)
    setFailed(false)
    setEditing(true)
  }

  async function submit() {
    setFailed(false)
    if (await onSave({ title, body })) {
      setEditing(false)
      return
    }
    setFailed(true)
  }

  const byline = (
    <p className="text-body-secondary small mb-1">
      {t('taskDetail.opened', { name: task.created_by_name || t('taskDetail.someone') })}
      {' · '}
      {t('tasks.row.photos', { count: task.photo_count })}
    </p>
  )

  if (editing) {
    return (
      <section className="mb-4">
        {byline}
        <Card>
          <Card.Body>
            {failed && (
              <Alert variant="danger" className="py-2">
                {t('taskDetail.controls.failed')}
              </Alert>
            )}
            <Form.Group className="mb-3">
              <Form.Label htmlFor="task-title">{t('taskDetail.controls.question')}</Form.Label>
              <Form.Control
                id="task-title"
                value={title}
                disabled={busy}
                autoFocus
                onChange={(event) => {
                  setTitle(event.target.value)
                }}
              />
            </Form.Group>
            <Form.Group className="mb-3">
              <Form.Label htmlFor="task-body">{t('taskDetail.controls.body')}</Form.Label>
              <Form.Control
                id="task-body"
                as="textarea"
                rows={5}
                value={body}
                disabled={busy}
                onChange={(event) => {
                  setBody(event.target.value)
                }}
              />
              <Form.Text>{t('taskDetail.controls.bodyHint')}</Form.Text>
            </Form.Group>
            <div className="d-flex gap-2">
              <Button
                variant="primary"
                size="sm"
                disabled={busy || title.trim() === ''}
                onClick={() => {
                  void submit()
                }}
              >
                {busy ? t('taskDetail.controls.saving') : t('taskDetail.controls.save')}
              </Button>
              <Button
                variant="outline-secondary"
                size="sm"
                disabled={busy}
                onClick={() => {
                  setEditing(false)
                }}
              >
                {t('taskDetail.controls.cancel')}
              </Button>
            </div>
          </Card.Body>
        </Card>
      </section>
    )
  }

  return (
    <section className="mb-4">
      {byline}
      {/* The question is the page. Everything else on it exists to answer this. */}
      <div className="d-flex align-items-start gap-2">
        <h1 className="kk-page-title mb-0 flex-grow-1 kk-min-w-0">{task.title}</h1>
        {canEdit && (
          <Button
            variant="outline-secondary"
            size="sm"
            className="flex-shrink-0"
            onClick={open}
            title={t('taskDetail.controls.edit')}
          >
            <Icon name="pencil" /> {t('taskDetail.controls.edit')}
          </Button>
        )}
      </div>

      {task.body !== '' && (
        <Card className="mt-3">
          <Card.Body>
            <Markdown>{task.body}</Markdown>
          </Card.Body>
        </Card>
      )}
    </section>
  )
}
