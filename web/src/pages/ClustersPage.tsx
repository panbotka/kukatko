import { useCallback, useEffect, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'

import { EmptyState } from '../components/EmptyState'
import { ErrorState } from '../components/ErrorState'
import { GridDensityControl } from '../components/library/GridDensityControl'
import { ClusterCard } from '../components/people/ClusterCard'
import { useDocumentTitle } from '../hooks/useDocumentTitle'
import { useGridDensity } from '../hooks/useGridDensity'
import { gridTemplateColumns, REVIEW_GRID_SCOPE } from '../lib/gridDensity'
import {
  assignCluster,
  type ClusterAssignRequest,
  type ClusterView,
  fetchClusters,
  removeClusterFace,
  type RemoveFaceRequest,
} from '../services/people'

import '../components/review/review.css'

/** Fetch lifecycle of the cluster queue. */
type State =
  | { status: 'loading' }
  | { status: 'error' }
  | { status: 'ready'; clusters: ClusterView[] }

/**
 * The cluster review queue: unnamed face clusters, each named in one action
 * (assigning every face to a new or existing subject). It is the primary, fast
 * path for bulk people-tagging. Naming a cluster removes it from the list;
 * detaching a stray face refreshes (or drops) that cluster in place. The whole
 * flow is editor/admin-only (the route guards it) and updates optimistically.
 */
export function ClustersPage() {
  const { t } = useTranslation()
  useDocumentTitle(t('clusters.title'))
  const [state, setState] = useState<State>({ status: 'loading' })
  const [busyUid, setBusyUid] = useState<string | null>(null)
  const [actionError, setActionError] = useState(false)
  // The cluster grid is the one the user sizes here: a card is a face plus a
  // name field, and how many of them fit across is the same choice every other
  // review tool offers, on the same stored number.
  const { density } = useGridDensity(REVIEW_GRID_SCOPE)

  const load = useCallback((signal?: AbortSignal) => {
    setState({ status: 'loading' })
    fetchClusters(signal)
      .then((clusters) => {
        setState({ status: 'ready', clusters })
      })
      .catch((err: unknown) => {
        if (err instanceof DOMException && err.name === 'AbortError') {
          return
        }
        setState({ status: 'error' })
      })
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    load(controller.signal)
    return () => {
      controller.abort()
    }
  }, [load])

  const assign = useCallback(async (uid: string, req: ClusterAssignRequest) => {
    setBusyUid(uid)
    setActionError(false)
    try {
      await assignCluster(uid, req)
      // The cluster is consumed server-side: drop it from the queue.
      setState((prev) =>
        prev.status === 'ready'
          ? { status: 'ready', clusters: prev.clusters.filter((c) => c.uid !== uid) }
          : prev,
      )
    } catch {
      setActionError(true)
    } finally {
      setBusyUid(null)
    }
  }, [])

  const removeFace = useCallback(async (uid: string, ref: RemoveFaceRequest) => {
    setBusyUid(uid)
    setActionError(false)
    try {
      const refreshed = await removeClusterFace(uid, ref)
      setState((prev) => {
        if (prev.status !== 'ready') {
          return prev
        }
        const clusters = refreshed
          ? prev.clusters.map((c) => (c.uid === uid ? refreshed : c))
          : prev.clusters.filter((c) => c.uid !== uid)
        return { status: 'ready', clusters }
      })
    } catch {
      setActionError(true)
    } finally {
      setBusyUid(null)
    }
  }, [])

  return (
    <>
      {/* `flex-md-nowrap` is what actually puts the stepper beside the title: a
          flex line is laid out from each item's *max-content* width, and the
          subtitle's is a whole sentence, so with wrapping left on at every width
          the control always dropped to a second line and sat at its left edge —
          `justify-content-between` never got to do anything. Below `md` the wrap
          is right, and there the control belongs under the text rather than
          squeezed beside it. */}
      <div className="d-flex flex-wrap flex-md-nowrap align-items-start justify-content-between gap-3">
        <div>
          <h1 className="kk-page-title mb-1">{t('clusters.title')}</h1>
          <p className="text-secondary">{t('clusters.subtitle')}</p>
        </div>
        {state.status === 'ready' && state.clusters.length > 0 && (
          <GridDensityControl scope={REVIEW_GRID_SCOPE} />
        )}
      </div>

      {actionError && (
        <Alert
          variant="danger"
          dismissible
          onClose={() => {
            setActionError(false)
          }}
        >
          {t('clusters.actionError')}
        </Alert>
      )}

      {state.status === 'loading' && (
        <div className="d-flex justify-content-center py-5">
          <Spinner animation="border" role="status">
            <span className="visually-hidden">{t('clusters.loading')}</span>
          </Spinner>
        </div>
      )}

      {state.status === 'error' && (
        <ErrorState
          title={t('clusters.error')}
          onRetry={() => {
            load()
          }}
        />
      )}

      {state.status === 'ready' && state.clusters.length === 0 && (
        <EmptyState title={t('clusters.empty.title')} hint={t('clusters.empty.hint')} />
      )}

      {state.status === 'ready' && state.clusters.length > 0 && (
        <div
          className="d-grid kk-review-grid"
          data-density={density}
          /* The responsive 1/2/3 columns this grid had are gone: the count is the
             user's, shared with every other review tool. `kk-review-grid` is what
             keeps that count honest — a card carries a name field and a button,
             so without it the `1fr` tracks would grow to their content and run
             off the side of a phone. */
          style={{
            gridTemplateColumns: gridTemplateColumns(density),
            gap: `${String(REVIEW_GRID_SCOPE.gapPx)}px`,
          }}
        >
          {state.clusters.map((cluster) => (
            <ClusterCard
              key={cluster.uid}
              cluster={cluster}
              busy={busyUid === cluster.uid}
              onAssign={(req) => {
                void assign(cluster.uid, req)
              }}
              onRemoveFace={(ref) => {
                void removeFace(cluster.uid, ref)
              }}
            />
          ))}
        </div>
      )}
    </>
  )
}
