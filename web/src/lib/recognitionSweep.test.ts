import { describe, expect, it } from 'vitest'

import { type ReviewItem } from './candidateReview'
import {
  clampConfidencePercent,
  focusKey,
  focusSequence,
  hasActionable,
  nextFocusKey,
  type PersonState,
  personActionableCount,
  SWEEP_DEFAULT_PERCENT,
  SWEEP_MAX_PERCENT,
  SWEEP_MIN_PERCENT,
} from './recognitionSweep'
import { type Candidate } from '../services/faces'
import { type Photo } from '../services/photos'
import { type Subject } from '../services/people'

/** item builds a review item on a photo uid with the given status. */
function item(uid: string, status: ReviewItem['status']): ReviewItem {
  const candidate = {
    photo: { uid } as unknown as Photo,
    face_index: 0,
    bbox: { relative: [0, 0, 0.3, 0.3], pixel: [0, 0, 30, 30] },
    distance: 0.2,
    match_count: 1,
    action: 'create_marker',
  } as unknown as Candidate
  return { candidate, status }
}

/** person builds a person state with the given items. */
function person(uid: string, items: ReviewItem[]): PersonState {
  return { subject: { uid, name: uid } as unknown as Subject, items }
}

describe('clampConfidencePercent', () => {
  it('keeps a value inside the slider range and rounds it', () => {
    expect(clampConfidencePercent(72.6)).toBe(73)
    expect(clampConfidencePercent(SWEEP_MIN_PERCENT - 10)).toBe(SWEEP_MIN_PERCENT)
    expect(clampConfidencePercent(SWEEP_MAX_PERCENT + 10)).toBe(SWEEP_MAX_PERCENT)
  })

  it('falls back to the default for a non-finite value', () => {
    expect(clampConfidencePercent(Number.NaN)).toBe(SWEEP_DEFAULT_PERCENT)
  })
})

describe('personActionableCount / hasActionable', () => {
  it('counts pending and errored items but not done ones', () => {
    const p = person('Alice', [item('a', 'pending'), item('b', 'error'), item('c', 'done')])
    expect(personActionableCount(p)).toBe(2)
    expect(hasActionable(p)).toBe(true)
  })

  it('reports a fully-done person as cleared', () => {
    const p = person('Bob', [item('a', 'done'), item('b', 'done')])
    expect(personActionableCount(p)).toBe(0)
    expect(hasActionable(p)).toBe(false)
  })
})

describe('focusSequence', () => {
  it('flattens only actionable cards across people, in order', () => {
    const people = [
      person('Alice', [item('a', 'pending'), item('b', 'done')]),
      person('Bob', [item('c', 'error')]),
    ]
    const seq = focusSequence(people)
    expect(seq.map((entry) => entry.key)).toEqual([
      focusKey('Alice', item('a', 'pending').candidate),
      focusKey('Bob', item('c', 'error').candidate),
    ])
    expect(seq[0].subjectUid).toBe('Alice')
  })
})

describe('nextFocusKey', () => {
  const seq = focusSequence([person('Alice', [item('a', 'pending'), item('b', 'pending')])])
  const first = seq[0].key
  const second = seq[1].key

  it('advances to the following entry', () => {
    expect(nextFocusKey(seq, first)).toBe(second)
  })

  it('falls back to the previous entry at the end', () => {
    expect(nextFocusKey(seq, second)).toBe(first)
  })

  it('returns the first entry when the current key is gone', () => {
    expect(nextFocusKey(seq, 'missing')).toBe(first)
  })

  it('returns null for an empty sequence or a single cleared card', () => {
    expect(nextFocusKey([], null)).toBeNull()
    const single = focusSequence([person('Solo', [item('a', 'pending')])])
    expect(nextFocusKey(single, single[0].key)).toBeNull()
  })
})
