import { act, renderHook as renderBareHook } from '@testing-library/react'
import { type ReactNode } from 'react'
import { MemoryRouter, useLocation, useNavigate } from 'react-router-dom'
import { type StateSnapshot } from 'react-virtuoso'
import { beforeEach, describe, expect, it } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
import { readGridScroll, writeGridScroll } from '../lib/gridScroll'

import {
  useGridScrollMemory,
  type UseGridScrollMemoryOptions,
  useRememberedGridScroll,
} from './useGridScrollMemory'

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

/** The signed-in reader whose positions the memory keeps; a test may switch it. */
let reader = 'u1'

/** Signs `reader` in: the memory is kept per account. */
function SignedIn({ children }: { children: ReactNode }) {
  const value = { user: { uid: reader } } as unknown as AuthContextValue
  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}

/** A router's first entry, which it reports as a pop — as a load of the tab is. */
function Router({ children }: { children: ReactNode }) {
  return (
    <SignedIn>
      <MemoryRouter>{children}</MemoryRouter>
    </SignedIn>
  )
}

/**
 * `renderHook` inside a freshly created router: the memory asks how the view was
 * reached, and a router's first entry is a pop, the arrival that restores.
 */
const renderHook = ((callback: (props: unknown) => unknown, options?: object) =>
  renderBareHook(callback, { wrapper: Router, ...options })) as typeof renderBareHook

/**
 * The memory of whatever grid sits at `/grid`, with the router's `navigate` to
 * reach it by. The first entry is somewhere else, so the grid is only ever
 * arrived at by an explicit navigation — a push, a replace or a pop back.
 */
function renderNavigableGrid() {
  return renderBareHook(
    () => {
      const navigate = useNavigate()
      const location = useLocation()
      const key = location.pathname === '/grid' ? '/grid' : ''
      return {
        navigate,
        memory: useGridScrollMemory({ key, count: 5 }),
        remembered: useRememberedGridScroll(key),
      }
    },
    {
      wrapper: ({ children }: { children: ReactNode }) => (
        <SignedIn>
          <MemoryRouter initialEntries={['/grid', '/elsewhere']} initialIndex={1}>
            {children}
          </MemoryRouter>
        </SignedIn>
      ),
    },
  )
}

beforeEach(() => {
  reader = 'u1'
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

    expect(readGridScroll('u1', '/')).toEqual({ count: 300, scrollY: 0, snapshot: snapshot(4000) })
  })

  it('hands back what a previous visit left', () => {
    writeGridScroll('u1', '/', { count: 300, scrollY: 4000, snapshot: snapshot(3800) })

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
    writeGridScroll('u1', '/', { count: 300, scrollY: 4000, snapshot: snapshot(3800) })

    const { unmount } = renderHook(() => useGridScrollMemory({ key: '/', count: 300 }))
    unmount()

    expect(readGridScroll('u1', '/')?.snapshot).toEqual(snapshot(3800))
  })

  it('ignores the grid sitting at the top on its way to a deeper position', () => {
    writeGridScroll('u1', '/', { count: 300, scrollY: 3800, snapshot: snapshot(3800) })

    const { result, unmount } = renderHook(() => useGridScrollMemory({ key: '/', count: 300 }))
    // The grid reports itself at the top before the restore lands. Recording that
    // would throw away the very position it is being restored to.
    act(() => {
      result.current.onStateChanged(snapshot(0))
    })
    unmount()

    expect(readGridScroll('u1', '/')?.snapshot).toEqual(snapshot(3800))
  })

  it('records the top once the grid has been seen away from it', () => {
    writeGridScroll('u1', '/', { count: 300, scrollY: 3800, snapshot: snapshot(3800) })

    const { result, unmount } = renderHook(() => useGridScrollMemory({ key: '/', count: 300 }))
    act(() => {
      result.current.onStateChanged(snapshot(3800))
      // The reader scrolled back to the top themselves: that is a position, and
      // it must replace the deep one.
      result.current.onStateChanged(snapshot(0))
    })
    unmount()

    expect(readGridScroll('u1', '/')?.snapshot).toEqual(snapshot(0))
  })

  it('writes nothing while the caller is still restoring', () => {
    writeGridScroll('u1', '/people/su_1', { count: 300, scrollY: 2400 })

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
    expect(readGridScroll('u1', '/people/su_1')?.scrollY).toBe(2400)

    rerender({ key: '/people/su_1', count: 300, restoring: false })
    act(() => {
      scrollWindowTo(2400)
    })
    unmount()

    expect(readGridScroll('u1', '/people/su_1')).toEqual({ count: 300, scrollY: 2400 })
  })

  it('records the window offset for a grid that reports no state of its own', () => {
    const { unmount } = renderHook(() => useGridScrollMemory({ key: '/people/su_1', count: 250 }))

    act(() => {
      scrollWindowTo(1800)
    })
    unmount()

    expect(readGridScroll('u1', '/people/su_1')).toEqual({ count: 250, scrollY: 1800 })
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

    expect(readGridScroll('u1', '/')).toEqual({ count: 0, scrollY: 1800, snapshot: snapshot(1800) })
  })

  it('ignores the window sitting at the top on its way to a deeper position', () => {
    writeGridScroll('u1', '/', { count: 0, scrollY: 3800, snapshot: snapshot(3800) })

    const { unmount } = renderHook(() => useGridScrollMemory({ key: '/', count: 0 }))
    // The document is still filling and pinned to its top. Recording that would
    // throw away the very position being restored — and the snapshot with it.
    act(() => {
      scrollWindowTo(0)
    })
    unmount()

    expect(readGridScroll('u1', '/')).toEqual({ count: 0, scrollY: 3800, snapshot: snapshot(3800) })
  })

  it('hands the photograph the viewer recorded to the grid, once', () => {
    writeGridScroll('u1', '/', { count: 0, scrollY: 3800, snapshot: snapshot(3800), uid: 'ph_9' })

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

    expect(readGridScroll('u1', '/?sort=oldest')).toBeNull()
    expect(readGridScroll('u1', '/')?.snapshot).toEqual(snapshot(4000))
  })

  it('stays inert without a key', () => {
    const { result, unmount } = renderHook(() => useGridScrollMemory({ key: '', count: 10 }))

    act(() => {
      result.current.onStateChanged(snapshot(500))
    })
    unmount()

    expect(window.sessionStorage.getItem('kukatko.gridScroll.u1')).toBeNull()
  })

  it('does not hand one reader the position another left in the same tab', () => {
    writeGridScroll('u1', '/', { count: 300, scrollY: 4000, snapshot: snapshot(3800), uid: 'ph_9' })
    reader = 'u2'

    const { result } = renderHook(() => useGridScrollMemory({ key: '/' }))

    expect(result.current.restoreFrom).toBeUndefined()
    expect(result.current.restoreScrollY).toBe(0)
    expect(result.current.restoreUid).toBeUndefined()
    // …and the first reader's memory is theirs, still whole, when they are back.
    expect(readGridScroll('u1', '/')?.uid).toBe('ph_9')
  })

  it('writes a reader’s position under their own account', () => {
    reader = 'u2'
    const { result, unmount } = renderHook(() => useGridScrollMemory({ key: '/', count: 300 }))

    act(() => {
      result.current.onStateChanged(snapshot(4000))
    })
    unmount()

    expect(readGridScroll('u2', '/')?.snapshot).toEqual(snapshot(4000))
    expect(readGridScroll('u1', '/')).toBeNull()
  })

  it('remembers nothing outside a signed-in session', () => {
    const { result, unmount } = renderBareHook(
      () => useGridScrollMemory({ key: '/', count: 300 }),
      {
        wrapper: ({ children }: { children: ReactNode }) => <MemoryRouter>{children}</MemoryRouter>,
      },
    )

    act(() => {
      result.current.onStateChanged(snapshot(4000))
    })
    unmount()

    expect(window.sessionStorage.length).toBe(0)
  })

  it('restores on a pop back to the view', () => {
    writeGridScroll('u1', '/grid', {
      count: 300,
      scrollY: 4000,
      snapshot: snapshot(3800),
      uid: 'ph_9',
    })
    const { result } = renderNavigableGrid()

    act(() => {
      void result.current.navigate(-1)
    })

    expect(result.current.memory.restoreFrom).toEqual(snapshot(3800))
    expect(result.current.memory.restoreScrollY).toBe(4000)
    expect(result.current.memory.restoreUid).toBe('ph_9')
    expect(result.current.remembered?.count).toBe(300)
  })

  it('starts a view pushed to at its top, however deep it was left', () => {
    writeGridScroll('u1', '/grid', {
      count: 300,
      scrollY: 4000,
      snapshot: snapshot(3800),
      uid: 'ph_9',
    })
    const { result } = renderNavigableGrid()

    // A navigation link: the reader asked for this list, not for their place in it.
    act(() => {
      void result.current.navigate('/grid')
    })

    expect(result.current.memory.restoreFrom).toBeUndefined()
    expect(result.current.memory.restoreScrollY).toBe(0)
    expect(result.current.memory.restoreUid).toBeUndefined()
    // Nor does the page re-fetch its way back to the remembered length.
    expect(result.current.remembered).toBeNull()
  })

  it('does not restore a view replaced into either', () => {
    writeGridScroll('u1', '/grid', { count: 300, scrollY: 4000, snapshot: snapshot(3800) })
    const { result } = renderNavigableGrid()

    act(() => {
      void result.current.navigate('/grid', { replace: true })
    })

    expect(result.current.memory.restoreFrom).toBeUndefined()
    expect(result.current.remembered).toBeNull()
  })

  it('remembers the top of a view pushed to, so a later pop does not revive the old place', () => {
    writeGridScroll('u1', '/grid', { count: 300, scrollY: 4000, snapshot: snapshot(3800) })
    const { result } = renderNavigableGrid()
    act(() => {
      void result.current.navigate('/grid')
    })

    // The reader opens a photograph without scrolling: the grid they were shown is
    // its top, and coming back must land there — not on the position of a visit
    // they were never returned to.
    act(() => {
      void result.current.navigate('/photos/ph_1')
    })

    const left = readGridScroll('u1', '/grid')
    expect(left?.scrollY).toBe(0)
    expect(left?.snapshot).toBeUndefined()
  })

  it('keeps a restore in progress when the view is replaced in place', () => {
    writeGridScroll('u1', '/grid', { count: 300, scrollY: 4000, snapshot: snapshot(3800) })
    const { result } = renderNavigableGrid()
    act(() => {
      void result.current.navigate(-1)
    })

    // A position-only param rewritten as the grid scrolls is a replace that keeps
    // the view: it must not turn the restore it is part of into none.
    act(() => {
      void result.current.navigate('/grid?at=12', { replace: true })
    })

    expect(result.current.memory.restoreFrom).toEqual(snapshot(3800))
  })
})
