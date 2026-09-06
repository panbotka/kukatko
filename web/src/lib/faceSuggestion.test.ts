import { describe, expect, it } from 'vitest'

import { type Suggestion } from '../services/people'

import { rankSuggestions, SUGGESTION_DISPLAY_FLOOR } from './faceSuggestion'

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
      [suggestion('Alice', 0.1), suggestion('Bob', 0.4), suggestion('Cyril', 0.2)],
      3,
    )

    expect(ranked.offered).toEqual([])
    expect(ranked.uncertain?.subject_name).toBe('Bob')
  })

  it('has nothing to say about a face with no suggestions', () => {
    expect(rankSuggestions([], 3)).toEqual({ offered: [], uncertain: null })
  })
})
