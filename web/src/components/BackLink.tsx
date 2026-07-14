import { Link } from 'react-router-dom'

import { Icon } from './Icon'

/** Props for {@link BackLink}. */
export interface BackLinkProps {
  /**
   * The destination — the full href of the list the page came from, query string
   * included. A real URL (never `history.back()`), so the list's view state
   * (filters, sort, page) round-trips through the query params and Back works
   * even when the detail page was opened straight from a bookmark.
   */
  to: string
  /**
   * The visible, already-translated label naming the destination ("Zpět na
   * alba"). It is also the link's accessible name: the arrow is decorative, so a
   * screen reader announces this text alone.
   */
  label: string
  /** Extra classes merged onto the link. */
  className?: string
}

/**
 * The way back from a detail page to the list it belongs to: a left arrow plus
 * the destination's name ("Zpět na alba"), not a bare glyph that leaves the
 * reader guessing where it leads.
 *
 * Shared by every detail page (album, label, person, photo) so the affordance
 * looks and behaves the same everywhere. It renders a router `<Link>`, which is
 * keyboard focusable, shows a focus ring and an underline on hover, and — being
 * a real href — can be opened in a new tab or middle-clicked like any link.
 */
export function BackLink({ to, label, className }: BackLinkProps) {
  const classes = ['kk-back-link']
  if (className !== undefined && className !== '') {
    classes.push(className)
  }

  return (
    <Link to={to} className={classes.join(' ')}>
      <Icon name="arrow-left" className="kk-back-link__arrow" />
      <span>{label}</span>
    </Link>
  )
}
