import { act, renderHook } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'

import { SLIDESHOW_DEFAULTS } from '../lib/slideshowSettings'

import { useSlideshowSettings } from './useSlideshowSettings'

const STORAGE_KEY = 'kukatko.slideshow.settings'

beforeEach(() => {
  window.localStorage.clear()
})

afterEach(() => {
  window.localStorage.clear()
})

describe('useSlideshowSettings', () => {
  it('starts from the defaults when nothing is persisted', () => {
    const { result } = renderHook(() => useSlideshowSettings())
    expect(result.current.settings).toEqual(SLIDESHOW_DEFAULTS)
  })

  it('persists the effect and speed and survives a remount', () => {
    const first = renderHook(() => useSlideshowSettings())

    act(() => {
      first.result.current.setEffect('slide')
      first.result.current.setIntervalMs(3000)
    })

    expect(first.result.current.settings).toEqual({ effect: 'slide', intervalMs: 3000 })

    // A fresh hook (e.g. after a reload) reads the persisted preference back.
    const second = renderHook(() => useSlideshowSettings())
    expect(second.result.current.settings).toEqual({ effect: 'slide', intervalMs: 3000 })

    // And it really went to localStorage.
    expect(JSON.parse(window.localStorage.getItem(STORAGE_KEY) ?? '{}')).toEqual({
      effect: 'slide',
      intervalMs: 3000,
    })
  })

  it('sanitises invalid persisted values back to defaults', () => {
    window.localStorage.setItem(STORAGE_KEY, JSON.stringify({ effect: 'spin', intervalMs: -1 }))
    const { result } = renderHook(() => useSlideshowSettings())
    expect(result.current.settings.effect).toBe(SLIDESHOW_DEFAULTS.effect)
    expect(result.current.settings.intervalMs).toBeGreaterThanOrEqual(1000)
  })
})
