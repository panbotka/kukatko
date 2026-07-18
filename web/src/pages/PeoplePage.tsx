import { useEffect, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { useAuth } from '../auth/AuthContext'
import { EmptyState } from '../components/EmptyState'
import { SubjectTile } from '../components/people/SubjectTile'
import { TileGridSkeleton } from '../components/Skeleton'
import { fetchSubjects, type SubjectCount } from '../services/people'

/** Fetch lifecycle of the people list. */
type State =
  | { status: 'loading' }
  | { status: 'error' }
  | { status: 'ready'; subjects: SubjectCount[] }

/**
 * The people index: a responsive grid of subjects (cover, name, photo count),
 * each linking to its page. Editors and admins also get a link to the cluster
 * review queue, the fast bulk-naming path. The whole view is read-only here;
 * naming and editing happen on the subject and cluster pages.
 */
export function PeoplePage() {
  const { t } = useTranslation()
  const { canWrite } = useAuth()
  const [state, setState] = useState<State>({ status: 'loading' })

  useEffect(() => {
    const controller = new AbortController()
    setState({ status: 'loading' })
    fetchSubjects(controller.signal)
      .then((subjects) => {
        setState({ status: 'ready', subjects })
      })
      .catch((err: unknown) => {
        if (err instanceof DOMException && err.name === 'AbortError') {
          return
        }
        setState({ status: 'error' })
      })
    return () => {
      controller.abort()
    }
  }, [])

  return (
    <>
      <div className="d-flex justify-content-between align-items-center mb-3 flex-wrap gap-2">
        <h1 className="kk-page-title mb-0">{t('people.title')}</h1>
        {canWrite && (
          <Link to="/people/clusters" className="btn btn-outline-primary">
            {t('people.reviewClusters')}
          </Link>
        )}
      </div>

      {state.status === 'loading' && (
        <TileGridSkeleton label={t('people.loading')} minTile={140} captionLines={1} />
      )}

      {state.status === 'error' && <Alert variant="danger">{t('people.error')}</Alert>}

      {state.status === 'ready' && state.subjects.length === 0 && (
        <EmptyState title={t('people.empty.title')} hint={t('people.empty.hint')} />
      )}

      {state.status === 'ready' && state.subjects.length > 0 && (
        <div
          style={{
            display: 'grid',
            gridTemplateColumns: 'repeat(auto-fill, minmax(140px, 1fr))',
            gap: '12px',
          }}
        >
          {state.subjects.map((subject) => (
            <SubjectTile key={subject.uid} subject={subject} />
          ))}
        </div>
      )}
    </>
  )
}
