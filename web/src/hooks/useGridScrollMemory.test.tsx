import { act, renderHook } from '@testing-library/react'
import { type StateSnapshot } from 'react-virtuoso'
import { beforeEach, describe, expect, it } from 'vitest'

import { readGridScroll, writeGridScroll } from '../lib/gridScroll'

import { useGridScrollMemory, type UseGridScrollMemoryOptions } from './useGridScrollMemory'

/** A plausible virtuoso snapshot at the given offset. */
function snapshot(scrollTop: number): StateSnapshot {
  return {
    ranges: [{ startIndex: 0, endIndex: 12, size: 220 }],
    scrollTop,
  }
}

/** Moves the window, which jsdom otherwise pins to 0 and never scrolls. */
function scrollWindowTo(y: number) {
  Object.defineProperty(window, 'scrollY', { value: y, configurable: true, writable: true })
  window.dispatchEvent(new Event('scroll'))
}

beforeEach(() => {
  window.sessionStorage.clear()
  Object.defineProperty(window, 'scrollY', { value: 0, configurable: true, writable: true })
})

describe('useGridScrollMemory', () => {
  it('remembers the last reported position when the page is left', () => {
    const { result, unmount } = renderHook(() => useGridScrollMemory({ key: '/', count: 300 }))

    act(() => {
      result.current.onStateChanged(snapshot(1000))
      result.current.onStateChanged(snapshot(4000))
    })
    // Leaving for a photo unmounts the page: the position has to be written out
    // there and then, not on some later timer that never runs.
    unmount()

    expect(readGridScroll('/')).toEqual({ count: 300, scrollY: 0, snapshot: snapshot(4000) })
  })

  it('hands back what a previous visit left', () => {
    writeGridScroll('/', { count: 300, scrollY: 4000, snapshot: snapshot(3800) })

    const { result } = renderHook(() => useGridScrollMemory({ key: '/' }))

    expect(result.current.restoreFrom).toEqual(snapshot(3800))
    expect(result.current.restoreScrollY).toBe(4000)
  })

  it('has nothing to restore for a view it has never seen', () => {
    const { result } = renderHook(() => useGridScrollMemory({ key: '/labels/lb_1' }))

    expect(result.current.restoreFrom).toBeUndefined()
    expect(result.current.restoreScrollY).toBe(0)
  })

  it('leaves the remembered position alone when the reader touches nothing', () => {
    writeGridScroll('/', { count: 300, scrollY: 4000, snapshot: snapshot(3800) })

    const { unmount } = renderHook(() => useGridScrollMemory({ key: '/', count: 300 }))
    unmount()

    expect(readGridScroll('/')?.snapshot).toEqual(snapshot(3800))
  })

  it('ignores the grid sitting at the top on its way to a deeper position', () => {
    writeGridScroll('/', { count: 300, scrollY: 3800, snapshot: snapshot(3800) })

    const { result, unmount } = renderHook(() => useGridScrollMemory({ key: '/', count: 300 }))
    // The grid reports itself at the top before the restore lands. Recording that
    // would throw away the very position it is being restored to.
    act(() => {
      result.current.onStateChanged(snapshot(0))
    })
    unmount()

    expect(readGridScroll('/')?.snapshot).toEqual(snapshot(3800))
  })

  it('records the top once the grid has been seen away from it', () => {
    writeGridScroll('/', { count: 300, scrollY: 3800, snapshot: snapshot(3800) })

    const { result, unmount } = renderHook(() => useGridScrollMemory({ key: '/', count: 300 }))
    act(() => {
      result.current.onStateChanged(snapshot(3800))
      // The reader scrolled back to the top themselves: that is a position, and
      // it must replace the deep one.
      result.current.onStateChanged(snapshot(0))
    })
    unmount()

    expect(readGridScroll('/')?.snapshot).toEqual(snapshot(0))
  })

  it('writes nothing while the caller is still restoring', () => {
    writeGridScroll('/people/su_1', { count: 300, scrollY: 2400 })

    const { rerender, unmount } = renderHook(
      (props: UseGridScrollMemoryOptions) => useGridScrollMemory(props),
      {
        initialProps: {
          key: '/people/su_1',
          count: 100,
          restoring: true,
        },
      },
    )
    // The half-loaded gallery is pinned to the top of a short document; nothing
    // it reports on the way to 2400 may overwrite 2400.
    act(() => {
      scrollWindowTo(0)
    })
    expect(readGridScroll('/people/su_1')?.scrollY).toBe(2400)

    rerender({ key: '/people/su_1', count: 300, restoring: false })
    act(() => {
      scrollWindowTo(2400)
    })
    unmount()

    expect(readGridScroll('/people/su_1')).toEqual({ count: 300, scrollY: 2400 })
  })

  it('records the window offset for a grid that reports no state of its own', () => {
    const { unmount } = renderHook(() => useGridScrollMemory({ key: '/people/su_1', count: 250 }))

    act(() => {
      scrollWindowTo(1800)
    })
    unmount()

    expect(readGridScroll('/people/su_1')).toEqual({ count: 250, scrollY: 1800 })
  })

  it('records the window offset for a virtualized grid too', () => {
    const { result, unmount } = renderHook(() => useGridScrollMemory({ key: '/', count: 0 }))

    // The virtualized wall runs virtuoso with `useWindowScroll`, so the window's
    // offset is its offset — there is no per-page option deciding otherwise, and
    // the snapshot beside it describes the same position.
    act(() => {
      scrollWindowTo(1800)
      result.current.onStateChanged(snapshot(1800))
    })
    unmount()

    expect(readGridScroll('/')).toEqual({ count: 0, scrollY: 1800, snapshot: snapshot(1800) })
  })

  it('ignores the window sitting at the top on its way to a deeper position', () => {
    writeGridScroll('/', { count: 0, scrollY: 3800, snapshot: snapshot(3800) })

    const { unmount } = renderHook(() => useGridScrollMemory({ key: '/', count: 0 }))
    // The document is still filling and pinned to its top. Recording that would
    // throw away the very position being restored — and the snapshot with it.
    act(() => {
      scrollWindowTo(0)
    })
    unmount()

    expect(readGridScroll('/')).toEqual({ count: 0, scrollY: 3800, snapshot: snapshot(3800) })
  })

  it('hands the photograph the viewer recorded to the grid, once', () => {
    writeGridScroll('/', { count: 0, scrollY: 3800, snapshot: snapshot(3800), uid: 'ph_9' })

    const first = renderHook(() => useGridScrollMemory({ key: '/' }))
    expect(first.result.current.restoreUid).toBe('ph_9')
    first.unmount()

    // A reload, or coming back again having scrolled on since, must not be pulled
    // to the same photograph a second time.
    const second = renderHook(() => useGridScrollMemory({ key: '/' }))
    expect(second.result.current.restoreUid).toBeUndefined()
  })

  it('does not carry one view position over to another', () => {
    const { result, rerender, unmount } = renderHook(
      (props: UseGridScrollMemoryOptions) => useGridScrollMemory(props),
      { initialProps: { key: '/', count: 300 } },
    )
    act(() => {
      result.current.onStateChanged(snapshot(4000))
    })

    // Changing a filter renumbers every position: the new view starts fresh and
    // must not inherit the old one's offset.
    rerender({ key: '/?sort=oldest', count: 0 })
    unmount()

    expect(readGridScroll('/?sort=oldest')).toBeNull()
    expect(readGridScroll('/')?.snapshot).toEqual(snapshot(4000))
  })

  it('stays inert without a key', () => {
    const { result, unmount } = renderHook(() => useGridScrollMemory({ key: '', count: 10 }))

    act(() => {
      result.current.onStateChanged(snapshot(500))
    })
    unmount()

    expect(window.sessionStorage.getItem('kukatko.gridScroll')).toBeNull()
  })
})
