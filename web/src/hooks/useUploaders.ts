import { useEffect, useMemo, useRef, useState } from 'react'

import { fetchPhotoUploaders, type PhotoListParams, type UploaderBucket } from '../services/photos'

/** Reports whether a rejection is just this effect's own abort on cleanup. */
function isAbort(err: unknown): boolean {
  return err instanceof DOMException && err.name === 'AbortError'
}

/**
 * Loads the option list behind the uploader filter: who contributed to the view
 * `params` describes, largest contribution first, each with its count.
 *
 * The counts depend on the rest of the view — in an event album the list is that
 * event's contributors, which is the whole point of the facet — so the request
 * carries the current filters and is repeated whenever they change. The
 * **uploader** scope is stripped first: it is what these counts are the options
 * for, and a facet must not narrow its own options, or picking a person would
 * leave that person as the only one on offer and the reader could never switch.
 * Trying one contributor after another therefore keeps the very same counts on
 * offer, whichever of them is currently in force.
 *
 * A failed request leaves the list empty rather than surfacing an error: a
 * filter bar that cannot offer a facet is a degraded bar, not a broken page, and
 * the grid itself reports load failures. In-flight requests are aborted when
 * `params` changes or the caller unmounts, and every response is checked against
 * the request that is current when it lands — aborting is a no-op once a
 * response is already on the wire — so a slow one cannot overwrite a newer one.
 *
 * `params` should be memoised by the caller (e.g. derived from URL state) so its
 * identity changes only when the query actually changes.
 */
export function useUploaders(params: PhotoListParams): UploaderBucket[] {
  const [uploaders, setUploaders] = useState<UploaderBucket[]>([])

  // Drop the uploader scope so picking one does not re-request — and does not
  // hollow out — the very list it was picked from.
  const facetParams = useMemo<PhotoListParams>(() => ({ ...params, uploader: '' }), [params])

  // Monotonic id of the newest request. The abort on cleanup only helps while the
  // response is still in flight; a filter change that lands after the previous
  // counts have already arrived would otherwise flash the old numbers.
  const latest = useRef(0)

  useEffect(() => {
    const requestId = latest.current + 1
    latest.current = requestId
    const controller = new AbortController()
    fetchPhotoUploaders(facetParams, controller.signal)
      .then((res) => {
        if (latest.current !== requestId) {
          return
        }
        setUploaders(res.uploaders)
      })
      .catch((err: unknown) => {
        if (isAbort(err) || latest.current !== requestId) {
          return
        }
        setUploaders([])
      })
    return () => {
      controller.abort()
    }
  }, [facetParams])

  return uploaders
}
