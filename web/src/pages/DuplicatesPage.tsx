import type { TFunction } from 'i18next'
import { useEffect, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'

import { EmptyState } from '../components/EmptyState'
import { ErrorState } from '../components/ErrorState'
import { RateLimitNotice } from '../components/RateLimitNotice'
import { DuplicateGroupCard } from '../components/duplicates/DuplicateGroupCard'
import { MergeConfirmModal } from '../components/duplicates/MergeConfirmModal'
import { GridDensityControl } from '../components/library/GridDensityControl'
import { GridSkeleton } from '../components/library/GridSkeleton'
import { useDocumentTitle } from '../hooks/useDocumentTitle'
import { useGridDensity } from '../hooks/useGridDensity'
import { useRateLimitRetry } from '../hooks/useRateLimitRetry'
import { useReloadKey } from '../hooks/useReloadKey'
import { REVIEW_GRID_SCOPE } from '../lib/gridDensity'
import { ApiError } from '../services/auth'
import {
  type DuplicateGroup,
  type MergeResult,
  fetchDuplicates,
  mergeDuplicates,
} from '../services/duplicates'

/** Page size for the duplicate-group listing. */
const PAGE_SIZE = 20

/** Top-level load status of the duplicates view. */
type Status = 'loading' | 'ready' | 'error' | 'unavailable'

/** A merge awaiting the user's confirmation: the group, chosen keeper and preview. */
interface PendingMerge {
  group: DuplicateGroup
  keeperUid: string
  preview: MergeResult
}

/**
 * Resolves a failed mutation to a localized, user-facing message. Raw server
 * text is never surfaced: a 503 maps to the "detection disabled" string and
 * everything else to the generic action-failed message, both translated.
 */
function actionMessage(err: unknown, t: TFunction): string {
  if (err instanceof ApiError && err.status === 503) {
    return t('duplicates.unavailable')
  }
  return t('duplicates.actionError')
}

/**
 * The duplicates review page (editor/admin): groups of likely-duplicate photos
 * shown side by side. For each group the user picks which photo to keep and
 * merges the rest into it — the keeper inherits every album, tag and person the
 * copies carried, and the copies are archived (recoverable from the trash) — or
 * dismisses the group as "not a duplicate". A preview of what will move is shown
 * for confirmation before anything changes; the server never merges on its own.
 */
export function DuplicatesPage() {
  const { t } = useTranslation()
  useDocumentTitle(t('duplicates.title'))
  const [groups, setGroups] = useState<DuplicateGroup[]>([])
  // The stepper here sizes the **members inside a group**, not the list of
  // groups: this page is a list of questions and the judging happens within one.
  const { density } = useGridDensity(REVIEW_GRID_SCOPE)
  const [status, setStatus] = useState<Status>('loading')
  const [nextOffset, setNextOffset] = useState<number | null>(null)
  const [loadingMore, setLoadingMore] = useState(false)
  const [moreError, setMoreError] = useState(false)
  const [busyGroupId, setBusyGroupId] = useState<string | null>(null)
  const [pending, setPending] = useState<PendingMerge | null>(null)
  const [confirming, setConfirming] = useState(false)
  const [actionError, setActionError] = useState<string | null>(null)
  const [resultMessage, setResultMessage] = useState<string | null>(null)

  // The first page loads on mount and again whenever `reloadKey` is bumped (an
  // explicit retry, or the rate-limit retry below). A 429 is not a failure: the
  // page keeps its loading state, says it is waiting for the server and retries
  // on its own (useRateLimitRetry); only another error, or a 429 that outlasts
  // the retries, lands in the error state.
  const [reloadKey, reload] = useReloadKey()
  const { waiting: firstWaiting, handle: handleFirstError, reset: resetFirst } = useRateLimitRetry()
  useEffect(() => {
    const controller = new AbortController()
    fetchDuplicates({ limit: PAGE_SIZE, offset: 0 }, controller.signal)
      .then((res) => {
        setGroups(res.groups)
        setNextOffset(res.next_offset)
        setMoreError(false)
        resetFirst()
        setStatus('ready')
      })
      .catch((err: unknown) => {
        if (controller.signal.aborted || handleFirstError(err, reload)) {
          return
        }
        setStatus(err instanceof ApiError && err.status === 503 ? 'unavailable' : 'error')
      })
    return () => {
      controller.abort()
    }
  }, [reloadKey, reload, handleFirstError, resetFirst])

  // A failed "load more" keeps the page `ready` with every group loaded so far
  // and is reported inline through `moreError` instead (the same way the library
  // grid and the trash handle it), so one bad page never wipes the list the user
  // is halfway through reviewing. A 429 there keeps the button busy, says the
  // server needs a moment, and asks for the same page again on its own.
  const { waiting: moreWaiting, handle: handleMoreError, reset: resetMore } = useRateLimitRetry()
  const fetchMore = async (offset: number) => {
    try {
      const res = await fetchDuplicates({ limit: PAGE_SIZE, offset })
      setGroups((prev) => [...prev, ...res.groups])
      setNextOffset(res.next_offset)
      resetMore()
    } catch (err) {
      if (
        handleMoreError(err, () => {
          void fetchMore(offset)
        })
      ) {
        return
      }
      setMoreError(true)
    }
    setLoadingMore(false)
  }

  const loadMore = () => {
    if (nextOffset === null || loadingMore) {
      return
    }
    setLoadingMore(true)
    setMoreError(false)
    void fetchMore(nextOffset)
  }

  // remove drops a group from the view by id (after merge or dismiss).
  const remove = (groupId: string) => {
    setGroups((prev) => prev.filter((group) => group.id !== groupId))
  }

  // Whether a resolve is in flight anywhere on the page. It locks *every*
  // group's actions, not just the busy card's: during a dry-run there is no
  // modal (and no backdrop) yet, so without a page-level lock a click on another
  // group would overwrite `busyGroupId` and silently discard the merge already
  // under way.
  const resolving = busyGroupId !== null

  // beginResolve previews the merge for the chosen keeper and, on success, opens
  // the confirmation modal with what would move. The group stays until confirmed.
  const beginResolve = async (group: DuplicateGroup, keeperUid: string) => {
    if (resolving) {
      return
    }
    setBusyGroupId(group.id)
    setActionError(null)
    setResultMessage(null)
    try {
      const preview = await mergeDuplicates({
        keeper_uid: keeperUid,
        member_uids: group.members.map((m) => m.uid),
        dry_run: true,
      })
      setPending({ group, keeperUid, preview })
    } catch (err) {
      setActionError(actionMessage(err, t))
      setBusyGroupId(null)
    }
  }

  // confirmResolve performs the merge the user confirmed and drops the group.
  const confirmResolve = async () => {
    if (pending === null) {
      return
    }
    setConfirming(true)
    try {
      const result = await mergeDuplicates({
        keeper_uid: pending.keeperUid,
        member_uids: pending.group.members.map((m) => m.uid),
      })
      remove(pending.group.id)
      setResultMessage(t('duplicates.merged', { count: result.archived }))
      setPending(null)
    } catch (err) {
      setActionError(actionMessage(err, t))
    } finally {
      setConfirming(false)
      setBusyGroupId(null)
    }
  }

  // cancelResolve closes the confirmation modal without merging.
  const cancelResolve = () => {
    setPending(null)
    setBusyGroupId(null)
  }

  const dismiss = (groupId: string) => {
    remove(groupId)
  }

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
          <h1 className="kk-page-title mb-1">{t('duplicates.title')}</h1>
          <p className="text-secondary mb-0">{t('duplicates.subtitle')}</p>
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

      {status === 'loading' && firstWaiting && <RateLimitNotice />}

      {status === 'loading' && <GridSkeleton />}

      {status === 'unavailable' && <Alert variant="warning">{t('duplicates.unavailable')}</Alert>}

      {status === 'error' && (
        <ErrorState
          title={t('duplicates.error')}
          onRetry={() => {
            resetFirst()
            setStatus('loading')
            reload()
          }}
        />
      )}

      {status === 'ready' && groups.length === 0 && (
        <EmptyState title={t('duplicates.empty.title')} hint={t('duplicates.empty.hint')} />
      )}

      {status === 'ready' &&
        groups.map((group) => (
          <DuplicateGroupCard
            key={group.id}
            group={group}
            busy={resolving}
            onResolve={(g, keeperUid) => {
              void beginResolve(g, keeperUid)
            }}
            onDismiss={dismiss}
            density={density}
          />
        ))}

      {status === 'ready' && nextOffset !== null && (
        <div className="text-center mt-3">
          <Button variant="outline-secondary" size="sm" disabled={loadingMore} onClick={loadMore}>
            {loadingMore ? <Spinner animation="border" size="sm" /> : t('duplicates.loadMore')}
          </Button>
          {/* The button above doubles as the retry: the failed page can simply
              be requested again, with the groups already loaded left in place. */}
          {moreWaiting && <RateLimitNotice variant="inline" />}
          {moreError && <div className="text-danger small mt-2">{t('duplicates.moreError')}</div>}
        </div>
      )}

      <MergeConfirmModal
        preview={pending?.preview ?? null}
        busy={confirming}
        onConfirm={() => {
          void confirmResolve()
        }}
        onCancel={cancelResolve}
      />
    </>
  )
}
