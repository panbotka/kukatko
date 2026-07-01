import { type MouseEvent } from 'react'
import { useTranslation } from 'react-i18next'

import { type RatingFlag } from '../../services/photos'

/** Props for {@link FlagControl}. */
export interface FlagControlProps {
  /** The current pick/reject flag. */
  flag: RatingFlag
  /**
   * Called with the new flag when a button is clicked. Clicking the active flag
   * again clears it back to `'none'`. Omit for a read-only display.
   */
  onFlag?: (value: RatingFlag) => void
  /** Disables the buttons while a request is in flight. */
  disabled?: boolean
  /** Glyph size in pixels. Defaults to 16. */
  size?: number
  /** Extra classes on the wrapping group. */
  className?: string
}

/** A pick (check) or reject (slashed circle) glyph. */
function FlagIcon({ kind, size }: { kind: 'pick' | 'reject'; size: number }) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 16 16"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.6"
      strokeLinecap="round"
      aria-hidden="true"
      focusable="false"
      className="d-block"
    >
      {kind === 'pick' ? (
        <path d="M3.5 8.5l3 3 6-7" />
      ) : (
        <>
          <circle cx="8" cy="8" r="5.5" />
          <path d="M4.5 4.5l7 7" />
        </>
      )}
    </svg>
  )
}

/**
 * Two toggle buttons for the per-user pick/reject flag. The active flag is
 * highlighted; clicking it again clears the flag to `'none'` (the "clear"
 * affordance). When `onFlag` is omitted the control renders read-only. Purely
 * controlled — optimistic state lives in
 * {@link import('../../hooks/useRating').useRating}.
 */
export function FlagControl({
  flag,
  onFlag,
  disabled = false,
  size = 16,
  className,
}: FlagControlProps) {
  const { t } = useTranslation()

  const toggle = (value: 'pick' | 'reject') => (event: MouseEvent<HTMLButtonElement>) => {
    // Sibling of a tile link/button: never navigate or toggle selection.
    event.preventDefault()
    event.stopPropagation()
    onFlag?.(flag === value ? 'none' : value)
  }

  const buttonClass = (value: 'pick' | 'reject', activeClass: string) =>
    `btn btn-sm p-1 lh-1 border-0 bg-transparent d-inline-flex ${
      flag === value ? activeClass : 'text-secondary'
    }`

  return (
    <span
      className={`d-inline-flex align-items-center gap-1 ${className ?? ''}`}
      role="group"
      aria-label={t('rating.flag')}
    >
      <button
        type="button"
        aria-pressed={flag === 'pick'}
        aria-label={t('rating.pick')}
        title={t('rating.pick')}
        disabled={disabled || onFlag === undefined}
        onClick={toggle('pick')}
        className={buttonClass('pick', 'text-success')}
      >
        <FlagIcon kind="pick" size={size} />
      </button>
      <button
        type="button"
        aria-pressed={flag === 'reject'}
        aria-label={t('rating.reject')}
        title={t('rating.reject')}
        disabled={disabled || onFlag === undefined}
        onClick={toggle('reject')}
        className={buttonClass('reject', 'text-danger')}
      >
        <FlagIcon kind="reject" size={size} />
      </button>
    </span>
  )
}
