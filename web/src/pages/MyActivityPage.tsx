import { useEffect, useMemo, useState } from 'react'
import Button from 'react-bootstrap/Button'
import Card from 'react-bootstrap/Card'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { BackLink } from '../components/BackLink'
import { EmptyState } from '../components/EmptyState'
import { ErrorState } from '../components/ErrorState'
import { Icon } from '../components/Icon'
import { RecordTable, type RecordColumn } from '../components/RecordTable'
import { ListSkeleton } from '../components/Skeleton'
import { useDocumentTitle } from '../hooks/useDocumentTitle'
import { useReloadKey } from '../hooks/useReloadKey'
import {
  ACTIVITY_DEFAULTS,
  ACTIVITY_LINK_LIMIT,
  ACTIVITY_PAGE_SIZE,
  type ActivityView,
  activityActionKey,
  activityTargetKey,
  viewToParams,
} from '../lib/activityView'
import { auditDetailLinks, auditTargetHref } from '../lib/auditView'
import { formatDateTime } from '../lib/format'
import { useUrlState } from '../lib/urlState'
import { type AuditListResponse, type AuditRecord, fetchMyActivity } from '../services/audit'

/** Load state of the own-activity listing. */
type State =
  | { status: 'loading' }
  | { status: 'error' }
  | { status: 'ready'; data: AuditListResponse }

/**
 * "Moje aktivita" (`/account/activity`): the signed-in user's own actions,
 * newest-first, from `GET /api/v1/audit/mine`.
 *
 * It exists for self-repair, not for supervision: someone knows they clicked
 * something wrong a minute ago and needs to find out what it was and where.
 * Everything follows from that — the rows say what happened, where, and when,
 * and every row that has somewhere to go is a link straight to the photo, album,
 * label or person it changed (the same routing table the admin audit log uses).
 * There is no "who" column, because the answer is always the reader; and no
 * filter form, because the recent end of a one-user list is where the answer is.
 *
 * The narrowing to the reader is entirely the server's: the endpoint takes the
 * actor from the session, so this page cannot ask for anyone else's actions. Any
 * signed-in role may open it. See docs/FRONTEND.md.
 */
export function MyActivityPage() {
  const { t, i18n } = useTranslation()
  useDocumentTitle(t('activity.title'))
  const [view, setView] = useUrlState<ActivityView>(ACTIVITY_DEFAULTS)
  const params = useMemo(() => viewToParams(view), [view])
  const [state, setState] = useState<State>({ status: 'loading' })
  const [reloadKey, reload] = useReloadKey()

  useEffect(() => {
    const controller = new AbortController()
    setState({ status: 'loading' })
    fetchMyActivity(params, controller.signal)
      .then((data) => {
        if (!controller.signal.aborted) {
          setState({ status: 'ready', data })
        }
      })
      .catch((err: unknown) => {
        if (err instanceof DOMException && err.name === 'AbortError') {
          return
        }
        setState({ status: 'error' })
      })
    return () => {
      controller.abort()
    }
  }, [params, reloadKey])

  /** Jumps to a page offset, clamped at zero (pushes history so Back works). */
  function goToOffset(next: number) {
    setView({ offset: String(Math.max(0, next)) })
  }

  // One definition drives both layouts: three columns on a tablet or desktop,
  // the same fields as "label: value" lines on a phone card.
  const columns: RecordColumn<AuditRecord>[] = [
    {
      key: 'when',
      header: t('activity.columns.when'),
      cellClassName: 'text-nowrap',
      cell: (record) => formatDateTime(record.created_at, i18n.language),
    },
    {
      key: 'what',
      header: t('activity.columns.what'),
      cell: (record) => <ActivityAction record={record} />,
    },
    {
      key: 'where',
      header: t('activity.columns.where'),
      cell: (record) => <ActivityTarget record={record} />,
    },
  ]

  return (
    <>
      <div className="mb-3">
        <BackLink to="/account" label={t('activity.back')} className="mb-2" />
        <h1 className="kk-page-title mb-1">{t('activity.title')}</h1>
        <p className="text-secondary mb-0">{t('activity.subtitle')}</p>
      </div>

      <Card>
        <Card.Body>
          {state.status === 'loading' && <ListSkeleton label={t('activity.loading')} count={6} />}

          {state.status === 'error' && <ErrorState title={t('activity.error')} onRetry={reload} />}

          {state.status === 'ready' && state.data.entries.length === 0 && (
            <EmptyState
              icon={<Icon name="clock-history" />}
              title={t('activity.empty.title')}
              hint={t('activity.empty.hint')}
            />
          )}

          {state.status === 'ready' && state.data.entries.length > 0 && (
            <>
              <RecordTable
                records={state.data.entries}
                columns={columns}
                rowKey={(record) => String(record.id)}
                className="mb-0 align-middle"
              />
              <ActivityPager data={state.data} onOffset={goToOffset} />
            </>
          )}
        </Card.Body>
      </Card>
    </>
  )
}

/**
 * The What cell: the action in words ("Úprava fotky"), falling back to the raw
 * action label for an action this build has no translation for — a new one from
 * the backend must still be readable, not a blank cell.
 */
function ActivityAction({ record }: { record: AuditRecord }) {
  const { t } = useTranslation()
  const key = activityActionKey(record.action)
  return <>{key === undefined ? record.action : t(key)}</>
}

/**
 * The Where cell: a link to the thing the entry changed — the photo, album,
 * label or person — named in words rather than by UID, because the reader is
 * here to get back to it, not to read identifiers.
 *
 * Two fallbacks, in order. An entry with no target of its own (a bulk edit names
 * its photos only in the payload) links the UIDs the payload carries, capped at
 * {@link ACTIVITY_LINK_LIMIT} with a note of how many were left out. An entry
 * whose target has no page at all (a user, an API token, the announcement) names
 * the kind as plain text — there is nowhere honest to send the reader.
 */
function ActivityTarget({ record }: { record: AuditRecord }) {
  const { t } = useTranslation()
  const typeKey = activityTargetKey(record.target_type)
  const typeLabel = typeKey === undefined ? record.target_type : t(typeKey)
  const href = auditTargetHref(record)
  if (href !== null) {
    return (
      <Link to={href} className="d-inline-flex align-items-center gap-1">
        <Icon name="box-arrow-up-right" />
        {typeLabel || t('activity.target.unknown')}
      </Link>
    )
  }
  const { groups, hidden } = auditDetailLinks(record.details, ACTIVITY_LINK_LIMIT)
  if (groups.length > 0) {
    return (
      <>
        <ul className="list-unstyled mb-0" data-testid="activity-links">
          {groups.map((group) =>
            group.links.map((link) => (
              <li key={link.href} className="text-break">
                <Link to={link.href}>{link.uid}</Link>
              </li>
            )),
          )}
        </ul>
        {hidden > 0 && (
          <div className="text-secondary small">{t('activity.moreLinks', { n: hidden })}</div>
        )}
      </>
    )
  }
  return <span className="text-secondary">{typeLabel || t('activity.target.unknown')}</span>
}

/** Props for {@link ActivityPager}. */
interface ActivityPagerProps {
  data: AuditListResponse
  onOffset: (next: number) => void
}

/** Prev/Next controls plus the "showing X–Y of N" range for the current page. */
function ActivityPager({ data, onOffset }: ActivityPagerProps) {
  const { t } = useTranslation()
  const from = data.total === 0 ? 0 : data.offset + 1
  const to = data.offset + data.entries.length
  return (
    <div className="mt-3 d-flex justify-content-between align-items-center gap-3 flex-wrap">
      <span className="text-secondary small">
        {t('activity.pagination.range', { from, to, total: data.total })}
      </span>
      <div className="btn-group">
        <Button
          variant="outline-secondary"
          size="sm"
          disabled={data.offset === 0}
          onClick={() => {
            onOffset(data.offset - ACTIVITY_PAGE_SIZE)
          }}
        >
          {t('activity.pagination.prev')}
        </Button>
        <Button
          variant="outline-secondary"
          size="sm"
          disabled={data.next_offset === null}
          onClick={() => {
            if (data.next_offset !== null) {
              onOffset(data.next_offset)
            }
          }}
        >
          {t('activity.pagination.next')}
        </Button>
      </div>
    </div>
  )
}
