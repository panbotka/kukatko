import { useEffect, useState } from 'react'
import { Alert, Button, Form, Modal, Spinner } from 'react-bootstrap'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router-dom'

import { type DirectoryUser } from '../../services/directory'
import {
  addTaskPhotos,
  createTask,
  fetchTasks,
  type Task,
  type TaskInput,
  type TaskState,
} from '../../services/tasks'
import { Icon } from '../Icon'
import { PersonAvatar } from '../PersonAvatar'

import { AnswerOptionsEditor } from './AnswerOptionsEditor'
import { PersonPicker } from './PersonPicker'
import { TaskStateBadge } from './TaskStateBadge'

/** Props for {@link NewTaskModal}. */
export interface NewTaskModalProps {
  show: boolean
  onClose: () => void
  /**
   * The photographs the question is about. A task may be opened over none — the
   * group can be filled in afterwards — which is what the bare "new task" button
   * does. With photographs in hand the dialog also offers adding them to a
   * question that already exists.
   */
  photoUids?: string[]
}

/** Which of the two things the dialog is doing. */
type Mode = 'new' | 'existing'

/**
 * The states a task may be opened in. The two are the two directions the queue
 * runs in: a question waits for somebody to answer, working hands somebody the
 * doing. The closed states are results, not openings, and review is where work
 * arrives rather than where it starts.
 */
export const OPENING_STATES = ['question', 'working'] as const satisfies readonly TaskState[]

/** One of {@link OPENING_STATES}. */
type OpeningState = (typeof OPENING_STATES)[number]

/**
 * Asks about a group of photographs — either by opening a new question, or by
 * adding them to one that is already waiting.
 *
 * Both live in one dialog because from where the reader stands they are one
 * intent: "these pictures need somebody to look at them". Which of the two it
 * turns into is a detail of whether the question has been asked before, and
 * splitting it into two buttons would have meant a reader who picked the wrong
 * one had to start over — and a batch bar with one more control on it.
 *
 * Opening a task takes two fields, because a question that takes a form to ask
 * does not get asked. The two beneath them are for handing work over rather
 * than asking: who should be on it from the start (otherwise assigning is a
 * second step on the task page) and whether it opens as a question — somebody
 * should answer — or as work — somebody should do it. Either path ends on the
 * task's own page: that is both the confirmation and the page whose link gets
 * sent to whoever knows the answer.
 */
export function NewTaskModal({ show, onClose, photoUids = [] }: NewTaskModalProps) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const [mode, setMode] = useState<Mode>('new')
  const [title, setTitle] = useState('')
  const [body, setBody] = useState('')
  const [options, setOptions] = useState<string[]>([])
  // Whom the task is handed to, kept as whole directory entries so the chips
  // can show names while the request sends uids.
  const [who, setWho] = useState<DirectoryUser[]>([])
  const [picking, setPicking] = useState(false)
  const [openingState, setOpeningState] = useState<OpeningState>('question')
  const [busy, setBusy] = useState(false)
  const [failed, setFailed] = useState(false)

  // Adding to an existing question only makes sense with photographs in hand:
  // from the bare "new task" button there is nothing to add.
  const canAddToExisting = photoUids.length > 0

  useEffect(() => {
    if (!show) {
      return
    }
    setMode('new')
    setFailed(false)
    setWho([])
    setPicking(false)
    setOpeningState('question')
  }, [show])

  function submit() {
    setBusy(true)
    setFailed(false)
    // Each optional field is sent only when there is something to send: a
    // question with no options is answered in free text, one nobody was handed
    // to has no participants, and the server's default state is the dialog's.
    const input: TaskInput = { title, body, photo_uids: photoUids }
    if (options.length > 0) {
      input.options = options
    }
    if (who.length > 0) {
      input.participants = who.map((person) => person.uid)
    }
    if (openingState !== 'question') {
      input.state = openingState
    }
    createTask(input)
      .then((task) => {
        void navigate(`/tasks/${task.uid}`)
      })
      .catch(() => {
        setFailed(true)
        setBusy(false)
      })
  }

  function addTo(task: Task) {
    setBusy(true)
    setFailed(false)
    addTaskPhotos(task.uid, photoUids)
      .then(() => {
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

        {canAddToExisting && (
          <div className="d-flex gap-2 mb-3">
            <Button
              size="sm"
              variant={mode === 'new' ? 'primary' : 'outline-secondary'}
              onClick={() => {
                setMode('new')
              }}
            >
              {t('tasks.add.new')}
            </Button>
            <Button
              size="sm"
              variant={mode === 'existing' ? 'primary' : 'outline-secondary'}
              onClick={() => {
                setMode('existing')
              }}
            >
              {t('tasks.add.existing')}
            </Button>
          </div>
        )}

        {mode === 'new' ? (
          <>
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
            <Form.Group className="mb-3">
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
            <AnswerOptionsEditor
              idPrefix="new-task"
              value={options}
              onChange={setOptions}
              disabled={busy}
            />
            <Form.Group className="mb-3">
              <div className="form-label" id="new-task-who">
                {t('tasks.new.who')}
              </div>
              <div
                className="d-flex align-items-center gap-2 flex-wrap"
                role="group"
                aria-labelledby="new-task-who"
              >
                {who.map((person) => (
                  <span key={person.uid} className="kk-task-person">
                    <PersonAvatar name={person.name} userUid={person.uid} />
                    <span className="kk-min-w-0 text-truncate">{person.name}</span>
                    <Button
                      variant="link"
                      size="sm"
                      className="kk-task-person__remove"
                      disabled={busy}
                      aria-label={t('taskDetail.people.remove', { name: person.name })}
                      onClick={() => {
                        setWho((current) => current.filter((p) => p.uid !== person.uid))
                      }}
                    >
                      <Icon name="x-lg" />
                    </Button>
                  </span>
                ))}
                <Button
                  variant="outline-secondary"
                  size="sm"
                  disabled={busy}
                  onClick={() => {
                    setPicking(true)
                  }}
                >
                  <Icon name="plus-lg" /> {t('taskDetail.people.add')}
                </Button>
              </div>
              <Form.Text>{t('tasks.new.whoHint')}</Form.Text>
            </Form.Group>
            <Form.Group className="mb-3">
              <Form.Label htmlFor="new-task-state">{t('taskDetail.controls.state')}</Form.Label>
              <Form.Select
                id="new-task-state"
                value={openingState}
                disabled={busy}
                onChange={(event) => {
                  setOpeningState(event.target.value === 'working' ? 'working' : 'question')
                }}
              >
                {OPENING_STATES.map((state) => (
                  <option key={state} value={state}>
                    {t(`tasks.state.${state}`)}
                  </option>
                ))}
              </Form.Select>
              <Form.Text>
                {openingState === 'working'
                  ? t('tasks.new.stateWorking')
                  : t('tasks.new.stateQuestion')}
              </Form.Text>
            </Form.Group>
            <PersonPicker
              show={picking}
              busy={busy}
              already={who.map((person) => person.uid)}
              onPick={(person) => {
                setWho((current) => [...current, person])
                setPicking(false)
              }}
              onClose={() => {
                setPicking(false)
              }}
            />
          </>
        ) : (
          <OpenTaskPicker busy={busy} onPick={addTo} />
        )}

        {/* One line on what the task is over: the selection's size, or — from
            the bare button — that it is over nothing, which is allowed and
            worth saying rather than leaving the reader to wonder. */}
        {photoUids.length > 0 ? (
          <p className="text-body-secondary small mb-0 mt-2">
            {t('tasks.row.photos', { count: photoUids.length })}
          </p>
        ) : (
          mode === 'new' && (
            <p className="text-body-secondary small mb-0 mt-2">{t('tasks.new.noPhotos')}</p>
          )
        )}
      </Modal.Body>
      <Modal.Footer>
        <Button variant="secondary" onClick={onClose} disabled={busy}>
          {t('confirmModal.cancel')}
        </Button>
        {mode === 'new' && (
          <Button variant="primary" onClick={submit} disabled={busy || title.trim() === ''}>
            {busy ? t('taskDetail.controls.saving') : t('tasks.create')}
          </Button>
        )}
      </Modal.Footer>
    </Modal>
  )
}

/** Fetch lifecycle of the open questions the picker chooses from. */
type PickerState = { status: 'loading' } | { status: 'error' } | { status: 'ready'; tasks: Task[] }

/**
 * Picks the question to add to. Only the open ones are offered: a closed task is
 * the record of a decision, and adding photographs to it after the fact would
 * make its frozen group a lie about what the work touched.
 *
 * Picking is the action — there is no second confirm step, because the choice
 * itself is the whole of it, and the page it lands on shows what happened.
 */
function OpenTaskPicker({ busy, onPick }: { busy: boolean; onPick: (task: Task) => void }) {
  const { t } = useTranslation()
  const [state, setState] = useState<PickerState>({ status: 'loading' })

  useEffect(() => {
    const controller = new AbortController()
    fetchTasks({ open: true, limit: 50 }, controller.signal)
      .then((page) => {
        setState({ status: 'ready', tasks: page.tasks })
      })
      .catch((err: unknown) => {
        if (!(err instanceof DOMException && err.name === 'AbortError')) {
          setState({ status: 'error' })
        }
      })
    return () => {
      controller.abort()
    }
  }, [])

  if (state.status === 'loading') {
    return (
      <div className="d-flex justify-content-center py-3">
        <Spinner animation="border" role="status" size="sm">
          <span className="visually-hidden">{t('tasks.loading')}</span>
        </Spinner>
      </div>
    )
  }

  if (state.status === 'error') {
    return (
      <Alert variant="danger" className="py-2 mb-0">
        {t('tasks.error')}
      </Alert>
    )
  }

  if (state.tasks.length === 0) {
    return <p className="text-body-secondary mb-0">{t('tasks.add.noneOpen')}</p>
  }

  // A flex column, not `d-grid`: a grid's implicit column is sized by its widest
  // item, so one long question made every row overflow the dialog and carried
  // the state badge outside it. Stretched flex items take the container's width,
  // which is what lets the title truncate.
  return (
    <div className="d-flex flex-column gap-1">
      {state.tasks.map((task) => (
        <Button
          key={task.uid}
          variant="outline-secondary"
          className="d-flex align-items-center gap-2 text-start"
          disabled={busy}
          onClick={() => {
            onPick(task)
          }}
        >
          <Icon name="plus-lg" />
          <span className="flex-grow-1 kk-min-w-0 text-truncate">{task.title}</span>
          <TaskStateBadge state={task.state} />
        </Button>
      ))}
    </div>
  )
}
