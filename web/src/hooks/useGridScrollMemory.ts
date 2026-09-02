import { useCallback, useEffect, useMemo, useRef } from 'react'
import { type StateSnapshot } from 'react-virtuoso'

import { type GridScrollState, readGridScroll, writeGridScroll } from '../lib/gridScroll'

/**
 * How long the memory waits after the last position change before writing it.
 * Scrolling reports a position every frame, and serializing the store sixty
 * times a second to keep only the last of them would be paid for in scroll
 * smoothness. The pending write is flushed on unmount and on `pagehide`, so
 * leaving the page never loses the position it was left at.
 */
const WRITE_DELAY_MS = 200

/** Options for {@link useGridScrollMemory}. */
export interface UseGridScrollMemoryOptions {
  /**
   * Identity of the view whose position is being remembered — see
   * {@link import('../lib/gridScroll').gridScrollKey}. An empty key turns the
   * memory off (nothing is read, nothing is written).
   */
  key: string
  /**
   * How many photos the grid currently holds, remembered alongside the position
   * so a list that grows by appending pages can load its way back to the same
   * length before restoring. The page reads it back straight from
   * {@link import('../lib/gridScroll').readGridScroll} — it needs the number
   * *before* the list hook it feeds, which is upstream of this one. Leave at 0
   * for a windowed list (the library), which is as tall as the whole result from
   * its first response.
   */
  count?: number
  /**
   * Where the position comes from. `virtuoso` (the default) records the
   * snapshots a {@link import('../components/library/PhotoGrid').PhotoGrid}
   * reports; `window` records the window's own scroll offset, for a grid that
   * renders every tile itself and so has no virtuoso to ask.
   */
  track?: 'virtuoso' | 'window'
  /**
   * While true nothing is written. A caller driving the view back to its
   * remembered position sets this until it gets there, so the offsets on the way
   * — a half-loaded document pinned to its top — cannot overwrite the very
   * position being restored.
   */
  restoring?: boolean
}

/** What {@link useGridScrollMemory} hands back to the page. */
export interface GridScrollMemory {
  /**
   * The remembered virtuoso state, for `PhotoGrid`'s `restoreStateFrom`;
   * undefined when this view has no remembered position.
   */
  restoreFrom: StateSnapshot | undefined
  /** The remembered window offset, for a grid that is not virtualized. */
  restoreScrollY: number
  /** Pass to `PhotoGrid`'s `onStateChanged`. */
  onStateChanged: (snapshot: StateSnapshot) => void
}

/**
 * Remembers where a photo grid was scrolled to, per view, for the length of the
 * browser session — so stepping into a photo and coming back (with the browser's
 * Back button or the viewer's own "back to list", which is the same history pop)
 * lands on the tile that was left, not at the top of the library.
 *
 * The position is keyed on the view, not on the page: changing a filter renumbers
 * every position, so it must not restore one taken under a different result set.
 * Nothing here re-renders the caller — the position lives in refs and reaches
 * sessionStorage through a debounced write — and every step is best-effort: a
 * view with nothing remembered simply starts at the top, as before.
 */
export function useGridScrollMemory({
  key,
  count = 0,
  track = 'virtuoso',
  restoring = false,
}: UseGridScrollMemoryOptions): GridScrollMemory {
  const remembered = useMemo(() => readGridScroll(key), [key])

  // Everything the writer needs lives in refs: recording a scroll offset must
  // never re-render the grid that is being scrolled.
  const keyRef = useRef(key)
  const countRef = useRef(count)
  countRef.current = count
  const restoringRef = useRef(restoring)
  restoringRef.current = restoring
  const snapshotRef = useRef<StateSnapshot | undefined>(undefined)
  const scrollYRef = useRef(0)
  // Whether anything worth writing has been observed since this view became
  // current. Without it, a reader who opens a photo without scrolling would have
  // their remembered position overwritten by the view's untouched initial state.
  const dirtyRef = useRef(false)
  // The offset this view is being restored to, and whether the grid has been seen
  // away from its top since. Until it has, a reported offset of zero is the grid
  // *before* its restore landed — not the reader scrolling back to the top — and
  // recording it would throw away the position on the way to it.
  const restoreTopRef = useRef(remembered?.snapshot?.scrollTop ?? 0)
  const movedRef = useRef(false)
  // The loaded length as of the last recorded position. Only the flush that
  // happens *because* the view changed needs it: by then this render already
  // carries the new view's length, and writing that under the old view's key
  // would promise a list far shorter than the offset beside it.
  const pendingCountRef = useRef(0)
  const timerRef = useRef<number | undefined>(undefined)

  const persist = useCallback((count?: number) => {
    if (timerRef.current !== undefined) {
      window.clearTimeout(timerRef.current)
      timerRef.current = undefined
    }
    if (!dirtyRef.current || restoringRef.current || keyRef.current === '') {
      return
    }
    const state: GridScrollState = {
      count: count ?? countRef.current,
      scrollY: scrollYRef.current,
    }
    if (snapshotRef.current !== undefined) {
      state.snapshot = snapshotRef.current
    }
    writeGridScroll(keyRef.current, state)
  }, [])

  const schedule = useCallback(() => {
    dirtyRef.current = true
    pendingCountRef.current = countRef.current
    if (timerRef.current !== undefined) {
      return
    }
    timerRef.current = window.setTimeout(() => {
      timerRef.current = undefined
      persist()
    }, WRITE_DELAY_MS)
  }, [persist])

  const onStateChanged = useCallback(
    (snapshot: StateSnapshot) => {
      if (snapshot.scrollTop > 0) {
        movedRef.current = true
      } else if (!movedRef.current && restoreTopRef.current > 0) {
        // The grid is still at the top of a view that is being restored deeper
        // down: this is the state on the way there, not a position to keep.
        return
      }
      snapshotRef.current = snapshot
      schedule()
    },
    [schedule],
  )

  // A new view starts with a memory of its own: write out whatever the previous
  // one still had pending, then drop it so it cannot be written under this key.
  useEffect(() => {
    if (keyRef.current !== key) {
      persist(pendingCountRef.current)
    }
    keyRef.current = key
    restoreTopRef.current = remembered?.snapshot?.scrollTop ?? 0
    snapshotRef.current = undefined
    scrollYRef.current = 0
    movedRef.current = false
    dirtyRef.current = false
  }, [key, remembered, persist])

  // A grid that renders its own tiles reports nothing, so the window's offset is
  // the only position there is to record.
  useEffect(() => {
    if (track !== 'window') {
      return
    }
    const onScroll = () => {
      scrollYRef.current = window.scrollY
      schedule()
    }
    window.addEventListener('scroll', onScroll, { passive: true })
    return () => {
      window.removeEventListener('scroll', onScroll)
    }
  }, [track, schedule])

  // Leaving for a photo unmounts the page, so the last position has to be
  // written out there and then; `pagehide` covers the reload/close that never
  // unmounts anything.
  useEffect(() => {
    const flush = () => {
      persist()
    }
    window.addEventListener('pagehide', flush)
    return () => {
      window.removeEventListener('pagehide', flush)
      persist()
    }
  }, [persist])

  return {
    restoreFrom: remembered?.snapshot,
    restoreScrollY: remembered?.scrollY ?? 0,
    onStateChanged,
  }
}
