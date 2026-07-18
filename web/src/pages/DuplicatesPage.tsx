import type { TFunction } from 'i18next'
import { useCallback, useEffect, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'

import { EmptyState } from '../components/EmptyState'
import { ErrorState } from '../components/ErrorState'
import { DuplicateGroupCard } from '../components/duplicates/DuplicateGroupCard'
import { MergeConfirmModal } from '../components/duplicates/MergeConfirmModal'
import { GridSkeleton } from '../components/library/GridSkeleton'
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
  const [groups, setGroups] = useState<DuplicateGroup[]>([])
  const [status, setStatus] = useState<Status>('loading')
  const [nextOffset, setNextOffset] = useState<number | null>(null)
  const [loadingMore, setLoadingMore] = useState(false)
  const [busyGroupId, setBusyGroupId] = useState<string | null>(null)
  const [pending, setPending] = useState<PendingMerge | null>(null)
  const [confirming, setConfirming] = useState(false)
  const [actionError, setActionError] = useState<string | null>(null)
  const [resultMessage, setResultMessage] = useState<string | null>(null)

  // load fetches the page at the given offset, replacing the list on the first
  // page and appending afterwards. Status reflects the initial load only.
  const load = useCallback(async (offset: number, signal?: AbortSignal) => {
    try {
      const res = await fetchDuplicates({ limit: PAGE_SIZE, offset }, signal)
      setGroups((prev) => (offset === 0 ? res.groups : [...prev, ...res.groups]))
      setNextOffset(res.next_offset)
      setStatus('ready')
    } catch (err) {
      if (signal?.aborted === true) {
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
    if (nextOffset === null) {
      return
    }
    setLoadingMore(true)
    void load(nextOffset).finally(() => {
      setLoadingMore(false)
    })
  }

  // remove drops a group from the view by id (after merge or dismiss).
  const remove = (groupId: string) => {
    setGroups((prev) => prev.filter((group) => group.id !== groupId))
  }

  // beginResolve previews the merge for the chosen keeper and, on success, opens
  // the confirmation modal with what would move. The group stays until confirmed.
  const beginResolve = async (group: DuplicateGroup, keeperUid: string) => {
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
      <div className="mb-3">
        <h1 className="kk-page-title mb-1">{t('duplicates.title')}</h1>
        <p className="text-secondary mb-0">{t('duplicates.subtitle')}</p>
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

      {status === 'unavailable' && <Alert variant="warning">{t('duplicates.unavailable')}</Alert>}

      {status === 'error' && (
        <ErrorState
          title={t('duplicates.error')}
          onRetry={() => {
            setStatus('loading')
            void load(0)
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
            busy={busyGroupId === group.id}
            onResolve={(g, keeperUid) => {
              void beginResolve(g, keeperUid)
            }}
            onDismiss={dismiss}
          />
        ))}

      {status === 'ready' && nextOffset !== null && (
        <div className="text-center mt-3">
          <Button variant="outline-secondary" size="sm" disabled={loadingMore} onClick={loadMore}>
            {loadingMore ? <Spinner animation="border" size="sm" /> : t('duplicates.loadMore')}
          </Button>
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
