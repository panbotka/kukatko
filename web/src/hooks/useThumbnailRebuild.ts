import { useEffect, useState } from 'react'

import { REBUILD_POLL_DELAYS_MS, thumbnailRebuiltSince } from '../lib/renditionRebuild'
import { fetchPhoto, type PhotoDetail } from '../services/photos'

/** What to wait for: the photo whose edit was saved, and the save's own server stamp. */
export interface RebuildWatch {
  uid: string
  /** The saved edit's `updated_at`; the rebuild has landed once the thumbnail step ran at or after it. */
  since: string
}

/** The answer to a {@link RebuildWatch}: the detail that first reported the rebuild. */
export interface RebuildResult {
  /** The very watch this answers, so a caller can tell it from a superseded one's. */
  watch: RebuildWatch
  photo: PhotoDetail
}

/**
 * Waits for the thumbnail rebuild a saved edit enqueues, by polling the photo's
 * detail until its processing report says the thumbnails were built at or after
 * the save (`thumbnailRebuiltSince`).
 *
 * The first poll is immediate — an idle worker is often done before the panel's
 * button has stopped glowing — and the rest follow `REBUILD_POLL_DELAYS_MS`; when
 * those run out the watch gives up silently, and a failed poll simply waits for
 * the next one. The caller keeps its delta preview throughout, so giving up
 * costs nothing but the automatic refresh. A new watch (another save, another
 * photo) replaces the previous one outright: only the latest save's rendition is
 * worth waiting for, and its answer is the only one reported. The reported
 * result carries the watch it answers, so a caller comparing it to the watch it
 * holds never acts on a stale one.
 */
export function useThumbnailRebuild(watch: RebuildWatch | null): RebuildResult | null {
  const [result, setResult] = useState<RebuildResult | null>(null)

  useEffect(() => {
    if (watch === null) {
      return
    }
    let active = true
    let timer: number | null = null
    let attempt = 0
    const controller = new AbortController()

    const poll = () => {
      void fetchPhoto(watch.uid, controller.signal)
        .then((photo) => {
          if (!active) {
            return
          }
          if (thumbnailRebuiltSince(photo, watch.since)) {
            setResult({ watch, photo })
            return
          }
          schedule()
        })
        .catch(() => {
          // A failed poll is not a failed rebuild: wait for the next one.
          if (active) {
            schedule()
          }
        })
    }

    const schedule = () => {
      if (attempt >= REBUILD_POLL_DELAYS_MS.length) {
        return
      }
      timer = window.setTimeout(poll, REBUILD_POLL_DELAYS_MS[attempt])
      attempt += 1
    }

    poll()
    return () => {
      active = false
      controller.abort()
      if (timer !== null) {
        window.clearTimeout(timer)
      }
    }
  }, [watch])

  return result
}
