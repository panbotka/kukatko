import { useCallback, useRef, useState } from 'react'
import Badge from 'react-bootstrap/Badge'
import Button from 'react-bootstrap/Button'
import Dropdown from 'react-bootstrap/Dropdown'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'

import { useNotificationInbox } from '../../notifications/NotificationInboxContext'
import { ProjectLink } from '../../project/ProjectLink'
import {
  fetchNotifications,
  markAllNotificationsRead,
  markNotificationRead,
  type NotificationRecord,
  notificationTarget,
} from '../../services/notifications'
import { Icon } from '../Icon'

import { BELL_LIMIT, badgeText, INBOX_PATH } from './bell'
import { NotificationRow } from './NotificationRow'

/** The dropdown's list, as it loads. */
type ListState =
  | { status: 'idle' }
  | { status: 'loading' }
  | { status: 'error' }
  | { status: 'ready'; items: NotificationRecord[] }

/**
 * The in-app inbox's door in the navbar: a bell with the unread count on its
 * corner (from `NotificationInboxProvider`) that opens a dropdown of the newest
 * {@link BELL_LIMIT} notifications.
 *
 * The list is fetched each time the dropdown opens — it is only ever read
 * there, and a fresh read is what makes the dot on each entry true. Choosing an
 * entry marks it read (the notification page does too; marking is idempotent)
 * and follows its link. "Mark all read" clears the inbox in one request; "show
 * all" opens the full inbox page, and the settings link the preferences, where
 * the in-app channel is switched on per kind — it is off by default, which is
 * also what the empty state says.
 *
 * The bell itself never moves: the badge is absolutely positioned on its corner
 * (`kk-waiting-badge--over`), so the bar's width budget pays only for the
 * button.
 */
export function NotificationBell({ className }: { className?: string }) {
  const { t } = useTranslation()
  const { unread, refresh } = useNotificationInbox()
  const [list, setList] = useState<ListState>({ status: 'idle' })
  const [markError, setMarkError] = useState(false)
  const controller = useRef<AbortController | null>(null)
  const count = unread ?? 0

  const load = useCallback(() => {
    controller.current?.abort()
    const current = new AbortController()
    controller.current = current
    setList({ status: 'loading' })
    fetchNotifications({ limit: BELL_LIMIT }, current.signal)
      .then((page) => {
        if (controller.current === current) {
          setList({ status: 'ready', items: page.notifications })
        }
      })
      .catch((err: unknown) => {
        if (controller.current === current && !(err instanceof DOMException)) {
          setList({ status: 'error' })
        }
      })
  }, [])

  const onToggle = (open: boolean) => {
    if (open) {
      setMarkError(false)
      load()
      refresh()
    } else {
      controller.current?.abort()
    }
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
        const now = new Date().toISOString()
        setList((prev) =>
          prev.status === 'ready'
            ? {
                status: 'ready',
                items: prev.items.map((n) => ({ ...n, read_at: n.read_at ?? now })),
              }
            : prev,
        )
        refresh()
      })
      .catch(() => {
        setMarkError(true)
      })
  }

  const label = count > 0 ? t('inbox.bellUnread', { count }) : t('inbox.bell')

  return (
    <Dropdown align="end" onToggle={onToggle} className={className}>
      <Dropdown.Toggle
        as={Button}
        variant="outline-secondary"
        size="sm"
        className="kk-bell position-relative d-inline-flex align-items-center justify-content-center kukatko-tap-target"
        aria-label={label}
        title={label}
      >
        <Icon name={count > 0 ? 'bell-fill' : 'bell'} />
        {count > 0 && (
          <Badge
            pill
            bg="danger"
            className="kk-waiting-badge kk-waiting-badge--over"
            data-testid="bell-badge"
            aria-hidden="true"
          >
            {badgeText(count)}
          </Badge>
        )}
      </Dropdown.Toggle>
      <Dropdown.Menu className="kk-bell-menu">
        <div className="kk-bell-menu__head">
          <span className="fw-semibold">{t('inbox.latest')}</span>
          {count > 0 && (
            <Button variant="link" size="sm" className="kk-bell-menu__action" onClick={markAll}>
              <Icon name="check2-all" className="me-1" />
              {t('inbox.markAllRead')}
            </Button>
          )}
        </div>
        {markError && (
          <p className="text-danger small px-3 mb-2" role="alert">
            {t('inbox.markAllError')}
          </p>
        )}
        {list.status === 'loading' && (
          <div className="d-flex justify-content-center py-3">
            <Spinner animation="border" size="sm" role="status">
              <span className="visually-hidden">{t('inbox.loading')}</span>
            </Spinner>
          </div>
        )}
        {list.status === 'error' && (
          <p className="text-secondary small px-3 py-2 mb-0">{t('inbox.error')}</p>
        )}
        {list.status === 'ready' && list.items.length === 0 && (
          <div className="px-3 py-2 small">
            <p className="mb-1">{t('inbox.empty')}</p>
            <p className="text-secondary mb-0">{t('inbox.emptyHint')}</p>
          </div>
        )}
        {list.status === 'ready' &&
          list.items.map((n) => (
            <Dropdown.Item
              key={n.uid}
              as={ProjectLink}
              to={notificationTarget(n)}
              className="kk-bell-menu__item"
              onClick={() => {
                open(n)
              }}
            >
              <NotificationRow notification={n} />
            </Dropdown.Item>
          ))}
        <Dropdown.Divider />
        <div className="kk-bell-menu__foot">
          <Dropdown.Item as={ProjectLink} to={INBOX_PATH} className="kk-bell-menu__link">
            {t('inbox.showAll')}
          </Dropdown.Item>
          <Dropdown.Item
            as={ProjectLink}
            to="/account"
            className="kk-bell-menu__link kk-bell-menu__settings"
            title={t('inbox.settings')}
          >
            <Icon name="gear" />
            <span className="visually-hidden">{t('inbox.settings')}</span>
          </Dropdown.Item>
        </div>
      </Dropdown.Menu>
    </Dropdown>
  )
}
