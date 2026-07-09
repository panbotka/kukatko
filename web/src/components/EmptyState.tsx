import { type ReactNode } from 'react'

/**
 * The default illustration: an empty picture frame. Purely decorative — the
 * title carries the meaning — so it is hidden from assistive technology.
 */
function DefaultIcon() {
  return (
    <svg
      width="28"
      height="28"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.5"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      focusable="false"
    >
      <rect x="3" y="4" width="18" height="16" rx="2" />
      <circle cx="8.5" cy="9.5" r="1.5" />
      <path d="M3 16l4.5-4.5a2 2 0 0 1 2.8 0L15 16" />
      <path d="M14 15l1.5-1.5a2 2 0 0 1 2.8 0L21 16" />
    </svg>
  )
}

/** Props for {@link EmptyState}. */
export interface EmptyStateProps {
  /**
   * Short, concrete title — what is not here. Already translated by the caller
   * (every page owns its own i18n key so the copy can be specific).
   */
  title: string
  /**
   * One line telling the reader how to fill the collection. Omit it in the
   * compact variant, where there is no room for a second line.
   */
  hint?: string
  /**
   * Decorative illustration. Defaults to an empty picture frame; pass a subject
   * specific glyph where one exists. Rendered inside a circular well.
   */
  icon?: ReactNode
  /**
   * Optional call to action — usually the same button the populated view offers
   * ("Create album"), so an empty screen is an invitation rather than a wall.
   */
  action?: ReactNode
  /**
   * `md` (default) is the full-page treatment. `sm` fits inside a tile or a
   * narrow panel: smaller icon, no vertical breathing room.
   */
  size?: 'md' | 'sm'
  /** Extra classes merged onto the root element. */
  className?: string
}

/**
 * The shared empty-collection placeholder: icon, short title, one-line hint and
 * an optional action button, centred in the space the collection would occupy.
 *
 * It replaces the bare one-liners ("Bez štítků.", "Bez náhledu") that used to
 * stand in for an empty list, so every empty screen in the app reads the same
 * way and points at the next step. The whole block fades up on mount, unless the
 * reader asked for reduced motion.
 */
export function EmptyState({ title, hint, icon, action, size = 'md', className }: EmptyStateProps) {
  const classes = ['kk-empty-state', 'kk-appear']
  if (size === 'sm') {
    classes.push('kk-empty-state--sm')
  }
  if (className !== undefined && className !== '') {
    classes.push(className)
  }

  return (
    <div className={classes.join(' ')} data-testid="empty-state">
      <span className="kk-empty-state__icon" aria-hidden="true">
        {icon ?? <DefaultIcon />}
      </span>
      <p
        className={`kk-empty-state__title ${size === 'sm' ? 'kk-text-caption' : 'kk-section-title'}`}
      >
        {title}
      </p>
      {hint !== undefined && hint !== '' && (
        <p className="kk-empty-state__hint kk-text-caption">{hint}</p>
      )}
      {action !== undefined && <div className="kk-empty-state__action">{action}</div>}
    </div>
  )
}
