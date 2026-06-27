import { describe, expect, it } from 'vitest'

import { purgeCountdown } from './trashCountdown'

const DAY = 24 * 60 * 60 * 1000
const now = Date.parse('2026-06-27T12:00:00Z')

describe('purgeCountdown', () => {
  it('returns null when the photo is not archived', () => {
    expect(purgeCountdown(undefined, 30, now)).toBeNull()
    expect(purgeCountdown('', 30, now)).toBeNull()
  })

  it('returns null when retention is disabled', () => {
    expect(purgeCountdown('2026-06-01T00:00:00Z', 0, now)).toBeNull()
    expect(purgeCountdown('2026-06-01T00:00:00Z', -5, now)).toBeNull()
  })

  it('returns null for an unparseable timestamp', () => {
    expect(purgeCountdown('not-a-date', 30, now)).toBeNull()
  })

  it('rounds the remaining days up', () => {
    // Archived 28 days ago, 30-day retention → ~2 days left.
    const archived = new Date(now - 28 * DAY).toISOString()
    expect(purgeCountdown(archived, 30, now)).toEqual({ daysLeft: 2, due: false })

    // Archived 29.5 days ago → ~0.5 day → rounds up to 1.
    const archived2 = new Date(now - 29.5 * DAY).toISOString()
    expect(purgeCountdown(archived2, 30, now)).toEqual({ daysLeft: 1, due: false })
  })

  it('flags items past their purge time as due', () => {
    const archived = new Date(now - 31 * DAY).toISOString()
    expect(purgeCountdown(archived, 30, now)).toEqual({ daysLeft: 0, due: true })
  })

  it('treats the exact expiry moment as due', () => {
    const archived = new Date(now - 30 * DAY).toISOString()
    expect(purgeCountdown(archived, 30, now)).toEqual({ daysLeft: 0, due: true })
  })
})
