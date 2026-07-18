import { type SyntheticEvent, useEffect, useMemo, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import Card from 'react-bootstrap/Card'
import Col from 'react-bootstrap/Col'
import Form from 'react-bootstrap/Form'
import Row from 'react-bootstrap/Row'
import Spinner from 'react-bootstrap/Spinner'
import Table from 'react-bootstrap/Table'
import { useTranslation } from 'react-i18next'

import { useAuth } from '../auth/AuthContext'
import { EmptyState } from '../components/EmptyState'
import { ErrorState } from '../components/ErrorState'
import {
  AUDIT_DEFAULTS,
  type AuditFilters,
  type AuditView,
  pickFilters,
  viewToParams,
} from '../lib/auditView'
import { formatDateTime } from '../lib/format'
import { useUrlState } from '../lib/urlState'
import { type AuditListResponse, type AuditRecord, fetchAuditLog } from '../services/audit'
import { type AdminUser, fetchUsers } from '../services/users'

/** The columns the table renders — kept in one place so the expanded row spans them. */
const COLUMN_COUNT = 6

/** Top-level load status of the audit listing. */
type State =
  | { status: 'loading' }
  | { status: 'error' }
  | { status: 'ready'; data: AuditListResponse }

/** True when the entry carries anything worth expanding (a payload or a UA). */
function isExpandable(record: AuditRecord): boolean {
  return (
    record.user_agent !== null ||
    (record.details !== null && Object.keys(record.details).length > 0)
  )
}

/**
 * Resolves an actor UID to a display name using the loaded roster, falling back
 * to the raw UID when the user is unknown (e.g. deleted or not yet loaded) and
 * to an em dash for a system action with no actor.
 */
function actorLabel(actorUid: string | null, users: Map<string, AdminUser>): string {
  if (actorUid === null) {
    return '—'
  }
  const user = users.get(actorUid)
  return user ? user.display_name || user.username : actorUid
}

/**
 * The admin audit-log page (`/audit`): a newest-first, filterable, paginated
 * view of the durable audit trail from `GET /api/v1/audit`. Filters and the page
 * offset live in the URL so Back restores the exact view. Actor UIDs are shown
 * as names by reusing the admin user roster; name resolution is best-effort and
 * never blocks the table from rendering.
 */
export function AuditPage() {
  const { t, i18n } = useTranslation()
  const { isAdmin } = useAuth()
  const [view, setView] = useUrlState<AuditView>(AUDIT_DEFAULTS)
  const params = useMemo(() => viewToParams(view), [view])
  const [state, setState] = useState<State>({ status: 'loading' })
  const [reloadKey, setReloadKey] = useState(0)
  const [users, setUsers] = useState<Map<string, AdminUser>>(new Map())
  const [draft, setDraft] = useState<AuditFilters>(() => pickFilters(view))
  const [expanded, setExpanded] = useState<ReadonlySet<number>>(new Set())

  // Keep the filter form in step with the committed URL (Back/Forward, reset).
  useEffect(() => {
    setDraft(pickFilters(view))
  }, [view])

  // Load the actor roster once so UIDs can be shown as names. Best-effort: on
  // failure the table still renders, falling back to the raw UID.
  useEffect(() => {
    if (!isAdmin) {
      return undefined
    }
    const controller = new AbortController()
    fetchUsers(controller.signal)
      .then((list) => {
        setUsers(new Map(list.map((user) => [user.uid, user])))
      })
      .catch(() => {
        // Name resolution is optional; ignore and show UIDs.
      })
    return () => {
      controller.abort()
    }
  }, [isAdmin])

  // Load the current page whenever the filters or offset (or a retry) change.
  useEffect(() => {
    if (!isAdmin) {
      return undefined
    }
    const controller = new AbortController()
    setState({ status: 'loading' })
    fetchAuditLog(params, controller.signal)
      .then((data) => {
        setState({ status: 'ready', data })
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
  }, [isAdmin, params, reloadKey])

  const userOptions = useMemo(
    () =>
      [...users.values()].sort((a, b) =>
        (a.display_name || a.username).localeCompare(b.display_name || b.username),
      ),
    [users],
  )

  if (!isAdmin) {
    return <Alert variant="danger">{t('audit.adminOnly')}</Alert>
  }

  const offset = params.offset ?? 0

  function applyFilters(e: SyntheticEvent) {
    e.preventDefault()
    setExpanded(new Set())
    setView({ ...draft, offset: '0' })
  }

  function resetFilters() {
    setExpanded(new Set())
    setDraft(pickFilters(AUDIT_DEFAULTS))
    setView(AUDIT_DEFAULTS)
  }

  function goToOffset(next: number) {
    setExpanded(new Set())
    setView({ offset: String(Math.max(0, next)) })
  }

  function toggleDetails(id: number) {
    setExpanded((prev) => {
      const next = new Set(prev)
      if (next.has(id)) {
        next.delete(id)
      } else {
        next.add(id)
      }
      return next
    })
  }

  return (
    <>
      <div className="mb-3">
        <h1 className="kk-page-title mb-1">{t('audit.title')}</h1>
        <p className="text-secondary mb-0">{t('audit.subtitle')}</p>
      </div>

      <Card className="mb-3">
        <Card.Body>
          <Form
            onSubmit={(e) => {
              applyFilters(e)
            }}
          >
            <Row className="g-3">
              <Col xs={12} md={6} lg={4}>
                <Form.Group controlId="audit-filter-actor">
                  <Form.Label>{t('audit.filters.actor')}</Form.Label>
                  <Form.Select
                    value={draft.user}
                    onChange={(e) => {
                      setDraft((d) => ({ ...d, user: e.target.value }))
                    }}
                  >
                    <option value="">{t('audit.filters.allActors')}</option>
                    {userOptions.map((user) => (
                      <option key={user.uid} value={user.uid}>
                        {user.display_name || user.username}
                      </option>
                    ))}
                  </Form.Select>
                </Form.Group>
              </Col>
              <Col xs={12} md={6} lg={4}>
                <Form.Group controlId="audit-filter-action">
                  <Form.Label>{t('audit.filters.action')}</Form.Label>
                  <Form.Control
                    type="text"
                    value={draft.action}
                    placeholder={t('audit.filters.actionPlaceholder')}
                    onChange={(e) => {
                      setDraft((d) => ({ ...d, action: e.target.value }))
                    }}
                  />
                </Form.Group>
              </Col>
              <Col xs={12} md={6} lg={4}>
                <Form.Group controlId="audit-filter-entity-type">
                  <Form.Label>{t('audit.filters.entityType')}</Form.Label>
                  <Form.Control
                    type="text"
                    value={draft.entity_type}
                    placeholder={t('audit.filters.entityTypePlaceholder')}
                    onChange={(e) => {
                      setDraft((d) => ({ ...d, entity_type: e.target.value }))
                    }}
                  />
                </Form.Group>
              </Col>
              <Col xs={12} md={6} lg={4}>
                <Form.Group controlId="audit-filter-entity-uid">
                  <Form.Label>{t('audit.filters.entityUid')}</Form.Label>
                  <Form.Control
                    type="text"
                    value={draft.entity_uid}
                    onChange={(e) => {
                      setDraft((d) => ({ ...d, entity_uid: e.target.value }))
                    }}
                  />
                </Form.Group>
              </Col>
              <Col xs={6} md={3} lg={2}>
                <Form.Group controlId="audit-filter-since">
                  <Form.Label>{t('audit.filters.since')}</Form.Label>
                  <Form.Control
                    type="date"
                    value={draft.since}
                    onChange={(e) => {
                      setDraft((d) => ({ ...d, since: e.target.value }))
                    }}
                  />
                </Form.Group>
              </Col>
              <Col xs={6} md={3} lg={2}>
                <Form.Group controlId="audit-filter-until">
                  <Form.Label>{t('audit.filters.until')}</Form.Label>
                  <Form.Control
                    type="date"
                    value={draft.until}
                    onChange={(e) => {
                      setDraft((d) => ({ ...d, until: e.target.value }))
                    }}
                  />
                </Form.Group>
              </Col>
            </Row>
            <div className="d-flex gap-2 mt-3">
              <Button type="submit" variant="primary">
                {t('audit.filters.apply')}
              </Button>
              <Button type="button" variant="outline-secondary" onClick={resetFilters}>
                {t('audit.filters.reset')}
              </Button>
            </div>
          </Form>
        </Card.Body>
      </Card>

      <Card>
        <Card.Body>
          {state.status === 'loading' && (
            <div className="text-center py-4" role="status" aria-live="polite">
              <Spinner animation="border" />
              <span className="visually-hidden">{t('audit.loading')}</span>
            </div>
          )}

          {state.status === 'error' && (
            <ErrorState
              title={t('audit.error')}
              onRetry={() => {
                setReloadKey((key) => key + 1)
              }}
            />
          )}

          {state.status === 'ready' && state.data.entries.length === 0 && (
            <EmptyState title={t('audit.empty.title')} hint={t('audit.empty.hint')} />
          )}

          {state.status === 'ready' && state.data.entries.length > 0 && (
            <>
              <Table striped hover responsive className="mb-3 align-middle">
                <thead>
                  <tr>
                    <th>{t('audit.columns.when')}</th>
                    <th>{t('audit.columns.actor')}</th>
                    <th>{t('audit.columns.action')}</th>
                    <th>{t('audit.columns.target')}</th>
                    <th>{t('audit.columns.ip')}</th>
                    <th>{t('audit.columns.details')}</th>
                  </tr>
                </thead>
                <tbody>
                  {state.data.entries.map((record) => (
                    <AuditEntryRow
                      key={record.id}
                      record={record}
                      users={users}
                      locale={i18n.language}
                      expanded={expanded.has(record.id)}
                      onToggle={() => {
                        toggleDetails(record.id)
                      }}
                    />
                  ))}
                </tbody>
              </Table>
              <div className="d-flex justify-content-between align-items-center gap-3 flex-wrap">
                <span className="text-secondary small">
                  {t('audit.pagination.range', {
                    from: offset + 1,
                    to: offset + state.data.entries.length,
                    total: state.data.total,
                  })}
                </span>
                <div className="btn-group">
                  <Button
                    variant="outline-secondary"
                    size="sm"
                    disabled={offset === 0}
                    onClick={() => {
                      goToOffset(offset - state.data.limit)
                    }}
                  >
                    {t('audit.pagination.prev')}
                  </Button>
                  <Button
                    variant="outline-secondary"
                    size="sm"
                    disabled={state.data.next_offset === null}
                    onClick={() => {
                      if (state.data.next_offset !== null) {
                        goToOffset(state.data.next_offset)
                      }
                    }}
                  >
                    {t('audit.pagination.next')}
                  </Button>
                </div>
              </div>
            </>
          )}
        </Card.Body>
      </Card>
    </>
  )
}

/** Props for one audit table row. */
interface AuditEntryRowProps {
  record: AuditRecord
  users: Map<string, AdminUser>
  locale: string
  expanded: boolean
  onToggle: () => void
}

/**
 * One audit entry: the summary columns, plus a toggle that reveals an expanded
 * row with the raw `details` payload and user agent when either is present.
 */
function AuditEntryRow({ record, users, locale, expanded, onToggle }: AuditEntryRowProps) {
  const { t } = useTranslation()
  const expandable = isExpandable(record)
  const detailsId = `audit-details-${String(record.id)}`
  return (
    <>
      <tr>
        <td className="text-nowrap">{formatDateTime(record.created_at, locale)}</td>
        <td className="text-break">{actorLabel(record.actor_uid, users)}</td>
        <td className="text-break">{record.action}</td>
        <td className="text-break">
          {record.target_type || '—'}
          {record.target_uid !== null && (
            <div className="text-secondary small text-break">{record.target_uid}</div>
          )}
        </td>
        <td className="text-nowrap">{record.ip ?? '—'}</td>
        <td>
          {expandable ? (
            <Button
              variant="link"
              size="sm"
              className="p-0"
              aria-expanded={expanded}
              aria-controls={detailsId}
              onClick={onToggle}
            >
              {expanded ? t('audit.details.hide') : t('audit.details.show')}
            </Button>
          ) : (
            <span className="text-secondary">—</span>
          )}
        </td>
      </tr>
      {expandable && expanded && (
        <tr>
          <td colSpan={COLUMN_COUNT} id={detailsId} className="bg-body-tertiary">
            <dl className="row mb-0 small">
              {record.details !== null && Object.keys(record.details).length > 0 && (
                <>
                  <dt className="col-sm-2">{t('audit.details.payload')}</dt>
                  <dd className="col-sm-10 mb-2">
                    <pre className="mb-0">{JSON.stringify(record.details, null, 2)}</pre>
                  </dd>
                </>
              )}
              {record.user_agent !== null && (
                <>
                  <dt className="col-sm-2">{t('audit.details.userAgent')}</dt>
                  <dd className="col-sm-10 mb-0 text-break">{record.user_agent}</dd>
                </>
              )}
            </dl>
          </td>
        </tr>
      )}
    </>
  )
}
