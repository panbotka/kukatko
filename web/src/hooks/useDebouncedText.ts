import { useEffect, useRef, useState } from 'react'

/**
 * How long a text filter waits after the last keystroke before it commits. Long
 * enough that a typed word costs one request rather than one per letter, short
 * enough that the reader does not notice they are waiting — the same order as
 * the search page's own box and the place lookup.
 */
export const TEXT_FILTER_DEBOUNCE_MS = 300

/** The draft value and the setter a debounced text field is driven by. */
export type UseDebouncedTextResult = readonly [draft: string, setDraft: (next: string) => void]

/**
 * A text field that keeps up with the keyboard but commits on a pause.
 *
 * A filter field wired straight to the URL costs a request per keystroke: typing
 * `svatba` refetches the grid six times, each one resetting the result — and with
 * it the count the filters drawer states — so the number the reader is watching
 * flickers through six intermediate answers on the way to the one they asked
 * for. The draft is local, so typing stays instant; `commit` runs once
 * `delayMs` after the last change, so the query, the grid and the count move
 * once.
 *
 * The draft follows the committed value whenever *that* changes from outside —
 * a cleared filter, a removed chip, Back — but never bounces the reader's own
 * in-flight typing back at them: the last value passed through this hook in
 * either direction is remembered, and only a value differing from it is treated
 * as external. A `commit` the caller ignores therefore leaves the field alone
 * rather than snapping it back mid-word.
 */
export function useDebouncedText(
  value: string,
  commit: (next: string) => void,
  delayMs: number = TEXT_FILTER_DEBOUNCE_MS,
): UseDebouncedTextResult {
  const [draft, setDraft] = useState(value)
  // Read at fire time, so a caller re-creating the callback every render (which
  // every inline arrow does) neither restarts the timer nor commits stale state.
  const commitRef = useRef(commit)
  commitRef.current = commit
  // The value the two sides last agreed on. Anything else arriving as `value` is
  // an outside change and replaces the draft; anything else in `draft` is
  // unsent typing and is what the timer commits.
  const syncedRef = useRef(value)

  useEffect(() => {
    if (value === syncedRef.current) {
      return
    }
    syncedRef.current = value
    setDraft(value)
  }, [value])

  useEffect(() => {
    if (draft === syncedRef.current) {
      return
    }
    const timer = setTimeout(() => {
      syncedRef.current = draft
      commitRef.current(draft)
    }, delayMs)
    return () => {
      clearTimeout(timer)
    }
  }, [draft, delayMs])

  return [draft, setDraft]
}
