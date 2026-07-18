import { type ImgHTMLAttributes, useCallback, useState } from 'react'

/** Props for {@link FadeInImage}: every native `<img>` attribute bar the load
 * handler this component owns. */
export type FadeInImageProps = Omit<ImgHTMLAttributes<HTMLImageElement>, 'onLoad'>

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
  ...rest
}: FadeInImageProps) {
  const [loaded, setLoaded] = useState(false)

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
    <img
      {...rest}
      ref={measure}
      loading={loading}
      decoding={decoding}
      onLoad={() => {
        setLoaded(true)
      }}
      className={classes}
    />
  )
}
