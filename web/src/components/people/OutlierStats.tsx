import Alert from 'react-bootstrap/Alert'
import { useTranslation } from 'react-i18next'

import { distancePercent, OUTLIER_LIMIT } from '../../lib/outlierReview'
import { type OutlierResult } from '../../services/people'

/** Props for {@link OutlierStats}. */
export interface OutlierStatsProps {
  /** The response being summarised. */
  result: OutlierResult
  /** How many cards the grid actually shows (after the threshold narrowed it). */
  shown: number
}

/**
 * OutlierStats is the one-glance summary above the /outliers grid: how many
 * faces were scored, how far the average one sits from the centroid, how many
 * the current threshold leaves on screen, and the line explaining the order
 * ("ranked by distance from the centroid, most suspicious first") — the ranking
 * is the whole product here, so it is spelled out rather than left to be guessed.
 *
 * The **faces with no embedding** count is the part that earns its place. A face
 * detected while the embedding sidecar was offline cannot be scored at all, so
 * it is missing from this list entirely — silently omitting it would let a
 * curator believe they had reviewed everything. It says so instead.
 */
export function OutlierStats({ result, shown }: OutlierStatsProps) {
  const { t } = useTranslation()
  const capped = shown >= OUTLIER_LIMIT

  return (
    <div className="mb-3" data-testid="outlier-stats">
      <div className="d-flex flex-wrap gap-3 small">
        <span>
          <span className="text-secondary">{t('outliersPage.stats.total')}: </span>
          <span className="fw-semibold">{result.count}</span>
        </span>
        <span>
          <span className="text-secondary">{t('outliersPage.stats.avgDistance')}: </span>
          <span className="fw-semibold">
            {t('outliersPage.stats.percent', { percent: distancePercent(result.avg_distance) })}
          </span>
        </span>
        <span>
          <span className="text-secondary">{t('outliersPage.stats.shown')}: </span>
          <span className="fw-semibold">{shown}</span>
        </span>
      </div>
      <p className="text-secondary small mb-0 mt-1">{t('outliersPage.stats.explanation')}</p>

      {/* A face the sidecar never embedded cannot be checked — say so, rather
          than let an empty list read as "all clear". */}
      {result.no_embedding > 0 && (
        <Alert variant="info" className="py-2 small mt-2 mb-0" data-testid="outlier-no-embedding">
          {t('outliersPage.stats.noEmbedding', { count: result.no_embedding })}
        </Alert>
      )}

      {capped && (
        <Alert variant="secondary" className="py-2 small mt-2 mb-0" data-testid="outlier-capped">
          {t('outliersPage.stats.capped', { count: OUTLIER_LIMIT })}
        </Alert>
      )}

      {!result.meaningful && (
        <Alert variant="info" className="py-2 small mt-2 mb-0" data-testid="outlier-not-meaningful">
          {t('outliersPage.stats.notMeaningful')}
        </Alert>
      )}
    </div>
  )
}
