import { useCallback, useMemo, useState } from 'react'

/** State and actions for a multi-item grid selection. */
export interface UseSelectionResult {
  /** Whether selection mode is active (tiles show checkboxes instead of links). */
  active: boolean
  /** The currently selected item UIDs. */
  selected: Set<string>
  /** Number of selected items, for convenience in the toolbar. */
  count: number
  /** Enters selection mode. */
  enable: () => void
  /** Leaves selection mode and clears the selection. */
  disable: () => void
  /** Toggles whether `uid` is selected, anchoring a later shift-range there. */
  toggle: (uid: string) => void
  /**
   * Selects the contiguous range between the last toggled item (the anchor) and
   * `uid`, inclusive, in the order given by `orderedUids` — the Shift+click
   * gesture. The range only ever adds to the selection. Without an anchor, or
   * when either end is no longer in `orderedUids` (the item left the grid), it
   * degrades to a plain {@link UseSelectionResult.toggle}.
   */
  toggleRange: (uid: string, orderedUids: string[]) => void
  /** Adds every UID in `uids` to the selection (e.g. select-all-in-view). */
  selectMany: (uids: string[]) => void
  /** Clears the selection without leaving selection mode. */
  clear: () => void
}

/**
 * Tracks a multi-item selection over a photo grid: which tiles are selected and
 * whether selection mode is active. Used by the library and album/label grids to
 * drive bulk add-to-album / add-label and remove-from-album affordances. Leaving
 * selection mode clears the selection so a later session starts fresh.
 *
 * The last plainly toggled tile is remembered as the *anchor*, so a Shift+click
 * can select everything between it and the clicked tile in one gesture
 * ({@link UseSelectionResult.toggleRange}). Clearing the selection also drops
 * the anchor — a fresh batch starts with no stale range endpoint.
 */
export function useSelection(): UseSelectionResult {
  const [active, setActive] = useState(false)
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [anchor, setAnchor] = useState<string | null>(null)

  const enable = useCallback(() => {
    setActive(true)
  }, [])

  const disable = useCallback(() => {
    setActive(false)
    setSelected(new Set())
    setAnchor(null)
  }, [])

  const clear = useCallback(() => {
    setSelected(new Set())
    setAnchor(null)
  }, [])

  const toggle = useCallback((uid: string) => {
    setAnchor(uid)
    setSelected((prev) => {
      const next = new Set(prev)
      if (next.has(uid)) {
        next.delete(uid)
      } else {
        next.add(uid)
      }
      return next
    })
  }, [])

  const toggleRange = useCallback(
    (uid: string, orderedUids: string[]) => {
      const from = anchor === null ? -1 : orderedUids.indexOf(anchor)
      const to = orderedUids.indexOf(uid)
      if (from < 0 || to < 0) {
        toggle(uid)
        return
      }
      const [start, end] = from <= to ? [from, to] : [to, from]
      const range = orderedUids.slice(start, end + 1)
      setSelected((prev) => {
        const next = new Set(prev)
        for (const item of range) {
          next.add(item)
        }
        return next
      })
    },
    [anchor, toggle],
  )

  const selectMany = useCallback((uids: string[]) => {
    setSelected((prev) => {
      const next = new Set(prev)
      for (const uid of uids) {
        next.add(uid)
      }
      return next
    })
  }, [])

  return useMemo(
    () => ({
      active,
      selected,
      count: selected.size,
      enable,
      disable,
      toggle,
      toggleRange,
      selectMany,
      clear,
    }),
    [active, selected, enable, disable, toggle, toggleRange, selectMany, clear],
  )
}
