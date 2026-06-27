import { useCallback, useEffect, useRef, useState } from 'react'

import { favoritePhoto } from '../services/photos'

/** State and action for an optimistic per-photo favorite toggle. */
export interface UseFavoriteResult {
  /** The current (optimistic) favorite state. */
  favorite: boolean
  /** True while a toggle request is in flight (the control should be disabled). */
  pending: boolean
  /** Toggles the favorite state optimistically, rolling back on failure. */
  toggle: () => void
}

/**
 * Drives an optimistic favorite toggle for one photo. `initial` is the
 * server-known state (`photo.is_favorite`); the hook flips it immediately on
 * {@link UseFavoriteResult.toggle}, calls `PUT`/`DELETE …/favorite`, and rolls
 * back if the request fails so the UI never lies about the stored state. A
 * toggle while one is already in flight is ignored. Changing `uid` or the
 * server `initial` resyncs the optimistic state to the server's value. Favoriting
 * is a personal action allowed for every signed-in user, so no role gate is
 * applied here.
 */
export function useFavorite(uid: string, initial: boolean): UseFavoriteResult {
  const [favorite, setFavorite] = useState(initial)
  const [pending, setPending] = useState(false)

  const favoriteRef = useRef(favorite)
  favoriteRef.current = favorite
  const pendingRef = useRef(false)
  const mountedRef = useRef(true)

  // Resync to the server's value when the photo or its stored flag changes.
  useEffect(() => {
    setFavorite(initial)
  }, [uid, initial])

  useEffect(() => {
    mountedRef.current = true
    return () => {
      mountedRef.current = false
    }
  }, [])

  const toggle = useCallback(() => {
    if (pendingRef.current) {
      return
    }
    const next = !favoriteRef.current
    setFavorite(next)
    setPending(true)
    pendingRef.current = true
    favoritePhoto(uid, next)
      .catch(() => {
        // Roll back the optimistic flip; the stored state never changed.
        if (mountedRef.current) {
          setFavorite(!next)
        }
      })
      .finally(() => {
        pendingRef.current = false
        if (mountedRef.current) {
          setPending(false)
        }
      })
  }, [uid])

  return { favorite, pending, toggle }
}
