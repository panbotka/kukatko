import { type ReactNode } from 'react'
import CloseButton from 'react-bootstrap/CloseButton'
import { Link } from 'react-router-dom'

import { ENTITY_STYLE, type EntityKind } from './entityStyle'
import { Icon } from './Icon'

/** The remove control an editor gets on a chip. */
export interface EntityChipRemove {
  /** The X's accessible name — it names what leaves, not merely "remove". */
  label: string
  /** Detaches the photo from the album/label the chip stands for. */
  onRemove: () => void
}

/** Props for {@link EntityChip}. */
export interface EntityChipProps {
  /** Which catalog entity the chip stands for — decides its hue and glyph. */
  kind: EntityKind
  /** Where the chip leads: that entity's scoped list. */
  to: string
  /** The chip's visible text — a name, optionally with a count beside it. */
  children: ReactNode
  /** Extra classes for the pill (e.g. `fw-normal` for a search hit). */
  className?: string
  /** An editor's remove control; without it the chip is read-only. */
  remove?: EntityChipRemove
}

/**
 * One catalog membership — an album, a label, a person — as a pill chip that
 * links to that entity's scoped list: the shared `ENTITY_STYLE` hue and leading
 * glyph, the name, and for an editor a trailing X that detaches it.
 *
 * **The pill is the link.** Wrapping a small `<a>` in a bigger decorative pill
 * looks the same and taps very differently: measured on production (390 × 844,
 * coarse pointer) the photo panel's album chip was a 79.1 × 12.0px link inside a
 * 111 × 20.9px pill — under even WCAG 2.2's 24px floor, let alone the app's own
 * 44px one, because the app-wide floor in `app.css` lists `.btn` and friends and
 * a `.badge` is none of them. So the anchor carries the pill's own classes and
 * the glyph sits inside it, which makes the whole chip one target that the
 * `a.badge` rule in that floor then lifts to 2.75rem on a coarse pointer.
 *
 * With a remove control the pill cannot *be* the anchor — an `<a>` may not
 * contain a button — so it becomes a span whose link is its direct child and
 * stretches over its full height (`.badge > a` in the same block), with the X
 * trimming it at the end. Either way the target is the pill, not a band of text
 * across its middle.
 */
export function EntityChip({ kind, to, children, className, remove }: EntityChipProps) {
  const style = ENTITY_STYLE[kind]
  const row = 'd-inline-flex align-items-center gap-1'
  const pill = `badge rounded-pill ${style.className} ${row}${className === undefined ? '' : ` ${className}`}`
  const label = (
    <>
      <Icon name={style.icon} />
      {children}
    </>
  )

  if (remove === undefined) {
    return (
      <Link to={to} className={`${pill} text-white text-decoration-none`}>
        {label}
      </Link>
    )
  }

  return (
    <span className={pill}>
      <Link to={to} className={`${row} text-white text-decoration-none`}>
        {label}
      </Link>
      <CloseButton
        variant="white"
        aria-label={remove.label}
        // The same sentence again as the mouse's hover hint: an X on a chip is a
        // guess until it says which chip it detaches.
        title={remove.label}
        onClick={remove.onRemove}
      />
    </span>
  )
}
