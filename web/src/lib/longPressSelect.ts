import { type TouchPoint } from './gestures'

/**
 * How long a finger has to rest on a tile before the selection gesture engages
 * (ms). Long enough not to fire while the wall is being flicked past, short
 * enough to feel deliberate — the value every mobile gallery has settled on.
 */
export const LONG_PRESS_MS = 450

/**
 * How far the finger may drift while the timer runs and still count as a press
 * (px). Beyond it the touch is a scroll, and the gesture stands down for good —
 * a wall that is being flicked must never start selecting under the finger.
 */
export const LONG_PRESS_SLOP = 12

/**
 * Where a touch is in the long-press gesture: nothing tracked, a finger resting
 * on a tile with the timer still running, or the engaged drag that paints a
 * selection across the tiles it crosses.
 */
export type LongPressPhase = 'idle' | 'pending' | 'selecting'

/** The gesture's whole state. Immutable — {@link longPressStep} returns a new one. */
export interface LongPressState {
  phase: LongPressPhase
  /** The tile the finger went down on; `null` while idle. */
  uid: string | null
  /** Where the finger went down, for the slop test; `null` while idle. */
  origin: TouchPoint | null
  /**
   * Tiles this drag has already added, so crossing one a second time (a finger
   * wandering back over a row) adds nothing twice.
   */
  covered: readonly string[]
}

/** The resting state: no touch is being tracked. */
export const IDLE_LONG_PRESS: LongPressState = {
  phase: 'idle',
  uid: null,
  origin: null,
  covered: [],
}

/**
 * What happened to the finger, as the state machine sees it: it went down on a
 * tile, the hold timer elapsed, it moved (over the tile named by `uid`, or over
 * nothing), or it left the screen / the gesture was abandoned.
 */
export type LongPressEvent =
  | { type: 'press'; uid: string; point: TouchPoint }
  | { type: 'hold' }
  | { type: 'move'; point: TouchPoint; uid: string | null }
  | { type: 'end' }

/** One transition of the gesture: the next state plus what it means to do. */
export interface LongPressStep {
  state: LongPressState
  /** Tiles this step adds to the selection, in the order they were crossed. */
  added: readonly string[]
  /** True on the one step where the press engaged — the moment to buzz. */
  engaged: boolean
}

/** Tuning for {@link longPressStep}. */
export interface LongPressOptions {
  /** Travel (px) that cancels a pending press. Default {@link LONG_PRESS_SLOP}. */
  slop?: number
}

/** A step that changes nothing — the common "this event does not apply" answer. */
function stay(state: LongPressState): LongPressStep {
  return { state, added: [], engaged: false }
}

/** Euclidean travel between two touch points. */
function travel(from: TouchPoint, to: TouchPoint): number {
  return Math.hypot(to.x - from.x, to.y - from.y)
}

/**
 * Advances the long-press selection gesture by one event.
 *
 * The whole gesture, DOM-free and therefore directly unit-testable: a finger
 * that goes down on a tile starts a *pending* press; the hold timer firing
 * *engages* it (selecting that tile, `engaged` — the moment the caller buzzes
 * and starts swallowing scroll); every tile the engaged drag then crosses is
 * added once, Google-Photos style. Movement beyond `slop` **before** the timer
 * fires abandons the gesture entirely — that touch is a scroll and must stay
 * one, so it does not merely postpone the press, it drops it (the caller cancels
 * its timer on seeing the state go idle).
 *
 * The gesture is deliberately **additive**: dragging back over a tile leaves it
 * selected. Undoing a pick is the tile's own tap, which is a far more precise
 * instrument than a finger being dragged across a wall.
 */
export function longPressStep(
  state: LongPressState,
  event: LongPressEvent,
  options: LongPressOptions = {},
): LongPressStep {
  const { slop = LONG_PRESS_SLOP } = options
  switch (event.type) {
    case 'press':
      // A new finger always restarts the gesture, whatever the old one was doing.
      return stay({ phase: 'pending', uid: event.uid, origin: event.point, covered: [] })
    case 'hold': {
      // A timer that outlived its press (it was cancelled, or the finger already
      // lifted) decides nothing.
      if (state.phase !== 'pending' || state.uid === null) {
        return stay(state)
      }
      return {
        state: { ...state, phase: 'selecting', covered: [state.uid] },
        added: [state.uid],
        engaged: true,
      }
    }
    case 'move': {
      if (state.phase === 'pending') {
        if (state.origin !== null && travel(state.origin, event.point) <= slop) {
          return stay(state)
        }
        return stay(IDLE_LONG_PRESS)
      }
      if (state.phase !== 'selecting' || event.uid === null || state.covered.includes(event.uid)) {
        return stay(state)
      }
      return {
        state: { ...state, covered: [...state.covered, event.uid] },
        added: [event.uid],
        engaged: false,
      }
    }
    case 'end':
      return stay(IDLE_LONG_PRESS)
  }
}
