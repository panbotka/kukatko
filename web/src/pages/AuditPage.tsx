import { type TFunction } from 'i18next'
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
  AUDIT_INSTANCE_ONLY,
  type AuditFilters,
  type AuditView,
  auditDetailLinks,
  auditTargetHref,
  pickFilters,
  uploadLinkActor,
  viewToParams,
} from '../lib/auditView'
import { formatDateTime } from '../lib/format'
import { useUrlState } from '../lib/urlState'
import { projectPathFor } from '../project/paths'
import { ProjectLink } from '../project/ProjectLink'
import {
  type AuditChange,
  type AuditChanges,
  type AuditListParams,
  type AuditListResponse,
  type AuditRecord,
  fetchAuditLog,
  fetchInstanceAuditLog,
} from '../services/audit'
import { listDirectory } from '../services/directory'
import { type AdminProject, fetchAdminProjects, fetchInstanceAccounts } from '../services/projects'

/**
 * Which trail the page reads: one project's (`/p/<slug>/audit`, its admin's)
 * or the whole instance's (`/system/audit`, the system administrator's).
 */
export type AuditScope = 'project' | 'instance'

/** One person the actor filter offers. */
interface ActorOption {
  uid: string
  name: string
}

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
 * Resolves an entry's actor to a display name: a former member of the project
 * (the page's `former_actors`) as one — never by their live name, which the
 * server does not send — then the page's own `actors` map (the server names
 * everybody else who acted in the page), then the loaded roster, and the raw UID
 * only when none knows the account; an em dash for a system action with no actor. An anonymous upload
 * through an upload link has no actor either, but it has somebody behind it: it
 * is named by its link and the name the guest typed (just the link when they
 * typed none).
 */
function actorLabel(
  record: AuditRecord,
  page: Pick<AuditListResponse, 'actors' | 'former_actors'>,
  users: Map<string, ActorOption>,
  t: TFunction,
): string {
  const { actors } = page
  const actorUid = record.actor_uid
  if (actorUid === null) {
    const guest = uploadLinkActor(record)
    if (guest === undefined) {
      return '—'
    }
    const { title, name } = guest
    if (title === '') {
      return name === ''
        ? t('audit.uploadLinkActor.untitled')
        : t('audit.uploadLinkActor.untitledNamed', { name })
    }
    return name === ''
      ? t('audit.uploadLinkActor.anonymous', { title })
      : t('audit.uploadLinkActor.named', { title, name })
  }
  if (page.former_actors?.includes(actorUid) === true) {
    return t('formerMember')
  }
  if (Object.hasOwn(actors, actorUid)) {
    return actors[actorUid]
  }
  const user = users.get(actorUid)
  return user ? user.name : actorUid
}

/**
 * Where a link of an entry goes: into the current project on a project's page
 * (`undefined`), into the entry's own project on the instance page (its slug),
 * or nowhere for an instance entry there (`null`) — it has no project to open
 * the thing in.
 */
type LinkProject = string | null | undefined

/** The project an entry's links open in, for `scope` (see {@link LinkProject}). */
function linkProjectOf(record: AuditRecord, scope: AuditScope): LinkProject {
  return scope === 'project' ? undefined : record.project
}

/** Props of {@link EntryLink}. */
interface EntryLinkProps {
  to: string
  project: LinkProject
  className?: string
  children: string
}

/**
 * A link out of an entry, into the project {@link LinkProject} names; plain
 * muted text when it names none.
 */
function EntryLink({ to, project, className, children }: EntryLinkProps) {
  if (project === undefined) {
    return (
      <ProjectLink to={to} className={className}>
        {children}
      </ProjectLink>
    )
  }
  if (project === null) {
    return <span className={`text-secondary ${className ?? ''}`.trim()}>{children}</span>
  }
  return (
    <Link to={projectPathFor(project, to)} className={className}>
      {children}
    </Link>
  )
}

/** Props of {@link AuditPage}. */
interface AuditPageProps {
  /** Which trail the page reads; a project's unless said otherwise. */
  scope?: AuditScope
}

/** Reads the actor filter's people: the project's directory, or every account. */
async function loadActors(scope: AuditScope, signal: AbortSignal): Promise<ActorOption[]> {
  if (scope === 'project') {
    return (await listDirectory(signal)).map((user) => ({ uid: user.uid, name: user.name }))
  }
  return (await fetchInstanceAccounts(signal)).map((account) => ({
    uid: account.uid,
    name: account.display_name === '' ? account.username : account.display_name,
  }))
}

/** Reads one page of the scope's trail. */
function loadPage(
  scope: AuditScope,
  params: AuditListParams,
  signal: AbortSignal,
): Promise<AuditListResponse> {
  return scope === 'project' ? fetchAuditLog(params, signal) : fetchInstanceAuditLog(params, signal)
}

/**
 * The audit-log page: a newest-first, filterable, paginated view of a durable
 * audit trail. Filters and the page offset live in the URL so Back restores the
 * exact view.
 *
 * In a project (`/p/<slug>/audit`, its admin's) it reads that project's trail
 * from `GET /api/v1/p/<slug>/audit`, and the actor filter offers the project's
 * member directory (`GET /api/v1/p/<slug>/users`).
 *
 * On the instance (`/system/audit`, the system administrator's, `scope` =
 * `instance`) it reads the whole trail from `GET /api/v1/audit`: every
 * project's entries and the instance's own — accounts created and edited,
 * instance roles granted and taken, accounts blocked, passwords reset — which
 * no project's page shows. A project filter narrows it to one project or to the
 * instance alone, a column names each entry's project, and an entry's links
 * open in that entry's project. The actor filter offers every account.
 *
 * Actors are named by the page's own `actors` map — the server names everyone
 * who acted in the page, so a still-waiting member shows by name rather than as
 * a raw uid, and a project's former member as a former member — and the roster
 * fills in the rest. Name resolution
 * never blocks the table from rendering.
 */
export function AuditPage({ scope = 'project' }: AuditPageProps) {
  const { t, i18n } = useTranslation()
  const instance = scope === 'instance'
  useDocumentTitle(instance ? t('audit.instanceTitle') : t('audit.title'))
  const { isAdmin, isMaintainer } = useAuth()
  const allowed = instance ? isMaintainer : isAdmin
  const [view, setView] = useUrlState<AuditView>(AUDIT_DEFAULTS)
  const params = useMemo(() => viewToParams(view, scope), [view, scope])
  const [state, setState] = useState<State>({ status: 'loading' })
  const [reloadKey, setReloadKey] = useState(0)
  const [users, setUsers] = useState<Map<string, ActorOption>>(new Map())
  const [projects, setProjects] = useState<AdminProject[]>([])
  const [draft, setDraft] = useState<AuditFilters>(() => pickFilters(view))
  const [expanded, setExpanded] = useState<ReadonlySet<number>>(new Set())

  // Keep the filter form in step with the committed URL (Back/Forward, reset).
  useEffect(() => {
    setDraft(pickFilters(view))
  }, [view])

  // Load the actor roster once so UIDs can be shown as names. Best-effort: on
  // failure the table still renders, naming actors from the page's own map.
  useEffect(() => {
    if (!allowed) {
      return undefined
    }
    const controller = new AbortController()
    loadActors(scope, controller.signal)
      .then((list) => {
        setUsers(new Map(list.map((user) => [user.uid, user])))
      })
      .catch(() => {
        // Name resolution is optional; ignore and show UIDs.
      })
    return () => {
      controller.abort()
    }
  }, [allowed, scope])

  // The project filter's choices, on the instance page only. Best-effort too:
  // without them the filter still offers "everything" and "the instance only".
  useEffect(() => {
    if (!allowed || !instance) {
      return undefined
    }
    const controller = new AbortController()
    fetchAdminProjects(controller.signal)
      .then((list) => {
        setProjects([...list].sort((a, b) => a.name.localeCompare(b.name)))
      })
      .catch(() => {
        // The two fixed choices remain.
      })
    return () => {
      controller.abort()
    }
  }, [allowed, instance])

  // Load the current page whenever the filters or offset (or a retry) change.
  useEffect(() => {
    if (!allowed) {
      return undefined
    }
    const controller = new AbortController()
    setState({ status: 'loading' })
    loadPage(scope, params, controller.signal)
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
  }, [allowed, scope, params, reloadKey])

  const userOptions = useMemo(
    () => [...users.values()].sort((a, b) => a.name.localeCompare(b.name)),
    [users],
  )

  if (!allowed) {
    return (
      <Alert variant="danger">{instance ? t('audit.maintainerOnly') : t('audit.adminOnly')}</Alert>
    )
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
      cell: (record) =>
        actorLabel(record, state.status === 'ready' ? state.data : { actors: {} }, users, t),
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
      cell: (record) => <AuditTarget record={record} project={linkProjectOf(record, scope)} />,
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

  if (instance) {
    // Which project each entry is filed under, right after the time: on the
    // instance's page it is the first thing that tells two entries apart.
    columns.splice(1, 0, {
      key: 'project',
      header: t('audit.columns.project'),
      cellClassName: 'text-nowrap',
      cell: (record) =>
        record.project ?? <span className="text-secondary">{t('audit.instanceEntry')}</span>,
    })
  }

  return (
    <>
      <div className="mb-3">
        <h1 className="kk-page-title mb-1">
          {instance ? t('audit.instanceTitle') : t('audit.title')}
        </h1>
        <p className="text-secondary mb-0">
          {instance ? t('audit.instanceSubtitle') : t('audit.subtitle')}
        </p>
      </div>

      <Card className="mb-3">
        <Card.Body>
          <Form
            onSubmit={(e) => {
              applyFilters(e)
            }}
          >
            <Row className="g-3">
              {instance && (
                <Col xs={12} md={6} lg={4}>
                  <Form.Group controlId="audit-filter-project">
                    <Form.Label>{t('audit.filters.project')}</Form.Label>
                    <Form.Select
                      value={draft.project}
                      onChange={(e) => {
                        setDraft((d) => ({ ...d, project: e.target.value }))
                      }}
                    >
                      <option value="">{t('audit.filters.allProjects')}</option>
                      <option value={AUDIT_INSTANCE_ONLY}>{t('audit.filters.instanceOnly')}</option>
                      {projects.map((project) => (
                        <option key={project.uid} value={project.slug}>
                          {project.name}
                        </option>
                      ))}
                    </Form.Select>
                  </Form.Group>
                </Col>
              )}
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
                        {user.name}
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
                    <AuditEntryDetails
                      id={detailsId(record)}
                      record={record}
                      project={linkProjectOf(record, scope)}
                    />
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
function AuditTarget({ record, project }: { record: AuditRecord; project: LinkProject }) {
  const href = auditTargetHref(record)
  return (
    <>
      {record.target_type || '—'}
      {record.target_uid !== null && (
        <div className="small text-break">
          {href === null ? (
            <span className="text-secondary">{record.target_uid}</span>
          ) : (
            <EntryLink to={href} project={project}>
              {record.target_uid}
            </EntryLink>
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
  /** The project the payload's links open in (see {@link LinkProject}). */
  project: LinkProject
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
function AuditEntryDetails({ id, record, project }: AuditEntryDetailsProps) {
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
                    <EntryLink key={link.href} to={link.href} project={project} className="me-2">
                      {link.uid}
                    </EntryLink>
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
