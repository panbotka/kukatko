import { type AuditListParams } from '../services/audit'

/**
 * URL-encoded view state for the audit-log page: the five filters, a date range,
 * and the pagination `offset`. Every value is a string so the whole view
 * round-trips through the query string and Back/Forward restores it exactly —
 * the project's "Zpět vždy funguje" convention. `since`/`until` hold the raw
 * `YYYY-MM-DD` value from the date inputs; {@link viewToParams} widens them to
 * RFC 3339 day boundaries when calling the API.
 *
 * A type alias rather than an interface, so it keeps the implicit index
 * signature the urlState `Record<string, string>` constraint requires.
 */
// eslint-disable-next-line @typescript-eslint/consistent-type-definitions -- see above
export type AuditView = {
  user: string
  action: string
  entity_type: string
  entity_uid: string
  since: string
  until: string
  offset: string
}

/**
 * Default audit view: no filters, first page. Declared at module scope so the
 * urlState setter keeps a stable identity and a value equal to a default is
 * omitted from the URL (keeping it shareable).
 */
export const AUDIT_DEFAULTS: AuditView = {
  user: '',
  action: '',
  entity_type: '',
  entity_uid: '',
  since: '',
  until: '',
  offset: '0',
}

/** Page size for the audit listing (the endpoint's own default). */
export const AUDIT_PAGE_SIZE = 100

/** The filter fields of the view, without the pagination `offset`. */
export type AuditFilters = Omit<AuditView, 'offset'>

/** Extracts just the filter fields from a full view (drops `offset`). */
export function pickFilters(view: AuditView): AuditFilters {
  return {
    user: view.user,
    action: view.action,
    entity_type: view.entity_type,
    entity_uid: view.entity_uid,
    since: view.since,
    until: view.until,
  }
}

/**
 * Widens a `YYYY-MM-DD` date to an RFC 3339 timestamp at the given end of the
 * day in UTC, or returns `undefined` for an empty value. The boundary is UTC
 * (not the viewer's local zone) so the same URL yields the same page regardless
 * of where it is opened; `since` takes the day's start, `until` its inclusive
 * end.
 */
function dayBoundary(date: string, edge: 'start' | 'end'): string | undefined {
  if (date === '') {
    return undefined
  }
  return `${date}T${edge === 'start' ? '00:00:00' : '23:59:59'}Z`
}

/** Maps the URL view onto the audit service's request parameters. */
export function viewToParams(view: AuditView): AuditListParams {
  return {
    user: view.user,
    action: view.action,
    entity_type: view.entity_type,
    entity_uid: view.entity_uid,
    since: dayBoundary(view.since, 'start'),
    until: dayBoundary(view.until, 'end'),
    limit: AUDIT_PAGE_SIZE,
    offset: Number(view.offset) || 0,
  }
}
