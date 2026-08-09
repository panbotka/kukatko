import { describe, expect, it } from 'vitest'

import { formatRelativeTime } from './relativeTime'

const NOW = Date.parse('2026-08-09T12:00:00Z')

/** An instant `ms` before the fixed `NOW`. */
function ago(ms: number): string {
  return new Date(NOW - ms).toISOString()
}

const SECOND = 1000
const MINUTE = 60 * SECOND
const HOUR = 60 * MINUTE
const DAY = 24 * HOUR

describe('formatRelativeTime', () => {
  it('says "now" for something that just happened', () => {
    expect(formatRelativeTime(ago(2 * SECOND), 'cs', NOW)).toBe('nyní')
    expect(formatRelativeTime(ago(2 * SECOND), 'en', NOW)).toBe('now')
  })

  it('counts minutes, then hours, then days', () => {
    expect(formatRelativeTime(ago(5 * MINUTE), 'cs', NOW)).toBe('před 5 min')
    expect(formatRelativeTime(ago(2 * HOUR), 'cs', NOW)).toBe('před 2 h')
    expect(formatRelativeTime(ago(3 * DAY), 'cs', NOW)).toBe('před 3 dny')
  })

  it('speaks English when asked to', () => {
    expect(formatRelativeTime(ago(2 * HOUR), 'en', NOW)).toBe('2h ago')
    expect(formatRelativeTime(ago(3 * DAY), 'en', NOW)).toBe('3d ago')
  })

  it('names yesterday rather than counting a day', () => {
    expect(formatRelativeTime(ago(25 * HOUR), 'cs', NOW)).toBe('včera')
    expect(formatRelativeTime(ago(25 * HOUR), 'en', NOW)).toBe('yesterday')
  })

  it('climbs to weeks, months and years for older instants', () => {
    expect(formatRelativeTime(ago(14 * DAY), 'en', NOW)).toBe('2w ago')
    expect(formatRelativeTime(ago(90 * DAY), 'en', NOW)).toBe('3mo ago')
    expect(formatRelativeTime(ago(800 * DAY), 'en', NOW)).toBe('2y ago')
  })

  it('reads a clock a moment ahead as "now", not as the future', () => {
    // The server stamped the comment a couple of seconds ahead of this browser;
    // telling the reader their own comment happens "in 3 s" would be nonsense.
    expect(formatRelativeTime(new Date(NOW + 3 * SECOND).toISOString(), 'cs', NOW)).toBe('nyní')
  })

  it('still formats a genuinely future instant as the future', () => {
    expect(formatRelativeTime(new Date(NOW + 2 * HOUR).toISOString(), 'en', NOW)).toBe('in 2h')
  })

  it('accepts epoch milliseconds and Date objects, not only ISO strings', () => {
    expect(formatRelativeTime(NOW - 2 * HOUR, 'en', NOW)).toBe('2h ago')
    expect(formatRelativeTime(new Date(NOW - 2 * HOUR), 'en', NOW)).toBe('2h ago')
  })

  it('returns an empty string for a value that is not a date', () => {
    expect(formatRelativeTime('not a date', 'cs', NOW)).toBe('')
  })
})
