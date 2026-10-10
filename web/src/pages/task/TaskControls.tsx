import { useEffect, useRef, useState } from 'react'
import { Alert, Button, Card, Form } from 'react-bootstrap'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { Icon } from '../../components/Icon'
import { type QuickTransition, quickTransitions } from '../../lib/taskTransitions'
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
 * The bookkeeping half of a task's page: how far along it is, how it ended, and
 * the query the group came from. It is rendered only for a writer, and it sits
 * below the discussion on purpose — a viewer who was sent the link should reach
 * the answer box without scrolling past controls they cannot use, and a writer
 * reaches for this card only once the conversation has told them something.
 *
 * The wording of the question is *not* here: it is edited in place at the top of
 * the page (`TaskQuestion`), where the words themselves are.
 *
 * The usual move is one tap: a row of quick buttons sits above the full state
 * picker, offering only the moves that fit the current state (see
 * `quickTransitions` — from `review` "Schválit" and "Vrátit agentovi", from
 * `working` "Potřebuji odpověď" and "K revizi", …). An open-to-open move saves at
 * once; the `<select>` stays for the rare jump the row does not offer.
 *
 * Closing is the one move with a rule attached: the resolution field appears as
 * soon as a closed state is picked — from the picker or from a closing quick
 * button, which opens the field already focused instead of saving — because the
 * server refuses a task that is closed and silent, and discovering that as a
 * failed save would be worse than being asked for it up front.
 *
 * The page keys this card on the task's state, resolution and query, so a move
 * made elsewhere (the hand-over from the discussion) resets the picker to it.
 */
export function TaskControls({ task, busy, onSave, onDelete, onChanged }: TaskControlsProps) {
  const { t } = useTranslation()
  const [state, setState] = useState<TaskState>(task.state)
  const [resolution, setResolution] = useState(task.resolution)
  const [query, setQuery] = useState(task.query)
  const [failed, setFailed] = useState(false)
  // Bumped by a closing quick button: the resolution field takes focus once it
  // is on screen, so the curator types the reason straight away.
  const [focusResolution, setFocusResolution] = useState(0)
  const resolutionRef = useRef<HTMLTextAreaElement>(null)

  const closing = isClosedState(state)
  const changed = state !== task.state || resolution !== task.resolution || query !== task.query
  const quick = quickTransitions(task.state)

  useEffect(() => {
    if (focusResolution > 0) {
      resolutionRef.current?.focus()
    }
  }, [focusResolution])

  async function apply(edit: TaskEdit, refetch = true) {
    setFailed(false)
    const ok = await onSave(edit)
    if (!ok) {
      setFailed(true)
      return
    }
    if (refetch) {
      onChanged()
    }
  }

  function quickMove(move: QuickTransition) {
    if (move.closes) {
      setState(move.to)
      setFocusResolution((n) => n + 1)
      return
    }
    // The saved task comes back through onSave and redraws the badge and the
    // participants by itself; a whole-page refetch would only flash the wall.
    void apply({ state: move.to }, false)
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

        <section className="mb-3" aria-label={t('taskDetail.quick.label')}>
          <p className="text-body-secondary small mb-2">{t('taskDetail.quick.label')}</p>
          <div className="d-flex flex-wrap gap-2">
            {quick.map((move) => (
              <Button
                key={move.id}
                variant={move.primary ? 'primary' : 'outline-primary'}
                className="kk-task-quick"
                disabled={busy}
                aria-pressed={move.closes ? state === move.to : undefined}
                onClick={() => {
                  quickMove(move)
                }}
              >
                {t(`taskDetail.quick.${move.id}`)}
              </Button>
            ))}
          </div>
        </section>

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
              ref={resolutionRef}
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

        <div className="d-flex flex-wrap gap-2 align-items-center">
          <Button
            variant="primary"
            size="sm"
            disabled={busy || !changed || (closing && resolution === '')}
            onClick={() => {
              void apply({ state, resolution, query })
            }}
          >
            {busy ? t('taskDetail.controls.saving') : t('taskDetail.controls.save')}
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
      </Card.Body>
    </Card>
  )
}
