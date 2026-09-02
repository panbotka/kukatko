import { act, renderHook } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { TEXT_FILTER_DEBOUNCE_MS, useDebouncedText } from './useDebouncedText'

beforeEach(() => {
  vi.useFakeTimers()
})

afterEach(() => {
  vi.useRealTimers()
})

/** Runs the clock past the debounce, flushing whatever it was holding. */
function settle(ms = TEXT_FILTER_DEBOUNCE_MS): void {
  act(() => {
    vi.advanceTimersByTime(ms)
  })
}

describe('useDebouncedText', () => {
  it('starts on the committed value and reports typing immediately', () => {
    const commit = vi.fn()
    const { result } = renderHook(() => useDebouncedText('Canon', commit))

    expect(result.current[0]).toBe('Canon')
    act(() => {
      result.current[1]('Canon E')
    })
    // The field is the reader's, not the query's: it keeps up with the keyboard
    // whatever the timer is doing.
    expect(result.current[0]).toBe('Canon E')
    expect(commit).not.toHaveBeenCalled()
  })

  it('commits once, on the pause, whatever the reader typed on the way', () => {
    const commit = vi.fn()
    const { result } = renderHook(() => useDebouncedText('', commit))

    for (const draft of ['s', 'sv', 'sva', 'svat']) {
      act(() => {
        result.current[1](draft)
      })
      settle(TEXT_FILTER_DEBOUNCE_MS - 1)
    }
    // Four keystrokes inside the window are still zero requests.
    expect(commit).not.toHaveBeenCalled()

    settle()
    expect(commit).toHaveBeenCalledTimes(1)
    expect(commit).toHaveBeenCalledWith('svat')
  })

  it('does not commit again once the caller has caught up', () => {
    const commit = vi.fn()
    const { result, rerender } = renderHook(
      ({ value }: { value: string }) => useDebouncedText(value, commit),
      { initialProps: { value: '' } },
    )

    act(() => {
      result.current[1]('Canon')
    })
    settle()
    expect(commit).toHaveBeenCalledTimes(1)

    // The caller writes the committed value back down as its own prop, which is
    // the normal round trip through the URL. It must not read as a fresh change.
    rerender({ value: 'Canon' })
    settle()
    expect(commit).toHaveBeenCalledTimes(1)
    expect(result.current[0]).toBe('Canon')
  })

  it('follows the value when it changes from outside', () => {
    const commit = vi.fn()
    const { result, rerender } = renderHook(
      ({ value }: { value: string }) => useDebouncedText(value, commit),
      { initialProps: { value: 'Canon' } },
    )

    // Clear-all, a removed chip, Back: the filter went away without the field
    // being touched, so the field must go with it rather than keep displaying a
    // filter that is no longer on.
    rerender({ value: '' })
    expect(result.current[0]).toBe('')
    settle()
    // And the reset is not itself typing, so it costs no commit back.
    expect(commit).not.toHaveBeenCalled()
  })

  it('leaves the draft alone when the caller ignores the commit', () => {
    const commit = vi.fn()
    const { result, rerender } = renderHook(
      ({ value }: { value: string }) => useDebouncedText(value, commit),
      { initialProps: { value: '' } },
    )

    act(() => {
      result.current[1]('Canon')
    })
    settle()
    // A caller that drops the value (a rejected write, a page mid-navigation)
    // re-renders with the old one. Snapping the field back to it would eat what
    // the reader is in the middle of typing.
    rerender({ value: '' })
    expect(result.current[0]).toBe('Canon')
    expect(commit).toHaveBeenCalledTimes(1)
  })

  it('reads the latest callback, so an inline arrow neither restarts nor goes stale', () => {
    const first = vi.fn()
    const second = vi.fn()
    const { result, rerender } = renderHook(
      ({ commit }: { commit: (next: string) => void }) => useDebouncedText('', commit),
      { initialProps: { commit: first } },
    )

    act(() => {
      result.current[1]('Canon')
    })
    settle(TEXT_FILTER_DEBOUNCE_MS - 1)
    // Every render of the caller hands over a new arrow. A timer keyed on the
    // callback would restart here and the pause would never elapse.
    rerender({ commit: second })
    settle(1)

    expect(first).not.toHaveBeenCalled()
    expect(second).toHaveBeenCalledWith('Canon')
  })
})
