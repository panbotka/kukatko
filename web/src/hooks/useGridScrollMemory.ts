import { useCallback, useEffect, useMemo, useRef } from 'react'
import { type StateSnapshot } from 'react-virtuoso'

import {
  forgetGridPhoto,
  type GridScrollState,
  readGridScroll,
  writeGridScroll,
} from '../lib/gridScroll'

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
   * While true nothing is written. A caller driving the view back to its
   * remembered position sets this until it gets there, so the offsets on the way
   * — a half-loaded document pinned to its top — cannot overwrite the very
   * position being restored.
   */
  restoring?: boolean
}

/**
 * What {@link useGridScrollMemory} hands back — everything a
 * {@link import('../components/library/PhotoGrid').PhotoGrid} needs to put the
 * reader back where they were, in one value. It is handed over whole
 * (`<PhotoGrid scroll={…} />`) rather than unpacked into a prop each, because
 * half a memory is worse than none: a grid restored without reporting overwrites
 * nothing, a grid reporting without restoring lands at the top every time, and
 * neither mistake shows up in a test that exercises the hook by itself.
 */
export interface GridScrollMemory {
  /**
   * The remembered virtuoso state — the offset plus the row measurements that
   * give it meaning; undefined when this view has no remembered position.
   */
  restoreFrom: StateSnapshot | undefined
  /**
   * The remembered window offset: the restore target for a grid with no virtuoso
   * to ask, and the fallback for one whose snapshot could not be read back.
   */
  restoreScrollY: number
  /**
   * The photograph the reader last had open from this view, to be revealed once
   * the grid has landed; undefined when they opened none.
   */
  restoreUid: string | undefined
  /** Reports the grid's position, whenever it changes. */
  onStateChanged: (snapshot: StateSnapshot) => void
}

/**
 * How far down this view is being restored to, whichever of the two positions it
 * kept says so. Zero — nothing remembered — means a reported top is simply the
 * top, with no restore for it to be mistaken for.
 */
function restoreTop(remembered: GridScrollState | null): number {
  return Math.max(remembered?.scrollY ?? 0, remembered?.snapshot?.scrollTop ?? 0)
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
  const restoreTopRef = useRef(restoreTop(remembered))
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

  // Whether an offset just reported is a position worth keeping. The grid is
  // reported at the top twice for different reasons — before its restore has
  // landed, and after the reader has scrolled back up themselves — and only the
  // second is a position. Until the view has been seen away from the top, a zero
  // belongs to the restore on its way there.
  const worthKeeping = useCallback((offset: number) => {
    if (offset > 0) {
      movedRef.current = true
      return true
    }
    return movedRef.current || restoreTopRef.current === 0
  }, [])

  const onStateChanged = useCallback(
    (snapshot: StateSnapshot) => {
      if (!worthKeeping(snapshot.scrollTop)) {
        return
      }
      snapshotRef.current = snapshot
      schedule()
    },
    [schedule, worthKeeping],
  )

  // A new view starts with a memory of its own: write out whatever the previous
  // one still had pending, then drop it so it cannot be written under this key.
  useEffect(() => {
    if (keyRef.current !== key) {
      persist(pendingCountRef.current)
    }
    keyRef.current = key
    // The photograph the viewer left against this view is consumed here: the grid
    // gets it once, through `restoreUid`, and the store keeps only the position.
    forgetGridPhoto(key)
    restoreTopRef.current = restoreTop(remembered)
    snapshotRef.current = undefined
    scrollYRef.current = 0
    movedRef.current = false
    dirtyRef.current = false
  }, [key, remembered, persist])

  // Every grid here scrolls the document — the virtualized wall runs virtuoso
  // with `useWindowScroll`, the person gallery renders its own tiles — so the
  // window's offset is always *a* truthful position, and always worth recording.
  // It used to be an option, which six of the seven pages did not pass, and their
  // memory kept nothing the browser would recognise.
  useEffect(() => {
    const onScroll = () => {
      if (!worthKeeping(window.scrollY)) {
        return
      }
      scrollYRef.current = window.scrollY
      schedule()
    }
    window.addEventListener('scroll', onScroll, { passive: true })
    return () => {
      window.removeEventListener('scroll', onScroll)
    }
  }, [schedule, worthKeeping])

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
    restoreUid: remembered?.uid,
    onStateChanged,
  }
}
