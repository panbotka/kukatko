import type { TFunction } from 'i18next'
import { useEffect, useMemo, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import Col from 'react-bootstrap/Col'
import Modal from 'react-bootstrap/Modal'
import Row from 'react-bootstrap/Row'
import { useTranslation } from 'react-i18next'

import { EmptyState } from '../components/EmptyState'
import { FilterBar } from '../components/library/FilterBar'
import { GridSkeleton } from '../components/library/GridSkeleton'
import { SelectionBar } from '../components/organize/SelectionBar'
import { TrashCard } from '../components/trash/TrashCard'
import { usePaginatedPhotos } from '../hooks/usePaginatedPhotos'
import { useSelection } from '../hooks/useSelection'
import { LIBRARY_DEFAULTS, type LibraryView, viewToParams } from '../lib/libraryView'
import { useUrlState } from '../lib/urlState'
import { ApiError } from '../services/auth'
import {
  emptyTrash,
  fetchPhotos,
  fetchTrashInfo,
  purgePhoto,
  unarchivePhoto,
} from '../services/photos'

/** A pending permanent-delete confirmation: one photo, the selection, or all. */
type Confirm =
  | { mode: 'single'; uid: string }
  | { mode: 'bulk'; uids: string[] }
  | { mode: 'empty' }

/**
 * Resolves a failed mutation to a localized, user-facing message. Raw server
 * text is never surfaced: a 503 maps to the "disabled in configuration" string
 * and everything else to the generic action-failed message, both translated.
 */
function actionMessage(err: unknown, t: TFunction): string {
  if (err instanceof ApiError && err.status === 503) {
    return t('trash.unavailable')
  }
  return t('trash.actionError')
}

/**
 * The trash: every archived (soft-deleted) photo, with the standard filter/sort
 * bar over a grid of cards. Each card shows how long until the photo is
 * auto-purged and offers Restore (unarchive) and Delete forever. A selection mode
 * supports bulk restore and bulk permanent delete, and an Empty trash action
 * purges everything at once. All permanent deletions go through an explicit
 * confirmation dialog. The view state (filters, sort) lives in the URL.
 */
export function TrashPage() {
  const { t } = useTranslation()
  const [view, setView] = useUrlState<LibraryView>(LIBRARY_DEFAULTS)
  const selection = useSelection()

  const [retentionDays, setRetentionDays] = useState(0)
  const [reloadKey, setReloadKey] = useState(0)
  const [pending, setPending] = useState(false)
  const [actionError, setActionError] = useState<string | null>(null)
  const [confirm, setConfirm] = useState<Confirm | null>(null)

  // Always scope the listing to archived photos; the remaining filters/sort apply
  // on top. The reload key forces a refetch after a restore or purge mutates the set.
  const params = useMemo(() => ({ ...viewToParams(view), archived: 'only' as const }), [view])
  const { photos, total, status, loadingMore, moreError, hasMore, loadMore, retry } =
    usePaginatedPhotos(params, fetchPhotos, { key: String(reloadKey) })

  useEffect(() => {
    const controller = new AbortController()
    fetchTrashInfo(controller.signal)
      .then((info) => {
        setRetentionDays(info.retention_days)
      })
      .catch(() => {
        // A missing retention window only hides the countdown; not worth surfacing.
      })
    return () => {
      controller.abort()
    }
  }, [])

  const reload = () => {
    setReloadKey((key) => key + 1)
  }

  // run applies an async mutation over the given uids, surfacing any failure and
  // refreshing the list (and clearing the selection) once it settles.
  const run = async (uids: string[], op: (uid: string) => Promise<void>) => {
    setPending(true)
    setActionError(null)
    try {
      for (const uid of uids) {
        await op(uid)
      }
      selection.clear()
      reload()
    } catch (err) {
      setActionError(actionMessage(err, t))
    } finally {
      setPending(false)
    }
  }

  const restore = (uids: string[]) => run(uids, unarchivePhoto)

  const confirmDelete = async () => {
    if (confirm === null) {
      return
    }
    const pendingConfirm = confirm
    setConfirm(null)
    if (pendingConfirm.mode === 'empty') {
      setPending(true)
      setActionError(null)
      try {
        await emptyTrash()
        selection.clear()
        reload()
      } catch (err) {
        setActionError(actionMessage(err, t))
      } finally {
        setPending(false)
      }
      return
    }
    const uids = pendingConfirm.mode === 'single' ? [pendingConfirm.uid] : pendingConfirm.uids
    await run(uids, purgePhoto)
  }

  const selected = [...selection.selected]

  return (
    <>
      <div className="d-flex justify-content-between align-items-center mb-3 flex-wrap gap-2">
        <h1 className="kk-page-title mb-0">{t('trash.title')}</h1>
        <div className="d-flex gap-1 flex-wrap">
          {!selection.active && (
            <Button variant="outline-secondary" size="sm" onClick={selection.enable}>
              {t('library.select')}
            </Button>
          )}
          <Button
            variant="outline-danger"
            size="sm"
            disabled={pending || (status === 'ready' && photos.length === 0)}
            onClick={() => {
              setConfirm({ mode: 'empty' })
            }}
          >
            {t('trash.emptyTrash')}
          </Button>
        </div>
      </div>

      {selection.active && (
        <SelectionBar count={selection.count} onCancel={selection.disable}>
          <Button
            variant="outline-secondary"
            size="sm"
            disabled={photos.length === 0}
            onClick={() => {
              selection.selectMany(photos.map((p) => p.uid))
            }}
          >
            {t('library.selectAll')}
          </Button>
          <Button
            variant="primary"
            size="sm"
            disabled={pending || selection.count === 0}
            onClick={() => {
              void restore(selected)
            }}
          >
            {t('trash.restoreSelected')}
          </Button>
          <Button
            variant="danger"
            size="sm"
            disabled={pending || selection.count === 0}
            onClick={() => {
              setConfirm({ mode: 'bulk', uids: selected })
            }}
          >
            {t('trash.deleteSelected')}
          </Button>
        </SelectionBar>
      )}

      {/* No density picker: the trash lists retention cards, not the photo grid. */}
      <FilterBar view={view} onChange={setView} total={total} showDensity={false} />

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

      {status === 'error' && (
        <Alert variant="danger" className="d-flex align-items-center justify-content-between">
          <span>{t('library.error.load')}</span>
          <Button variant="outline-light" size="sm" onClick={retry}>
            {t('library.error.retry')}
          </Button>
        </Alert>
      )}

      {status === 'ready' && photos.length === 0 && (
        <EmptyState title={t('trash.empty.title')} hint={t('trash.empty.hint')} />
      )}

      {status === 'ready' && photos.length > 0 && (
        <>
          <Row xs={2} sm={3} md={4} lg={5} className="g-3">
            {photos.map((photo) => (
              <Col key={photo.uid}>
                <TrashCard
                  photo={photo}
                  retentionDays={retentionDays}
                  selected={selection.selected.has(photo.uid)}
                  busy={pending}
                  onToggleSelect={selection.toggle}
                  onRestore={(uid) => {
                    void restore([uid])
                  }}
                  onDelete={(uid) => {
                    setConfirm({ mode: 'single', uid })
                  }}
                />
              </Col>
            ))}
          </Row>

          {hasMore && (
            <div className="text-center mt-3">
              <Button
                variant="outline-secondary"
                size="sm"
                disabled={loadingMore}
                onClick={loadMore}
              >
                {loadingMore ? t('library.loadingMore') : t('trash.loadMore')}
              </Button>
              {moreError && <div className="text-danger small mt-2">{t('library.error.more')}</div>}
            </div>
          )}
        </>
      )}

      <Modal
        show={confirm !== null}
        onHide={() => {
          setConfirm(null)
        }}
        centered
      >
        <Modal.Header closeButton>
          <Modal.Title>{t('trash.confirm.title')}</Modal.Title>
        </Modal.Header>
        <Modal.Body>
          {confirm?.mode === 'empty' && t('trash.confirm.empty')}
          {confirm?.mode === 'single' && t('trash.confirm.single')}
          {confirm?.mode === 'bulk' && t('trash.confirm.bulk', { count: confirm.uids.length })}
        </Modal.Body>
        <Modal.Footer>
          <Button
            variant="secondary"
            onClick={() => {
              setConfirm(null)
            }}
          >
            {t('trash.confirm.cancel')}
          </Button>
          <Button
            variant="danger"
            onClick={() => {
              void confirmDelete()
            }}
          >
            {t('trash.confirm.confirm')}
          </Button>
        </Modal.Footer>
      </Modal>
    </>
  )
}
