import { describe, expect, it } from 'vitest'

import {
  formatByteCount,
  formatBytes,
  formatCaptureParts,
  formatCaptureRange,
  formatCount,
  formatDate,
  formatDateTime,
  formatDateTimeMinutes,
  formatDuration,
  formatMonth,
  formatMonthName,
  formatPercent,
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

describe('formatCount', () => {
  it('groups thousands in the active locale', () => {
    // Czech groups with a narrow no-break space, so compare on the digits.
    expect(formatCount(20310, 'cs').replace(/\s/gu, ' ')).toBe('20 310')
    expect(formatCount(20310, 'en')).toBe('20,310')
  })

  it('leaves small counts ungrouped', () => {
    expect(formatCount(0, 'en')).toBe('0')
    expect(formatCount(7, 'cs')).toBe('7')
  })

  it('rounds fractions and clamps non-finite input to 0', () => {
    expect(formatCount(2.6, 'en')).toBe('3')
    expect(formatCount(Number.NaN, 'en')).toBe('0')
    expect(formatCount(Number.POSITIVE_INFINITY, 'en')).toBe('0')
  })
})

describe('formatPercent', () => {
  it('renders a share in the active locale', () => {
    // Czech separates the sign with a no-break space, so compare on the digits.
    expect(formatPercent(0.42, 'cs').replace(/\s/gu, ' ')).toBe('42 %')
    expect(formatPercent(0.42, 'en')).toBe('42%')
  })

  it('keeps a sliver readable instead of rounding it to zero', () => {
    // 50 of 20 092 embeddings — the import-verify coverage the audit measured.
    expect(formatPercent(0.0025, 'en')).toBe('0.3%')
  })

  it('clamps out-of-range and non-finite input', () => {
    expect(formatPercent(1.5, 'en')).toBe('100%')
    expect(formatPercent(-0.2, 'en')).toBe('0%')
    expect(formatPercent(Number.NaN, 'en')).toBe('0%')
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

describe('formatDateTimeMinutes', () => {
  // A second's worth of precision no reader wants, so the difference from
  // formatDateTime is visible in every assertion below.
  const iso = '2026-03-09T14:05:40Z'

  it('drops the seconds that formatDateTime keeps', () => {
    // Stated as the contrast rather than a literal, so the pair cannot silently
    // drift apart and neither pins one locale's punctuation.
    expect(formatDateTime(iso, 'cs')).toMatch(/:\d\d:\d\d/)
    expect(formatDateTimeMinutes(iso, 'cs')).not.toMatch(/:\d\d:\d\d/)
  })

  it('keeps the date, the hour and the minute', () => {
    const short = formatDateTimeMinutes(iso, 'cs')
    expect(short).toContain('2026')
    expect(short).toMatch(/\d{1,2}:\d\d/)
  })

  it('follows the requested locale rather than the host default', () => {
    expect(formatDateTimeMinutes(iso, 'cs')).not.toBe(formatDateTimeMinutes(iso, 'en-US'))
  })

  it('does not round a time up to the next minute', () => {
    // The dropped :40 must truncate, not round — 05 stays 05. The expected minute
    // comes from Date rather than the literal, because the output is in the host's
    // timezone and only the hour of it shifts.
    const minute = String(new Date(iso).getMinutes()).padStart(2, '0')
    expect(formatDateTimeMinutes(iso, 'cs')).toContain(`:${minute}`)
  })

  it('returns the original string for an unparseable value', () => {
    expect(formatDateTimeMinutes('not-a-date', 'cs')).toBe('not-a-date')
    expect(formatDateTimeMinutes('', 'en')).toBe('')
  })
})

describe('formatCaptureParts', () => {
  const iso = '2026-03-09T14:05:40Z'

  it('joins back into exactly the label it was cut from', () => {
    // The whole point of the split: a caller that shows only `date` must be
    // shortening the very string the rest of the app renders, not a lookalike.
    for (const locale of ['cs', 'en', 'en-US', 'en-GB', 'de']) {
      const parts = formatCaptureParts(iso, locale)
      expect(parts.date + parts.separator + parts.time).toBe(formatDateTimeMinutes(iso, locale))
    }
  })

  it('keeps the year on the date side, whichever end the locale puts it', () => {
    // Czech ends the date with the year ("9. 3. 2026"), US English ends it with
    // the year too but leads with the month — either way it must survive the cut,
    // because dropping the time is what a narrow header does INSTEAD of losing it.
    expect(formatCaptureParts(iso, 'cs').date).toContain('2026')
    expect(formatCaptureParts(iso, 'en-US').date).toContain('2026')
  })

  it('leaves no clock on the date side and no calendar on the time side', () => {
    const minute = String(new Date(iso).getMinutes()).padStart(2, '0')
    for (const locale of ['cs', 'en-US']) {
      const parts = formatCaptureParts(iso, locale)
      expect(parts.date).not.toMatch(/\d{1,2}:\d\d/)
      expect(parts.time).toContain(`:${minute}`)
      expect(parts.time).not.toContain('2026')
    }
  })

  it('carries the AM/PM with the time it qualifies', () => {
    // "5:17" without its "PM" would be worse than no time at all, so the day
    // period belongs to the part that is dropped as a whole.
    const parts = formatCaptureParts(iso, 'en-US')
    expect(parts.time).toMatch(/[AP]M/)
    expect(parts.date).not.toMatch(/[AP]M/)
  })

  it('gives the separator to the time, so the date never keeps a dangling space', () => {
    for (const locale of ['cs', 'en-US']) {
      const { date, separator } = formatCaptureParts(iso, locale)
      expect(separator.trim()).not.toBe(' ')
      expect(date).toBe(date.trimEnd())
    }
  })

  it('returns the original string as the date for an unparseable value', () => {
    expect(formatCaptureParts('not-a-date', 'cs')).toEqual({
      date: 'not-a-date',
      separator: '',
      time: '',
    })
    expect(formatCaptureParts('', 'en')).toEqual({ date: '', separator: '', time: '' })
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

describe('formatMonthName', () => {
  it('names the month without the year, for an axis that says the year elsewhere', () => {
    const en = formatMonthName(2026, 1, 'en-US')
    expect(en.toLowerCase()).toContain('jan')
    expect(en).not.toContain('2026')
    expect(formatMonthName(2026, 1, 'cs')).not.toBe(en)
  })

  it('is exactly the name half of formatMonth', () => {
    expect(formatMonth(2026, 7, 'cs')).toBe(`${formatMonthName(2026, 7, 'cs')} 2026`)
  })

  it('returns an empty string for an out-of-range month', () => {
    expect(formatMonthName(2026, 0, 'en')).toBe('')
    expect(formatMonthName(2026, 13, 'en')).toBe('')
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
