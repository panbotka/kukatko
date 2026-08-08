import { type ImgHTMLAttributes, useCallback, useState } from 'react'

import { Skeleton } from './Skeleton'

/** Props for {@link FadeInImage}: every native `<img>` attribute bar the load
 * handler this component owns. */
export interface FadeInImageProps extends Omit<ImgHTMLAttributes<HTMLImageElement>, 'onLoad'> {
  /**
   * Draw a shimmering {@link Skeleton} behind the image until it has decoded,
   * instead of leaving the caller's empty well showing. Pass it where a whole
   * grid fills in over seconds — the people index, whose face crops are cut from
   * previews measured in megapixels — so the page reads as loading rather than
   * as broken. The caller's box must be positioned (`position: relative`), which
   * the tile media well already is; the placeholder fills it absolutely and
   * inherits its corners.
   */
  skeleton?: boolean
}

/**
 * A thumbnail / photo `<img>` that fades and subtly settles in once the browser
 * has decoded it, rather than snapping into place. It leans on the token-driven
 * `.kk-media-img` entrance (which collapses to an instant swap under
 * `prefers-reduced-motion`) and holds no space of its own — callers give it a
 * fixed box with a placeholder surface (the sunken thumbnail well) so the layout
 * never shifts as images stream in.
 *
 * Defaults to `loading="lazy"` and `decoding="async"`, both overridable. Every
 * other attribute — `src`, `alt`, `style`, `onError`, the caller's own
 * `className` — passes straight through, so a caller keeps full control of the
 * image and its failure handling.
 */
export function FadeInImage({
  className,
  loading = 'lazy',
  decoding = 'async',
  skeleton = false,
  onError,
  ...rest
}: FadeInImageProps) {
  const [loaded, setLoaded] = useState(false)
  // A source that will never arrive must stop the shimmer: a placeholder that
  // pulses forever says "still loading" about an image that has already given up.
  // The caller's own handler still runs — `OutlierCard` steps down the thumbnail
  // ladder from here — and a retry that succeeds fires `onLoad` regardless.
  const [failed, setFailed] = useState(false)

  // A cached image can finish decoding before React attaches `onLoad`, so its
  // load event never fires and the fade would stick at zero opacity. Catch that
  // when the node mounts: an already-complete image with real pixels is revealed
  // at once.
  const measure = useCallback((node: HTMLImageElement | null) => {
    if (node?.complete === true && node.naturalWidth > 0) {
      setLoaded(true)
    }
  }, [])

  const classes = ['kk-media-img', loaded ? 'is-loaded' : '', className].filter(Boolean).join(' ')

  return (
    <>
      {skeleton && !loaded && !failed && (
        <Skeleton
          className="position-absolute top-0 start-0 w-100 h-100"
          style={{ borderRadius: 'inherit' }}
        />
      )}
      <img
        {...rest}
        ref={measure}
        loading={loading}
        decoding={decoding}
        onLoad={() => {
          setLoaded(true)
        }}
        onError={(event) => {
          setFailed(true)
          onError?.(event)
        }}
        className={classes}
      />
    </>
  )
}
