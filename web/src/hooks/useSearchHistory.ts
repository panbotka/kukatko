import { useCallback, useEffect, useRef, useState } from 'react'

import {
  clearSearchHistory,
  fetchSearchHistory,
  recordSearch,
  type SearchHistoryEntry,
} from '../services/searchHistory'

/**
 * How long a query must sit unchanged before it counts as "actually searched".
 *
 * It is deliberately far longer than the search page's own 350 ms input
 * debounce. The page commits — and therefore runs — a query as soon as typing
 * pauses, so every prefix of a slowly typed word is a real search; remembering
 * them all would fill the history with `sv`, `sva`, `svat`. Waiting until the
 * query has genuinely stopped changing records the one the reader meant.
 */
const RECORD_DELAY_MS = 2000

/** State returned by {@link useSearchHistory}. */
export interface SearchHistoryState {
  /** The recent searches, most recent first; empty while loading or on failure. */
  entries: SearchHistoryEntry[]
  /** True while the first load of this activation is still in flight. */
  loading: boolean
  /** Forgets the whole history, locally at once and server-side behind it. */
  clear: () => void
}

/**
 * Loads the signed-in user's recent searches, for the dropdown the search box
 * shows while it is focused and empty.
 *
 * `active` is "the dropdown could be on screen right now". The list is fetched
 * when it turns true and refetched on every later activation: the history lives
 * server-side so that another device can extend it, and re-reading it each time
 * the dropdown opens is what makes that visible. Nothing is fetched while the
 * dropdown cannot be seen, so a page that merely *has* a search box pays nothing.
 *
 * A failed load leaves the list empty rather than surfacing an error. A search box
 * that cannot offer history is a plainer search box, not a broken page — and an
 * empty dropdown is the same thing a first-time user sees.
 */
export function useSearchHistory(active: boolean): SearchHistoryState {
  const [entries, setEntries] = useState<SearchHistoryEntry[]>([])
  const [loading, setLoading] = useState(false)
  // Monotonic id of the newest load, so a slow response cannot overwrite a newer
  // one (or repopulate a list the user has just cleared).
  const latest = useRef(0)

  useEffect(() => {
    if (!active) {
      return
    }
    const requestId = latest.current + 1
    latest.current = requestId
    const controller = new AbortController()
    setLoading(true)
    fetchSearchHistory(controller.signal)
      .then((list) => {
        if (latest.current !== requestId) {
          return
        }
        setEntries(list)
        setLoading(false)
      })
      .catch(() => {
        if (latest.current !== requestId) {
          return
        }
        // Silent: no history is a plainer dropdown, never an error.
        setEntries([])
        setLoading(false)
      })
    return () => {
      controller.abort()
    }
  }, [active])

  const clear = useCallback(() => {
    // Empty it locally first: clearing is the one action whose whole point is that
    // the list is gone, so it must not wait on a round trip. The id bump makes any
    // load still in flight land on nothing.
    latest.current += 1
    setEntries([])
    setLoading(false)
    clearSearchHistory().catch(() => {
      // Silent: the next activation refetches, so a failed clear self-corrects.
    })
  }, [])

  return { entries, loading, clear }
}

/**
 * Remembers `query` as a search the user ran, once it has stopped changing for
 * {@link RECORD_DELAY_MS}.
 *
 * The caller passes the query that was *executed* — on the search page that is
 * the one committed to the URL, not the keystrokes — and this hook adds the
 * settling delay on top, so a query typed in bursts is recorded once rather than
 * as each of its prefixes. A blank query records nothing, and the same query is
 * never posted twice in a row from one mount.
 *
 * Failures are swallowed: a history that missed an entry is not worth an error in
 * front of someone who is searching.
 */
export function useRecordSearch(query: string, delayMs: number = RECORD_DELAY_MS): void {
  const trimmed = query.trim()
  // What was last posted, so a re-render (or a Back that lands on the same query)
  // does not record it again.
  const lastRecorded = useRef<string | null>(null)

  useEffect(() => {
    if (trimmed === '' || trimmed === lastRecorded.current) {
      return
    }
    const controller = new AbortController()
    const timer = setTimeout(() => {
      lastRecorded.current = trimmed
      recordSearch(trimmed, controller.signal).catch(() => {
        // Silent, and forget it was tried: the next visit may well succeed.
        if (lastRecorded.current === trimmed) {
          lastRecorded.current = null
        }
      })
    }, delayMs)
    return () => {
      clearTimeout(timer)
      controller.abort()
    }
  }, [trimmed, delayMs])
}
