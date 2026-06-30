import { describe, expect, it } from 'vitest'

import { formatBytes, formatDuration } from './format'

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
