import { useCallback, useMemo, useState } from 'react'

import { useAuth } from '../auth/AuthContext'
import { type PhotoGridSelection } from '../components/library/PhotoGrid'
import { type BulkEditOutcome } from '../components/organize/BulkEditModal'

import { useSelection, type UseSelectionResult } from './useSelection'

/** Options for {@link useBulkEdit}. */
export interface UseBulkEditOptions {
  /**
   * Called after a successful apply, once the selection has been cleared. A bulk
   * edit changes what the current filters — and an album/label scope — match, so
   * the page has to refetch its list rather than keep showing the pre-edit one.
   * The outcome (the operations applied and the per-photo results) rides along
   * for pages that can update in place instead — e.g. /expand drops just the
   * photos that were added to the collection, keeping the scroll position.
   */
  onEdited?: (outcome?: BulkEditOutcome) => void
  /**
   * Opt into hover-select: the grid is always selectable (a corner checkmark on
   * every tile) for a writer, with no explicit "enter selection mode" step, and
   * turns selection-first the moment anything is picked. Every photo-list page
   * uses this, so multi-select works the same way everywhere; a page then shows
   * its selection toolbar on `selection.count > 0` rather than on
   * `selection.active`. The explicit mode is left for the non-list grids (the
   * /expand candidate review), which still gate selection behind a button.
   */
  hoverSelect?: boolean
}

/** Selection state plus the bulk-edit dialog wiring for one photo list. */
export interface UseBulkEditResult {
  /** Whether the acting user may bulk edit. Viewers never see the control. */
  canBulkEdit: boolean
  /** The underlying grid selection (enter/leave selection mode, toggle tiles). */
  selection: UseSelectionResult
  /** The UIDs to submit: exactly what is selected, never the whole result set. */
  photoUids: string[]
  /** Selection wiring for `PhotoGrid`; `undefined` outside selection mode. */
  gridSelection: PhotoGridSelection | undefined
  /** Whether the bulk-edit dialog is open. */
  editing: boolean
  /** Opens the dialog on the current selection. */
  open: () => void
  /** Dismisses the dialog, leaving the selection intact (e.g. a failed apply). */
  close: () => void
  /** Closes the dialog after a successful apply: clears selection, then reloads. */
  finish: (outcome?: BulkEditOutcome) => void
}

/**
 * Bundles everything a photo list needs to offer bulk metadata editing: the grid
 * selection, the role gate, and the open/close state of the bulk-edit dialog.
 * Pair it with `BulkEditControl`, which renders the trigger and the dialog from
 * this result, so a page only wires `gridSelection` into its `PhotoGrid` and
 * shows its selection toolbar once something is picked (or, in the explicit
 * mode, a button that calls `selection.enable()`).
 *
 * Selection mode survives an apply — only the selection itself is cleared — so a
 * second batch can be picked straight away, and no identifier of a photo the
 * edit moved out of the view can linger in it. A failed apply keeps the
 * selection so the reader can retry without re-picking every tile.
 */
export function useBulkEdit(options: UseBulkEditOptions = {}): UseBulkEditResult {
  const { onEdited, hoverSelect = false } = options
  const { canWrite } = useAuth()
  const selection = useSelection()
  const [editing, setEditing] = useState(false)

  const open = useCallback(() => {
    setEditing(true)
  }, [])

  const close = useCallback(() => {
    setEditing(false)
  }, [])

  const finish = useCallback(
    (outcome?: BulkEditOutcome) => {
      setEditing(false)
      selection.clear()
      onEdited?.(outcome)
    },
    [selection, onEdited],
  )

  const photoUids = useMemo(() => [...selection.selected], [selection.selected])

  const gridSelection = useMemo<PhotoGridSelection | undefined>(() => {
    // A viewer never selects, so the grid stays a plain link grid for them.
    if (!canWrite) {
      return undefined
    }
    // Hover-select is always on for a writer (no explicit mode to enter); the
    // plain mode only wires the grid once selection mode is entered.
    if (hoverSelect) {
      return {
        active: selection.active,
        hoverSelect: true,
        selected: selection.selected,
        onToggle: selection.toggle,
        onToggleRange: selection.toggleRange,
      }
    }
    return selection.active
      ? {
          active: true,
          selected: selection.selected,
          onToggle: selection.toggle,
          onToggleRange: selection.toggleRange,
        }
      : undefined
  }, [
    canWrite,
    hoverSelect,
    selection.active,
    selection.selected,
    selection.toggle,
    selection.toggleRange,
  ])

  return useMemo(
    () => ({
      canBulkEdit: canWrite,
      selection,
      photoUids,
      gridSelection,
      editing,
      open,
      close,
      finish,
    }),
    [canWrite, selection, photoUids, gridSelection, editing, open, close, finish],
  )
}
