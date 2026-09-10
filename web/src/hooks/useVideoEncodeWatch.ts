import { useEffect, useState } from 'react'

import { ENCODE_POLL_INTERVAL_MS, encodeInProgress, videoEncode } from '../lib/videoEncode'
import { fetchPhoto, type PhotoDetail } from '../services/photos'

/**
 * Waits for the streaming encode of the clip in the viewer, by re-fetching its
 * detail every {@link ENCODE_POLL_INTERVAL_MS} until the encode has settled —
 * and reports the detail that first said so, which is what lets the player swap
 * the original file for the streaming rendition without a page reload.
 *
 * Pass `null` for "nothing to wait for" and nothing is requested at all: the
 * caller decides with `shouldWatchEncode`, so a still photo, an already-encoded
 * clip and an instance with streaming switched off cost exactly zero requests.
 * The watch also stops the moment the encode reaches a terminal state (the
 * rendition landed, the encode failed, the step turned out not to apply) and
 * whenever the tab is hidden — a viewer left open in a background tab is not
 * watching anything, and resumes on its own when it comes back to the front.
 *
 * A failed poll is not a failed encode: the next tick simply asks again. The
 * result carries the whole detail, so the caller replaces the photo it holds
 * with the server's own truth rather than patching one flag into it.
 *
 * @param uid The clip to watch, or `null` to watch nothing.
 * @returns The detail that first reported the encode settled, else `null`.
 */
export function useVideoEncodeWatch(uid: string | null): PhotoDetail | null {
  const [settled, setSettled] = useState<PhotoDetail | null>(null)

  // Another clip is another watch: the previous one's answer describes a photo
  // that is no longer on screen.
  const [watched, setWatched] = useState(uid)
  if (watched !== uid) {
    setWatched(uid)
    setSettled(null)
  }

  useEffect(() => {
    if (uid === null) {
      return undefined
    }
    // Bound once, so the narrowing above survives into the closures below (a
    // parameter's is not carried into a nested function).
    const target = uid
    let active = true
    let timer: number | null = null
    let inFlight = false
    const controller = new AbortController()

    const stop = (): void => {
      if (timer !== null) {
        window.clearTimeout(timer)
        timer = null
      }
    }

    const schedule = (): void => {
      if (!active || document.visibilityState === 'hidden') {
        return
      }
      stop()
      timer = window.setTimeout(poll, ENCODE_POLL_INTERVAL_MS)
    }

    function poll(): void {
      if (!active || inFlight) {
        return
      }
      timer = null
      inFlight = true
      void fetchPhoto(target, controller.signal)
        .then((photo) => {
          inFlight = false
          if (!active) {
            return
          }
          if (encodeInProgress(videoEncode(photo))) {
            schedule()
            return
          }
          setSettled(photo)
        })
        .catch(() => {
          // A failed poll is not a failed encode: ask again on the next tick.
          inFlight = false
          schedule()
        })
    }

    // A hidden tab is nobody watching: the poll stands down, and the first tick
    // after the tab comes back is immediate, so a rendition that landed in the
    // meantime is on screen the moment the reader is.
    const visibility = (): void => {
      if (document.visibilityState === 'hidden') {
        stop()
        return
      }
      if (timer === null) {
        poll()
      }
    }

    document.addEventListener('visibilitychange', visibility)
    schedule()
    return () => {
      active = false
      stop()
      controller.abort()
      document.removeEventListener('visibilitychange', visibility)
    }
  }, [uid])

  return settled
}
