import { useEffect, useRef, useState } from 'react'

import { type ShortcutMap, useKeyboardShortcuts } from './useKeyboardShortcuts'

/** Inputs for {@link useGridKeyboardNavigation}. */
export interface UseGridKeyboardNavigationOptions {
  /** Number of tiles currently available to focus (the loaded photo count). */
  count: number
  /** When false the shortcuts are inert (e.g. before the first page loads). */
  enabled: boolean
  /**
   * A value that changes whenever the underlying list is replaced (filters, sort
   * or scope): focus is reset to "none" so the highlight never lands on a stale
   * tile after the grid reloads.
   */
  resetKey: string
  /** Returns the current number of grid columns (for row-wise up/down moves). */
  getColumns: () => number
  /** Scrolls the tile at `index` into view (follows virtualization). */
  scrollToIndex: (index: number) => void
  /** Opens the focused tile's detail (Enter). */
  onOpen: (index: number) => void
  /** Toggles selection of the focused tile, entering selection mode (`x`). */
  onToggleSelect: (index: number) => void
  /** Toggles favorite on the focused tile (`f`). */
  onToggleFavorite: (index: number) => void
  /** Whether there is a non-empty selection to clear (drives Escape order). */
  hasSelection: boolean
  /** Clears the current selection (first Escape when a selection exists). */
  onClearSelection: () => void
}

/** The focus state exposed by {@link useGridKeyboardNavigation}. */
export interface GridKeyboardNavigation {
  /** Index of the tile with the keyboard focus highlight, or -1 for none. */
  readonly focusedIndex: number
}

/**
 * Drives keyboard navigation over the virtualized photo grid: it tracks a focus
 * highlight and registers the grid shortcuts via {@link useKeyboardShortcuts}.
 * Arrow keys and `j`/`k`/`h`/`l` move the highlight (left/right by one, up/down by
 * a row using the live column count), scrolling the focused tile into view so the
 * highlight follows virtualization. `Enter` opens it, `x` selects it (entering
 * selection mode), `f` favorites it, and `Escape` clears the selection first, then
 * the focus. The first directional key focuses the first tile.
 */
export function useGridKeyboardNavigation(
  options: UseGridKeyboardNavigationOptions,
): GridKeyboardNavigation {
  const {
    count,
    enabled,
    resetKey,
    getColumns,
    scrollToIndex,
    onOpen,
    onToggleSelect,
    onToggleFavorite,
    hasSelection,
    onClearSelection,
  } = options

  const [focusedIndex, setFocusedIndex] = useState(-1)
  const focusedRef = useRef(focusedIndex)
  focusedRef.current = focusedIndex

  // Drop the highlight when the underlying list is swapped out (new filters etc.),
  // so it never points at a tile that no longer exists.
  useEffect(() => {
    setFocusedIndex(-1)
  }, [resetKey])

  // Moves the highlight by `delta` tiles. From "no focus" (-1) any move lands on
  // the first tile; otherwise it steps and clamps within the loaded range.
  const move = (delta: number) => {
    if (count <= 0) {
      return
    }
    const current = focusedRef.current
    const base = current < 0 ? 0 : current + delta
    const next = Math.min(Math.max(base, 0), count - 1)
    setFocusedIndex(next)
    scrollToIndex(next)
  }

  const handlers: ShortcutMap = {
    ArrowRight: () => {
      move(1)
    },
    l: () => {
      move(1)
    },
    ArrowLeft: () => {
      move(-1)
    },
    h: () => {
      move(-1)
    },
    ArrowDown: () => {
      move(getColumns())
    },
    j: () => {
      move(getColumns())
    },
    ArrowUp: () => {
      move(-getColumns())
    },
    k: () => {
      move(-getColumns())
    },
    Enter: () => {
      if (focusedRef.current >= 0) {
        onOpen(focusedRef.current)
      }
    },
    x: () => {
      if (focusedRef.current >= 0) {
        onToggleSelect(focusedRef.current)
      }
    },
    f: () => {
      if (focusedRef.current >= 0) {
        onToggleFavorite(focusedRef.current)
      }
    },
  }

  // Only own Escape when there is something to clear, so it does not swallow the
  // key from an overlay (e.g. the help modal) when the grid has no focus/selection.
  if (hasSelection || focusedRef.current >= 0) {
    handlers.Escape = () => {
      if (hasSelection) {
        onClearSelection()
      } else {
        setFocusedIndex(-1)
      }
    }
  }

  useKeyboardShortcuts(handlers, { enabled })

  return { focusedIndex }
}
