import { describe, expect, it } from 'vitest'

import { type Bbox, type FaceView, type Suggestion } from '../services/people'

import {
  bulkConfirmations,
  rankSuggestions,
  SUGGESTION_DISPLAY_FLOOR,
  topSuggestion,
} from './faceSuggestion'

/** suggestion builds a ranked candidate at the given confidence. */
function suggestion(name: string, confidence: number): Suggestion {
  return {
    subject_uid: `su_${name}`,
    subject_name: name,
    distance: 1 - confidence,
    confidence,
  }
}

describe('rankSuggestions', () => {
  it('offers every suggestion that clears the floor, capped at max', () => {
    const ranked = rankSuggestions(
      [suggestion('Alice', 0.9), suggestion('Bob', 0.7), suggestion('Cyril', 0.6)],
      2,
    )

    expect(ranked.offered.map((s) => s.subject_name)).toEqual(['Alice', 'Bob'])
    expect(ranked.uncertain).toBeNull()
  })

  it('drops the weak ones from a mixed list rather than ranking them behind', () => {
    const ranked = rankSuggestions([suggestion('Alice', 0.8), suggestion('Bob', 0.1)], 3)

    expect(ranked.offered.map((s) => s.subject_name)).toEqual(['Alice'])
    expect(ranked.uncertain).toBeNull()
  })

  it('treats a suggestion exactly at the floor as offerable', () => {
    const ranked = rankSuggestions([suggestion('Alice', SUGGESTION_DISPLAY_FLOOR)], 3)

    expect(ranked.offered.map((s) => s.subject_name)).toEqual(['Alice'])
  })

  it('offers nothing below the floor and keeps only the strongest as uncertain', () => {
    const ranked = rankSuggestions(
      [suggestion('Alice', 0.1), suggestion('Bob', 0.35), suggestion('Cyril', 0.2)],
      3,
    )

    expect(ranked.offered).toEqual([])
    expect(ranked.uncertain?.subject_name).toBe('Bob')
  })

  it('offers a 40 % suggestion — the floor was lowered to admit exactly those', () => {
    // The band 0.40–0.50 used to be muted text. Measured against the names a
    // human already gave, its top suggestion is the right person 86 % of the
    // time, so it is a button now (see `docs/THRESHOLDS.md`).
    const ranked = rankSuggestions([suggestion('Alice', 0.42)], 3)

    expect(ranked.offered.map((s) => s.subject_name)).toEqual(['Alice'])
    expect(ranked.uncertain).toBeNull()
  })

  it('has nothing to say about a face with no suggestions', () => {
    expect(rankSuggestions([], 3)).toEqual({ offered: [], uncertain: null })
  })
})

/** face builds an unnamed detection carrying the given ranked suggestions. */
function face(faceIndex: number, suggestions: Suggestion[] = []): FaceView {
  return {
    face_index: faceIndex,
    bbox: [0.1, 0.2, 0.3, 0.4] as Bbox,
    det_score: 0.9,
    action: 'create_marker',
    suggestions,
  }
}

describe('bulkConfirmations', () => {
  it('takes every unnamed face whose strongest suggestion is offered', () => {
    const batch = bulkConfirmations([
      face(0, [suggestion('Alice', 0.9), suggestion('Bob', 0.6)]),
      face(1, [suggestion('Cyril', 0.7)]),
    ])

    expect(batch.map((item) => [item.face.face_index, item.subject.subject_name])).toEqual([
      [0, 'Alice'],
      [1, 'Cyril'],
    ])
  })

  it('picks the strongest suggestion by comparison, not by list order', () => {
    const batch = bulkConfirmations([face(0, [suggestion('Bob', 0.6), suggestion('Alice', 0.9)])])

    expect(batch.map((item) => item.subject.subject_name)).toEqual(['Alice'])
  })

  it('leaves out a face whose best suggestion is only uncertain', () => {
    // 0.39 is below the display floor, so the panel shows it muted rather than as
    // a button — and a button is exactly what the bulk action presses.
    expect(bulkConfirmations([face(0, [suggestion('Alice', 0.39)])])).toEqual([])
  })

  it('follows the floor rather than a threshold of its own', () => {
    // A 42 % suggestion is offered as a chip, so "confirm all" presses it too:
    // the bulk action is the rows, repeated.
    const batch = bulkConfirmations([face(0, [suggestion('Alice', 0.42)])])

    expect(batch.map((item) => item.subject.subject_name)).toEqual(['Alice'])
  })

  it('leaves out a face with no suggestions at all', () => {
    expect(bulkConfirmations([face(0)])).toEqual([])
  })

  it('leaves out a face that already names somebody', () => {
    const named: FaceView = {
      ...face(0, [suggestion('Alice', 0.9)]),
      marker_uid: 'mk_1',
      subject_name: 'Zoe',
      action: 'assign_person',
    }

    expect(bulkConfirmations([named])).toEqual([])
  })

  it('leaves out a marker with no embedding behind it', () => {
    // A negative index is a marker the detector never produced a face for; it can
    // only ever be named by hand.
    expect(bulkConfirmations([face(-1, [suggestion('Alice', 0.9)])])).toEqual([])
  })

  it('confirms one person once: the weaker of two faces suggesting them is left', () => {
    const batch = bulkConfirmations([
      face(0, [suggestion('Alice', 0.7)]),
      face(1, [suggestion('Alice', 0.95)]),
      face(2, [suggestion('Bob', 0.8)]),
    ])

    expect(batch.map((item) => [item.face.face_index, item.subject.subject_name])).toEqual([
      [1, 'Alice'],
      [2, 'Bob'],
    ])
  })

  it('keeps the panel order even when the winner was found late', () => {
    const batch = bulkConfirmations([
      face(0, [suggestion('Bob', 0.8)]),
      face(1, [suggestion('Alice', 0.6)]),
      face(2, [suggestion('Alice', 0.9)]),
    ])

    expect(batch.map((item) => item.face.face_index)).toEqual([0, 2])
  })
})

describe('topSuggestion', () => {
  it('names the strongest offered candidate, found by comparison', () => {
    const top = topSuggestion(face(0, [suggestion('Bob', 0.6), suggestion('Alice', 0.9)]))

    expect(top?.subject_name).toBe('Alice')
  })

  it('names nobody when the best candidate is below the floor', () => {
    expect(topSuggestion(face(0, [suggestion('Alice', 0.39)]))).toBeNull()
  })

  it('names nobody for a face that already names somebody', () => {
    const named = {
      ...face(0, [suggestion('Alice', 0.9)]),
      marker_uid: 'mk_1',
      subject_name: 'Zoe',
    }

    expect(topSuggestion(named)).toBeNull()
  })

  it('names nobody for a marker with no embedding behind it', () => {
    expect(topSuggestion(face(-1, [suggestion('Alice', 0.9)]))).toBeNull()
  })
})
