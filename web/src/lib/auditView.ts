import { type AuditListParams, type AuditRecord } from '../services/audit'

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

/**
 * Where an audit target opens, keyed by the `target_type` the backend records.
 * This single table drives both the Target column and the UIDs inside a details
 * payload, so the two can never disagree about where a photo (or an album, a
 * label, a person) lives.
 *
 * Target types with no page of their own — `users`, `api_tokens`,
 * `announcement`, `audit_log` — are deliberately absent and stay plain text.
 * `markers` is absent for a different reason: a marker UID addresses nothing on
 * its own, so it is routed through the entry's own details to the photo it sits
 * on (see {@link markerHref}).
 */
const TARGET_ROUTES: Record<string, string | undefined> = {
  photos: '/photos',
  albums: '/albums',
  labels: '/labels',
  subjects: '/people',
}

/**
 * The entity a details key names, mapped onto its `target_type` — so
 * `photo_uid`/`photo_uids` resolve through the very same {@link TARGET_ROUTES}
 * entry as a `photos` target does.
 */
const DETAIL_KEY_ENTITIES: Record<string, string | undefined> = {
  photo: 'photos',
  album: 'albums',
  label: 'labels',
  subject: 'subjects',
  marker: 'markers',
}

/** A details key naming one entity (`photo_uid`) or many (`photo_uids`). */
const DETAIL_UID_KEY = /^([a-z]+)_uids?$/

/**
 * How many links the expanded details render before they stop. A bulk edit lists
 * every photo it touched — hundreds of UIDs — and the raw payload below the links
 * carries the full list anyway, so the block shows a readable prefix and says how
 * many it left out.
 */
export const AUDIT_DETAIL_LINK_LIMIT = 25

/** The photo viewer's URL, with one person's marker in view when given. */
function photoHref(photoUid: string, subjectUid: string | null): string {
  const path = `/photos/${encodeURIComponent(photoUid)}`
  if (subjectUid === null) {
    return path
  }
  // `person` scopes the viewer to that person and `info=1` opens the panel where
  // the faces are named, so a face entry lands on the marker it is about.
  return `${path}?person=${encodeURIComponent(subjectUid)}&info=1`
}

/** Reads a non-empty string field out of a details payload, or null. */
function detailString(details: Record<string, unknown> | null, key: string): string | null {
  const value = details?.[key]
  return typeof value === 'string' && value !== '' ? value : null
}

/**
 * Where a marker leads: the photo it sits on, named by the entry's own details,
 * focused on the person when the payload names one. Null when the payload does
 * not say which photo — a marker UID alone addresses no page, so there is
 * nothing honest to link to.
 */
function markerHref(details: Record<string, unknown> | null): string | null {
  const photoUid = detailString(details, 'photo_uid')
  return photoUid === null ? null : photoHref(photoUid, detailString(details, 'subject_uid'))
}

/** Resolves one entity reference (`target_type` + UID) to its in-app route. */
function entityHref(
  type: string,
  uid: string,
  details: Record<string, unknown> | null,
): string | null {
  if (type === 'markers') {
    return markerHref(details)
  }
  const base = TARGET_ROUTES[type]
  return base === undefined ? null : `${base}/${encodeURIComponent(uid)}`
}

/**
 * The in-app destination of an audit entry's target — the photo, album, label or
 * person whose change the entry records — or null when the target addresses no
 * page (a user, an API token, the announcement) or the entry names no target at
 * all (a bulk action lists its targets in `details` instead).
 *
 * The link is offered even for an entity that is already gone. The audit log
 * outlives what it audits — it is the record of the deletion — so a dead link is
 * normal, not exceptional, and the destination pages say "this no longer exists"
 * (`albumDetail.missing` and friends) instead of failing blankly. Hiding the
 * link would only send the reader back to copying UIDs by hand.
 */
export function auditTargetHref(record: AuditRecord): string | null {
  // A marker takes its destination from the details, so it needs no target UID;
  // every other type addresses its own page and cannot do without one.
  if (
    record.target_type !== 'markers' &&
    (record.target_uid === null || record.target_uid === '')
  ) {
    return null
  }
  return entityHref(record.target_type, record.target_uid ?? '', record.details)
}

/** One UID found inside a details payload, and where it leads. */
export interface AuditUidLink {
  uid: string
  /** In-app destination, by the same mapping the Target column uses. */
  href: string
}

/** The UIDs one details key names, grouped so the key is printed once. */
export interface AuditLinkGroup {
  /** The payload key the UIDs came from, e.g. `photo_uid` or `photo_uids`. */
  key: string
  links: AuditUidLink[]
}

/** What {@link auditDetailLinks} found in one entry's details payload. */
export interface AuditDetailLinks {
  groups: AuditLinkGroup[]
  /** References dropped past the limit; 0 when they all fit. */
  hidden: number
}

/** The UIDs a details value holds: one string, a list of them, or nothing. */
function uidList(value: unknown): string[] {
  const items: unknown[] = Array.isArray(value) ? (value as unknown[]) : [value]
  return items.filter((item): item is string => typeof item === 'string' && item !== '')
}

/**
 * Collects the linkable UIDs a details payload names, grouped by the key they
 * came from and resolved through the same table as {@link auditTargetHref}.
 *
 * The target is not always the useful destination: `label.reject` targets the
 * label but carries the photo it was rejected on, and a bulk action leaves the
 * target empty and lists every photo it touched. Both are only reachable from
 * the payload, which is why the keys are linked at all.
 *
 * Only `<entity>_uid` / `<entity>_uids` keys of a known entity are considered;
 * anything else (`ps_uid`, `keeper_uid`, a plain `count`) is left to the raw
 * payload. A destination already linked is not repeated — a face entry naming
 * both `photo_uid` and `marker_uid` on the same photo yields one link, not two.
 */
export function auditDetailLinks(
  details: Record<string, unknown> | null,
  limit: number = AUDIT_DETAIL_LINK_LIMIT,
): AuditDetailLinks {
  if (details === null) {
    return { groups: [], hidden: 0 }
  }
  const groups: AuditLinkGroup[] = []
  const seen = new Set<string>()
  let taken = 0
  let hidden = 0
  for (const [key, value] of Object.entries(details)) {
    const match = DETAIL_UID_KEY.exec(key)
    if (match === null) {
      continue
    }
    const entity = DETAIL_KEY_ENTITIES[match[1]]
    if (entity === undefined) {
      continue
    }
    const links: AuditUidLink[] = []
    for (const uid of uidList(value)) {
      const href = entityHref(entity, uid, details)
      if (href === null || seen.has(href)) {
        continue
      }
      seen.add(href)
      if (taken >= limit) {
        hidden += 1
        continue
      }
      taken += 1
      links.push({ uid, href })
    }
    if (links.length > 0) {
      groups.push({ key, links })
    }
  }
  return { groups, hidden }
}
