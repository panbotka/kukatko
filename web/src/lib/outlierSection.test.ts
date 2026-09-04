import { describe, expect, it } from 'vitest'

import { type OutlierFace } from '../services/people'

import {
  answerQuestion,
  askableQuestions,
  hiddenCount,
  OUTLIER_SECTION_BATCH,
  revealedQuestions,
  toQuestions,
} from './outlierSection'

/** A ranked face; `index` distinguishes it and orders it in the ranking. */
function face(index: number): OutlierFace {
  return {
    photo_uid: `p${String(index)}`,
    face_index: 0,
    bbox: [0.1, 0.1, 0.2, 0.2],
    det_score: 0.9,
    distance: 0.5 - index / 100,
    marker_uid: `mk${String(index)}`,
    width: 1000,
    height: 800,
    orientation: 1,
  }
}

/** `count` ranked faces, in the order the endpoint hands them over. */
function faces(count: number): OutlierFace[] {
  return Array.from({ length: count }, (_, i) => face(i))
}

describe('outlierSection', () => {
  it('seeds every face unanswered, in the ranking order', () => {
    const items = toQuestions(faces(3))
    expect(items.map((item) => item.face.photo_uid)).toEqual(['p0', 'p1', 'p2'])
    expect(items.every((item) => item.answer === 'pending')).toBe(true)
  })

  it('answers one face and leaves its neighbours alone', () => {
    const items = answerQuestion(toQuestions(faces(3)), 'p1:0', 'removed')
    expect(items.map((item) => item.answer)).toEqual(['pending', 'removed', 'pending'])
  })

  it('takes the answer back on the same face', () => {
    const removed = answerQuestion(toQuestions(faces(2)), 'p0:0', 'removed')
    const restored = answerQuestion(removed, 'p0:0', 'pending')
    expect(restored.map((item) => item.answer)).toEqual(['pending', 'pending'])
  })

  it('never asks about a face whose picture could not be produced', () => {
    const items = toQuestions(faces(3))
    const askable = askableQuestions(items, new Set(['p1:0']))
    expect(askable.map((item) => item.face.photo_uid)).toEqual(['p0', 'p2'])
  })

  it('shows one batch and counts the rest as hidden', () => {
    const items = toQuestions(faces(OUTLIER_SECTION_BATCH + 3))
    expect(revealedQuestions(items, OUTLIER_SECTION_BATCH)).toHaveLength(OUTLIER_SECTION_BATCH)
    expect(hiddenCount(items, OUTLIER_SECTION_BATCH)).toBe(3)
  })

  it('hides nothing when the person has fewer outliers than one batch', () => {
    const items = toQuestions(faces(3))
    expect(revealedQuestions(items, OUTLIER_SECTION_BATCH)).toHaveLength(3)
    expect(hiddenCount(items, OUTLIER_SECTION_BATCH)).toBe(0)
  })

  it('slides the next ranked face in when a shown one turns out unrenderable', () => {
    const items = toQuestions(faces(OUTLIER_SECTION_BATCH + 1))
    const askable = askableQuestions(items, new Set(['p0:0']))
    const shown = revealedQuestions(askable, OUTLIER_SECTION_BATCH)
    expect(shown).toHaveLength(OUTLIER_SECTION_BATCH)
    expect(shown.at(-1)?.face.photo_uid).toBe(`p${String(OUTLIER_SECTION_BATCH)}`)
    expect(hiddenCount(askable, OUTLIER_SECTION_BATCH)).toBe(0)
  })

  it('treats a nonsensical reveal count as showing nothing', () => {
    const items = toQuestions(faces(2))
    expect(revealedQuestions(items, -5)).toEqual([])
    expect(hiddenCount(items, -5)).toBe(2)
  })
})
