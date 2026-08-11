import type { TFunction } from 'i18next'
import { useCallback, useEffect, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'
import { Virtuoso } from 'react-virtuoso'

import { EmptyState } from '../components/EmptyState'
import { ErrorState } from '../components/ErrorState'
import { Icon } from '../components/Icon'
import { GridDensityControl } from '../components/library/GridDensityControl'
import { GridSkeleton } from '../components/library/GridSkeleton'
import { DuplicateMarkerGroupCard } from '../components/people/DuplicateMarkerGroupCard'
import { useDocumentTitle } from '../hooks/useDocumentTitle'
import { useGridDensity } from '../hooks/useGridDensity'
import { dropMarker, groupKey, MIN_GROUP_SIZE, removeGroup } from '../lib/duplicateMarkers'
import { REVIEW_GRID_SCOPE } from '../lib/gridDensity'
import { ApiError } from '../services/auth'
import {
  type DuplicateMarkerGroup,
  fetchDuplicateMarkers,
  invalidateMarker,
  keepMarker,
} from '../services/dupmarkers'
import { dismissDuplicateMarkers } from '../services/feedback'

/** Page size for the findings listing. */
const PAGE_SIZE = 20

/** Top-level load status of the repeated-marker view. */
type Status = 'loading' | 'ready' | 'error' | 'unavailable'

/**
 * Resolves a failed decision to a localized, user-facing message. Raw server text
 * is never surfaced: a 503 maps to the "not available" string, a 404 to "the
 * group moved on" (somebody else fixed it, or a re-detection redrew the boxes),
 * and everything else to the generic action-failed message.
 */
function actionMessage(err: unknown, t: TFunction): string {
  if (err instanceof ApiError && err.status === 503) {
    return t('duplicateMarkers.unavailable')
  }
  if (err instanceof ApiError && err.status === 404) {
    return t('duplicateMarkers.stale')
  }
  return t('duplicateMarkers.actionError')
}

/**
 * The repeated-marker review page (editor/admin): every photo where one and the
 * same person is tagged more than once, worst first.
 *
 * It is always a mistake, and a costly one — on a group shot the matcher put one
 * name on two or three neighbouring boxes, so the people beside her lost their
 * tag and her own face count is inflated. The page is deliberately curatorial:
 * nothing is merged or deleted on its own, and each finding is settled by an
 * explicit click. Keeping one marker **detaches** the others (they stay as
 * regions, ready to be handed to whoever they really show), flagging a box
 * invalid leaves its row in place, and "leave it be" records a durable opinion
 * for the genuine cases — a mirror, a double exposure, a photo of a photo — so
 * the group is not offered again on the next reload.
 *
 * A settled finding leaves the list at once and the next one moves up, the same
 * loop as `/review`: there are dozens of these, and they should take minutes.
 * The list is virtualized because a card is tall — a whole-photo preview plus one
 * close-up per marker — and twenty of them mounted at once is a lot of images.
 */
export function DuplicateMarkersPage() {
  const { t } = useTranslation()
  useDocumentTitle(t('duplicateMarkers.title'))
  const [groups, setGroups] = useState<DuplicateMarkerGroup[]>([])
  // The stepper sizes the **numbered crops inside a finding**, not the list of
  // findings: the page is a queue of questions, the judging happens within one.
  const { density } = useGridDensity(REVIEW_GRID_SCOPE)
  const [status, setStatus] = useState<Status>('loading')
  const [total, setTotal] = useState(0)
  const [nextOffset, setNextOffset] = useState<number | null>(null)
  const [loadingMore, setLoadingMore] = useState(false)
  const [moreError, setMoreError] = useState(false)
  const [busy, setBusy] = useState(false)
  const [actionError, setActionError] = useState<string | null>(null)
  const [resultMessage, setResultMessage] = useState<string | null>(null)

  // load fetches the page at the given offset, replacing the list on the first
  // page and appending afterwards. Status reflects the initial load only: a
  // failed append keeps the page `ready` with everything loaded so far and is
  // reported inline through `moreError`, so one bad "load more" never wipes the
  // list somebody is halfway through reviewing.
  const load = useCallback(async (offset: number, signal?: AbortSignal) => {
    try {
      const res = await fetchDuplicateMarkers({ limit: PAGE_SIZE, offset }, signal)
      setGroups((prev) => (offset === 0 ? res.groups : [...prev, ...res.groups]))
      setTotal(res.total)
      setNextOffset(res.next_offset)
      setMoreError(false)
      setStatus('ready')
    } catch (err) {
      if (signal?.aborted === true) {
        return
      }
      if (offset > 0) {
        setMoreError(true)
        return
      }
      setStatus(err instanceof ApiError && err.status === 503 ? 'unavailable' : 'error')
    }
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    void load(0, controller.signal)
    return () => {
      controller.abort()
    }
  }, [load])

  const loadMore = () => {
    if (nextOffset === null || loadingMore) {
      return
    }
    setLoadingMore(true)
    setMoreError(false)
    void load(nextOffset).finally(() => {
      setLoadingMore(false)
    })
  }

  // decide runs one write and, on success, applies `settle` to the list so the
  // card leaves (or shrinks) without a refetch — a refetch would renumber and
  // reorder everything under the pointer mid-review. Every decision is serialized
  // behind one page-level `busy` flag: they are quick, and two in flight over the
  // same photo could race each other's view of the group.
  const decide = useCallback(
    async (
      run: () => Promise<void>,
      settle: (prev: DuplicateMarkerGroup[]) => DuplicateMarkerGroup[],
      message: string,
    ) => {
      if (busy) {
        return
      }
      setBusy(true)
      setActionError(null)
      try {
        await run()
        setGroups(settle)
        // The total is the size of the whole finding set, not of the loaded page,
        // so it has to be decremented by hand — the alternative is a refetch.
        setTotal((prev) => Math.max(prev - 1, 0))
        setResultMessage(message)
      } catch (err) {
        setActionError(actionMessage(err, t))
      } finally {
        setBusy(false)
      }
    },
    [busy, t],
  )

  const handleKeep = useCallback(
    (group: DuplicateMarkerGroup, markerUid: string) => {
      const key = groupKey(group)
      void decide(
        async () => {
          await keepMarker({
            photo_uid: group.photo_uid,
            subject_uid: group.subject_uid,
            keep_marker_uid: markerUid,
          })
        },
        (prev) => removeGroup(prev, key),
        t('duplicateMarkers.kept', {
          name: group.subject_name,
          count: group.markers.length - 1,
        }),
      )
    },
    [decide, t],
  )

  const handleInvalid = useCallback(
    (group: DuplicateMarkerGroup, markerUid: string) => {
      const key = groupKey(group)
      // Flagging one box does not necessarily settle the finding — a three-marker
      // group is still a finding at two — so the total only moves when the card
      // actually leaves, which the card's own marker count already tells us.
      // That is why this one does not go through `decide`.
      const settles = group.markers.length - 1 < MIN_GROUP_SIZE
      if (busy) {
        return
      }
      setBusy(true)
      setActionError(null)
      void invalidateMarker(markerUid)
        .then(() => {
          setGroups((prev) => dropMarker(prev, key, markerUid))
          if (settles) {
            setTotal((current) => Math.max(current - 1, 0))
          }
          setResultMessage(t('duplicateMarkers.invalidated'))
        })
        .catch((err: unknown) => {
          setActionError(actionMessage(err, t))
        })
        .finally(() => {
          setBusy(false)
        })
    },
    [busy, t],
  )

  const handleDismiss = useCallback(
    (group: DuplicateMarkerGroup) => {
      const key = groupKey(group)
      void decide(
        async () => {
          await dismissDuplicateMarkers({
            photo_uid: group.photo_uid,
            subject_uid: group.subject_uid,
          })
        },
        (prev) => removeGroup(prev, key),
        t('duplicateMarkers.dismissed', { name: group.subject_name }),
      )
    },
    [decide, t],
  )

  return (
    <>
      {/* `flex-md-nowrap` is what actually puts the stepper beside the title: a
          flex line is laid out from each item's *max-content* width, and the
          subtitle's is a whole paragraph, so with wrapping left on at every width
          the control always dropped to a second line and sat at its left edge —
          `justify-content-between` never got to do anything. Below `md` the wrap
          is right, and there the control belongs under the text rather than
          squeezed beside it. */}
      <div className="mb-3 d-flex flex-wrap flex-md-nowrap align-items-start justify-content-between gap-3">
        <div>
          <h1 className="kk-page-title mb-1">{t('duplicateMarkers.title')}</h1>
          <p className="text-secondary mb-0">{t('duplicateMarkers.subtitle')}</p>
        </div>
        {status === 'ready' && groups.length > 0 && (
          <GridDensityControl scope={REVIEW_GRID_SCOPE} />
        )}
      </div>

      {resultMessage !== null && (
        <Alert
          variant="success"
          dismissible
          onClose={() => {
            setResultMessage(null)
          }}
        >
          {resultMessage}
        </Alert>
      )}

      {actionError !== null && (
        <Alert
          variant="danger"
          dismissible
          onClose={() => {
            setActionError(null)
          }}
        >
          {actionError}
        </Alert>
      )}

      {status === 'loading' && <GridSkeleton />}

      {status === 'unavailable' && (
        <Alert variant="warning">{t('duplicateMarkers.unavailable')}</Alert>
      )}

      {status === 'error' && (
        <ErrorState
          title={t('duplicateMarkers.error')}
          onRetry={() => {
            setStatus('loading')
            void load(0)
          }}
        />
      )}

      {status === 'ready' && groups.length === 0 && (
        <EmptyState
          icon={<Icon name="person-check" />}
          title={t('duplicateMarkers.empty.title')}
          hint={t('duplicateMarkers.empty.hint')}
        />
      )}

      {status === 'ready' && groups.length > 0 && (
        <>
          <p className="text-secondary small" data-testid="dup-marker-count">
            {t('duplicateMarkers.remaining', { count: total })}
          </p>
          <Virtuoso
            useWindowScroll
            data={groups}
            computeItemKey={(_index, group) => groupKey(group)}
            itemContent={(_index, group) => (
              <DuplicateMarkerGroupCard
                group={group}
                busy={busy}
                onKeep={handleKeep}
                onInvalid={handleInvalid}
                onDismiss={handleDismiss}
                density={density}
              />
            )}
          />
        </>
      )}

      {status === 'ready' && nextOffset !== null && (
        <div className="text-center mt-3">
          <Button variant="outline-secondary" size="sm" disabled={loadingMore} onClick={loadMore}>
            {loadingMore ? (
              <Spinner animation="border" size="sm" />
            ) : (
              t('duplicateMarkers.loadMore')
            )}
          </Button>
          {/* The button above doubles as the retry: the failed page can simply be
              requested again, with the findings already loaded left in place. */}
          {moreError && (
            <div className="text-danger small mt-2">{t('duplicateMarkers.moreError')}</div>
          )}
        </div>
      )}
    </>
  )
}
