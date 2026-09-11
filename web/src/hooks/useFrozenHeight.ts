import { type RefObject, useLayoutEffect, useState } from 'react'

/** What was measured, and for which subject. */
interface Frozen {
  /** The subject the height belongs to; see {@link useFrozenHeight}'s `key`. */
  key: string | null
  /** The measured height in CSS pixels, or null when nothing could be measured. */
  height: number | null
}

/**
 * The height an element had the moment a subject opened, held until the subject
 * changes — so a block whose content shrinks under the reader keeps the space it
 * started with instead of collapsing.
 *
 * It exists for lists that are *answered away*. The album face-tagging run lists
 * one row per unnamed face and takes a row out the moment it is confirmed; with
 * the block sized by its content, every yes shortened the page, and everything
 * above it — the photograph and the boxes drawn on it, which are sized against
 * the height left over — moved. The reader loses the row they were about to press
 * next. Freezing the block makes the answering rhythm still: the rows disappear
 * one by one inside a box that does not move.
 *
 * The `key` is what the frozen value belongs to (a photo's uid). A new key
 * measures again, in a **layout** effect — before the browser paints, so the
 * recomputation is never seen as a jump — and until that measurement lands the
 * hook reports null rather than the previous subject's height, which would be a
 * stranger's number applied to this content.
 *
 * Deliberately **not** re-measured on resize: the only signal that may change the
 * reserved height is moving to another subject. A phone hiding its URL bar fires
 * a resize mid-run, and re-measuring there would be exactly the jump the freeze
 * exists to prevent — the caller caps the value in CSS (`max-height` in viewport
 * units) instead, which follows a rotation on its own.
 *
 * An unmeasurable element (jsdom, which lays nothing out) reports null, i.e. no
 * frozen height at all: the caller then simply sizes by content, as it did
 * before.
 */
export function useFrozenHeight(
  ref: RefObject<HTMLElement | null>,
  key: string | null,
): number | null {
  const [frozen, setFrozen] = useState<Frozen>({ key, height: null })

  useLayoutEffect(() => {
    const element = ref.current
    // Rounded up: a fractional box against an integer one shows a scrollbar for
    // the half pixel the content is said to overflow by.
    const height = element === null ? 0 : Math.ceil(element.getBoundingClientRect().height)
    setFrozen({ key, height: height > 0 ? height : null })
  }, [key, ref])

  return frozen.key === key ? frozen.height : null
}
