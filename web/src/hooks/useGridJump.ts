import { type RefObject, useCallback, useEffect, useState } from 'react'
import { type VirtuosoGridHandle } from 'react-virtuoso'

/** Inputs for {@link useGridJump}: the grid handle and its paging state. */
export interface UseGridJumpOptions {
  /** Ref to the virtuoso grid handle used to scroll imperatively. */
  gridRef: RefObject<VirtuosoGridHandle | null>
  /** How many photos are currently loaded (the reachable index ceiling). */
  loadedCount: number
  /** True while more pages remain to be loaded. */
  hasMore: boolean
  /** True while a page is being appended (avoids stacking load-more calls). */
  loadingMore: boolean
  /** Requests the next page; a no-op when none remain. */
  loadMore: () => void
}

/**
 * Returns a `jumpTo(index)` that scrolls the virtualized grid to a photo index,
 * loading pages first when the target lies beyond what is loaded. It records the
 * pending target and, via an effect, either scrolls (once the index is loaded),
 * drives {@link UseGridJumpOptions.loadMore} (while more pages remain), or clamps
 * to the last loaded item (when the list is fully loaded but shorter than the
 * target). This lets the timeline scrubber jump ahead of the infinite-scroll
 * cursor to any month.
 */
export function useGridJump({
  gridRef,
  loadedCount,
  hasMore,
  loadingMore,
  loadMore,
}: UseGridJumpOptions): (index: number) => void {
  const [target, setTarget] = useState<number | null>(null)

  useEffect(() => {
    if (target === null) {
      return
    }
    if (loadedCount > target) {
      gridRef.current?.scrollToIndex({ index: target, align: 'start' })
      setTarget(null)
      return
    }
    if (!hasMore) {
      // The list is fully loaded but shorter than the target; land on the last
      // item rather than leaving the jump hanging.
      gridRef.current?.scrollToIndex({ index: Math.max(0, loadedCount - 1), align: 'start' })
      setTarget(null)
      return
    }
    if (!loadingMore) {
      loadMore()
    }
  }, [target, loadedCount, hasMore, loadingMore, loadMore, gridRef])

  return useCallback((index: number) => {
    setTarget(index)
  }, [])
}
