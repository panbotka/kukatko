import { type MouseEvent } from 'react'
import { useTranslation } from 'react-i18next'

/** Props for {@link RatingStars}. */
export interface RatingStarsProps {
  /** The current star rating, 0–5. */
  rating: number
  /**
   * Called with the new rating when a star is clicked. Clicking the star that
   * equals the current rating clears it to 0. Omit for a read-only display.
   */
  onRate?: (value: number) => void
  /** Disables the buttons while a request is in flight. */
  disabled?: boolean
  /** Star glyph size in pixels. Defaults to 18. */
  size?: number
  /** Extra classes on the wrapping group (e.g. positioning). */
  className?: string
}

/** A filled or outlined star glyph. */
function StarIcon({ filled, size }: { filled: boolean; size: number }) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 16 16"
      fill={filled ? 'currentColor' : 'none'}
      stroke="currentColor"
      strokeWidth="1.2"
      aria-hidden="true"
      focusable="false"
      className="d-block"
    >
      <path d="M8 1.5l1.9 3.9 4.3.6-3.1 3 .7 4.3L8 11.8 4.2 13.3l.7-4.3-3.1-3 4.3-.6z" />
    </svg>
  )
}

/**
 * A row of five clickable stars for a 0–5 rating. Filled stars show the current
 * value; clicking a star sets that rating, and clicking the current rating again
 * clears it to 0. When `onRate` is omitted the stars render read-only (no
 * buttons), so the same component doubles as a compact display. It is purely
 * controlled — the optimistic state lives in {@link import('../../hooks/useRating').useRating}.
 */
export function RatingStars({
  rating,
  onRate,
  disabled = false,
  size = 18,
  className,
}: RatingStarsProps) {
  const { t } = useTranslation()
  const stars = [1, 2, 3, 4, 5]

  if (onRate === undefined) {
    return (
      <span
        className={`d-inline-flex align-items-center ${className ?? ''}`}
        role="img"
        aria-label={t('rating.value', { n: rating })}
      >
        {stars.map((value) => (
          <span key={value} className={value <= rating ? 'text-warning' : 'text-secondary'}>
            <StarIcon filled={value <= rating} size={size} />
          </span>
        ))}
      </span>
    )
  }

  const handleClick = (value: number) => (event: MouseEvent<HTMLButtonElement>) => {
    // Sibling of a tile link/button: never navigate or toggle selection.
    event.preventDefault()
    event.stopPropagation()
    onRate(value === rating ? 0 : value)
  }

  return (
    <span
      className={`d-inline-flex align-items-center ${className ?? ''}`}
      role="group"
      aria-label={t('rating.label')}
    >
      {stars.map((value) => (
        <button
          key={value}
          type="button"
          aria-pressed={value <= rating}
          aria-label={t('rating.rate', { n: value })}
          title={t('rating.rate', { n: value })}
          disabled={disabled}
          onClick={handleClick(value)}
          className={`btn btn-sm p-0 border-0 lh-1 bg-transparent d-inline-flex ${
            value <= rating ? 'text-warning' : 'text-secondary'
          }`}
        >
          <StarIcon filled={value <= rating} size={size} />
        </button>
      ))}
    </span>
  )
}
