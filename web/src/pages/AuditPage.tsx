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
import { Link } from 'react-router-dom'

import { useAuth } from '../auth/AuthContext'
import { EmptyState } from '../components/EmptyState'
import { ErrorState } from '../components/ErrorState'
import { RecordTable, type RecordColumn } from '../components/RecordTable'
import { useDocumentTitle } from '../hooks/useDocumentTitle'
import {
  AUDIT_DEFAULTS,
  type AuditFilters,
  type AuditView,
  auditDetailLinks,
  auditTargetHref,
  pickFilters,
  viewToParams,
} from '../lib/auditView'
import { formatDateTime } from '../lib/format'
import { useUrlState } from '../lib/urlState'
import {
  type AuditChange,
  type AuditChanges,
  type AuditListResponse,
  type AuditRecord,
  fetchAuditLog,
} from '../services/audit'
import { type AdminUser, fetchUsers } from '../services/users'

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
 * Narrows an entry's `details.changes` to a well-formed old→new map, or null when
 * it is absent or malformed (legacy rows, non-edit actions). A value counts only
 * when it is an object carrying both `old` and `new`, so a stray `changes` key of
 * another shape safely falls back to the raw-JSON rendering.
 */
function readChanges(details: Record<string, unknown> | null): AuditChanges | null {
  const raw = details?.changes
  if (raw === null || typeof raw !== 'object' || Array.isArray(raw)) {
    return null
  }
  const out: AuditChanges = {}
  for (const [field, value] of Object.entries(raw)) {
    if (value !== null && typeof value === 'object' && 'old' in value && 'new' in value) {
      out[field] = value as AuditChange
    }
  }
  return Object.keys(out).length > 0 ? out : null
}

/** The id of an entry's expanded block, shared by its toggle's `aria-controls`. */
function detailsId(record: AuditRecord): string {
  return `audit-details-${String(record.id)}`
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
  useDocumentTitle(t('audit.title'))
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

  // One definition drives both layouts: the six summary columns on a tablet or
  // desktop, the same fields as "label: value" lines on a phone card.
  const columns: RecordColumn<AuditRecord>[] = [
    {
      key: 'when',
      header: t('audit.columns.when'),
      cellClassName: 'text-nowrap',
      cell: (record) => formatDateTime(record.created_at, i18n.language),
    },
    {
      key: 'actor',
      header: t('audit.columns.actor'),
      cellClassName: 'text-break',
      cell: (record) => actorLabel(record.actor_uid, users),
    },
    {
      key: 'action',
      header: t('audit.columns.action'),
      cellClassName: 'text-break',
      cell: (record) => record.action,
    },
    {
      key: 'target',
      header: t('audit.columns.target'),
      cellClassName: 'text-break',
      cell: (record) => <AuditTarget record={record} />,
    },
    {
      key: 'ip',
      header: t('audit.columns.ip'),
      cellClassName: 'text-nowrap',
      cell: (record) => record.ip ?? '—',
    },
    {
      key: 'details',
      header: t('audit.columns.details'),
      cell: (record) =>
        isExpandable(record) ? (
          <Button
            variant="link"
            size="sm"
            className="p-0"
            aria-expanded={expanded.has(record.id)}
            aria-controls={detailsId(record)}
            onClick={() => {
              toggleDetails(record.id)
            }}
          >
            {expanded.has(record.id) ? t('audit.details.hide') : t('audit.details.show')}
          </Button>
        ) : (
          <span className="text-secondary">—</span>
        ),
    },
  ]

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
              <RecordTable
                records={state.data.entries}
                columns={columns}
                rowKey={(record) => String(record.id)}
                detail={(record) =>
                  isExpandable(record) && expanded.has(record.id) ? (
                    <AuditEntryDetails id={detailsId(record)} record={record} />
                  ) : null
                }
                className="mb-0 align-middle"
              />
              {/* The gap belongs to the pagination row, not to the listing: a
                  `.table` carries a bottom margin of its own but the card stack
                  does not, and the spacing has to read the same on both. */}
              <div className="mt-3 d-flex justify-content-between align-items-center gap-3 flex-wrap">
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

/**
 * The Target cell: the entity type, with its UID underneath as a link to the
 * thing the entry records a change of ({@link auditTargetHref}) — the photo, the
 * album, the label, the person, or for a face entry the photo the marker sits
 * on. A target with no page of its own (a user, an API token, the announcement)
 * keeps the plain muted UID it always had, so the row stays scannable either
 * way: same size, same place, only a link where there is somewhere to go.
 */
function AuditTarget({ record }: { record: AuditRecord }) {
  const href = auditTargetHref(record)
  return (
    <>
      {record.target_type || '—'}
      {record.target_uid !== null && (
        <div className="small text-break">
          {href === null ? (
            <span className="text-secondary">{record.target_uid}</span>
          ) : (
            <Link to={href}>{record.target_uid}</Link>
          )}
        </div>
      )}
    </>
  )
}

/** Props for the expanded payload of one audit entry. */
interface AuditEntryDetailsProps {
  /** The element id the entry's toggle names in `aria-controls`. */
  id: string
  record: AuditRecord
}

/**
 * The expanded half of an audit entry: the UIDs the payload names as links, the
 * `details` payload itself — as an old → new table when the record carries a
 * well-formed `changes` map, otherwise the raw JSON — plus the user agent. It is
 * the same block either way: the table puts it in a row spanning every column, a
 * phone card puts it under the record's fields.
 *
 * The links come first because the target is not always the useful destination:
 * `label.reject` targets the label but happened on a photo, and a bulk edit
 * names no target at all — its photos exist only here.
 *
 * The raw payload is wrapped rather than left to overflow (`.kk-audit-payload`);
 * one long JSON line used to set the scroll width of the whole responsive table
 * and drag the summary columns sideways with it.
 */
function AuditEntryDetails({ id, record }: AuditEntryDetailsProps) {
  const { t } = useTranslation()
  const changes = readChanges(record.details)
  const { groups, hidden } = auditDetailLinks(record.details)
  return (
    <dl className="row mb-0 small" id={id}>
      {groups.length > 0 && (
        <>
          <dt className="col-sm-2">{t('audit.details.links')}</dt>
          <dd className="col-sm-10 mb-2">
            <ul className="list-unstyled mb-0" data-testid="audit-links">
              {groups.map((group) => (
                <li key={group.key} className="text-break">
                  <code className="text-secondary">{group.key}</code>{' '}
                  {group.links.map((link) => (
                    <Link key={link.href} to={link.href} className="me-2">
                      {link.uid}
                    </Link>
                  ))}
                </li>
              ))}
            </ul>
            {hidden > 0 && (
              <div className="text-secondary">{t('audit.details.moreLinks', { n: hidden })}</div>
            )}
          </dd>
        </>
      )}
      {record.details !== null && Object.keys(record.details).length > 0 && (
        <>
          <dt className="col-sm-2">
            {changes ? t('audit.changes.title') : t('audit.details.payload')}
          </dt>
          <dd className="col-sm-10 mb-2">
            {changes ? (
              <ChangesTable changes={changes} />
            ) : (
              <pre className="kk-audit-payload mb-0">{JSON.stringify(record.details, null, 2)}</pre>
            )}
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
  )
}

/**
 * Renders an edit's `details.changes` map as a compact old → new table (field,
 * old value, new value), one row per changed field, sorted by field name so the
 * order is stable. Used in place of the raw-JSON dump for edit actions.
 */
function ChangesTable({ changes }: { changes: AuditChanges }) {
  const { t } = useTranslation()
  const rows = Object.entries(changes).sort(([a], [b]) => a.localeCompare(b))
  return (
    <Table size="sm" bordered className="mb-0 align-middle" data-testid="audit-changes">
      <thead>
        <tr>
          <th>{t('audit.changes.field')}</th>
          <th>{t('audit.changes.old')}</th>
          <th>{t('audit.changes.new')}</th>
        </tr>
      </thead>
      <tbody>
        {rows.map(([field, change]) => (
          <tr key={field}>
            <td className="text-break">
              <code>{field}</code>
            </td>
            <td className="text-break">
              <ChangeValue value={change.old} />
            </td>
            <td className="text-break">
              <ChangeValue value={change.new} />
            </td>
          </tr>
        ))}
      </tbody>
    </Table>
  )
}

/**
 * Renders one side of a change. A null/undefined/empty-string value (a cleared
 * field) shows a muted placeholder; an object is JSON-stringified; everything
 * else renders as its string form.
 */
function ChangeValue({ value }: { value: unknown }) {
  const { t } = useTranslation()
  if (value === null || value === undefined || value === '') {
    return <span className="text-secondary">{t('audit.changes.empty')}</span>
  }
  if (typeof value === 'string') {
    return <>{value}</>
  }
  if (typeof value === 'number' || typeof value === 'boolean') {
    return <>{String(value)}</>
  }
  // Objects, arrays and any exotic value render as compact JSON.
  return <code>{JSON.stringify(value)}</code>
}
