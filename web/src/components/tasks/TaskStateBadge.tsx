import { Badge } from 'react-bootstrap'
import { useTranslation } from 'react-i18next'

import { type TaskState } from '../../services/tasks'
import { Icon } from '../Icon'

import { TASK_STATE_STYLE } from './taskState'

/**
 * Draws one task state as a badge: its hue, its glyph and its name in the
 * reader's language. The colours themselves are documented in
 * {@link TASK_STATE_STYLE}, which the queue's row stripe reads too.
 */
export function TaskStateBadge({ state }: { state: TaskState }) {
  const { t } = useTranslation()
  const style = TASK_STATE_STYLE[state]
  return (
    <Badge className={`kk-task-state ${style.className}`}>
      <Icon name={style.icon} /> {t(`tasks.state.${state}`)}
    </Badge>
  )
}
