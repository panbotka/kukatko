import { type ImgHTMLAttributes, type Ref, useState } from 'react'

import { ThumbUnavailable } from './ThumbUnavailable'

/** Props for {@link ThumbImage}: every native `<img>` attribute. */
export interface ThumbImageProps extends ImgHTMLAttributes<HTMLImageElement> {
  /** Forwarded to the `<img>` (a frame measurer's ref, say). */
  ref?: Ref<HTMLImageElement>
  /** Extra classes for the box the placeholder is painted in on failure. */
  fallbackClassName?: string
}

/** A pixel length from an `<img>` `width`/`height` attribute, for the box's style. */
function px(value: number | string | undefined): string | undefined {
  if (value === undefined) {
    return undefined
  }
  return typeof value === 'number' ? `${String(value)}px` : value
}

/**
 * A plain thumbnail `<img>` that gives way to the "no preview available"
 * placeholder ({@link ThumbUnavailable}) when its source fails to load, instead
 * of leaving the browser's broken-image icon and the alt text in an empty frame.
 *
 * The placeholder is drawn in a box that takes the image's place: it carries the
 * image's own classes (so a class that sized the image sizes the box), its
 * `width`/`height` attributes and `fallbackClassName`, and it is the positioned
 * box the placeholder fills (`.kk-thumb-unavailable-box`, overridable by any
 * class that positions the image itself). The failure is remembered per
 * address, so a new `src` — the next photo, a rebuilt rendition — is simply
 * tried. The caller's own `onError` still runs.
 */
export function ThumbImage({ src, onError, fallbackClassName, ...rest }: ThumbImageProps) {
  const [failedSrc, setFailedSrc] = useState<string | undefined>(undefined)
  if (src !== undefined && src === failedSrc) {
    const classes = ['kk-thumb-unavailable-box', rest.className, fallbackClassName]
      .filter(Boolean)
      .join(' ')
    return (
      <span className={classes} style={{ width: px(rest.width), height: px(rest.height) }}>
        <ThumbUnavailable />
      </span>
    )
  }
  return (
    <img
      {...rest}
      src={src}
      onError={(event) => {
        setFailedSrc(src)
        onError?.(event)
      }}
    />
  )
}
