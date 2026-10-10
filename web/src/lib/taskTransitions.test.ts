import { describe, expect, it } from 'vitest'

import { TASK_STATES } from '../services/tasks'

import { quickTransitions } from './taskTransitions'

describe('quickTransitions', () => {
  it('offers the documented moves from each state', () => {
    const moves = Object.fromEntries(
      TASK_STATES.map((state) => [state, quickTransitions(state).map((m) => `${m.id}→${m.to}`)]),
    )
    expect(moves).toEqual({
      question: ['handOver→working', 'reject→rejected'],
      working: ['needAnswer→question', 'toReview→review'],
      review: ['approve→done', 'sendBack→working'],
      done: ['reopen→question'],
      rejected: ['reopen→question'],
    })
  })

  it('never offers a move to the state the task is already in', () => {
    for (const state of TASK_STATES) {
      expect(quickTransitions(state).map((m) => m.to)).not.toContain(state)
    }
  })

  it('flags the closing moves and makes the first move the primary one', () => {
    const review = quickTransitions('review')
    expect(review.map((m) => [m.id, m.closes, m.primary])).toEqual([
      ['approve', true, true],
      ['sendBack', false, false],
    ])
    expect(quickTransitions('question').find((m) => m.id === 'reject')?.closes).toBe(true)
  })
})
