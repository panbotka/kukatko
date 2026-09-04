import { useCallback, useEffect, useRef, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'

import { EmptyState } from '../components/EmptyState'
import { ErrorState } from '../components/ErrorState'
import { GridDensityControl } from '../components/library/GridDensityControl'
import { GridSkeleton } from '../components/library/GridSkeleton'
import { ClusterCard } from '../components/people/ClusterCard'
import { ReviewGrid } from '../components/review/ReviewGrid'
import { useDocumentTitle } from '../hooks/useDocumentTitle'
import { useGridDensity } from '../hooks/useGridDensity'
import { REVIEW_GRID_SCOPE } from '../lib/gridDensity'
import {
  assignCluster,
  type ClusterAssignRequest,
  type ClusterView,
  fetchClusters,
  removeClusterFace,
  type RemoveFaceRequest,
} from '../services/people'

import '../components/review/review.css'

/**
 * How many groups one request asks for. A screenful and a bit: enough that the
 * first page fills the grid, small enough that it arrives at once even on a
 * library with thousands of groups.
 */
const PAGE_SIZE = 24

/** Top-level load status of the cluster queue (the first page's). */
type Status = 'loading' | 'error' | 'ready'

/**
 * The cluster review queue: unnamed face groups, each named in one action
 * (assigning every face to a new or existing subject). It is the primary, fast
 * path for bulk people-tagging. Naming a group removes it from the list;
 * detaching a stray face refreshes (or drops) that group in place. The whole
 * flow is editor/admin-only (the route guards it) and updates optimistically.
 *
 * The queue arrives in pages and is virtualized: the first groups appear as soon
 * as the first page lands, more load as the reader scrolls, and only the rows in
 * view are ever mounted — a library with thousands of groups opens as fast as
 * one with ten.
 *
 * A group is listed only once the server has prepared its cached summary. While
 * groups are still being prepared the page says so, with the count, rather than
 * spinning: preparing them is background work that opening the page schedules.
 */
export function ClustersPage() {
  const { t } = useTranslation()
  useDocumentTitle(t('clusters.title'))
  const [clusters, setClusters] = useState<ClusterView[]>([])
  const [ready, setReady] = useState(0)
  const [pending, setPending] = useState(0)
  const [nextOffset, setNextOffset] = useState<number | null>(null)
  const [status, setStatus] = useState<Status>('loading')
  const [loadingMore, setLoadingMore] = useState(false)
  const [moreError, setMoreError] = useState(false)
  const [busyUid, setBusyUid] = useState<string | null>(null)
  const [actionError, setActionError] = useState(false)
  // Guards the scroll-driven append: virtuoso can fire `endReached` again while
  // a page is still in flight, and a second request for the same offset would
  // append the same groups twice.
  const loadingRef = useRef(false)
  // The cluster grid is the one the user sizes here: a card is a face plus a
  // name field, and how many of them fit across is the same choice every other
  // review tool offers, on the same stored number.
  const { density } = useGridDensity(REVIEW_GRID_SCOPE)

  // load fetches the page at the given offset, replacing the list on the first
  // page and appending afterwards. `status` reflects the initial load only: a
  // failed append keeps the page `ready` with every group loaded so far and is
  // reported inline through `moreError` instead, so one bad append never wipes
  // the queue the reader is halfway through.
  const load = useCallback(async (offset: number, signal?: AbortSignal) => {
    loadingRef.current = true
    try {
      const page = await fetchClusters({ limit: PAGE_SIZE, offset }, signal)
      setClusters((prev) => (offset === 0 ? page.clusters : [...prev, ...page.clusters]))
      setReady(page.total)
      setPending(page.pending)
      setNextOffset(page.next_offset)
      setMoreError(false)
      setStatus('ready')
    } catch (err) {
      if (signal?.aborted === true || (err instanceof DOMException && err.name === 'AbortError')) {
        return
      }
      if (offset > 0) {
        setMoreError(true)
        return
      }
      setStatus('error')
    } finally {
      loadingRef.current = false
    }
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    void load(0, controller.signal)
    return () => {
      controller.abort()
    }
  }, [load])

  // reload restarts from the first page: the retry after a failure, and the
  // "look again" once the background preparation has had a moment.
  const reload = useCallback(() => {
    setStatus('loading')
    setMoreError(false)
    void load(0)
  }, [load])

  const loadMore = useCallback(() => {
    if (nextOffset === null || loadingRef.current) {
      return
    }
    setLoadingMore(true)
    setMoreError(false)
    void load(nextOffset).finally(() => {
      setLoadingMore(false)
    })
  }, [load, nextOffset])

  // drop removes a group from the queue and from the ready count, so the "still
  // being prepared" line stays truthful as groups are consumed.
  const drop = useCallback((uid: string) => {
    setClusters((prev) => prev.filter((c) => c.uid !== uid))
    setReady((prev) => Math.max(prev - 1, 0))
  }, [])

  const assign = useCallback(
    async (uid: string, req: ClusterAssignRequest) => {
      setBusyUid(uid)
      setActionError(false)
      try {
        await assignCluster(uid, req)
        // The cluster is consumed server-side: drop it from the queue.
        drop(uid)
      } catch {
        setActionError(true)
      } finally {
        setBusyUid(null)
      }
    },
    [drop],
  )

  const removeFace = useCallback(
    async (uid: string, ref: RemoveFaceRequest) => {
      setBusyUid(uid)
      setActionError(false)
      try {
        const refreshed = await removeClusterFace(uid, ref)
        if (refreshed === null) {
          drop(uid)
          return
        }
        setClusters((prev) => prev.map((c) => (c.uid === uid ? refreshed : c)))
      } catch {
        setActionError(true)
      } finally {
        setBusyUid(null)
      }
    },
    [drop],
  )

  const hasCards = clusters.length > 0

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
        {status === 'ready' && hasCards && <GridDensityControl scope={REVIEW_GRID_SCOPE} />}
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

      {/* The groups the server has not prepared yet, in the reader's own words
          and with the count — the alternative was a spinner that never ended,
          because preparing them is background work that takes as long as it
          takes. */}
      {status === 'ready' && pending > 0 && (
        <Alert variant="info" className="d-flex flex-wrap align-items-center gap-2">
          <span>{t('clusters.preparing', { ready, pending })}</span>
          <Button variant="outline-light" size="sm" className="ms-auto" onClick={reload}>
            {t('clusters.refresh')}
          </Button>
        </Alert>
      )}

      {status === 'loading' && <GridSkeleton label={t('clusters.loading')} />}

      {status === 'error' && <ErrorState title={t('clusters.error')} onRetry={reload} />}

      {status === 'ready' && !hasCards && pending === 0 && (
        <EmptyState title={t('clusters.empty.title')} hint={t('clusters.empty.hint')} />
      )}

      {status === 'ready' && hasCards && (
        <ReviewGrid
          items={clusters}
          density={density}
          itemKey={(cluster) => cluster.uid}
          onEndReached={loadMore}
          footer={
            nextOffset === null ? null : (
              <div className="text-center mt-3">
                {loadingMore && <Spinner animation="border" size="sm" />}
                {moreError && (
                  <>
                    <div className="text-danger small mb-2">{t('clusters.moreError')}</div>
                    <Button variant="outline-secondary" size="sm" onClick={loadMore}>
                      {t('clusters.loadMore')}
                    </Button>
                  </>
                )}
              </div>
            )
          }
          renderItem={(cluster) => (
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
          )}
        />
      )}
    </>
  )
}
