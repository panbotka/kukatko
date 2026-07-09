import { useCallback, useEffect, useRef, useState } from 'react'

import { fetchPhoto } from '../services/photos'

/** State and error handler for an `<img>` whose source may have expired. */
export interface UseThumbSrcResult {
  /** The address to put in `<img src>`. */
  src: string
  /** True once the image has failed and no retry is left; render a placeholder. */
  failed: boolean
  /** Pass as the `<img onError>` handler. */
  onError: () => void
}

/**
 * Keeps a photo's thumbnail rendering when its address is a signed URL that has
 * expired.
 *
 * A signed media URL is short-lived by design (one hour), so a payload fetched
 * before a long idle, or held in a virtualised list, can hand an `<img>` a URL
 * the media Worker will refuse. That must not leave a permanently broken tile,
 * and it must not be papered over with a long TTL — the whole point of the short
 * one is to bound the damage from a leaked URL.
 *
 * So the first load failure refetches the photo, whose payload carries a freshly
 * signed URL, and retries with it. Exactly once: if the second address fails too,
 * or the refetch fails, or the server hands back the same address it just gave us
 * (which is what the filesystem backend does — its URLs are routes and never go
 * stale, so a failure there is a genuinely missing thumbnail), the image is
 * reported as failed and the caller renders its placeholder. A fresh `thumbUrl`
 * prop — a new page of results, a refreshed payload — resets the retry budget.
 */
export function useThumbSrc(uid: string, thumbUrl: string): UseThumbSrcResult {
  const [src, setSrc] = useState(thumbUrl)
  const [failed, setFailed] = useState(false)
  const retriedRef = useRef(false)
  const mountedRef = useRef(true)

  useEffect(() => {
    mountedRef.current = true
    return () => {
      mountedRef.current = false
    }
  }, [])

  // A new address from the server is a clean slate: show it, and allow one retry.
  useEffect(() => {
    setSrc(thumbUrl)
    setFailed(false)
    retriedRef.current = false
  }, [thumbUrl])

  const onError = useCallback(() => {
    if (retriedRef.current) {
      setFailed(true)
      return
    }
    retriedRef.current = true
    fetchPhoto(uid)
      .then((fresh) => {
        if (!mountedRef.current) {
          return
        }
        // An unchanged address would not even re-trigger a load, let alone
        // succeed: the thumbnail is missing, not stale.
        if (fresh.thumb_url === '' || fresh.thumb_url === src) {
          setFailed(true)
          return
        }
        setSrc(fresh.thumb_url)
      })
      .catch(() => {
        if (mountedRef.current) {
          setFailed(true)
        }
      })
  }, [uid, src])

  return { src, failed, onError }
}
