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

/** The selectable personal-marking states, in display order (eye, pick, reject). */
type FlagValue = 'eye' | 'pick' | 'reject'

/**
 * One personal-marking state's presentation: the i18n keys for its name and its
 * one-sentence explanation, the outline and filled bootstrap-icons glyphs, and
 * the Bootstrap text-colour utility applied when the state is active.
 */
interface FlagSpec {
  readonly value: FlagValue
  // Literal i18n keys (not a wide `string`) so the typed `t()` accepts them.
  readonly labelKey: 'rating.eye' | 'rating.pick' | 'rating.reject'
  readonly hintKey: 'rating.eyeHint' | 'rating.pickHint' | 'rating.rejectHint'
  readonly icon: IconName
  readonly iconActive: IconName
  readonly activeClass: string
}

// The three marks: 👁 look at later (neutral accent), 👍 pick (green), 👎 reject
// (red). The glyphs are shapes, the names are what the mark is *for* — an icon
// called "thumbs up" teaches nobody that it picks a photo for further work — so
// the label names the act and the tooltip spells it out in a sentence. The
// stored values pick/reject/eye stay as they are (API and `flag:` query key).
const FLAG_SPECS: readonly FlagSpec[] = [
  {
    value: 'eye',
    labelKey: 'rating.eye',
    hintKey: 'rating.eyeHint',
    icon: 'eye',
    iconActive: 'eye-fill',
    activeClass: 'text-info',
  },
  {
    value: 'pick',
    labelKey: 'rating.pick',
    hintKey: 'rating.pickHint',
    icon: 'hand-thumbs-up',
    iconActive: 'hand-thumbs-up-fill',
    activeClass: 'text-success',
  },
  {
    value: 'reject',
    labelKey: 'rating.reject',
    hintKey: 'rating.rejectHint',
    icon: 'hand-thumbs-down',
    iconActive: 'hand-thumbs-down-fill',
    activeClass: 'text-danger',
  },
]

/**
 * Three toggle buttons for the per-user personal marking — 👁 look at later,
 * 👍 pick, 👎 reject. Each button is named after the act, not the glyph, and
 * its tooltip explains the mark in one sentence. The active mark is highlighted
 * with its filled glyph and a distinct colour; clicking it again clears the
 * mark to `'none'` (the "clear" affordance). When `onFlag` is omitted the
 * control renders read-only. Purely controlled — optimistic state lives in
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
        // The accessible name stays short (it is announced on every button),
        // the tooltip carries the sentence that explains what the mark is for.
        return (
          <button
            key={spec.value}
            type="button"
            aria-pressed={active}
            aria-label={label}
            title={t(spec.hintKey)}
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
