import { type MouseEvent } from 'react'
import { Link, type LinkProps, useNavigate } from 'react-router-dom'

import { useMorph, useMorphMark } from './MorphContext'

/** Props for {@link MorphLink}. */
export interface MorphLinkProps extends Omit<LinkProps, 'state'> {
  /**
   * Identifies the element this link morphs from and into. Both halves of the
   * pair use the same id — the grid tile for a photo and the viewer's figure for
   * the same photo both say `photo.uid` — which is what pairs them up.
   */
  morphId: string
  /**
   * Opaque history state carried with the navigation. Narrowed from the router's
   * own `any` to `unknown`: this component only ever passes it through, and the
   * project's lint forbids handling `any` values.
   */
  state?: unknown
}

/**
 * Whether a click is the plain left-click a router link handles itself.
 *
 * Everything else has to be left to the browser: Ctrl/Cmd-click and a middle
 * click open a new tab, Shift-click a new window, Alt-click downloads, and a
 * link with a `target` goes somewhere this document is not. Taking those over
 * would trade a decorative animation for broken browser behaviour.
 */
function isPlainClick(event: MouseEvent<HTMLAnchorElement>): boolean {
  return (
    event.button === 0 &&
    !event.metaKey &&
    !event.ctrlKey &&
    !event.shiftKey &&
    !event.altKey &&
    (event.currentTarget.target === '' || event.currentTarget.target === '_self')
  )
}

/**
 * A router `<Link>` whose navigation morphs: the element it is on grows into the
 * matching element on the page it leads to, instead of the two swapping.
 *
 * It is a drop-in replacement for `<Link>` and degrades to exactly one. Where the
 * browser has no View Transitions API — or the user asked for reduced motion —
 * the click is not intercepted at all and react-router does what it always did,
 * which is the point: the morph is decoration, never the navigation itself.
 *
 * The link also carries the mark while it is the one being morphed, so a caller
 * only has to swap `Link` for `MorphLink`; the other half of the pair marks
 * itself with `useMorphMark`.
 */
export function MorphLink({
  morphId,
  to,
  state,
  replace = false,
  reloadDocument = false,
  onClick,
  children,
  ...rest
}: MorphLinkProps) {
  const navigate = useNavigate()
  const { enabled, morph } = useMorph()
  const mark = useMorphMark(morphId)

  return (
    <Link
      {...rest}
      {...mark}
      to={to}
      state={state}
      replace={replace}
      reloadDocument={reloadDocument}
      onClick={(event) => {
        // The caller's own handler runs first and may cancel the navigation
        // outright — the photo grid does exactly that in selection mode, where a
        // tile toggles its selection instead of opening.
        onClick?.(event)
        if (event.defaultPrevented || reloadDocument || !enabled || !isPlainClick(event)) {
          return
        }
        event.preventDefault()
        morph(morphId, () => {
          void navigate(to, { state, replace })
        })
      }}
    >
      {children}
    </Link>
  )
}
