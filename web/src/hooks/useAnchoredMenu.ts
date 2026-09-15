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
 * The `will-change` hints that promise one of the properties below, and so make
 * an element the containing block for its fixed descendants before the property
 * is ever set.
 */
const CONTAINING_WILL_CHANGE = ['transform', 'perspective', 'filter', 'backdrop-filter']

/** The `contain` values that make an element the containing block. */
const CONTAINING_CONTAIN = ['layout', 'paint', 'strict', 'content']

/** Reports whether a computed value says the property is actually in effect. */
function isSet(value: string): boolean {
  return value !== '' && value !== 'none'
}

/** Reads a computed length, treating "not laid out" (jsdom, `auto`) as zero. */
function px(value: string): number {
  const parsed = Number.parseFloat(value)
  return Number.isNaN(parsed) ? 0 : parsed
}

/**
 * Reports whether `element` is the containing block for `position: fixed`
 * descendants — i.e. whether a fixed child of it is placed against *its* box
 * rather than against the viewport.
 *
 * A transform is the usual culprit and the one that broke the photo viewer, but
 * the same is true of `perspective`, `filter`, `backdrop-filter`, a `will-change`
 * naming any of them, `contain: layout|paint|strict|content` and a container
 * query context. Any value other than `none` counts — an identity matrix
 * included, which is why the viewer's drawer qualifies while it sits at
 * `translateX(0)`.
 */
function isFixedContainingBlock(element: HTMLElement): boolean {
  const style = getComputedStyle(element)
  const willChange = style.willChange
  const contain = style.getPropertyValue('contain')
  const containerType = style.getPropertyValue('container-type')
  return (
    isSet(style.transform) ||
    isSet(style.perspective) ||
    isSet(style.filter) ||
    isSet(style.getPropertyValue('backdrop-filter')) ||
    CONTAINING_WILL_CHANGE.some((property) => willChange.includes(property)) ||
    CONTAINING_CONTAIN.some((value) => contain.includes(value)) ||
    (containerType !== '' && containerType !== 'normal')
  )
}

/**
 * The viewport coordinates a fixed child of `element` is placed against: the
 * padding-box origin of the nearest ancestor that is a containing block for
 * fixed positioning, or `(0, 0)` when there is none and the viewport itself
 * holds the child.
 *
 * Subtracting this from a viewport measurement is what makes the overlay land
 * where it was measured. A scaled or rotated ancestor would need more than an
 * offset, but nothing in the app transforms a menu's ancestor that way — the
 * drawers translate.
 */
function containingBlockOrigin(element: HTMLElement): { top: number; left: number } {
  for (let parent = element.parentElement; parent !== null; parent = parent.parentElement) {
    if (!isFixedContainingBlock(parent)) {
      continue
    }
    const rect = parent.getBoundingClientRect()
    const style = getComputedStyle(parent)
    // A fixed child is laid out against the padding box; the rect is the border box.
    return {
      top: rect.top + px(style.borderTopWidth),
      left: rect.left + px(style.borderLeftWidth),
    }
  }
  return { top: 0, left: 0 }
}

/**
 * Places a field's suggestion list so a scrolling ancestor cannot clip it.
 *
 * An `overflow: auto` ancestor — `modal-dialog-scrollable`'s body, above all —
 * cuts off any absolutely positioned child that reaches past its box, which is
 * exactly what a dropdown under an input does. Two layouts answer that, and the
 * viewport picks between them:
 *
 * - **desktop:** a `position: fixed` overlay measured off the anchor's viewport
 *   box, so no ancestor's overflow applies to it. That viewport measurement is
 *   then rebased onto the containing block an ancestor may have claimed — a
 *   transform of any value, an identity matrix included, makes one and would
 *   otherwise throw the menu as far off screen as the ancestor sits from the
 *   viewport's corner (the photo viewer's sliding info drawer did exactly that).
 *   It is re-measured on a
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
    // `top`/`left` are resolved against the containing block, which is the
    // viewport only while no ancestor claims fixed descendants for itself; the
    // height above is a length, so it never needs the same correction.
    const origin = containingBlockOrigin(element)
    setPosition({
      top: rect.bottom + GAP - origin.top,
      left: rect.left - origin.left,
      width: rect.width,
      maxHeight,
    })
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
