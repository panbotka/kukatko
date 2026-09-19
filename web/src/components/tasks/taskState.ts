import { type TaskState } from '../../services/tasks'
import { type IconName } from '../Icon'

/** Presentation for one task state: its colour class and its glyph. */
export interface TaskStateStyle {
  /**
   * The `.kk-task-state--*` modifier (defined once in `styles/tokens.css`) that
   * paints a badge in this state's hue with legible text.
   */
  className: string
  /** The leading glyph that carries the meaning when the hue cannot. */
  icon: IconName
}

/**
 * The single source of truth for what each state looks like — read by the badge
 * and by the stripe down the side of a row in the queue, so a state is the same
 * colour wherever it appears.
 *
 * The five hues are deliberately far apart rather than five shades of one
 * warning colour: the queue is read by scanning it, and a page where every row
 * is the same colour is a page you have to read word by word. They run warm to
 * cool in the order work moves — amber is waiting on a person, cyan means
 * somebody is on it, violet is waiting for a yes, green is finished — and the
 * one closed state that is not a success is slate, because "rejected with a
 * reason" is a full result and not a failure to paint red.
 *
 * Colour is only ever an aid: every badge also carries its name in words and a
 * glyph, so nothing here is lost on a colour-blind reader or in a screenshot
 * printed in grey.
 */
export const TASK_STATE_STYLE: Record<TaskState, TaskStateStyle> = {
  question: { className: 'kk-task-state--question', icon: 'question-circle' },
  working: { className: 'kk-task-state--working', icon: 'hourglass-split' },
  review: { className: 'kk-task-state--review', icon: 'eye' },
  done: { className: 'kk-task-state--done', icon: 'check-lg' },
  rejected: { className: 'kk-task-state--rejected', icon: 'slash-circle' },
}
