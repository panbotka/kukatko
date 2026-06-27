import { useCallback, useEffect, useRef, useState } from 'react'

import { fetchMapPhotos, type MapFeature, type MapPhotoParams } from '../services/map'

/** Lifecycle of a map photo fetch. */
export type MapPhotosStatus = 'loading' | 'ready' | 'error'

/** State returned by {@link useMapPhotos}. */
export interface MapPhotosState {
  features: MapFeature[]
  status: MapPhotosStatus
  /** Re-runs the fetch with the current params (e.g. after an error). */
  retry: () => void
}

/**
 * Loads the geotagged-photo GeoJSON feed for the given filters, refetching
 * whenever the params change. The whole map's markers come back in one request
 * (the backend caps and forces has-GPS), so there is no pagination. In-flight
 * requests are aborted on change/unmount and stale responses are ignored, so
 * rapid filter changes never race.
 *
 * `params` should be memoised by the caller so the effect only re-runs when the
 * filters actually change.
 */
export function useMapPhotos(params: MapPhotoParams): MapPhotosState {
  const [features, setFeatures] = useState<MapFeature[]>([])
  const [status, setStatus] = useState<MapPhotosStatus>('loading')
  // Bumping this forces the load effect to re-run for a manual retry.
  const [reloadToken, setReloadToken] = useState(0)
  const latestRequest = useRef(0)

  useEffect(() => {
    const requestId = latestRequest.current + 1
    latestRequest.current = requestId
    const controller = new AbortController()
    setStatus('loading')

    fetchMapPhotos(params, controller.signal)
      .then((collection) => {
        if (latestRequest.current !== requestId) {
          return
        }
        setFeatures(collection.features)
        setStatus('ready')
      })
      .catch((err: unknown) => {
        if (controller.signal.aborted || latestRequest.current !== requestId) {
          return
        }
        if (err instanceof DOMException && err.name === 'AbortError') {
          return
        }
        setStatus('error')
      })

    return () => {
      controller.abort()
    }
  }, [params, reloadToken])

  const retry = useCallback(() => {
    setReloadToken((token) => token + 1)
  }, [])

  return { features, status, retry }
}
