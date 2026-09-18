import { useState } from 'react'
import { Alert, Button, Card, Form } from 'react-bootstrap'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { Icon } from '../../components/Icon'
import {
  isClosedState,
  type Task,
  type TaskEdit,
  TASK_STATES,
  type TaskState,
} from '../../services/tasks'

/** Props for {@link TaskControls}. */
export interface TaskControlsProps {
  task: Task
  /** True while a save is in flight, so every control stands down together. */
  busy: boolean
  /** Applies a partial change; resolves false when it did not land. */
  onSave: (edit: TaskEdit) => Promise<boolean>
  /** Opens the delete confirmation. */
  onDelete: () => void
  /** Asks the page to refetch — a membership change happened elsewhere. */
  onChanged: () => void
}

/**
 * The curation half of a task's page: the state, the resolution and the wording
 * of the question. It is rendered only for a writer, and it sits below the
 * discussion on purpose — a viewer who was sent the link should reach the answer
 * box without scrolling past controls they cannot use.
 *
 * Closing is the one move with a rule attached: the resolution field appears as
 * soon as a closed state is picked, because the server refuses a task that is
 * closed and silent, and discovering that as a failed save would be worse than
 * being asked for it up front.
 */
export function TaskControls({ task, busy, onSave, onDelete, onChanged }: TaskControlsProps) {
  const { t } = useTranslation()
  const [state, setState] = useState<TaskState>(task.state)
  const [resolution, setResolution] = useState(task.resolution)
  const [editing, setEditing] = useState(false)
  const [title, setTitle] = useState(task.title)
  const [body, setBody] = useState(task.body)
  const [query, setQuery] = useState(task.query)
  const [failed, setFailed] = useState(false)

  const closing = isClosedState(state)
  const stateChanged = state !== task.state
  const resolutionChanged = resolution !== task.resolution

  async function apply(edit: TaskEdit) {
    setFailed(false)
    const ok = await onSave(edit)
    if (!ok) {
      setFailed(true)
      return
    }
    onChanged()
  }

  return (
    <Card className="mb-4">
      <Card.Body>
        <h2 className="h6 text-body-secondary">{t('taskDetail.controls.title')}</h2>

        {failed && (
          <Alert variant="danger" className="py-2">
            {t('taskDetail.controls.failed')}
          </Alert>
        )}

        <Form.Group className="mb-3">
          <Form.Label htmlFor="task-state">{t('taskDetail.controls.state')}</Form.Label>
          <Form.Select
            id="task-state"
            value={state}
            disabled={busy}
            onChange={(event) => {
              setState(event.target.value as TaskState)
            }}
          >
            {TASK_STATES.map((choice) => (
              <option key={choice} value={choice}>
                {t(`tasks.state.${choice}`)}
              </option>
            ))}
          </Form.Select>
        </Form.Group>

        {closing && (
          <Form.Group className="mb-3">
            <Form.Label htmlFor="task-resolution">{t('taskDetail.controls.resolution')}</Form.Label>
            <Form.Control
              id="task-resolution"
              as="textarea"
              rows={3}
              value={resolution}
              disabled={busy}
              placeholder={t('taskDetail.controls.resolutionPlaceholder')}
              onChange={(event) => {
                setResolution(event.target.value)
              }}
            />
            <Form.Text>{t('taskDetail.controls.resolutionHint')}</Form.Text>
          </Form.Group>
        )}

        <div className="d-flex flex-wrap gap-2 align-items-center">
          <Button
            variant="primary"
            size="sm"
            disabled={
              busy || (!stateChanged && !resolutionChanged) || (closing && resolution === '')
            }
            onClick={() => {
              void apply({ state, resolution })
            }}
          >
            {busy ? t('taskDetail.controls.saving') : t('taskDetail.controls.save')}
          </Button>
          <Button
            variant="outline-secondary"
            size="sm"
            onClick={() => {
              setEditing((open) => !open)
            }}
          >
            <Icon name="pencil" /> {t('taskDetail.controls.edit')}
          </Button>
          {task.query !== '' && (
            <Link
              to={`/search?q=${encodeURIComponent(task.query)}`}
              className="btn btn-outline-secondary btn-sm"
            >
              <Icon name="search" /> {t('taskDetail.controls.showQuery')}
            </Link>
          )}
          <Button variant="outline-danger" size="sm" className="ms-auto" onClick={onDelete}>
            <Icon name="trash" /> {t('taskDetail.controls.delete')}
          </Button>
        </div>

        {editing && (
          <div className="mt-3 border-top pt-3">
            <Form.Group className="mb-3">
              <Form.Label htmlFor="task-title">{t('taskDetail.controls.question')}</Form.Label>
              <Form.Control
                id="task-title"
                value={title}
                disabled={busy}
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
            </Form.Group>
            <Form.Group className="mb-3">
              <Form.Label htmlFor="task-query">{t('taskDetail.controls.query')}</Form.Label>
              <Form.Control
                id="task-query"
                value={query}
                disabled={busy}
                onChange={(event) => {
                  setQuery(event.target.value)
                }}
              />
              <Form.Text>{t('taskDetail.controls.queryHint')}</Form.Text>
            </Form.Group>
            <Button
              variant="primary"
              size="sm"
              disabled={busy || title.trim() === ''}
              onClick={() => {
                void apply({ title, body, query })
              }}
            >
              {busy ? t('taskDetail.controls.saving') : t('taskDetail.controls.save')}
            </Button>
          </div>
        )}
      </Card.Body>
    </Card>
  )
}
