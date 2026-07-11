import { type ParseKeys } from 'i18next'
import { type MouseEvent } from 'react'
import { useTranslation } from 'react-i18next'

import { type RatingFlag } from '../../services/photos'
import { Icon, type IconName } from '../Icon'

/** Props for {@link FlagControl}. */
export interface FlagControlProps {
  /** The current personal mark. */
  flag: RatingFlag
  /**
   * Called with the new mark when a button is clicked. Clicking the active mark
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

/** One selectable personal-mark state: its stored value, icons, label and accent. */
interface MarkOption {
  /** The stored flag value this button sets (never `'none'`; clearing is implicit). */
  value: Exclude<RatingFlag, 'none'>
  /** The outline bootstrap-icons glyph shown when the mark is inactive. */
  icon: IconName
  /** The filled bootstrap-icons glyph shown when the mark is active. */
  activeIcon: IconName
  /** The i18n key for the accessible label and tooltip. */
  labelKey: ParseKeys
  /** The Bootstrap text-color utility applied when the mark is active. */
  activeClass: string
}

/**
 * The three mutually-exclusive personal marks in display order: the neutral eye,
 * thumbs-up (stored `pick`) and thumbs-down (stored `reject`). Each renders the
 * outline icon when inactive and the filled icon in its accent color when active.
 */
const MARK_OPTIONS: readonly MarkOption[] = [
  {
    value: 'eye',
    icon: 'eye',
    activeIcon: 'eye-fill',
    labelKey: 'rating.eye',
    activeClass: 'text-info',
  },
  {
    value: 'pick',
    icon: 'hand-thumbs-up',
    activeIcon: 'hand-thumbs-up-fill',
    labelKey: 'rating.pick',
    activeClass: 'text-success',
  },
  {
    value: 'reject',
    icon: 'hand-thumbs-down',
    activeIcon: 'hand-thumbs-down-fill',
    labelKey: 'rating.reject',
    activeClass: 'text-danger',
  },
]

/**
 * Three toggle buttons for the per-user personal mark: the neutral eye, thumbs-up
 * and thumbs-down. The active mark is highlighted with its filled icon and accent
 * color; clicking it again clears the mark to `'none'` (the "clear" affordance).
 * When `onFlag` is omitted the control renders read-only. Purely controlled —
 * optimistic state lives in {@link import('../../hooks/useRating').useRating}.
 */
export function FlagControl({
  flag,
  onFlag,
  disabled = false,
  size = 16,
  className,
}: FlagControlProps) {
  const { t } = useTranslation()

  const toggle = (value: RatingFlag) => (event: MouseEvent<HTMLButtonElement>) => {
    // Sibling of a tile link/button: never navigate or toggle selection.
    event.preventDefault()
    event.stopPropagation()
    onFlag?.(flag === value ? 'none' : value)
  }

  return (
    <span
      className={`d-inline-flex align-items-center gap-1 ${className ?? ''}`}
      role="group"
      aria-label={t('rating.flag')}
    >
      {MARK_OPTIONS.map((option) => {
        const active = flag === option.value
        return (
          <button
            key={option.value}
            type="button"
            aria-pressed={active}
            aria-label={t(option.labelKey)}
            title={t(option.labelKey)}
            disabled={disabled || onFlag === undefined}
            onClick={toggle(option.value)}
            className={`btn btn-sm p-1 lh-1 border-0 bg-transparent d-inline-flex ${
              active ? option.activeClass : 'text-secondary'
            }`}
            style={{ fontSize: size }}
          >
            <Icon name={active ? option.activeIcon : option.icon} className="d-block" />
          </button>
        )
      })}
    </span>
  )
}
