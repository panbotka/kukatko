import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link, useLocation, useNavigate, useParams } from 'react-router-dom'

import { BackLink } from '../components/BackLink'
import { EmptyState } from '../components/EmptyState'
import { ErrorState } from '../components/ErrorState'
import { Icon } from '../components/Icon'
import { GridSkeleton } from '../components/library/GridSkeleton'
import { PhotoGrid } from '../components/library/PhotoGrid'
import { useDocumentTitle } from '../hooks/useDocumentTitle'
import { useGridScrollMemory } from '../hooks/useGridScrollMemory'
import { useReloadKey } from '../hooks/useReloadKey'
import { directEntryState, isFirstEntry } from '../lib/directEntry'
import { formatDateTime } from '../lib/format'
import { gridScrollKey } from '../lib/gridScroll'
import { LIBRARY_PATH } from '../lib/libraryView'
import { isNotFound } from '../services/auth'
import {
  fetchNotification,
  markNotificationRead,
  type NotificationDetail,
} from '../services/notifications'

/**
 * Fetch lifecycle of the notification. `missing` is a 404 kept apart from
 * `error`: a notification purged by retention, or a uid that belongs to somebody
 * else, is gone for good and gets its own friendly message, while a failed load
 * is worth retrying.
 */
type State =
  | { status: 'loading' }
  | { status: 'error' }
  | { status: 'missing' }
  | { status: 'ready'; detail: NotificationDetail }

/**
 * The one photograph a notification should forward straight to, or undefined
 * when it should render its own page. Exactly one visible photo *and* nothing
 * dropped: a set that shrank to one because the rest became private must still
 * say so, and it cannot once the reader is inside the viewer.
 */
function soleVisiblePhoto(detail: NotificationDetail): string | undefined {
  if (detail.photos.length !== 1 || detail.dropped_count > 0) {
    return undefined
  }
  return detail.photos[0]?.uid
}

/**
 * The deeplink page of one notification (`/n/:uid`) — the address a push
 * carries, kept short because the push payload has a hard size limit.
 *
 * It is a junction, not a destination. The notification's photographs are a
 * **frozen set** (the ones it was about, in their order, not a live search a
 * later tag would change), filtered by what the reader may see *now*:
 *
 *  - exactly one photograph → straight to the viewer, replacing this entry, so a
 *    grid of one tile never costs an extra tap and Back does not bounce here;
 *  - several → the set as the library's own virtualized grid, headed by the
 *    notification's title and body, so the reader sees what they tapped;
 *  - none → an empty state that says so.
 *
 * When photos were dropped since (archived, hidden, made private) the page says
 * how many, so the count in the notification never looks like a lie.
 *
 * The notification is marked read once per page visit, after it loaded and only
 * while it was still unread. A 404 is a friendly page with a way back to the
 * library, never a raw error.
 */
export function NotificationPage() {
  const { uid = '' } = useParams<{ uid: string }>()
  const { t, i18n } = useTranslation()
  const location = useLocation()
  const navigate = useNavigate()
  const [state, setState] = useState<State>({ status: 'loading' })
  const [reloadKey, reload] = useReloadKey()

  // Whether this page is where the visit started — a push opening a fresh
  // window, or the sign-in round trip that replaced its way here. Captured at
  // mount: the forward below replaces this entry, and a viewer reached that way
  // must reconstruct its way out rather than step back into nothing.
  const firstEntryRef = useRef(isFirstEntry(location.key))
  // The uid already marked read, so a re-render (or a retried load) never posts
  // a second time.
  const markedRef = useRef<string | null>(null)

  useEffect(() => {
    const controller = new AbortController()
    setState({ status: 'loading' })
    fetchNotification(uid, controller.signal)
      .then((detail) => {
        setState({ status: 'ready', detail })
      })
      .catch((err: unknown) => {
        if (err instanceof DOMException && err.name === 'AbortError') {
          return
        }
        setState({ status: isNotFound(err) ? 'missing' : 'error' })
      })
    return () => {
      controller.abort()
    }
  }, [uid, reloadKey])

  // Opening the page is reading the notification. Once it is known to exist —
  // a 404 has nothing to mark — and only while still unread: Back out of the
  // viewer remounts this page, and that visit has nothing left to record.
  useEffect(() => {
    if (state.status !== 'ready' || markedRef.current === uid) {
      return
    }
    markedRef.current = uid
    if (state.detail.read_at !== null) {
      return
    }
    // Fire and forget: failing to record the reading is no reason to keep the
    // reader from the photos, and the endpoint is idempotent for the next visit.
    markNotificationRead(uid).catch(() => undefined)
  }, [state, uid])

  const sole = state.status === 'ready' ? soleVisiblePhoto(state.detail) : undefined
  useEffect(() => {
    if (sole === undefined) {
      return
    }
    void navigate(`/photos/${encodeURIComponent(sole)}`, {
      replace: true,
      state: firstEntryRef.current ? directEntryState() : undefined,
    })
  }, [sole, navigate])

  useDocumentTitle(state.status === 'ready' ? state.detail.title : t('notification.title'))

  // Where the grid was left, so the viewer's way back returns to the tile it
  // was opened from. The set is loaded whole, so there is no count to restore.
  const gridScroll = useGridScrollMemory({
    key: gridScrollKey(location.pathname, location.search),
  })

  const backToLibrary = <BackLink to={LIBRARY_PATH} label={t('notification.back')} />

  if (state.status === 'missing') {
    return (
      <EmptyState
        title={t('notification.missing.title')}
        hint={t('notification.missing.hint')}
        action={
          <Link to={LIBRARY_PATH} className="btn btn-primary">
            {t('notification.back')}
          </Link>
        }
      />
    )
  }

  if (state.status === 'error') {
    return <ErrorState title={t('notification.error')} onRetry={reload} action={backToLibrary} />
  }

  // Loading, and the moment between knowing the one photograph and the viewer
  // taking over: a skeleton, never a flash of a one-tile grid.
  if (state.status === 'loading' || sole !== undefined) {
    return <GridSkeleton />
  }

  const { detail } = state
  const dropped = detail.dropped_count
  const empty = detail.photos.length === 0

  return (
    <>
      <div className="mb-3">
        {backToLibrary}
        <h1 className="kk-page-title mt-2 mb-1">{detail.title}</h1>
        {detail.body !== '' && <p className="mb-1">{detail.body}</p>}
        <p className="kk-text-caption text-secondary mb-0">
          <time dateTime={detail.created_at}>
            {formatDateTime(detail.created_at, i18n.language)}
          </time>
        </p>
      </div>

      {empty ? (
        <EmptyState
          title={t('notification.empty.title')}
          hint={
            dropped > 0
              ? t('notification.dropped', { count: dropped })
              : t('notification.empty.hint')
          }
        />
      ) : (
        <>
          {dropped > 0 && (
            <p className="text-secondary mb-3" data-testid="notification-dropped">
              <Icon name="eye-slash" className="me-2" />
              {t('notification.dropped', { count: dropped })}
            </p>
          )}
          <PhotoGrid
            photos={detail.photos}
            loadingMore={false}
            moreError={false}
            onRetry={reload}
            favoritable
            scroll={gridScroll}
          />
        </>
      )}
    </>
  )
}
