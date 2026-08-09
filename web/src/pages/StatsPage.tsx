import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'

import { ErrorState } from '../components/ErrorState'
import { LibraryStatsCards } from '../components/LibraryStatsCards'
import { CoverageMeters } from '../components/stats/CoverageMeters'
import { LibraryChartsPanel } from '../components/stats/LibraryChartsPanel'
import { useDocumentTitle } from '../hooks/useDocumentTitle'
import { useLibraryCharts } from '../hooks/useLibraryCharts'
import { useLibraryStats } from '../hooks/useLibraryStats'

/**
 * The library-statistics page: a small dashboard of what the library looks like.
 *
 * It reads two endpoints side by side. `GET /system/stats` gives the counts —
 * how big the library is and how much of it has been processed ({@link
 * LibraryStatsCards}) plus the three coverage shares ({@link CoverageMeters}) —
 * and answers in milliseconds. `GET /system/stats/charts` gives the series
 * behind them ({@link LibraryChartsPanel}): photos per year of capture, arrivals
 * per month, the top cameras, and what the library costs in bytes. They are two
 * requests on purpose: the charts are the heavier aggregation and must never
 * hold the numbers back, and each fails on its own — a chart outage still leaves
 * a readable page.
 *
 * Visible to every signed-in role: these are read-only aggregate counts, the same
 * numbers photo-sorter's status page showed, and knowing them is not an
 * operations privilege. Because everyone reads it, it speaks the family's
 * vocabulary rather than the pipeline's, and the counts that stand for work to be
 * done link to where that work happens ({@link LibraryStatsCards}); a bar of the
 * year histogram opens that year in the library. The System page renders the very
 * same counts as its Library section from the same one endpoint. A failed
 * aggregation shows an error with a retry rather than a grid of zeroes, which
 * would read as an empty library. See docs/FRONTEND.md.
 */
export function StatsPage() {
  const { t } = useTranslation()
  useDocumentTitle(t('stats.title'))
  const { state, reload } = useLibraryStats()
  const charts = useLibraryCharts()

  return (
    <>
      <div className="mb-3">
        <h1 className="kk-page-title mb-1">{t('stats.title')}</h1>
        <p className="text-secondary mb-0">{t('stats.subtitle')}</p>
      </div>

      {state.status === 'loading' && (
        <div className="d-flex justify-content-center py-5">
          <Spinner animation="border" role="status">
            <span className="visually-hidden">{t('stats.loading')}</span>
          </Spinner>
        </div>
      )}

      {state.status === 'error' && <ErrorState title={t('stats.error')} onRetry={reload} />}

      {state.status === 'ready' && (
        <div className="d-flex flex-column gap-3">
          <LibraryStatsCards stats={state.data} />
          <CoverageMeters stats={state.data} />

          {charts.state.status === 'loading' && (
            <div className="d-flex justify-content-center py-4">
              <Spinner animation="border" role="status">
                <span className="visually-hidden">{t('stats.charts.loading')}</span>
              </Spinner>
            </div>
          )}
          {charts.state.status === 'error' && (
            <ErrorState title={t('stats.charts.error')} onRetry={charts.reload} />
          )}
          {charts.state.status === 'ready' && <LibraryChartsPanel charts={charts.state.data} />}
        </div>
      )}
    </>
  )
}
