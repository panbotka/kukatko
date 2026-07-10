import { afterEach, beforeEach, describe, expect, it } from 'vitest'

import {
  readSettings,
  sanitizeSettings,
  SLIDESHOW_DEFAULTS,
  SLIDESHOW_INTERVALS_MS,
  type SlideshowSettings,
  writeSettings,
} from './slideshowSettings'

const STORAGE_KEY = 'kukatko.slideshow.settings'

/** Parses a raw storage payload the way {@link readSettings} would. */
function stored(raw: string): Partial<SlideshowSettings> {
  return JSON.parse(raw) as Partial<SlideshowSettings>
}

beforeEach(() => {
  window.localStorage.clear()
})

afterEach(() => {
  window.localStorage.clear()
})

describe('SLIDESHOW_INTERVALS_MS', () => {
  it('offers 1, 2, 3, 5, 10, 15 and 30 seconds, ascending', () => {
    expect([...SLIDESHOW_INTERVALS_MS]).toEqual([1000, 2000, 3000, 5000, 10000, 15000, 30000])
  })

  it('contains the default interval, so the picker can always show it', () => {
    expect(SLIDESHOW_DEFAULTS.intervalMs).toBe(5000)
    expect(SLIDESHOW_INTERVALS_MS).toContain(SLIDESHOW_DEFAULTS.intervalMs)
  })
})

describe('sanitizeSettings', () => {
  it('keeps every offered interval as-is', () => {
    for (const ms of SLIDESHOW_INTERVALS_MS) {
      expect(sanitizeSettings({ effect: 'fade', intervalMs: ms }).intervalMs).toBe(ms)
    }
  })

  it('snaps the retired 7 s interval to the nearest offered value', () => {
    const { intervalMs } = sanitizeSettings({ effect: 'fade', intervalMs: 7000 })

    expect(intervalMs).toBe(5000)
    expect(SLIDESHOW_INTERVALS_MS).toContain(intervalMs)
  })

  it('snaps any out-of-set interval to the nearest offered one', () => {
    // [raw, expected]; 12500 sits exactly between 10 s and 15 s — ties go short.
    const cases: [number, number][] = [
      [-1, 1000],
      [0, 1000],
      [900, 1000],
      [4400, 5000],
      [7000, 5000],
      [12000, 10000],
      [12500, 10000],
      [12600, 15000],
      [999999, 30000],
    ]
    for (const [raw, expected] of cases) {
      expect(sanitizeSettings({ effect: 'fade', intervalMs: raw }).intervalMs).toBe(expected)
    }
  })

  it('falls back to the default interval for a non-finite, missing or non-numeric value', () => {
    const { intervalMs: fallback } = SLIDESHOW_DEFAULTS

    expect(sanitizeSettings({ effect: 'fade', intervalMs: NaN }).intervalMs).toBe(fallback)
    expect(sanitizeSettings({ effect: 'fade', intervalMs: Infinity }).intervalMs).toBe(fallback)
    expect(sanitizeSettings({ effect: 'fade' }).intervalMs).toBe(fallback)
    expect(sanitizeSettings(null).intervalMs).toBe(fallback)
    // Tampered storage can hold a string where a number belongs.
    expect(sanitizeSettings(stored('{"intervalMs":"5000"}')).intervalMs).toBe(fallback)
  })

  it('narrows an unknown effect back to the default', () => {
    expect(sanitizeSettings(stored('{"effect":"spin","intervalMs":5000}')).effect).toBe(
      SLIDESHOW_DEFAULTS.effect,
    )
  })
})

describe('readSettings', () => {
  it('returns the defaults when nothing is persisted', () => {
    expect(readSettings()).toEqual(SLIDESHOW_DEFAULTS)
  })

  it('returns the defaults when the stored value is not parseable', () => {
    window.localStorage.setItem(STORAGE_KEY, '{ not json')

    expect(readSettings()).toEqual(SLIDESHOW_DEFAULTS)
  })

  it('resolves a persisted 7000 ms interval to an offered one', () => {
    window.localStorage.setItem(STORAGE_KEY, JSON.stringify({ effect: 'slide', intervalMs: 7000 }))

    const settings = readSettings()

    expect(settings.effect).toBe('slide')
    expect(settings.intervalMs).toBe(5000)
    expect(SLIDESHOW_INTERVALS_MS).toContain(settings.intervalMs)
  })

  it('round-trips through writeSettings', () => {
    writeSettings({ effect: 'none', intervalMs: 15000 })

    expect(readSettings()).toEqual({ effect: 'none', intervalMs: 15000 })
  })
})
