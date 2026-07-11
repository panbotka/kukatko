import { type MouseEvent } from 'react'
import { useTranslation } from 'react-i18next'

import { type RatingFlag } from '../../services/photos'
import { Icon, type IconName } from '../Icon'

/** Props for {@link FlagControl}. */
export interface FlagControlProps {
  /** The current personal-marking flag. */
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

/** The selectable personal-marking states, in display order (eye, up, down). */
type FlagValue = 'eye' | 'pick' | 'reject'

/**
 * One personal-marking state's presentation: the i18n label key, the outline and
 * filled bootstrap-icons glyphs, and the Bootstrap text-colour utility applied
 * when the state is active.
 */
interface FlagSpec {
  readonly value: FlagValue
  // A literal i18n key (not a wide `string`) so the typed `t()` accepts it.
  readonly labelKey: 'rating.eye' | 'rating.pick' | 'rating.reject'
  readonly icon: IconName
  readonly iconActive: IconName
  readonly activeClass: string
}

// The three marks: 👁 eye (neutral accent), 👍 thumbs-up (green), 👎 thumbs-down
// (red). Stored values pick/reject back thumbs-up/down (kept for compatibility).
const FLAG_SPECS: readonly FlagSpec[] = [
  {
    value: 'eye',
    labelKey: 'rating.eye',
    icon: 'eye',
    iconActive: 'eye-fill',
    activeClass: 'text-info',
  },
  {
    value: 'pick',
    labelKey: 'rating.pick',
    icon: 'hand-thumbs-up',
    iconActive: 'hand-thumbs-up-fill',
    activeClass: 'text-success',
  },
  {
    value: 'reject',
    labelKey: 'rating.reject',
    icon: 'hand-thumbs-down',
    iconActive: 'hand-thumbs-down-fill',
    activeClass: 'text-danger',
  },
]

/**
 * Three toggle buttons for the per-user personal marking (👁 eye, 👍 thumbs-up,
 * 👎 thumbs-down). The active mark is highlighted with its filled glyph and a
 * distinct colour; clicking it again clears the mark to `'none'` (the "clear"
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

  const toggle = (value: FlagValue) => (event: MouseEvent<HTMLButtonElement>) => {
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
      {FLAG_SPECS.map((spec) => {
        const active = flag === spec.value
        const label = t(spec.labelKey)
        return (
          <button
            key={spec.value}
            type="button"
            aria-pressed={active}
            aria-label={label}
            title={label}
            disabled={disabled || onFlag === undefined}
            onClick={toggle(spec.value)}
            style={{ fontSize: size }}
            className={`btn btn-sm p-1 lh-1 border-0 bg-transparent d-inline-flex ${
              active ? spec.activeClass : 'text-secondary'
            }`}
          >
            <Icon name={active ? spec.iconActive : spec.icon} className="d-block" />
          </button>
        )
      })}
    </span>
  )
}
