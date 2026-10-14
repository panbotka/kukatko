import type { TFunction } from 'i18next'
import { useCallback, useState } from 'react'
import { Button } from 'react-bootstrap'
import { useTranslation } from 'react-i18next'

import { ConfirmModal } from '../../components/ConfirmModal'
import { Icon } from '../../components/Icon'
import { PersonAvatar } from '../../components/PersonAvatar'
import { PersonPicker } from '../../components/tasks/PersonPicker'
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
 * The name a participant is shown under: „Bývalý člen projektu" for one who has
 * left the project (the server sends no name then), the name otherwise.
 */
function shownName(person: Participant, t: TFunction): string {
  return person.former === true ? t('formerMember') : person.name
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
 *
 * Taking somebody off is confirmed first rather than undone after. An undo would
 * have to put them back through `assign`, which records the remover as having
 * *asked* them — so a person who was on the task because they acted would come
 * back with a different provenance than they left with. A confirm keeps the
 * write faithful and costs one tap on a control that is rarely used.
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
  // Who the reader asked to take off, until they confirm or back out.
  const [removing, setRemoving] = useState<Participant | null>(null)

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
        setRemoving(null)
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
              ? t('taskDetail.people.acted', { name: shownName(person, t) })
              : t('taskDetail.people.askedBy', {
                  name: shownName(person, t),
                  by:
                    person.added_by_former === true
                      ? t('formerMember')
                      : (person.added_by_name ?? ''),
                })
          }
        >
          <PersonAvatar
            name={shownName(person, t)}
            userUid={person.former === true ? undefined : person.user_uid}
          />
          <span className="kk-min-w-0 text-truncate">{shownName(person, t)}</span>
          {canEdit && (
            <Button
              variant="link"
              size="sm"
              className="kk-task-person__remove kukatko-tap-target-touch"
              disabled={busy}
              aria-label={t('taskDetail.people.remove', { name: shownName(person, t) })}
              title={t('taskDetail.people.remove', { name: shownName(person, t) })}
              onClick={() => {
                setFailed(false)
                setRemoving(person)
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

      <ConfirmModal
        show={removing !== null}
        title={t('taskDetail.people.removeConfirm.title', {
          name: removing === null ? '' : shownName(removing, t),
        })}
        confirmLabel={t('taskDetail.people.removeConfirm.confirm')}
        busy={busy}
        onConfirm={() => {
          if (removing !== null) {
            void remove(removing.user_uid)
          }
        }}
        onCancel={() => {
          setRemoving(null)
        }}
      >
        {t('taskDetail.people.removeConfirm.body', {
          name: removing === null ? '' : shownName(removing, t),
        })}
      </ConfirmModal>

      <PersonPicker
        show={picking}
        busy={busy}
        already={participants.map((p) => p.user_uid)}
        onPick={(user) => {
          void add(user.uid)
        }}
        onClose={() => {
          setPicking(false)
        }}
      />
    </div>
  )
}
