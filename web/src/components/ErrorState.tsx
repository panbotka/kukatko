import { type ReactNode } from 'react'
import Button from 'react-bootstrap/Button'
import { useTranslation } from 'react-i18next'

import { Icon } from './Icon'

/** Props for {@link ErrorState}. */
export interface ErrorStateProps {
  /**
   * Short, plain message naming what failed — "Photos could not be loaded".
   * Already translated by the caller (each view owns its own key so the copy is
   * specific), never a raw backend error string.
   */
  title: string
  /** Optional second line with a touch more context or reassurance. */
  hint?: string
  /**
   * Retry handler. When given, a "Try again" button re-runs the failed load, so
   * a transient failure never leaves the reader stuck on a dead end.
   */
  onRetry?: () => void
  /** Overrides the default shared "Try again" label. */
  retryLabel?: string
  /**
   * Extra action rendered next to (or instead of) Retry — typically a way back
   * to the list on a detail page that failed to load its entity.
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
 * The shared load-failure placeholder: a warning glyph, a short message and a
 * Retry action, centred in the space the data would occupy. It is the error twin
 * of {@link EmptyState} — the same centred column, so the app reads consistently
 * — but with a danger-tinted medallion and `role="alert"`, so a failed load never
 * reads as an intentional empty collection. It replaces the bare
 * `Alert variant="danger"` one-liners (and hand-rolled Alert + retry blocks) that
 * used to stand in for a broken data view, and it never shows a raw error string.
 */
export function ErrorState({
  title,
  hint,
  onRetry,
  retryLabel,
  action,
  size = 'md',
  className,
}: ErrorStateProps) {
  const { t } = useTranslation()

  const classes = ['kk-empty-state', 'kk-empty-state--error', 'kk-appear']
  if (size === 'sm') {
    classes.push('kk-empty-state--sm')
  }
  if (className !== undefined && className !== '') {
    classes.push(className)
  }

  return (
    <div className={classes.join(' ')} role="alert" data-testid="error-state">
      <span className="kk-empty-state__icon" aria-hidden="true">
        <Icon name="exclamation-triangle" />
      </span>
      <p
        className={`kk-empty-state__title ${size === 'sm' ? 'kk-text-caption' : 'kk-section-title'}`}
      >
        {title}
      </p>
      {hint !== undefined && hint !== '' && (
        <p className="kk-empty-state__hint kk-text-caption">{hint}</p>
      )}
      {(onRetry !== undefined || action !== undefined) && (
        <div className="kk-empty-state__action d-flex flex-wrap justify-content-center gap-2">
          {onRetry !== undefined && (
            <Button variant="outline-light" size="sm" onClick={onRetry}>
              <Icon name="arrow-clockwise" className="me-1" />
              {retryLabel ?? t('errors.retry')}
            </Button>
          )}
          {action}
        </div>
      )}
    </div>
  )
}
