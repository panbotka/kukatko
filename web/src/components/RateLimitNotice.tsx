import Alert from 'react-bootstrap/Alert'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'

/** Props for {@link RateLimitNotice}. */
export interface RateLimitNoticeProps {
  /**
   * `block` (default) stands in for a whole view that is waiting — a calm info
   * alert with the reassurance line. `inline` is one small line for a spot
   * that keeps its content, such as under a "load more" button.
   */
  variant?: 'block' | 'inline'
  /** Extra classes merged onto the root element. */
  className?: string
}

/**
 * What a page shows while {@link import('../hooks/useRateLimitRetry').useRateLimitRetry}
 * waits out a 429: "too many requests at once — trying again in a moment",
 * with a spinner, because the retry is already scheduled and the reader need
 * not do anything. It is deliberately not a failure state: no danger tint, no
 * retry button, `role="status"` rather than `alert`.
 */
export function RateLimitNotice({ variant = 'block', className }: RateLimitNoticeProps) {
  const { t } = useTranslation()
  if (variant === 'inline') {
    return (
      <div
        role="status"
        className={['small text-secondary mt-2', className].filter(Boolean).join(' ')}
        data-testid="rate-limit-notice"
      >
        <Spinner animation="border" size="sm" className="me-2" aria-hidden="true" />
        {t('errors.rateLimited')}
      </div>
    )
  }
  return (
    <Alert
      variant="info"
      role="status"
      className={['d-flex align-items-start gap-2', className].filter(Boolean).join(' ')}
      data-testid="rate-limit-notice"
    >
      <Spinner animation="border" size="sm" className="mt-1 flex-shrink-0" aria-hidden="true" />
      <div>
        <div>{t('errors.rateLimited')}</div>
        <div className="small">{t('errors.rateLimitedHint')}</div>
      </div>
    </Alert>
  )
}
