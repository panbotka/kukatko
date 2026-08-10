import { useEffect, useState } from 'react'

import { fetchStoryboard, type Storyboard } from '../services/photos'

/**
 * How long to wait before asking again while a sprite is still being rendered.
 * Generation is one ffmpeg pass over the clip, so a short clip is ready within a
 * few seconds and a long one is not worth hurrying.
 */
const POLL_INTERVAL_MS = 5000

/**
 * How many times to re-ask before giving up. Four polls cover roughly twenty
 * seconds — long enough for the clips people actually watch, short enough that a
 * queue backed up behind an import does not leave a tab asking forever. Giving up
 * costs nothing: the sprite arrives on the next playback.
 */
const MAX_POLLS = 4

/**
 * Loads a video's scrub-preview storyboard, lazily and quietly.
 *
 * Nothing is requested until `enabled` turns true — the player switches it on
 * when playback starts — because the request is what schedules the render, and a
 * grid of videos that all asked on mount would enqueue a full decode each. Once
 * asked, a `pending` answer is re-polled a bounded number of times; `ready` and
 * `unavailable` are final and stop the loop.
 *
 * Every failure is swallowed and the state stays null. A missing preview is a
 * player without a preview, never an error on screen — which is exactly the
 * graceful degradation a video with no storyboard needs.
 *
 * @param uid The photo whose storyboard is wanted.
 * @param enabled Whether to ask at all (the player passes "has playback started").
 * @returns The ready storyboard, or null while pending, unavailable or failed.
 */
export function useStoryboard(uid: string, enabled: boolean): Storyboard | null {
  const [storyboard, setStoryboard] = useState<Storyboard | null>(null)
  // Drop the grid the moment the player moves to another photo — React's
  // "adjust state while rendering" pattern, so no stale sprite is ever used to
  // place previews over the new clip, not even for one frame.
  const [loadedFor, setLoadedFor] = useState(uid)
  if (loadedFor !== uid) {
    setLoadedFor(uid)
    setStoryboard(null)
  }

  useEffect(() => {
    if (!enabled || uid === '') {
      return undefined
    }
    const controller = new AbortController()
    let timer: ReturnType<typeof setTimeout> | undefined
    let polls = 0

    const ask = (): void => {
      fetchStoryboard(uid, controller.signal)
        .then((next) => {
          if (controller.signal.aborted) {
            return
          }
          if (next.status === 'ready') {
            setStoryboard(next)
            return
          }
          if (next.status === 'pending' && polls < MAX_POLLS) {
            polls += 1
            timer = setTimeout(ask, POLL_INTERVAL_MS)
          }
        })
        .catch(() => {
          // Silent: a video without scrub previews is still a working player.
        })
    }
    ask()

    return () => {
      controller.abort()
      if (timer !== undefined) {
        clearTimeout(timer)
      }
    }
  }, [uid, enabled])

  return storyboard
}
