import { useEffect, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { useAuth } from '../auth/AuthContext'
import { SubjectTile } from '../components/people/SubjectTile'
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
        <h1 className="h3 mb-0">{t('people.title')}</h1>
        {canWrite && (
          <Link to="/people/clusters" className="btn btn-outline-primary btn-sm">
            {t('people.reviewClusters')}
          </Link>
        )}
      </div>

      {state.status === 'loading' && (
        <div className="d-flex justify-content-center py-5">
          <Spinner animation="border" role="status">
            <span className="visually-hidden">{t('people.loading')}</span>
          </Spinner>
        </div>
      )}

      {state.status === 'error' && <Alert variant="danger">{t('people.error')}</Alert>}

      {state.status === 'ready' && state.subjects.length === 0 && (
        <div className="text-center text-secondary py-5">
          <p className="mb-1 fs-5">{t('people.empty.title')}</p>
          <p className="mb-0 small">{t('people.empty.hint')}</p>
        </div>
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
