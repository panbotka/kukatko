import { describe, expect, it } from 'vitest'

import { approximateAge, captureYear, formatLifeSpan, MAX_PLAUSIBLE_AGE } from './lifeYears'

describe('captureYear', () => {
  it('reads the year of an ISO capture time', () => {
    expect(captureYear('1946-08-12T10:30:00Z')).toBe(1946)
  })

  it('reads it in UTC, so a New Years Eve photo stays in its own year', () => {
    // Read in a zone west of Greenwich this would be 1945 — and, worse for the
    // decade navigation, the previous decade.
    expect(captureYear('1950-01-01T00:30:00Z')).toBe(1950)
    expect(captureYear('1949-12-31T23:30:00Z')).toBe(1949)
  })

  it('has no year for a missing, empty or unparseable timestamp', () => {
    expect(captureYear(undefined)).toBeNull()
    expect(captureYear(null)).toBeNull()
    expect(captureYear('')).toBeNull()
    expect(captureYear('sometime in the fifties')).toBeNull()
  })
})

describe('approximateAge', () => {
  it('is the difference of the two years', () => {
    expect(approximateAge('1946-08-12T10:30:00Z', 1923)).toBe(23)
  })

  it('ignores the month, because a birth year is all that is known', () => {
    // Both of these are "~23": with no birthday there is nothing finer to say,
    // and the label's "~" is what carries the uncertainty.
    expect(approximateAge('1946-01-02T00:00:00Z', 1923)).toBe(23)
    expect(approximateAge('1946-12-30T00:00:00Z', 1923)).toBe(23)
  })

  it('is zero on a photo from the birth year itself', () => {
    expect(approximateAge('1923-06-01T00:00:00Z', 1923)).toBe(0)
  })

  it('says nothing about a photo taken before the birth', () => {
    // The person is not on it, or one of the two dates is wrong. Either way a
    // negative age would be a claim about a picture nobody can vouch for.
    expect(approximateAge('1920-06-01T00:00:00Z', 1923)).toBeNull()
  })

  it('says nothing about an absurd age', () => {
    const stillFine = `${String(1923 + MAX_PLAUSIBLE_AGE)}-06-01T00:00:00Z`
    const tooOld = `${String(1923 + MAX_PLAUSIBLE_AGE + 1)}-06-01T00:00:00Z`
    expect(approximateAge(stillFine, 1923)).toBe(MAX_PLAUSIBLE_AGE)
    expect(approximateAge(tooOld, 1923)).toBeNull()
  })

  it('says nothing when either half is unknown', () => {
    expect(approximateAge('1946-08-12T10:30:00Z', null)).toBeNull()
    expect(approximateAge('1946-08-12T10:30:00Z', undefined)).toBeNull()
    expect(approximateAge(undefined, 1923)).toBeNull()
    expect(approximateAge('', 1923)).toBeNull()
  })
})

describe('formatLifeSpan', () => {
  it('writes a closed life as an en-dashed range', () => {
    expect(formatLifeSpan(1923, 1998)).toBe('1923–1998')
  })

  it('writes a known birth alone with the born-in mark', () => {
    expect(formatLifeSpan(1923, null)).toBe('*1923')
  })

  it('writes a known death alone rather than swallowing it', () => {
    expect(formatLifeSpan(null, 1998)).toBe('†1998')
  })

  it('has nothing to show when neither year was recorded', () => {
    expect(formatLifeSpan(null, null)).toBeNull()
    expect(formatLifeSpan(undefined, undefined)).toBeNull()
  })
})
