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
  /** Toggles whether `uid` is selected. */
  toggle: (uid: string) => void
  /** Clears the selection without leaving selection mode. */
  clear: () => void
}

/**
 * Tracks a multi-item selection over a photo grid: which tiles are selected and
 * whether selection mode is active. Used by the library and album/label grids to
 * drive bulk add-to-album / add-label and remove-from-album affordances. Leaving
 * selection mode clears the selection so a later session starts fresh.
 */
export function useSelection(): UseSelectionResult {
  const [active, setActive] = useState(false)
  const [selected, setSelected] = useState<Set<string>>(new Set())

  const enable = useCallback(() => {
    setActive(true)
  }, [])

  const disable = useCallback(() => {
    setActive(false)
    setSelected(new Set())
  }, [])

  const clear = useCallback(() => {
    setSelected(new Set())
  }, [])

  const toggle = useCallback((uid: string) => {
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

  return useMemo(
    () => ({ active, selected, count: selected.size, enable, disable, toggle, clear }),
    [active, selected, enable, disable, toggle, clear],
  )
}
