import { act, renderHook } from '@testing-library/react'
import { type GridStateSnapshot } from 'react-virtuoso'
import { beforeEach, describe, expect, it } from 'vitest'

import { readGridScroll, writeGridScroll } from '../lib/gridScroll'

import { useGridScrollMemory, type UseGridScrollMemoryOptions } from './useGridScrollMemory'

/** A plausible virtuoso snapshot at the given offset. */
function snapshot(scrollTop: number): GridStateSnapshot {
  return {
    gap: { column: 8, row: 8 },
    item: { height: 220, width: 220 },
    scrollTop,
    viewport: { height: 900, width: 1400 },
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
          track: 'window' as const,
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

    rerender({ key: '/people/su_1', count: 300, track: 'window', restoring: false })
    act(() => {
      scrollWindowTo(2400)
    })
    unmount()

    expect(readGridScroll('/people/su_1')).toEqual({ count: 300, scrollY: 2400 })
  })

  it('records the window offset for a grid that reports no state of its own', () => {
    const { unmount } = renderHook(() =>
      useGridScrollMemory({ key: '/people/su_1', count: 250, track: 'window' }),
    )

    act(() => {
      scrollWindowTo(1800)
    })
    unmount()

    expect(readGridScroll('/people/su_1')).toEqual({ count: 250, scrollY: 1800 })
  })

  it('leaves the window alone when tracking a virtualized grid', () => {
    const { unmount } = renderHook(() => useGridScrollMemory({ key: '/', count: 0 }))

    // A virtualized grid reports its own offset; the window's is meaningless
    // here, so a scroll must not on its own count as a position worth keeping.
    act(() => {
      scrollWindowTo(1800)
    })
    unmount()

    expect(readGridScroll('/')).toBeNull()
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
