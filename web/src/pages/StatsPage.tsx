import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'

import { ErrorState } from '../components/ErrorState'
import { LibraryStatsCards } from '../components/LibraryStatsCards'
import { useDocumentTitle } from '../hooks/useDocumentTitle'
import { useLibraryStats } from '../hooks/useLibraryStats'

/**
 * The library-statistics page (`GET /system/stats`): how big the library is and
 * how much of it has been processed — photos and videos, what is in the trash,
 * how much can already be searched by content or has a face on it (and,
 * explicitly, how much still cannot), how many people and animals are named, and
 * how it is organised into albums and labels.
 *
 * Visible to every signed-in role: these are read-only aggregate counts, the same
 * numbers photo-sorter's status page showed, and knowing them is not an
 * operations privilege. Because everyone reads it, it speaks the family's
 * vocabulary rather than the pipeline's, and the counts that stand for work to be
 * done link to where that work happens ({@link LibraryStatsCards}). The System
 * page renders the very same counts as its Library section from this one
 * endpoint. A failed aggregation shows an error with a retry rather than a grid
 * of zeroes, which would read as an empty library. See docs/FRONTEND.md.
 */
export function StatsPage() {
  const { t } = useTranslation()
  useDocumentTitle(t('stats.title'))
  const { state, reload } = useLibraryStats()

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

      {state.status === 'ready' && <LibraryStatsCards stats={state.data} />}
    </>
  )
}
