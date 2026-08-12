import { useCallback, useEffect, useRef, useState } from 'react'

import {
  clearSearchHistory,
  fetchSearchHistory,
  recordSearch,
  type SearchHistoryEntry,
} from '../services/searchHistory'

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
 * shows while it is focused — empty, and as prefix matches once something is
 * typed.
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
 * The write side of the history: returns `record(query)`, to be called when the
 * reader has actually **submitted** a query.
 *
 * Deliberately not driven by the query that is running. The search page runs a
 * query as soon as typing pauses, so watching what ran means remembering every
 * prefix of a slowly typed word — a four-second hesitation mid-word is enough to
 * put `sva` in the ring next to `svatba`, and a couple of hesitant sessions fill
 * the (capped) ring with prefixes of itself. Only a deliberate act records:
 * Enter, picking a recent search, running one from the command palette. The
 * caller knows which of its events those are; a timer never can.
 *
 * A blank query records nothing, and the same query is not posted twice in a row
 * from one mount, so leaning on Enter costs one request. The post is not tied to
 * the caller's lifetime — running a search from the palette navigates away
 * immediately, and an aborted record would be no record at all.
 *
 * Failures are swallowed: a history that missed an entry is not worth an error in
 * front of someone who is searching.
 */
export function useRecordSearch(): (query: string) => void {
  // What was last posted, so a double Enter (or picking the recent search that is
  // already in the box) does not send it again.
  const lastRecorded = useRef<string | null>(null)

  return useCallback((query: string) => {
    const trimmed = query.trim()
    if (trimmed === '' || trimmed === lastRecorded.current) {
      return
    }
    lastRecorded.current = trimmed
    recordSearch(trimmed).catch(() => {
      // Silent, and forget it was tried: the next submit may well succeed.
      if (lastRecorded.current === trimmed) {
        lastRecorded.current = null
      }
    })
  }, [])
}
