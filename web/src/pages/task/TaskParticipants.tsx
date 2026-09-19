import { useCallback, useEffect, useState } from 'react'
import { Alert, Button, Modal, Spinner } from 'react-bootstrap'
import { useTranslation } from 'react-i18next'

import { Icon } from '../../components/Icon'
import { PersonAvatar } from '../../components/PersonAvatar'
import { type DirectoryUser, listDirectory } from '../../services/directory'
import {
  assignTaskParticipant,
  type Participant,
  unassignTaskParticipant,
} from '../../services/tasks'

/** Props for {@link TaskParticipants}. */
export interface TaskParticipantsProps {
  taskUid: string
  participants: Participant[]
  /** Whether the reader may put somebody on the task or take them off. */
  canEdit: boolean
  /** Reports the list back up after a change, so the page redraws from it. */
  onChange: (participants: Participant[]) => void
}

/**
 * Who is on this question.
 *
 * Almost everybody here arrived by acting: opening a task, answering it or
 * moving it along puts you on it, so the row fills itself as the work happens
 * and nobody has to maintain it. The one deliberate act is asking a particular
 * person — putting a name against a question before they have done anything —
 * which is how it reaches the one relative who would know.
 *
 * The two are told apart in the title of each chip, not by a second visual
 * language: they are the same fact (this person is involved) arrived at two ways,
 * and a reader scanning the row wants the faces, not the provenance.
 */
export function TaskParticipants({
  taskUid,
  participants,
  canEdit,
  onChange,
}: TaskParticipantsProps) {
  const { t } = useTranslation()
  const [picking, setPicking] = useState(false)
  const [busy, setBusy] = useState(false)
  const [failed, setFailed] = useState(false)

  const remove = useCallback(
    async (userUid: string) => {
      setBusy(true)
      setFailed(false)
      try {
        onChange(await unassignTaskParticipant(taskUid, userUid))
      } catch {
        setFailed(true)
      } finally {
        setBusy(false)
      }
    },
    [taskUid, onChange],
  )

  const add = useCallback(
    async (userUid: string) => {
      setBusy(true)
      setFailed(false)
      try {
        onChange(await assignTaskParticipant(taskUid, userUid))
        setPicking(false)
      } catch {
        setFailed(true)
      } finally {
        setBusy(false)
      }
    },
    [taskUid, onChange],
  )

  return (
    <div className="d-flex align-items-center gap-2 flex-wrap mb-3">
      <span className="kk-text-eyebrow text-body-secondary">{t('taskDetail.people.title')}</span>

      {participants.length === 0 && (
        <span className="text-body-secondary small">{t('taskDetail.people.nobody')}</span>
      )}

      {participants.map((person) => (
        <span
          key={person.user_uid}
          className="kk-task-person"
          title={
            person.added_by === undefined || person.added_by === ''
              ? t('taskDetail.people.acted', { name: person.name })
              : t('taskDetail.people.askedBy', {
                  name: person.name,
                  by: person.added_by_name ?? '',
                })
          }
        >
          <PersonAvatar name={person.name} userUid={person.user_uid} />
          <span className="kk-min-w-0 text-truncate">{person.name}</span>
          {canEdit && (
            <Button
              variant="link"
              size="sm"
              className="kk-task-person__remove"
              disabled={busy}
              aria-label={t('taskDetail.people.remove', { name: person.name })}
              onClick={() => {
                void remove(person.user_uid)
              }}
            >
              <Icon name="x-lg" />
            </Button>
          )}
        </span>
      ))}

      {canEdit && (
        <Button
          variant="outline-secondary"
          size="sm"
          disabled={busy}
          onClick={() => {
            setFailed(false)
            setPicking(true)
          }}
        >
          <Icon name="plus-lg" /> {t('taskDetail.people.add')}
        </Button>
      )}

      {failed && <span className="text-danger small">{t('taskDetail.controls.failed')}</span>}

      <PersonPicker
        show={picking}
        busy={busy}
        already={participants.map((p) => p.user_uid)}
        onPick={(uid) => {
          void add(uid)
        }}
        onClose={() => {
          setPicking(false)
        }}
      />
    </div>
  )
}

/** Fetch lifecycle of the directory the picker chooses from. */
type DirectoryState =
  | { status: 'loading' }
  | { status: 'error' }
  | { status: 'ready'; users: DirectoryUser[] }

/**
 * Picks somebody to ask. The directory is small — a family archive has a dozen
 * accounts, not a company's thousands — so it is a list to read rather than a
 * field to search, and it is fetched when the dialog opens rather than with the
 * page that hosts it.
 */
function PersonPicker({
  show,
  busy,
  already,
  onPick,
  onClose,
}: {
  show: boolean
  busy: boolean
  already: readonly string[]
  onPick: (uid: string) => void
  onClose: () => void
}) {
  const { t } = useTranslation()
  const [state, setState] = useState<DirectoryState>({ status: 'loading' })

  useEffect(() => {
    if (!show) {
      return
    }
    const controller = new AbortController()
    setState({ status: 'loading' })
    listDirectory(controller.signal)
      .then((users) => {
        setState({ status: 'ready', users })
      })
      .catch((err: unknown) => {
        if (!(err instanceof DOMException && err.name === 'AbortError')) {
          setState({ status: 'error' })
        }
      })
    return () => {
      controller.abort()
    }
  }, [show])

  const choices =
    state.status === 'ready' ? state.users.filter((u) => !already.includes(u.uid)) : []

  return (
    <Modal show={show} onHide={onClose} centered>
      <Modal.Header closeButton>
        <Modal.Title as="h2" className="h5">
          {t('taskDetail.people.pick')}
        </Modal.Title>
      </Modal.Header>
      <Modal.Body>
        {state.status === 'loading' && (
          <div className="d-flex justify-content-center py-3">
            <Spinner animation="border" role="status" size="sm">
              <span className="visually-hidden">{t('taskDetail.people.loading')}</span>
            </Spinner>
          </div>
        )}
        {state.status === 'error' && (
          <Alert variant="danger" className="py-2 mb-0">
            {t('taskDetail.people.loadFailed')}
          </Alert>
        )}
        {state.status === 'ready' && choices.length === 0 && (
          <p className="text-body-secondary mb-0">{t('taskDetail.people.everybody')}</p>
        )}
        {choices.length > 0 && (
          <div className="d-grid gap-1">
            {choices.map((user) => (
              <Button
                key={user.uid}
                variant="outline-secondary"
                className="d-flex align-items-center gap-2 text-start"
                disabled={busy}
                onClick={() => {
                  onPick(user.uid)
                }}
              >
                <PersonAvatar name={user.name} userUid={user.uid} />
                <span className="kk-min-w-0 text-truncate">{user.name}</span>
              </Button>
            ))}
          </div>
        )}
      </Modal.Body>
      <Modal.Footer>
        <Button variant="secondary" onClick={onClose} disabled={busy}>
          {t('confirmModal.cancel')}
        </Button>
      </Modal.Footer>
    </Modal>
  )
}
