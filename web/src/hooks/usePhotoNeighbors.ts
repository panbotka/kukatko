import { useEffect, useRef, useState } from 'react'

import {
  fetchPhotos,
  type PhotoListParams,
  searchPhotos,
  type SearchMode,
} from '../services/photos'

import { PAGE_SIZE } from './usePaginatedPhotos'
import { useSearchMode } from './useSearchMode'

/**
 * A neighbouring photo, as much of it as paging needs to know: where to go, and
 * what kind of media waits there. The kind is carried because the viewer warms
 * the neighbours' images ahead of time and a video is not worth warming (see
 * `lib/viewerPreload`).
 */
export interface NeighborPhoto {
  /** The photo's UID. */
  uid: string
  /** Media kind (`image`, `video`, `live`); absent counts as an image. */
  mediaType?: string
}

/** The previous/next photos around the current one in the list order. */
export interface PhotoNeighbors {
  /** The photo before the current one, or null at the start. */
  prev: NeighborPhoto | null
  /** The photo after the current one, or null at the end. */
  next: NeighborPhoto | null
  /**
   * True while the answer for the current photo is still being worked out, so a
   * caller can tell "there is no next photo" (a list end — the key is a no-op)
   * from "we do not know yet" (a press worth remembering). Locating a deep-linked
   * photo takes a page walk of a second or more, and a shortcut that silently did
   * nothing in that window read as a broken key.
   */
  pending: boolean
}

/**
 * Upper bound on pages scanned to locate the current photo and its neighbours, so
 * a deep-linked photo far down a huge list can never trigger an unbounded number
 * of requests. Beyond this the neighbours are reported as absent.
 */
const MAX_PAGES = 50

const NONE: PhotoNeighbors = { prev: null, next: null, pending: false }
const PENDING: PhotoNeighbors = { prev: null, next: null, pending: true }

/**
 * The stretch of list order a scan has already seen, kept across renders so
 * stepping through photos does not re-walk the list from the top for every photo.
 */
interface ScannedOrder {
  /** Identity of the list (params + search mode) the order was scanned from. */
  key: string
  /** The photos in list order, starting at the list's first page. */
  photos: NeighborPhoto[]
  /** Offset the next page starts at, or null once the list was exhausted. */
  nextOffset: number | null
}

/**
 * Answers `uid`'s neighbours from an already scanned stretch of the list, or null
 * when the scan cannot settle the question — a different list, a photo it never
 * reached, or a photo sitting at the end of what was scanned while more pages
 * remain (its next neighbour is one page further on). A pure function, so the
 * fast path is trivially testable.
 */
function neighborsIn(order: ScannedOrder | null, key: string, uid: string): PhotoNeighbors | null {
  if (order?.key !== key) {
    return null
  }
  const idx = order.photos.findIndex((photo) => photo.uid === uid)
  if (idx === -1) {
    return null
  }
  const last = idx === order.photos.length - 1
  if (last && order.nextOffset !== null) {
    return null
  }
  return {
    prev: idx > 0 ? order.photos[idx - 1] : null,
    next: last ? null : order.photos[idx + 1],
    pending: false,
  }
}

/**
 * Resolves the previous/next photo of `uid` within the list described by
 * `params`, so the detail page can offer prev/next navigation that respects the
 * originating list's filter and sort order. It pages through the list endpoint
 * (the same one the grid uses) accumulating UIDs until it has located `uid` and
 * its following neighbour, then stops — bounded by {@link MAX_PAGES}. In-flight
 * requests are aborted when `uid`/`params` change or on unmount.
 *
 * The scanned order is kept between renders, so stepping to a neighbour is
 * answered from it without a request; only a photo the scan never reached costs
 * a walk, and one that sits at the scanned tail resumes from where the last scan
 * stopped instead of starting over. That matters twice: the arrows stay live
 * while paging, and they never point at a stale pair belonging to the photo
 * before — a "next" that is the photo already on screen is a key that does
 * nothing.
 *
 * When `mode` is set the photo was opened from the search page, so paging goes
 * through `GET /search` (ranking `params.q`) instead of the library list — the
 * two return different orders for the same query, and prev/next must follow the
 * search order the grid showed. A semantic/hybrid `mode` is downgraded to
 * full-text while the embeddings sidecar is unreachable, exactly as the grid's
 * own search is: it keeps a shared or bookmarked search link from spending the
 * sidecar timeout on neighbours, and matches the order that grid would show.
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
  const [neighbors, setNeighbors] = useState<PhotoNeighbors>(enabled ? PENDING : NONE)
  // A hook cannot be called conditionally, so resolve with a stand-in when there
  // is no search mode at all and drop the result again below.
  const { mode: resolved } = useSearchMode(mode ?? 'fulltext')
  const searchMode = mode === undefined ? undefined : resolved
  const key = JSON.stringify({ params, mode: searchMode })
  const scanned = useRef<ScannedOrder | null>(null)

  useEffect(() => {
    if (!enabled) {
      setNeighbors(NONE)
      return
    }
    // Stepping within a list already walked: answer from it, no request and no
    // window in which the arrows are unknown.
    const known = neighborsIn(scanned.current, key, uid)
    if (known !== null) {
      setNeighbors(known)
      return
    }
    // Anything the previous photo's scan concluded is about that photo, not this
    // one; say so rather than leaving its pair on screen.
    setNeighbors(PENDING)
    const controller = new AbortController()
    let cancelled = false

    async function run() {
      // A photo at the end of what was scanned is one page short of an answer:
      // carry that stretch over and resume, instead of re-walking it.
      const previous = scanned.current
      const carry =
        previous !== null &&
        previous.key === key &&
        previous.nextOffset !== null &&
        previous.photos.some((photo) => photo.uid === uid)
          ? { photos: previous.photos, from: previous.nextOffset }
          : null
      const order: NeighborPhoto[] = carry === null ? [] : [...carry.photos]
      let offset: number | null = carry?.from ?? 0
      for (let page = 0; page < MAX_PAGES; page++) {
        if (offset === null) {
          break
        }
        const pageParams: PhotoListParams = { ...params, limit: PAGE_SIZE, offset }
        const res =
          searchMode === undefined
            ? await fetchPhotos(pageParams, controller.signal)
            : await searchPhotos(pageParams, searchMode, controller.signal)
        for (const photo of res.photos) {
          order.push({ uid: photo.uid, mediaType: photo.media_type })
        }
        offset = res.next_offset
        const found = order.findIndex((photo) => photo.uid === uid)
        // Stop once the current photo is located and its next neighbour is known,
        // or when the list is exhausted.
        if (found !== -1 && found < order.length - 1) {
          break
        }
      }
      if (cancelled) {
        return
      }
      scanned.current = { key, photos: order, nextOffset: offset }
      const idx = order.findIndex((photo) => photo.uid === uid)
      setNeighbors({
        prev: idx > 0 ? order[idx - 1] : null,
        next: idx !== -1 && idx < order.length - 1 ? order[idx + 1] : null,
        pending: false,
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
  }, [uid, key, enabled, params, searchMode])

  return neighbors
}
