import { useState } from 'react'
import { Alert, Button } from 'react-bootstrap'
import { useTranslation } from 'react-i18next'

import { createComment, taskSubject } from '../../services/comments'
import { type AnswerOption, answerOptions, type Task } from '../../services/tasks'

/** Props for {@link TaskAnswers}. */
export interface TaskAnswersProps {
  task: Pick<Task, 'uid' | 'state' | 'options'>
  /**
   * The body of the reader's most recent comment on the task, or null. The option
   * equal to it is drawn as chosen, and tapping it again does nothing — the
   * answer is already in the thread.
   */
  chosen: string | null
  /** Called after an answer landed, so the page can refresh the thread. */
  onAnswered: () => void
}

/**
 * The question's answers as a row of large buttons: the task's own options, or —
 * for a batch waiting in review — the built-in approve/return pair.
 *
 * Most questions the agent opens are a choice, and a batch in review needs a yes
 * or a no. Typing either into the composer at the foot of the page, on a phone,
 * past the photographs, is where answers were getting lost; this puts the choice
 * where the question is. A tap posts an **ordinary comment whose body is the
 * option text verbatim**, so the thread stays the one record and the agent reads
 * the answer with the same command as any other. The two built-in labels are
 * localised, the strings they post are not: the agent cannot know which language
 * the person read the page in.
 *
 * A viewer may tap as well as anyone: answering is what a viewer account is for.
 */
export function TaskAnswers({ task, chosen, onAnswered }: TaskAnswersProps) {
  const { t } = useTranslation()
  // The option whose comment is in flight, or null. One at a time: a second tap
  // while the first is posting would post twice.
  const [pending, setPending] = useState<string | null>(null)
  const [failed, setFailed] = useState(false)

  const options = answerOptions(task)
  if (options.length === 0) {
    return null
  }

  async function answer(option: AnswerOption) {
    if (pending !== null || option.text === chosen) {
      return
    }
    setPending(option.text)
    setFailed(false)
    try {
      await createComment(taskSubject(task.uid), option.text)
      onAnswered()
    } catch {
      setFailed(true)
    } finally {
      setPending(null)
    }
  }

  function label(option: AnswerOption): string {
    switch (option.builtin) {
      case 'approve':
        return t('taskDetail.answers.approve')
      case 'sendBack':
        return t('taskDetail.answers.sendBack')
      default:
        return option.text
    }
  }

  return (
    <section className="mb-4" aria-label={t('taskDetail.answers.label')}>
      <p className="text-body-secondary small mb-2">{t('taskDetail.answers.label')}</p>
      <div className="d-flex flex-wrap gap-2">
        {options.map((option) => {
          const isChosen = option.text === chosen
          return (
            <Button
              key={option.text}
              size="lg"
              variant={isChosen ? 'primary' : 'outline-primary'}
              aria-pressed={isChosen}
              disabled={pending !== null}
              onClick={() => {
                void answer(option)
              }}
            >
              {label(option)}
            </Button>
          )
        })}
      </div>
      {failed && (
        <Alert variant="danger" className="py-2 mt-2 mb-0">
          {t('taskDetail.answers.failed')}
        </Alert>
      )}
    </section>
  )
}
