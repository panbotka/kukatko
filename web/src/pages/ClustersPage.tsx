import { useCallback, useEffect, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Col from 'react-bootstrap/Col'
import Row from 'react-bootstrap/Row'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'

import { EmptyState } from '../components/EmptyState'
import { ClusterCard } from '../components/people/ClusterCard'
import {
  assignCluster,
  type ClusterAssignRequest,
  type ClusterView,
  fetchClusters,
  removeClusterFace,
  type RemoveFaceRequest,
} from '../services/people'

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
  const [state, setState] = useState<State>({ status: 'loading' })
  const [busyUid, setBusyUid] = useState<string | null>(null)
  const [actionError, setActionError] = useState(false)

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
      <h1 className="kk-page-title mb-1">{t('clusters.title')}</h1>
      <p className="text-secondary">{t('clusters.subtitle')}</p>

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

      {state.status === 'error' && <Alert variant="danger">{t('clusters.error')}</Alert>}

      {state.status === 'ready' && state.clusters.length === 0 && (
        <EmptyState title={t('clusters.empty.title')} hint={t('clusters.empty.hint')} />
      )}

      {state.status === 'ready' && state.clusters.length > 0 && (
        <Row xs={1} sm={2} lg={3} className="g-3">
          {state.clusters.map((cluster) => (
            <Col key={cluster.uid}>
              <ClusterCard
                cluster={cluster}
                busy={busyUid === cluster.uid}
                onAssign={(req) => {
                  void assign(cluster.uid, req)
                }}
                onRemoveFace={(ref) => {
                  void removeFace(cluster.uid, ref)
                }}
              />
            </Col>
          ))}
        </Row>
      )}
    </>
  )
}
