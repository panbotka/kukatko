import { Badge } from 'react-bootstrap'
import { useTranslation } from 'react-i18next'

import { type TaskState } from '../../services/tasks'

/**
 * The colour each state is drawn in. The three open states are warm — something
 * is waiting on somebody — and the two closed ones are quiet: "rejected" is a
 * result rather than a failure, so it is grey, not red.
 */
const VARIANT: Record<TaskState, string> = {
  question: 'warning text-dark',
  working: 'info text-dark',
  review: 'primary',
  done: 'success',
  rejected: 'secondary',
}

/** Draws one task state as a badge, named in the reader's language. */
export function TaskStateBadge({ state }: { state: TaskState }) {
  const { t } = useTranslation()
  return <Badge bg={VARIANT[state]}>{t(`tasks.state.${state}`)}</Badge>
}
