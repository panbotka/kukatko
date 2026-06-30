import type { TFunction } from 'i18next'
import { useCallback, useEffect, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'

import { DuplicateGroupCard } from '../components/duplicates/DuplicateGroupCard'
import { GridSkeleton } from '../components/library/GridSkeleton'
import { ApiError } from '../services/auth'
import { bulkUpdatePhotos } from '../services/bulk'
import { type DuplicateGroup, fetchDuplicates } from '../services/duplicates'

/** Page size for the duplicate-group listing. */
const PAGE_SIZE = 20

/** Top-level load status of the duplicates view. */
type Status = 'loading' | 'ready' | 'error' | 'unavailable'

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
 * archives the rest (through the bulk API, recoverable from the trash) or
 * dismisses the group as "not a duplicate", which removes it from the view. The
 * server never deletes anything on its own; every archive is an explicit choice.
 */
export function DuplicatesPage() {
  const { t } = useTranslation()
  const [groups, setGroups] = useState<DuplicateGroup[]>([])
  const [status, setStatus] = useState<Status>('loading')
  const [nextOffset, setNextOffset] = useState<number | null>(null)
  const [loadingMore, setLoadingMore] = useState(false)
  const [busyGroupId, setBusyGroupId] = useState<string | null>(null)
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

  // remove drops a group from the view by id (after archive or dismiss).
  const remove = (groupId: string) => {
    setGroups((prev) => prev.filter((group) => group.id !== groupId))
  }

  const resolve = async (group: DuplicateGroup, keeperUid: string) => {
    const archiveUids = group.members.map((m) => m.uid).filter((uid) => uid !== keeperUid)
    setBusyGroupId(group.id)
    setActionError(null)
    setResultMessage(null)
    try {
      const result = await bulkUpdatePhotos(archiveUids, { archive: true })
      remove(group.id)
      setResultMessage(t('duplicates.archived', { count: result.counts.updated }))
    } catch (err) {
      setActionError(actionMessage(err, t))
    } finally {
      setBusyGroupId(null)
    }
  }

  const dismiss = (groupId: string) => {
    remove(groupId)
  }

  return (
    <>
      <div className="mb-3">
        <h1 className="h3 mb-1">{t('duplicates.title')}</h1>
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
        <Alert variant="danger" className="d-flex align-items-center justify-content-between">
          <span>{t('duplicates.error')}</span>
          <Button
            variant="outline-light"
            size="sm"
            onClick={() => {
              setStatus('loading')
              void load(0)
            }}
          >
            {t('library.error.retry')}
          </Button>
        </Alert>
      )}

      {status === 'ready' && groups.length === 0 && (
        <div className="text-center text-secondary py-5">
          <p className="mb-1 fs-5">{t('duplicates.empty.title')}</p>
          <p className="mb-0 small">{t('duplicates.empty.hint')}</p>
        </div>
      )}

      {status === 'ready' &&
        groups.map((group) => (
          <DuplicateGroupCard
            key={group.id}
            group={group}
            busy={busyGroupId === group.id}
            onResolve={(g, keeperUid) => {
              void resolve(g, keeperUid)
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
    </>
  )
}
