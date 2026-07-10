import { createInstance, type TFunction } from 'i18next'
import { beforeAll, describe, expect, it } from 'vitest'

import { initOptions, type supportedLngs } from '../i18n'

import {
  formatDuration,
  slideshowDurationMs,
  slideshowRemainingMs,
  splitDuration,
} from './duration'

const SECOND = 1000
const MINUTE = 60 * SECOND
const HOUR = 60 * MINUTE

/** A `t` bound to one language, on a throwaway instance with the app's resources. */
async function translatorFor(lng: (typeof supportedLngs)[number]): Promise<TFunction> {
  const instance = createInstance()
  await instance.init({ ...initOptions, lng })
  return instance.t
}

let cs: TFunction
let en: TFunction

beforeAll(async () => {
  cs = await translatorFor('cs')
  en = await translatorFor('en')
})

describe('splitDuration', () => {
  it('splits into whole hours, minutes and seconds', () => {
    expect(splitDuration(45 * SECOND)).toEqual({ hours: 0, minutes: 0, seconds: 45 })
    expect(splitDuration(3 * MINUTE + 20 * SECOND)).toEqual({ hours: 0, minutes: 3, seconds: 20 })
    expect(splitDuration(HOUR + 5 * MINUTE + 9 * SECOND)).toEqual({
      hours: 1,
      minutes: 5,
      seconds: 9,
    })
  })

  it('rounds to the nearest second', () => {
    expect(splitDuration(1499)).toEqual({ hours: 0, minutes: 0, seconds: 1 })
    expect(splitDuration(1500)).toEqual({ hours: 0, minutes: 0, seconds: 2 })
  })

  it('collapses a negative or non-finite duration to zero', () => {
    expect(splitDuration(-5000)).toEqual({ hours: 0, minutes: 0, seconds: 0 })
    expect(splitDuration(Number.NaN)).toEqual({ hours: 0, minutes: 0, seconds: 0 })
    expect(splitDuration(Number.POSITIVE_INFINITY)).toEqual({ hours: 0, minutes: 0, seconds: 0 })
  })
})

describe('formatDuration', () => {
  it('shows plain seconds below a minute', () => {
    expect(formatDuration(45 * SECOND, cs)).toBe('45 s')
    expect(formatDuration(45 * SECOND, en)).toBe('45 s')
    expect(formatDuration(0, cs)).toBe('0 s')
  })

  it('shows minutes with seconds above a minute', () => {
    // The spec's example: 40 photos at 5 s a slide.
    expect(formatDuration(3 * MINUTE + 20 * SECOND, cs)).toBe('3 min 20 s')
    expect(formatDuration(3 * MINUTE + 20 * SECOND, en)).toBe('3 min 20 s')
  })

  it('drops a zero seconds part', () => {
    expect(formatDuration(2 * MINUTE, cs)).toBe('2 min')
    expect(formatDuration(MINUTE, en)).toBe('1 min')
  })

  it('shows hours and minutes once the show runs that long', () => {
    expect(formatDuration(HOUR + 5 * MINUTE, cs)).toBe('1 h 5 min')
    expect(formatDuration(HOUR + 5 * MINUTE, en)).toBe('1 h 5 min')
    // Seconds carry no information at this scale and are dropped.
    expect(formatDuration(2 * HOUR + 30 * MINUTE + 44 * SECOND, cs)).toBe('2 h 30 min')
  })

  it('drops a zero minutes part on a whole number of hours', () => {
    expect(formatDuration(2 * HOUR, cs)).toBe('2 h')
    expect(formatDuration(2 * HOUR, en)).toBe('2 h')
  })
})

describe('slideshowDurationMs', () => {
  it('is one interval per photo', () => {
    expect(slideshowDurationMs(40, 5 * SECOND)).toBe(200 * SECOND)
    expect(slideshowDurationMs(400, 5 * SECOND)).toBe(2000 * SECOND)
  })

  it('is zero for an empty or nonsensical set', () => {
    expect(slideshowDurationMs(0, 5 * SECOND)).toBe(0)
    expect(slideshowDurationMs(-3, 5 * SECOND)).toBe(0)
    expect(slideshowDurationMs(10, -1000)).toBe(0)
  })
})

describe('slideshowRemainingMs', () => {
  it('counts the photos still to come, mid-show', () => {
    // Slide 7 of 40 (index 6) at 5 s a slide: 33 photos left → 2 min 45 s.
    const remaining = slideshowRemainingMs(6, 40, 5 * SECOND)
    expect(remaining).toBe(165 * SECOND)
    expect(formatDuration(remaining, cs)).toBe('2 min 45 s')
  })

  it('counts the whole show minus the first slide at the start', () => {
    expect(slideshowRemainingMs(0, 40, 5 * SECOND)).toBe(195 * SECOND)
  })

  it('is zero on the last slide, and never negative past the end', () => {
    expect(slideshowRemainingMs(39, 40, 5 * SECOND)).toBe(0)
    expect(slideshowRemainingMs(40, 40, 5 * SECOND)).toBe(0)
    expect(slideshowRemainingMs(0, 0, 5 * SECOND)).toBe(0)
  })
})
