import { useCallback, useEffect, useRef, useState } from 'react'

/** The load state of a preloaded image. */
export type ImageStatus = 'pending' | 'ready' | 'error'

/** Result of {@link useImagePreloader}. */
export interface ImagePreloader {
  /**
   * The state of `url`: `ready` once the image is decoded and can be painted
   * without a hitch, `error` when it will never load, `pending` otherwise —
   * including for a URL that is not, or no longer, in the preload window.
   */
  statusOf: (url: string) => ImageStatus
  /**
   * Preloads exactly `urls` and releases every image outside that set. Cheap to
   * call on every cursor move: URLs already in the window keep their element,
   * their download and their status.
   */
  prime: (urls: readonly string[]) => void
}

/** Detaches an image from its download so the bytes and bitmap can be collected. */
function release(img: HTMLImageElement): void {
  img.onload = null
  img.onerror = null
  // Removing `src` re-runs the spec's "update the image data" algorithm, which
  // aborts an in-flight fetch. Unlike `src = ''` it requests nothing new.
  img.removeAttribute('src')
}

/**
 * Starts loading `url` into `img` and reports the outcome exactly once.
 *
 * `decode()` is what makes the result usable: it resolves only when the bitmap
 * is ready to paint, whereas `onload` fires as soon as the bytes have arrived
 * and still leaves a decode — the very stall this preloader exists to hide — on
 * the frame that first shows the image. It is feature-detected because jsdom
 * and older browsers do not implement it.
 */
function startLoad(
  img: HTMLImageElement,
  url: string,
  settle: (status: 'ready' | 'error') => void,
): void {
  img.decoding = 'async'
  img.src = url
  if (typeof img.decode === 'function') {
    void img.decode().then(
      () => {
        settle('ready')
      },
      () => {
        settle('error')
      },
    )
    return
  }
  img.onload = () => {
    settle('ready')
  }
  img.onerror = () => {
    settle('error')
  }
}

/**
 * Keeps a bounded window of images downloaded and decoded ahead of time, and
 * reports when each of them is ready to paint.
 *
 * The caller drives it imperatively with {@link ImagePreloader.prime}: the URLs
 * it passes are the whole window, so anything outside is released right there.
 * That bound is what keeps a long slideshow from accumulating every decoded
 * frame it has ever shown; the last window is released on unmount. A late
 * `decode()` for an already-released image is ignored rather than resurrected.
 *
 * Statuses live in state, so `statusOf` gets a fresh identity whenever any image
 * settles — a caller can depend on it to react the instant an image is ready.
 */
export function useImagePreloader(): ImagePreloader {
  const imagesRef = useRef<Map<string, HTMLImageElement>>(new Map())
  const mountedRef = useRef(true)
  const [statuses, setStatuses] = useState<ReadonlyMap<string, ImageStatus>>(() => new Map())

  useEffect(() => {
    mountedRef.current = true
    const images = imagesRef.current
    return () => {
      mountedRef.current = false
      for (const img of images.values()) {
        release(img)
      }
      images.clear()
    }
  }, [])

  const prime = useCallback((urls: readonly string[]) => {
    const images = imagesRef.current
    const wanted = new Set(urls)

    let released = false
    for (const [url, img] of images) {
      if (!wanted.has(url)) {
        release(img)
        images.delete(url)
        released = true
      }
    }
    const added = [...wanted].filter((url) => !images.has(url))
    if (!released && added.length === 0) {
      return
    }

    // Publish the new window before any load can settle, so a status only ever
    // lands on a URL the window still wants.
    setStatuses((prev) => {
      const next = new Map<string, ImageStatus>()
      for (const url of wanted) {
        next.set(url, prev.get(url) ?? 'pending')
      }
      return next
    })

    for (const url of added) {
      const img = new Image()
      images.set(url, img)
      startLoad(img, url, (status) => {
        if (!mountedRef.current || imagesRef.current.get(url) !== img) {
          return
        }
        setStatuses((prev) => {
          if (!prev.has(url) || prev.get(url) === status) {
            return prev
          }
          const next = new Map(prev)
          next.set(url, status)
          return next
        })
      })
    }
  }, [])

  const statusOf = useCallback(
    (url: string): ImageStatus => statuses.get(url) ?? 'pending',
    [statuses],
  )

  return { statusOf, prime }
}
