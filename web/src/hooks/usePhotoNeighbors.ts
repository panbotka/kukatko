import { useEffect, useState } from 'react'

import {
  fetchPhotos,
  type PhotoListParams,
  searchPhotos,
  type SearchMode,
} from '../services/photos'

import { PAGE_SIZE } from './usePaginatedPhotos'

/** The previous/next photo UIDs around the current one in the list order. */
export interface PhotoNeighbors {
  /** UID of the photo before the current one, or null at the start. */
  prev: string | null
  /** UID of the photo after the current one, or null at the end. */
  next: string | null
}

/**
 * Upper bound on pages scanned to locate the current photo and its neighbours, so
 * a deep-linked photo far down a huge list can never trigger an unbounded number
 * of requests. Beyond this the neighbours are reported as absent.
 */
const MAX_PAGES = 50

const NONE: PhotoNeighbors = { prev: null, next: null }

/**
 * Resolves the previous/next photo of `uid` within the list described by
 * `params`, so the detail page can offer prev/next navigation that respects the
 * originating list's filter and sort order. It pages through the list endpoint
 * (the same one the grid uses) accumulating UIDs until it has located `uid` and
 * its following neighbour, then stops — bounded by {@link MAX_PAGES}. In-flight
 * requests are aborted when `uid`/`params` change or on unmount.
 *
 * When `mode` is set the photo was opened from the search page, so paging goes
 * through `GET /search` (ranking `params.q`) instead of the library list — the
 * two return different orders for the same query, and prev/next must follow the
 * search order the grid showed.
 *
 * `params` should be memoised by the caller so its identity changes only when the
 * query actually changes. When `enabled` is false (e.g. no originating list) the
 * hook reports no neighbours without fetching.
 */
export function usePhotoNeighbors(
  uid: string,
  params: PhotoListParams,
  enabled = true,
  mode?: SearchMode,
): PhotoNeighbors {
  const [neighbors, setNeighbors] = useState<PhotoNeighbors>(NONE)
  const key = JSON.stringify({ params, mode })

  useEffect(() => {
    if (!enabled) {
      setNeighbors(NONE)
      return
    }
    const controller = new AbortController()
    let cancelled = false

    async function run() {
      const order: string[] = []
      let offset = 0
      for (let page = 0; page < MAX_PAGES; page++) {
        const pageParams = { ...params, limit: PAGE_SIZE, offset }
        const res =
          mode === undefined
            ? await fetchPhotos(pageParams, controller.signal)
            : await searchPhotos(pageParams, mode, controller.signal)
        for (const photo of res.photos) {
          order.push(photo.uid)
        }
        const found = order.indexOf(uid)
        // Stop once the current photo is located and its next neighbour is known,
        // or when the list is exhausted.
        if ((found !== -1 && found < order.length - 1) || res.next_offset === null) {
          break
        }
        offset = res.next_offset
      }
      if (cancelled) {
        return
      }
      const idx = order.indexOf(uid)
      setNeighbors({
        prev: idx > 0 ? order[idx - 1] : null,
        next: idx !== -1 && idx < order.length - 1 ? order[idx + 1] : null,
      })
    }

    run().catch((err: unknown) => {
      if (err instanceof DOMException && err.name === 'AbortError') {
        return
      }
      if (!cancelled) {
        setNeighbors(NONE)
      }
    })

    return () => {
      cancelled = true
      controller.abort()
    }
  }, [uid, key, enabled, params, mode])

  return neighbors
}
