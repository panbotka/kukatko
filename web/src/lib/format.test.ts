import { describe, expect, it } from 'vitest'

import { formatBytes, formatDate, formatDateTime, formatDuration } from './format'

describe('formatBytes', () => {
  it('renders bytes without decimals', () => {
    expect(formatBytes(512)).toBe('512 B')
  })

  it('scales to binary units with one decimal', () => {
    expect(formatBytes(1536)).toBe('1.5 KB')
    expect(formatBytes(5 * 1024 * 1024)).toBe('5.0 MB')
  })

  it('clamps non-positive and non-finite input to 0 B', () => {
    expect(formatBytes(0)).toBe('0 B')
    expect(formatBytes(-10)).toBe('0 B')
    expect(formatBytes(Number.NaN)).toBe('0 B')
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
