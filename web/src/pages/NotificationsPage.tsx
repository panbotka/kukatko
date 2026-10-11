import { useCallback, useEffect, useRef, useState } from 'react'
import Button from 'react-bootstrap/Button'
import ToggleButton from 'react-bootstrap/ToggleButton'
import ToggleButtonGroup from 'react-bootstrap/ToggleButtonGroup'
import { useTranslation } from 'react-i18next'
import { Link, useSearchParams } from 'react-router-dom'
import { Virtuoso } from 'react-virtuoso'

import { EmptyState } from '../components/EmptyState'
import { ErrorState } from '../components/ErrorState'
import { Icon } from '../components/Icon'
import { type GridContext, GridFooter } from '../components/library/PhotoGrid'
import { NotificationRow } from '../components/notifications/NotificationRow'
import { useDocumentTitle } from '../hooks/useDocumentTitle'
import { useReloadKey } from '../hooks/useReloadKey'
import { useNotificationInbox } from '../notifications/NotificationInboxContext'
import {
  fetchNotifications,
  markAllNotificationsRead,
  markNotificationRead,
  type NotificationRecord,
  notificationTarget,
} from '../services/notifications'

/** How many notifications one page of the infinite list asks for. */
export const PAGE_SIZE = 30

/** The query parameter holding the filter, so Back and a reload keep it. */
const UNREAD_PARAM = 'unread'

/** The list as it loads: the first page, then more as the reader scrolls. */
type State =
  | { status: 'loading' }
  | { status: 'error' }
  | {
      status: 'ready'
      items: NotificationRecord[]
      total: number
      loadingMore: boolean
      moreError: boolean
    }

/**
 * The whole in-app inbox (`/notifications`): every notification delivered
 * in-app, newest first, as an infinite list — the bell's dropdown shows only
 * the newest few and links here.
 *
 * The "all / unread" filter lives in the URL (`?unread=1`), so Back and a reload
 * keep it. The list is virtualized (`react-virtuoso` over the window scroll):
 * an inbox is kept for up to 90 days and can run long. The next page loads when
 * the end is reached; a failed page shows a retry in the footer instead of
 * dropping what was already listed.
 *
 * Choosing an entry marks it read (idempotently) and follows its link; "mark
 * all read" clears every unread one at once. Both refetch the bell's count.
 */
export function NotificationsPage() {
  const { t } = useTranslation()
  const { refresh } = useNotificationInbox()
  const [params, setParams] = useSearchParams()
  const unreadOnly = params.get(UNREAD_PARAM) === '1'
  const [state, setState] = useState<State>({ status: 'loading' })
  const [reloadKey, reload] = useReloadKey()
  const [markError, setMarkError] = useState(false)
  // The page request in flight, so a filter change cancels a stale one.
  const controller = useRef<AbortController | null>(null)

  useDocumentTitle(t('inbox.title'))

  useEffect(() => {
    const current = new AbortController()
    controller.current?.abort()
    controller.current = current
    setState({ status: 'loading' })
    fetchNotifications({ unread: unreadOnly, limit: PAGE_SIZE }, current.signal)
      .then((page) => {
        setState({
          status: 'ready',
          items: page.notifications,
          total: page.total,
          loadingMore: false,
          moreError: false,
        })
      })
      .catch((err: unknown) => {
        if (!(err instanceof DOMException && err.name === 'AbortError')) {
          setState({ status: 'error' })
        }
      })
    return () => {
      current.abort()
    }
  }, [unreadOnly, reloadKey])

  const loadMore = useCallback(() => {
    if (state.status !== 'ready' || state.loadingMore || state.items.length >= state.total) {
      return
    }
    const current = new AbortController()
    controller.current = current
    const offset = state.items.length
    setState({ ...state, loadingMore: true, moreError: false })
    fetchNotifications({ unread: unreadOnly, limit: PAGE_SIZE, offset }, current.signal)
      .then((page) => {
        setState((prev) => {
          if (prev.status !== 'ready' || controller.current !== current) {
            return prev
          }
          const known = new Set(prev.items.map((n) => n.uid))
          const fresh = page.notifications.filter((n) => !known.has(n.uid))
          return {
            ...prev,
            items: [...prev.items, ...fresh],
            total: page.total,
            loadingMore: false,
          }
        })
      })
      .catch((err: unknown) => {
        if (err instanceof DOMException && err.name === 'AbortError') {
          return
        }
        setState((prev) =>
          prev.status === 'ready' ? { ...prev, loadingMore: false, moreError: true } : prev,
        )
      })
  }, [state, unreadOnly])

  const setFilter = (value: string) => {
    const next = new URLSearchParams(params)
    if (value === 'unread') {
      next.set(UNREAD_PARAM, '1')
    } else {
      next.delete(UNREAD_PARAM)
    }
    setParams(next)
  }

  const open = (n: NotificationRecord) => {
    if (n.read_at === null) {
      void markNotificationRead(n.uid)
        .catch(() => undefined)
        .finally(refresh)
    }
  }

  const markAll = () => {
    setMarkError(false)
    markAllNotificationsRead()
      .then(() => {
        refresh()
        reload()
      })
      .catch(() => {
        setMarkError(true)
      })
  }

  const hasUnread = state.status === 'ready' && state.items.some((n) => n.read_at === null)
  const context: GridContext | undefined =
    state.status === 'ready'
      ? { loadingMore: state.loadingMore, moreError: state.moreError, onRetry: loadMore }
      : undefined

  return (
    <div className="kk-inbox">
      <div className="d-flex flex-wrap align-items-center gap-2 mb-3">
        <h1 className="kk-page-title mb-0 me-auto">{t('inbox.title')}</h1>
        <Link to="/account" className="btn btn-sm btn-outline-secondary">
          <Icon name="gear" className="me-1" />
          {t('inbox.settings')}
        </Link>
      </div>
      <div className="d-flex flex-wrap align-items-center gap-2 mb-3">
        <ToggleButtonGroup
          type="radio"
          name="inbox-filter"
          value={unreadOnly ? 'unread' : 'all'}
          onChange={setFilter}
          aria-label={t('inbox.filterLabel')}
          size="sm"
        >
          <ToggleButton id="inbox-filter-all" value="all" variant="outline-secondary">
            {t('inbox.filterAll')}
          </ToggleButton>
          <ToggleButton id="inbox-filter-unread" value="unread" variant="outline-secondary">
            {t('inbox.filterUnread')}
          </ToggleButton>
        </ToggleButtonGroup>
        {hasUnread && (
          <Button variant="outline-secondary" size="sm" className="ms-auto" onClick={markAll}>
            <Icon name="check2-all" className="me-1" />
            {t('inbox.markAllRead')}
          </Button>
        )}
      </div>
      {markError && (
        <p className="text-danger small" role="alert">
          {t('inbox.markAllError')}
        </p>
      )}

      {state.status === 'loading' && <p className="text-secondary">{t('inbox.loading')}</p>}
      {state.status === 'error' && <ErrorState title={t('inbox.error')} onRetry={reload} />}
      {state.status === 'ready' && state.items.length === 0 && (
        <EmptyState
          icon={<Icon name="bell" />}
          title={unreadOnly ? t('inbox.emptyUnread') : t('inbox.empty')}
          hint={unreadOnly ? undefined : t('inbox.emptyHint')}
        />
      )}
      {state.status === 'ready' && state.items.length > 0 && (
        <div className="kk-inbox__list" data-testid="inbox-list">
          <Virtuoso
            useWindowScroll
            data={state.items}
            context={context}
            endReached={loadMore}
            components={{ Footer: GridFooter }}
            computeItemKey={(_index, n) => n.uid}
            itemContent={(_index, n) => (
              <Link
                to={notificationTarget(n)}
                className="kk-inbox__item"
                onClick={() => {
                  open(n)
                }}
              >
                <NotificationRow notification={n} />
              </Link>
            )}
          />
        </div>
      )}
    </div>
  )
}
