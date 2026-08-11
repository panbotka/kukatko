import { type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import './review.css'

/** Props for {@link EnlargeButton}. */
export interface EnlargeButtonProps {
  /** Opens the lightbox on the photo this button wraps. */
  onEnlarge: () => void
  /**
   * Overrides the accessible name. The default („Enlarge the photo") is right
   * on a grid of photos; a card that shows several of them can name which one.
   */
  label?: string
  /** Extra classes for the wrapper (layout only — the reset is in the CSS). */
  className?: string
  /** The photo (or crop) the click enlarges. */
  children: ReactNode
}

/**
 * The review tools' one gesture for „let me look properly": a photo in a grid,
 * wrapped in a real button that opens it in the lightbox.
 *
 * A button rather than a link, deliberately. Enlarging happens on this page and
 * produces no URL worth copying, while leaving *for* the photo's own page is the
 * corner anchor's job on the stage inside the overlay — one meaning per control,
 * which is the rule the whole shared UX rests on (see `ReviewStage`).
 *
 * The element is stripped back to nothing (no border, no padding, no button
 * background) so the picture stays exactly what the card drew, and carries only
 * a zoom cursor plus the app's ordinary focus ring — the wrapper must be
 * reachable by keyboard, since it is the only way into the overlay without a
 * mouse.
 */
export function EnlargeButton({ onEnlarge, label, className, children }: EnlargeButtonProps) {
  const { t } = useTranslation()
  const name = label ?? t('review.card.enlarge')

  return (
    <button
      type="button"
      className={`review-enlarge${className === undefined ? '' : ` ${className}`}`}
      onClick={onEnlarge}
      aria-label={name}
      title={name}
      data-testid="review-enlarge"
    >
      {children}
    </button>
  )
}
