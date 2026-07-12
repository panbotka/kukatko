import { type ReactNode } from 'react'

import { entityBadgeClassName, entityIcon, type EntityKind } from './entity'
import { Icon } from './Icon'

/** Props for {@link EntityBadge}. */
export interface EntityBadgeProps {
  /** Which catalog entity this token stands for; picks its colour and icon. */
  kind: EntityKind
  /** Extra classes appended after the base badge + kind colour classes. */
  className?: string
  /** The label text and any trailing controls (a link, a remove button). */
  children: ReactNode
}

/**
 * A Bootstrap `.badge` coloured and icon-led by the entity kind it represents
 * (album / tag / person). The leading {@link Icon} is decorative — colour is
 * only an aid — so the visible text label and the icon together keep the tokens
 * distinguishable for colour-blind readers. Callers pass the label, and any
 * trailing control such as a remove button, as children. The hues and classes
 * are defined once in `styles/tokens.css` and `./entity`.
 */
export function EntityBadge({ kind, className, children }: EntityBadgeProps) {
  const classes = [
    'badge',
    'd-inline-flex',
    'align-items-center',
    'gap-1',
    entityBadgeClassName(kind),
  ]
  if (className !== undefined) {
    classes.push(className)
  }
  return (
    <span className={classes.join(' ')}>
      <Icon name={entityIcon(kind)} />
      {children}
    </span>
  )
}
