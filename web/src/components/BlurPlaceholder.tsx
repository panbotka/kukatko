import { type CSSProperties } from 'react'

import { blurPlaceholderUrl } from '../lib/blurPlaceholder'

/** Props for {@link BlurPlaceholder}. */
export interface BlurPlaceholderProps {
  /**
   * The photo's BlurHash (`Photo.blurhash`). Absent — or malformed — renders
   * nothing at all, which leaves the caller's own neutral surface showing; a
   * photo catalogued before placeholders existed simply has none.
   */
  hash?: string
  /** Extra utility classes. */
  className?: string
  /** Inline style overrides. Merged after the decoded background. */
  style?: CSSProperties
}

/**
 * The blurred stand-in for a photograph: its BlurHash decoded into a 32-pixel
 * image and stretched across the caller's box, so a tile is the colours of its
 * photograph from the first frame instead of an empty grey well.
 *
 * It fills its parent absolutely (`inset: 0`) and holds no space of its own, so
 * it can never move the layout — the caller's box is sized before any image
 * arrives, and the placeholder merely paints inside it. Decorative
 * (`aria-hidden`): the real image alongside it carries the alt text.
 *
 * Render it as a **sibling before** the image it stands in for: both are static,
 * so the image paints on top and covers the blur as it fades in. The parent must
 * be positioned — the tile's media well and the viewer's figure both are.
 */
export function BlurPlaceholder({ hash, className, style }: BlurPlaceholderProps) {
  const url = blurPlaceholderUrl(hash)
  if (url === undefined) {
    return null
  }
  return (
    <span
      aria-hidden="true"
      className={['kk-media-blur', className].filter(Boolean).join(' ')}
      style={{ backgroundImage: `url("${url}")`, ...style }}
    />
  )
}
