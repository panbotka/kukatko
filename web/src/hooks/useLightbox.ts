import { useCallback, useRef, useState } from 'react'

/** What {@link useLightbox} gives a page to drive its overlay with. */
export interface Lightbox<T> {
  /** The item on show, or null while closed. */
  item: T | null
  /** Its position in the list, or -1 while closed. */
  index: number
  /** Whether the overlay is up. */
  isOpen: boolean
  /** Whether stepping forward/back would move anywhere. */
  hasNext: boolean
  hasPrev: boolean
  /** Opens on the item at `index`, remembering what had the focus. */
  open: (index: number) => void
  /** Closes and hands the focus back. */
  close: () => void
  /** Steps to the next/previous item; both stop at the ends. */
  next: () => void
  prev: () => void
}

/**
 * Which item of a list a review tool is currently showing enlarged.
 *
 * The hook holds an **index**, not the item: a review list is rebuilt on every
 * decision (a confirm gives the same candidate a new status), so remembering the
 * object would leave the overlay showing a stale copy of what the grid behind it
 * has already updated. Reading `items[index]` on each render keeps the two in
 * step for free.
 *
 * Stepping stops at both ends rather than wrapping. A viewer that jumps back to
 * the first photo after the last one reads as a stuck control, and "you have
 * reached the end" is exactly the thing worth feeling while working through a
 * queue of decisions.
 *
 * Closing returns the focus to whatever element opened the overlay — the card
 * the user came from — so a keyboard user is not dropped at the top of the
 * document after every look. An element that has since left the DOM (its card
 * was rejected away) is skipped rather than focused.
 */
export function useLightbox<T>(items: T[]): Lightbox<T> {
  const [index, setIndex] = useState(-1)
  const opener = useRef<HTMLElement | null>(null)

  // A shrinking list can strand the index past the end — closing is the honest
  // answer, since the item that was on show is gone.
  const live = index >= 0 && index < items.length
  const at = live ? index : -1

  const open = useCallback((next: number) => {
    opener.current = document.activeElement instanceof HTMLElement ? document.activeElement : null
    setIndex(next)
  }, [])

  const close = useCallback(() => {
    setIndex(-1)
    const previous = opener.current
    opener.current = null
    if (previous?.isConnected === true) {
      previous.focus()
    }
  }, [])

  const step = useCallback(
    (delta: number) => {
      setIndex((current) => {
        if (current < 0) {
          return current
        }
        return Math.min(Math.max(current + delta, 0), items.length - 1)
      })
    },
    [items.length],
  )

  const next = useCallback(() => {
    step(1)
  }, [step])
  const prev = useCallback(() => {
    step(-1)
  }, [step])

  return {
    item: at < 0 ? null : items[at],
    index: at,
    isOpen: at >= 0,
    hasNext: at >= 0 && at < items.length - 1,
    hasPrev: at > 0,
    open,
    close,
    next,
    prev,
  }
}
