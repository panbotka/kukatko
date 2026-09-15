import { type CSSProperties, type RefObject, useCallback, useLayoutEffect, useState } from 'react'

import { useIsNarrowViewport } from './useIsNarrowViewport'

/** Viewport-relative box for the desktop overlay, measured off the anchor. */
interface MenuPosition {
  /** Distance from the viewport top to the menu's top edge, in px. */
  top: number
  /** Distance from the viewport left to the menu's left edge, in px. */
  left: number
  /** Menu width (the anchor's width), in px. */
  width: number
  /** The height the menu may grow to before it scrolls its own list, in px. */
  maxHeight: number
}

/** How an anchored menu paints itself; both fields go on the list element. */
export interface AnchoredMenu {
  /** Class list for the list element — `.dropdown-menu` plus its positioning. */
  className: string
  /** Inline style for the list element: the measured box, or the in-flow cap. */
  style: CSSProperties
}

/** The gap between the anchor's bottom edge and the menu's top edge, in px. */
const GAP = 4

/** How close to the viewport's bottom edge the menu may reach, in px. */
const MARGIN = 8

/** The menu never grows shorter than this, even in a cramped viewport, in px. */
const MIN_HEIGHT = 120

/**
 * Places a field's suggestion list so a scrolling ancestor cannot clip it.
 *
 * An `overflow: auto` ancestor — `modal-dialog-scrollable`'s body, above all —
 * cuts off any absolutely positioned child that reaches past its box, which is
 * exactly what a dropdown under an input does. Two layouts answer that, and the
 * viewport picks between them:
 *
 * - **desktop:** a `position: fixed` overlay measured off the anchor's viewport
 *   box, so no ancestor's overflow applies to it. It is re-measured on a
 *   capture-phase `scroll` (the modal body scrolling *under* it is the case a
 *   bubbling listener never sees) and on `resize`, and it takes its layer from
 *   `.kk-overlay-menu` — Bootstrap's `.dropdown-menu` sits at z-index 1000,
 *   which loses to every sticky bar in the app.
 * - **phone:** an in-flow block (`position-static`) inside the ancestor's own
 *   scroll, so the field and its options stay reachable above the on-screen
 *   keyboard. A fixed box would be covered by the keyboard instead.
 *
 * Shared by {@link import('../components/MultiSelect').MultiSelect} and
 * {@link import('../components/photo/AddAutocomplete').AddAutocomplete} so the
 * two never drift apart.
 *
 * @param anchor The field the menu hangs off; its box is what gets measured.
 * @param open Whether the menu is on screen — nothing is measured while it is not.
 */
export function useAnchoredMenu(
  anchor: RefObject<HTMLElement | null>,
  open: boolean,
): AnchoredMenu {
  const narrow = useIsNarrowViewport()
  const [position, setPosition] = useState<MenuPosition | null>(null)

  // Re-measures the overlay from the anchor's current viewport box. The list may
  // grow to its content but never past half the viewport nor past the room left
  // below the field; beyond that it scrolls its own options rather than the page.
  const measure = useCallback(() => {
    const element = anchor.current
    if (element === null) {
      return
    }
    const rect = element.getBoundingClientRect()
    const maxHeight = Math.max(
      MIN_HEIGHT,
      Math.min(window.innerHeight * 0.5, window.innerHeight - rect.bottom - GAP - MARGIN),
    )
    setPosition({ top: rect.bottom + GAP, left: rect.left, width: rect.width, maxHeight })
  }, [anchor])

  // Only the desktop overlay needs coordinates, and only while it is open.
  useLayoutEffect(() => {
    if (!open || narrow) {
      setPosition(null)
      return
    }
    measure()
    window.addEventListener('scroll', measure, true)
    window.addEventListener('resize', measure)
    return () => {
      window.removeEventListener('scroll', measure, true)
      window.removeEventListener('resize', measure)
    }
  }, [open, narrow, measure])

  if (narrow) {
    return {
      className: 'dropdown-menu show position-static w-100 mt-1 shadow-sm overflow-auto',
      style: { maxHeight: '50vh' },
    }
  }

  return {
    className: 'dropdown-menu show shadow overflow-auto kk-overlay-menu',
    style:
      position === null
        ? // Hidden for the one frame before the layout effect measures it, so it
          // never flashes at the top-left corner.
          { position: 'fixed', visibility: 'hidden' }
        : {
            position: 'fixed',
            top: position.top,
            left: position.left,
            width: position.width,
            maxHeight: position.maxHeight,
          },
  }
}
