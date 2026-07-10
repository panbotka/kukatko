import { useCallback, useMemo } from 'react'
import { useSearchParams } from 'react-router-dom'

/**
 * Shape of view state stored in the URL: a flat map of string-valued keys
 * (filters, sort, search query, page, …). Numbers are stored as strings;
 * consumers parse them as needed. This keeps URLs human-readable and the
 * convention uniform across every list/library view.
 */
export type UrlState = Record<string, string>

/**
 * Decodes view state from query params, using `defaults` both as the key set
 * (only known keys are read) and as fallbacks for absent params.
 */
export function readUrlState<T extends UrlState>(params: URLSearchParams, defaults: T): T {
  const result = { ...defaults }
  for (const key of Object.keys(defaults)) {
    const value = params.get(key)
    if (value !== null) {
      result[key as keyof T] = value as T[keyof T]
    }
  }
  return result
}

/**
 * Encodes view state into query params, omitting any value equal to its
 * default (or empty) so the URL stays minimal — `/` rather than
 * `/?sort=newest&page=1&q=`.
 */
export function writeUrlState<T extends UrlState>(state: T, defaults: T): URLSearchParams {
  const params = new URLSearchParams()
  for (const key of Object.keys(defaults)) {
    const value = state[key]
    if (value !== '' && value !== defaults[key]) {
      params.set(key, value)
    }
  }
  return params
}

/** Options controlling how a state update affects browser history. */
export interface SetUrlStateOptions {
  /** Replace the current history entry instead of pushing a new one. */
  replace?: boolean
}

/** Merges a partial update into prior state. */
export type SetUrlState<T extends UrlState> = (
  patch: Partial<T>,
  options?: SetUrlStateOptions,
) => void

/**
 * Reads and writes view state to the URL query string via react-router (which
 * drives the History API). Pushing (the default) makes Back/Forward restore
 * prior state — the project's "Zpět vždy funguje" convention; pass
 * `{ replace: true }` for updates that should not create a history entry
 * (e.g. live-typed search input).
 *
 * `defaults` MUST be stable across renders (declare it at module scope or wrap
 * it in `useMemo`) so the returned setter keeps a stable identity.
 *
 * @example
 *   const DEFAULTS = { q: '', sort: 'newest', page: '1' }
 *   const [view, setView] = useUrlState(DEFAULTS)
 *   setView({ page: '2' })          // pushes ?page=2 — Back returns to page 1
 *   setView({ q: 'cat' }, { replace: true })
 */
export function useUrlState<T extends UrlState>(defaults: T): [T, SetUrlState<T>] {
  const [searchParams, setSearchParams] = useSearchParams()

  const state = useMemo(() => readUrlState(searchParams, defaults), [searchParams, defaults])

  const setState = useCallback<SetUrlState<T>>(
    (patch, options) => {
      const next = { ...state, ...patch }
      setSearchParams(writeUrlState(next, defaults), { replace: options?.replace ?? false })
    },
    [state, defaults, setSearchParams],
  )

  return [state, setState]
}
