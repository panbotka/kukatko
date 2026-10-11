import { describe, expect, it } from 'vitest'

import {
  addDays,
  endOfLocalDay,
  featureDateBounds,
  formatFeaturedUntil,
  isFeatured,
  toDateInputValue,
} from './featured'

describe('isFeatured', () => {
  const now = Date.parse('2026-10-11T12:00:00Z')

  it('is true only for an end still ahead', () => {
    expect(isFeatured({ featured_until: '2026-12-11T22:59:59Z' }, now)).toBe(true)
    expect(isFeatured({ featured_until: '2026-10-11T11:00:00Z' }, now)).toBe(false)
  })

  it('is false for a missing, null or malformed end', () => {
    expect(isFeatured({}, now)).toBe(false)
    expect(isFeatured({ featured_until: null }, now)).toBe(false)
    expect(isFeatured({ featured_until: 'soon' }, now)).toBe(false)
  })
})

describe('date input helpers', () => {
  it('formats a local date and moves it by days', () => {
    const date = new Date(2026, 9, 11, 15, 30)
    expect(toDateInputValue(date)).toBe('2026-10-11')
    expect(toDateInputValue(addDays(date, 61))).toBe('2026-12-11')
  })

  it('ends a picked day at its last local second', () => {
    const end = endOfLocalDay('2026-12-11')
    expect(end).toBeDefined()
    const date = new Date(end ?? '')
    expect([date.getFullYear(), date.getMonth(), date.getDate()]).toEqual([2026, 11, 11])
    expect([date.getHours(), date.getMinutes(), date.getSeconds()]).toEqual([23, 59, 59])
    expect(endOfLocalDay('')).toBeUndefined()
    expect(endOfLocalDay('11.12.2026')).toBeUndefined()
  })
})

describe('featureDateBounds', () => {
  const now = new Date(2026, 9, 11, 9, 0)

  it('offers today … today + max − 1, pre-filled with the default', () => {
    expect(featureDateBounds(61, 366, now)).toEqual({
      min: '2026-10-11',
      max: '2027-10-11',
      initial: '2026-12-11',
    })
  })

  it('never pre-fills an extension shorter than the running feature', () => {
    const current = new Date(2027, 2, 1, 23, 59, 59).toISOString()
    expect(featureDateBounds(61, 366, now, current).initial).toBe('2027-03-01')
    const soon = new Date(2026, 9, 20, 23, 59, 59).toISOString()
    expect(featureDateBounds(61, 366, now, soon).initial).toBe('2026-12-11')
  })

  it('clamps the pre-filled day to the cap', () => {
    expect(featureDateBounds(500, 366, now).initial).toBe('2027-10-11')
  })
})

describe('formatFeaturedUntil', () => {
  const now = new Date(2026, 9, 11)

  it('reads day and month, the year only when it differs', () => {
    const december = new Date(2026, 11, 11, 23, 59, 59).toISOString()
    expect(formatFeaturedUntil(december, 'cs', now)).toBe('11. 12.')
    expect(formatFeaturedUntil(december, 'en', now)).toBe('Dec 11')
    const nextYear = new Date(2027, 1, 3, 23, 59, 59).toISOString()
    expect(formatFeaturedUntil(nextYear, 'cs', now)).toBe('3. 2. 2027')
  })

  it('is empty for a malformed value', () => {
    expect(formatFeaturedUntil('nope', 'cs', now)).toBe('')
  })
})
