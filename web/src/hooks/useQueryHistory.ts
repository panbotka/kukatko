import { useEffect, useMemo, useRef } from 'react'
import { useLocation } from 'react-router-dom'

import { type SetUrlStateOptions } from '../lib/urlState'

/** The history options a query box writes its query with. */
export interface QueryHistory {
  /**
   * Options for a commit made while the reader is still typing (a debounced
   * pause). The first one of an edit pushes, so the view the reader started from
   * keeps its own entry; every later one replaces that same entry, so one
   * keystroke is never one entry.
   */
  typed: () => SetUrlStateOptions
  /**
   * Options for a submitted query — Enter, a picked suggestion. It lands in the
   * edit's own entry when typing already made one, otherwise it pushes; either
   * way the edit ends there, so the next typing starts a new entry.
   */
  submitted: () => SetUrlStateOptions
  /**
   * Ends the edit without writing anything: a submit whose query the URL already
   * holds (a pause committed it a moment earlier).
   */
  end: () => void
}

/** What the next location change is: the edit's own entry, the end of it, or neither. */
type Pending = 'own' | 'end' | null

/**
 * How a query box moves through browser history. "Back always works": a
 * submitted query is a new view, so Back must return to the one before it —
 * the previous query, or the unfiltered list. A box writing every debounced
 * pause with `replace` loses that view the moment the reader pauses; one
 * writing it with a push floods history with every prefix they hesitated on.
 *
 * So an edit owns exactly one history entry. Its first write pushes and the
 * entry it lands on is remembered (by `location.key`); later writes replace
 * that entry for as long as the reader is still on it. Any navigation the box
 * did not make — Back, a sort change, a removed chip — ends the edit, so typing
 * afterwards pushes a fresh entry instead of overwriting a view the reader can
 * step back to. A submit writes into the edit's entry (or pushes one) and ends
 * the edit.
 *
 * The returned object is stable, so it may sit in effect dependencies.
 */
export function useQueryHistory(): QueryHistory {
  const { key } = useLocation()
  // Read by the callbacks at call time, so their identity never changes.
  const keyRef = useRef(key)
  keyRef.current = key
  // The `location.key` of the entry the current edit owns, if any.
  const ownedRef = useRef<string | null>(null)
  // What the write just made means for the location change it causes.
  const pendingRef = useRef<Pending>(null)

  useEffect(() => {
    const pending = pendingRef.current
    pendingRef.current = null
    ownedRef.current = pending === 'own' ? key : null
  }, [key])

  return useMemo<QueryHistory>(() => {
    const owns = () => ownedRef.current !== null && ownedRef.current === keyRef.current
    return {
      typed: () => {
        const replace = owns()
        pendingRef.current = 'own'
        return { replace }
      },
      submitted: () => {
        const replace = owns()
        pendingRef.current = 'end'
        return { replace }
      },
      end: () => {
        ownedRef.current = null
        pendingRef.current = null
      },
    }
  }, [])
}
