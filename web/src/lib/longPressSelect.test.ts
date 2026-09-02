import { describe, expect, it } from 'vitest'

import {
  IDLE_LONG_PRESS,
  LONG_PRESS_SLOP,
  type LongPressEvent,
  type LongPressState,
  longPressStep,
} from './longPressSelect'

/** Replays a run of events, collecting everything each step added. */
function replay(
  events: LongPressEvent[],
  start: LongPressState = IDLE_LONG_PRESS,
): { state: LongPressState; added: string[]; engagements: number } {
  let state = start
  const added: string[] = []
  let engagements = 0
  for (const event of events) {
    const step = longPressStep(state, event)
    state = step.state
    added.push(...step.added)
    if (step.engaged) {
      engagements += 1
    }
  }
  return { state, added, engagements }
}

/** A press on `uid` at the origin, followed by the hold timer firing. */
function engaged(uid: string): LongPressEvent[] {
  return [{ type: 'press', uid, point: { x: 100, y: 100 } }, { type: 'hold' }]
}

describe('longPressStep', () => {
  it('starts idle and tracks nothing before a finger goes down', () => {
    expect(IDLE_LONG_PRESS.phase).toBe('idle')
    const step = longPressStep(IDLE_LONG_PRESS, {
      type: 'move',
      point: { x: 10, y: 10 },
      uid: 'a',
    })
    expect(step.state).toEqual(IDLE_LONG_PRESS)
    expect(step.added).toEqual([])
  })

  it('holds a press pending and selects the pressed tile when the timer fires', () => {
    const pressed = longPressStep(IDLE_LONG_PRESS, {
      type: 'press',
      uid: 'a',
      point: { x: 10, y: 10 },
    })
    expect(pressed.state.phase).toBe('pending')
    // Nothing is selected while the timer runs — a plain tap must stay a tap.
    expect(pressed.added).toEqual([])
    expect(pressed.engaged).toBe(false)

    const held = longPressStep(pressed.state, { type: 'hold' })
    expect(held.state.phase).toBe('selecting')
    expect(held.added).toEqual(['a'])
    expect(held.engaged).toBe(true)
  })

  it('tolerates a jitter within the slop while pending', () => {
    const { state, added } = replay([
      { type: 'press', uid: 'a', point: { x: 100, y: 100 } },
      { type: 'move', point: { x: 100 + LONG_PRESS_SLOP - 1, y: 100 }, uid: null },
      { type: 'hold' },
    ])
    expect(state.phase).toBe('selecting')
    expect(added).toEqual(['a'])
  })

  it('abandons the press when the finger travels beyond the slop first — that touch is a scroll', () => {
    const { state, added, engagements } = replay([
      { type: 'press', uid: 'a', point: { x: 100, y: 100 } },
      { type: 'move', point: { x: 100, y: 100 + LONG_PRESS_SLOP + 1 }, uid: null },
      // A timer that fires after the gesture was dropped decides nothing.
      { type: 'hold' },
    ])
    expect(state).toEqual(IDLE_LONG_PRESS)
    expect(added).toEqual([])
    expect(engagements).toBe(0)
  })

  it('honours a caller-supplied slop', () => {
    const pressed = longPressStep(IDLE_LONG_PRESS, {
      type: 'press',
      uid: 'a',
      point: { x: 0, y: 0 },
    })
    const generous = longPressStep(
      pressed.state,
      { type: 'move', point: { x: 0, y: 40 }, uid: null },
      { slop: 50 },
    )
    expect(generous.state.phase).toBe('pending')
    const strict = longPressStep(
      pressed.state,
      { type: 'move', point: { x: 0, y: 40 }, uid: null },
      { slop: 5 },
    )
    expect(strict.state.phase).toBe('idle')
  })

  it('adds every tile an engaged drag crosses, once each', () => {
    const { state, added } = replay([
      ...engaged('a'),
      { type: 'move', point: { x: 150, y: 100 }, uid: 'b' },
      { type: 'move', point: { x: 160, y: 100 }, uid: 'b' },
      { type: 'move', point: { x: 200, y: 100 }, uid: 'c' },
      // Dragging back over an already-added tile leaves it selected: the gesture
      // is additive, undoing a pick is the tile's own tap.
      { type: 'move', point: { x: 150, y: 100 }, uid: 'b' },
    ])
    expect(added).toEqual(['a', 'b', 'c'])
    expect(state.covered).toEqual(['a', 'b', 'c'])
  })

  it('ignores a drag over the gutter between tiles', () => {
    const { added } = replay([
      ...engaged('a'),
      { type: 'move', point: { x: 140, y: 100 }, uid: null },
      { type: 'move', point: { x: 150, y: 100 }, uid: 'b' },
    ])
    expect(added).toEqual(['a', 'b'])
  })

  it('never leaves the slop test standing once engaged — a long drag keeps selecting', () => {
    const { state, added } = replay([
      ...engaged('a'),
      { type: 'move', point: { x: 100, y: 900 }, uid: 'z' },
    ])
    expect(state.phase).toBe('selecting')
    expect(added).toEqual(['a', 'z'])
  })

  it('returns to idle when the finger lifts and forgets what it covered', () => {
    const { state } = replay([
      ...engaged('a'),
      { type: 'move', point: { x: 150, y: 100 }, uid: 'b' },
      { type: 'end' },
    ])
    expect(state).toEqual(IDLE_LONG_PRESS)
  })

  it('restarts cleanly when a new finger presses mid-gesture', () => {
    const { state, added } = replay([
      ...engaged('a'),
      { type: 'move', point: { x: 150, y: 100 }, uid: 'b' },
      { type: 'press', uid: 'c', point: { x: 300, y: 300 } },
    ])
    expect(state.phase).toBe('pending')
    expect(state.uid).toBe('c')
    expect(state.covered).toEqual([])
    expect(added).toEqual(['a', 'b'])
  })

  it('leaves a stale hold with no pressed tile alone', () => {
    const step = longPressStep({ ...IDLE_LONG_PRESS, phase: 'pending' }, { type: 'hold' })
    expect(step.state.phase).toBe('pending')
    expect(step.added).toEqual([])
    expect(step.engaged).toBe(false)
  })

  it('does not mutate the state it is given', () => {
    const before: LongPressState = {
      phase: 'selecting',
      uid: 'a',
      origin: { x: 1, y: 2 },
      covered: ['a'],
    }
    longPressStep(before, { type: 'move', point: { x: 9, y: 9 }, uid: 'b' })
    expect(before.covered).toEqual(['a'])
  })
})
