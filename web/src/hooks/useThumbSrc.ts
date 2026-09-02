import { useCallback, useEffect, useRef, useState } from 'react'

import { fetchPhoto, type Photo } from '../services/photos'

/** The address a refreshed payload is re-read from by default: the square crop. */
function defaultPick(photo: Photo): string {
  return photo.thumb_url
}

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
 *
 * Which address the refreshed payload is re-read from is the caller's to say
 * (`pick`), because a photo now carries more than one: the square crop every
 * medallion draws and the aspect-preserving rendition the justified wall does. A
 * caller showing a rendition the payload does not carry at all — a bigger rung
 * fetched by route, which never expires — passes a picker returning the empty
 * string, and a failure there is simply a failure.
 */
export function useThumbSrc(
  uid: string,
  thumbUrl: string,
  pick: (photo: Photo) => string = defaultPick,
): UseThumbSrcResult {
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

  // The picker is read through a ref so a caller may pass an inline arrow
  // without re-arming the retry on every render.
  const pickRef = useRef(pick)
  pickRef.current = pick

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
        const next = pickRef.current(fresh)
        // An unchanged address would not even re-trigger a load, let alone
        // succeed: the thumbnail is missing, not stale.
        if (next === '' || next === src) {
          setFailed(true)
          return
        }
        setSrc(next)
      })
      .catch(() => {
        if (mountedRef.current) {
          setFailed(true)
        }
      })
  }, [uid, src])

  return { src, failed, onError }
}
