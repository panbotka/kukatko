import { useTranslation } from 'react-i18next'

import { type Bucket, BUCKET_LABEL_KEY, BUCKET_VARIANT } from '../../lib/candidateReview'

/** The buckets shown in the legend, in the same order as the filter tabs. */
const LEGEND_BUCKETS: readonly Bucket[] = ['new', 'assign', 'done']

/**
 * CandidateLegend explains the colour code that runs across every badge and bounding
 * rectangle in the grid: what confirming a card would do. It reads the same
 * {@link BUCKET_VARIANT} map the cards do, so the swatch can never drift from the box.
 */
export function CandidateLegend() {
  const { t } = useTranslation()

  return (
    <ul className="list-inline mb-0 small text-secondary" aria-label={t('faceSearch.legend.label')}>
      {LEGEND_BUCKETS.map((bucket) => (
        <li key={bucket} className="list-inline-item d-inline-flex align-items-center gap-1 me-3">
          <span
            aria-hidden="true"
            className="d-inline-block rounded-1"
            style={{
              width: '0.9rem',
              height: '0.9rem',
              border: `3px solid var(--bs-${BUCKET_VARIANT[bucket]})`,
            }}
          />
          {t(BUCKET_LABEL_KEY[bucket])}
        </li>
      ))}
    </ul>
  )
}
