import { useCallback, useEffect, useRef, useState } from 'react'

import { ratePhoto, type RatingFlag } from '../services/photos'

/** State and actions for an optimistic per-photo rating (stars + flag). */
export interface UseRatingResult {
  /** The current (optimistic) star rating, 0–5. */
  rating: number
  /** The current (optimistic) pick/reject flag. */
  flag: RatingFlag
  /** True while a rating/flag request is in flight (controls should disable). */
  pending: boolean
  /** Sets the star rating optimistically, rolling back on failure. */
  setRating: (value: number) => void
  /** Sets the pick/reject flag optimistically, rolling back on failure. */
  setFlag: (value: RatingFlag) => void
}

/**
 * Drives an optimistic star-rating and pick/reject flag for one photo, mirroring
 * {@link import('./useFavorite').useFavorite}. `initialRating`/`initialFlag` are
 * the server-known values (`photo.rating` / `photo.flag`); each setter flips its
 * field immediately, calls `PUT …/rating` with just that field, and rolls that
 * field back if the request fails, so the UI never lies about the stored state.
 * Rating is a personal action allowed for every signed-in user, so no role gate
 * is applied here. Changing `uid` or the server values resyncs the optimistic
 * state.
 */
export function useRating(
  uid: string,
  initialRating: number,
  initialFlag: RatingFlag,
): UseRatingResult {
  const [rating, setRatingState] = useState(initialRating)
  const [flag, setFlagState] = useState(initialFlag)
  const [pending, setPending] = useState(false)

  const ratingRef = useRef(rating)
  ratingRef.current = rating
  const flagRef = useRef(flag)
  flagRef.current = flag
  // Count concurrent requests so `pending` clears only once all have settled
  // (rating and flag can be changed in quick succession, e.g. via hotkeys).
  const inflightRef = useRef(0)
  const mountedRef = useRef(true)

  // Resync to the server's values when the photo or its stored rating changes.
  useEffect(() => {
    setRatingState(initialRating)
    setFlagState(initialFlag)
  }, [uid, initialRating, initialFlag])

  useEffect(() => {
    mountedRef.current = true
    return () => {
      mountedRef.current = false
    }
  }, [])

  const run = useCallback((promise: Promise<void>, rollback: () => void) => {
    inflightRef.current += 1
    setPending(true)
    promise
      .catch(() => {
        // Roll back the optimistic change; the stored value never changed.
        if (mountedRef.current) {
          rollback()
        }
      })
      .finally(() => {
        inflightRef.current -= 1
        if (mountedRef.current && inflightRef.current === 0) {
          setPending(false)
        }
      })
  }, [])

  const setRating = useCallback(
    (value: number) => {
      const previous = ratingRef.current
      if (value === previous) {
        return
      }
      setRatingState(value)
      run(ratePhoto(uid, { rating: value }), () => {
        setRatingState(previous)
      })
    },
    [uid, run],
  )

  const setFlag = useCallback(
    (value: RatingFlag) => {
      const previous = flagRef.current
      if (value === previous) {
        return
      }
      setFlagState(value)
      run(ratePhoto(uid, { flag: value }), () => {
        setFlagState(previous)
      })
    },
    [uid, run],
  )

  return { rating, flag, pending, setRating, setFlag }
}
