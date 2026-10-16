import { useTranslation } from 'react-i18next'

import { Icon } from './Icon'

/** Props for {@link ThumbUnavailable}. */
export interface ThumbUnavailableProps {
  /**
   * `tile` is the library tile's own layout, which keeps its glyph and words
   * clear of the select circle and the favourite heart in the tile's corners
   * (`.kk-tile__unavailable` in `styles/tokens.css`). `fill` — the default —
   * simply centres them in whatever box it fills, dropping the words in a box too
   * small for them but never the glyph.
   */
  variant?: 'tile' | 'fill'
  /** Extra classes for the outer element. */
  className?: string
}

/**
 * The "no preview available" stand-in for a photo whose thumbnail cannot be shown:
 * a quiet image glyph and the words "Náhled není dostupný", announced as one
 * image of that name. It is what every place that draws a photo's thumbnail, face
 * crop or cover paints when the image fails to load — an original that cannot be
 * decoded answers its thumbnail with a 404 — so no reader ever meets the
 * browser's broken-image icon.
 *
 * Both variants are absolutely positioned and fill the nearest positioned box,
 * which is the media well every caller already draws the image in; out of flow,
 * they inherit no link underline from a tile that is a link.
 */
export function ThumbUnavailable({ variant = 'fill', className }: ThumbUnavailableProps) {
  const { t } = useTranslation()
  const label = t('library.tile.unavailable')
  const base = variant === 'tile' ? 'kk-tile__unavailable' : 'kk-thumb-unavailable'
  return (
    <span
      className={[base, 'text-secondary', className].filter(Boolean).join(' ')}
      role="img"
      aria-label={label}
    >
      <span className={`${base}-body`}>
        <Icon name="image" className={`${base}-icon`} />
        <span className={`${base}-text`}>{label}</span>
      </span>
    </span>
  )
}
