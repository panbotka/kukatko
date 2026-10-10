import { isClosedState, type TaskState } from '../services/tasks'

/**
 * The name of one quick transition. Each names the *intent* rather than the
 * target state, because the same target means different things from different
 * places — `working` from `review` is "send it back", from `question` it is
 * "the agent can go on".
 */
export type QuickTransitionId =
  | 'handOver'
  | 'needAnswer'
  | 'toReview'
  | 'sendBack'
  | 'approve'
  | 'reject'
  | 'reopen'

/** One one-click move offered above the task's full state picker. */
export interface QuickTransition {
  id: QuickTransitionId
  /** The state the move puts the task in. */
  to: TaskState
  /**
   * Whether the move closes the task. A closing move cannot be applied on its
   * own — the server refuses a closed task without a resolution — so it opens
   * the resolution field instead of saving.
   */
  closes: boolean
  /** The one move the current state is waiting for, drawn as the primary button. */
  primary: boolean
}

/**
 * The moves that make sense from each state, most likely first.
 *
 * - `question` (waiting on a person): the answer is in → **hand to the agent**
 *   (working); or the doubt was unfounded → **reject**.
 * - `working` (the agent's move): it needs a person after all → **need an
 *   answer** (question); or the work is finished → **to review**.
 * - `review` (waiting for approval): **approve** (done) or **send back to the
 *   agent** (working); rejecting outright is left to the full picker.
 * - `done` / `rejected`: **reopen** as a question — a closed task comes back
 *   because somebody doubts how it ended, which is a question again.
 *
 * Everything else (e.g. `question` → `review`) stays in the full `<select>`.
 */
const TRANSITIONS: Record<TaskState, readonly [QuickTransitionId, TaskState][]> = {
  question: [
    ['handOver', 'working'],
    ['reject', 'rejected'],
  ],
  working: [
    ['needAnswer', 'question'],
    ['toReview', 'review'],
  ],
  review: [
    ['approve', 'done'],
    ['sendBack', 'working'],
  ],
  done: [['reopen', 'question']],
  rejected: [['reopen', 'question']],
}

/** Returns the quick moves offered from `state`, the primary one first. */
export function quickTransitions(state: TaskState): QuickTransition[] {
  return TRANSITIONS[state].map(([id, to], index) => ({
    id,
    to,
    closes: isClosedState(to),
    primary: index === 0,
  }))
}
