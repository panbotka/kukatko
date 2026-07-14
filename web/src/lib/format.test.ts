import { describe, expect, it } from 'vitest'

import {
  formatByteCount,
  formatBytes,
  formatCaptureRange,
  formatDate,
  formatDateTime,
  formatDuration,
  formatMonth,
} from './format'

describe('formatBytes', () => {
  it('renders bytes without decimals', () => {
    expect(formatBytes(512)).toBe('512 B')
  })

  it('scales to binary units with one decimal', () => {
    expect(formatBytes(1536)).toBe('1.5 KB')
    expect(formatBytes(5 * 1024 * 1024)).toBe('5.0 MB')
  })

  it('localises the decimal separator when a locale is given', () => {
    expect(formatBytes(1536, 'cs')).toBe('1,5 KB')
    expect(formatBytes(1536, 'en')).toBe('1.5 KB')
    expect(formatBytes(512, 'cs')).toBe('512 B')
  })

  it('clamps non-positive and non-finite input to 0 B', () => {
    expect(formatBytes(0)).toBe('0 B')
    expect(formatBytes(-10)).toBe('0 B')
    expect(formatBytes(Number.NaN)).toBe('0 B')
  })
})

describe('formatByteCount', () => {
  it('groups the exact byte count in the active locale', () => {
    // Czech groups with a narrow no-break space, so compare on the digits.
    expect(formatByteCount(3145728, 'cs').replace(/\s/gu, ' ')).toBe('3 145 728 B')
    expect(formatByteCount(3145728, 'en')).toBe('3,145,728 B')
  })

  it('clamps non-positive and non-finite input to 0 B', () => {
    expect(formatByteCount(-1, 'en')).toBe('0 B')
    expect(formatByteCount(Number.NaN, 'en')).toBe('0 B')
  })
})

describe('formatDate / formatDateTime', () => {
  const iso = '2026-03-09T14:05:00Z'

  it('formats a date using the requested locale', () => {
    // Czech uses day-first dotted dates; en-US uses month-first slashes. We only
    // assert the locales differ so the formatting genuinely follows the UI
    // language rather than the host default.
    const cs = formatDate(iso, 'cs')
    const en = formatDate(iso, 'en-US')
    expect(cs).toContain('2026')
    expect(en).toContain('2026')
    expect(cs).not.toBe(en)
  })

  it('formats date and time including the year', () => {
    expect(formatDateTime(iso, 'cs')).toContain('2026')
  })

  it('returns the original string for an unparseable value', () => {
    expect(formatDate('not-a-date', 'cs')).toBe('not-a-date')
    expect(formatDateTime('', 'en')).toBe('')
  })
})

describe('formatMonth', () => {
  it('formats a 1-based year/month as a locale-aware month and year', () => {
    // January 2026 — assert the year is present and the two locales differ so
    // the month name genuinely follows the UI language.
    const cs = formatMonth(2026, 1, 'cs')
    const en = formatMonth(2026, 1, 'en-US')
    expect(cs).toContain('2026')
    expect(en).toContain('2026')
    expect(en.toLowerCase()).toContain('jan')
    expect(cs).not.toBe(en)
  })

  it('accepts month 12 (December) without rolling into the next year', () => {
    expect(formatMonth(2025, 12, 'en-US')).toContain('2025')
  })

  it('returns an empty string for an out-of-range month', () => {
    expect(formatMonth(2026, 0, 'en')).toBe('')
    expect(formatMonth(2026, 13, 'en')).toBe('')
  })
})

describe('formatCaptureRange', () => {
  // Every fixture sits at midday, mid-month, so no host timezone can shift it
  // into a neighbouring month or year and make the expectation depend on where
  // the test runs.
  it('renders a span inside one month as month/year', () => {
    expect(formatCaptureRange('2007-06-03T12:00:00Z', '2007-06-24T12:00:00Z')).toBe('6/2007')
  })

  it('renders a span inside one year as the bare year', () => {
    expect(formatCaptureRange('2006-02-10T12:00:00Z', '2006-11-20T12:00:00Z')).toBe('2006')
  })

  it('renders a span across years as first–last with an en dash', () => {
    expect(formatCaptureRange('1998-07-15T12:00:00Z', '1999-04-15T12:00:00Z')).toBe('1998–1999')
  })

  it('renders nothing for an album with no dated photos', () => {
    expect(formatCaptureRange(undefined, undefined)).toBe('')
    expect(formatCaptureRange('2006-02-10T12:00:00Z', undefined)).toBe('')
    expect(formatCaptureRange(undefined, '2006-02-10T12:00:00Z')).toBe('')
    expect(formatCaptureRange('not-a-date', 'not-a-date')).toBe('')
  })

  it('collapses a single photo to its own month', () => {
    expect(formatCaptureRange('2019-09-09T12:00:00Z', '2019-09-09T12:00:00Z')).toBe('9/2019')
  })
})

describe('formatDuration', () => {
  it('formats sub-hour durations as M:SS', () => {
    expect(formatDuration(154000)).toBe('2:34')
    expect(formatDuration(9000)).toBe('0:09')
  })

  it('formats hour-plus durations as H:MM:SS', () => {
    expect(formatDuration(3754000)).toBe('1:02:34')
  })

  it('clamps non-positive and non-finite input to 0:00', () => {
    expect(formatDuration(0)).toBe('0:00')
    expect(formatDuration(-5)).toBe('0:00')
    expect(formatDuration(Number.NaN)).toBe('0:00')
  })
})
