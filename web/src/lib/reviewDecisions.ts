import { type AuditListParams, type AuditRecord } from '../services/audit'

/**
 * View-model helpers for the admin per-user review-decision view
 * (`web/src/pages/ReviewDecisionsPage.tsx`). They turn the raw audit records the
 * `via=review` filter returns into the small shape the page renders — Ano/Ne, the
 * person or label involved, the photo — and map the page's URL view state onto
 * the audit service's request parameters. Kept apart from the page so the mapping
 * is unit-testable without rendering, mirroring `web/src/lib/auditView.ts`.
 *
 * The four review actions and their `details` shapes are written by
 * `internal/review` (answer.go) and `internal/facematch` (apply.go); this file is
 * the frontend's read side of that contract.
 */

/** Audit action for a review "yes" on a face (subject assigned to a marker). */
export const ACTION_FACE_ASSIGN = 'face.assign'
/** Audit action for a review "yes" on a label (label attached to a photo). */
export const ACTION_LABEL_ATTACH = 'label.attach'
/** Audit action for a review "no" on a face (face↔subject rejected). */
export const ACTION_FACE_REJECT = 'face.reject'
/** Audit action for a review "no" on a label (label rejected for a photo). */
export const ACTION_LABEL_REJECT = 'label.reject'

/** Whether a decision confirmed (Ano) or rejected (Ne) the suggestion. */
export type Verdict = 'yes' | 'no'

/** Whether the decision was about a person/subject (face) or a label. */
export type DecisionKind = 'face' | 'label'

/**
 * The Ano/Ne filter carried in the URL: an empty string shows both buckets,
 * `yes`/`no` restrict to one. The page toggle writes it; {@link viewToAuditParams}
 * forwards it to the backend so paging stays correct per bucket.
 */
export type DecisionFilter = '' | 'yes' | 'no'

/**
 * URL-encoded view state for the decision page: the selected user, the Ano/Ne
 * filter, and the pagination offset. Every value is a string so the whole view
 * round-trips through the query string and Back/Forward restores it — the
 * project's "Zpět vždy funguje" convention.
 *
 * A type alias rather than an interface so it keeps the implicit index signature
 * the urlState `Record<string, string>` constraint requires.
 */
// eslint-disable-next-line @typescript-eslint/consistent-type-definitions -- see above
export type ReviewDecisionsView = {
  user: string
  decision: string
  offset: string
}

/**
 * Default view: no user, both buckets, first page. Declared at module scope so
 * the urlState setter keeps a stable identity and a value equal to a default is
 * omitted from the URL (keeping it shareable).
 */
export const REVIEW_DECISIONS_DEFAULTS: ReviewDecisionsView = {
  user: '',
  decision: '',
  offset: '0',
}

/** Page size for the decision listing (a thumbnail grid, so lighter than audit). */
export const REVIEW_DECISIONS_PAGE_SIZE = 60

/** Narrows a raw view value to a supported Ano/Ne filter, defaulting to both. */
export function parseDecisionFilter(raw: string): DecisionFilter {
  return raw === 'yes' || raw === 'no' ? raw : ''
}

/**
 * Maps the URL view onto the audit service's request parameters, always pinning
 * `via=review` so only the review game's decisions are returned, and forwarding
 * the Ano/Ne filter so the backend pages each bucket.
 */
export function viewToAuditParams(view: ReviewDecisionsView): AuditListParams {
  const decision = parseDecisionFilter(view.decision)
  return {
    user: view.user,
    via: 'review',
    decision: decision === '' ? undefined : decision,
    limit: REVIEW_DECISIONS_PAGE_SIZE,
    offset: Number(view.offset) || 0,
  }
}

/**
 * One review decision as the page renders it: the Ano/Ne verdict, whether it was
 * about a person or a label, the resolved target name, the photo it referenced
 * (with a face index for face decisions), and when it happened.
 */
export interface ReviewDecision {
  id: number
  verdict: Verdict
  kind: DecisionKind
  /** The photo the decision was made on, or null when the record carries none. */
  photoUid: string | null
  /** The face's index on that photo for face decisions, else null. */
  faceIndex: number | null
  /** The person or label the decision concerned, resolved to a display name. */
  targetName: string
  createdAt: string
}

/** A UID→display-name lookup built from a subjects or labels roster. */
export type NameMap = ReadonlyMap<string, string>

/** Reads a string field from an audit record's details, or null when absent. */
function detailString(details: AuditRecord['details'], key: string): string | null {
  const value = details?.[key]
  return typeof value === 'string' && value !== '' ? value : null
}

/** Reads a numeric field from an audit record's details, or null when absent. */
function detailNumber(details: AuditRecord['details'], key: string): number | null {
  const value = details?.[key]
  return typeof value === 'number' ? value : null
}

/**
 * Resolves the person or label a decision concerned to a display name, using the
 * name recorded in the audit details when present (face confirmations carry the
 * subject name) and otherwise the roster map, falling back to the raw UID so the
 * row is never blank.
 */
function resolveTargetName(
  record: AuditRecord,
  kind: DecisionKind,
  subjects: NameMap,
  labels: NameMap,
): string {
  if (kind === 'label') {
    const uid = record.target_uid
    return (uid !== null ? labels.get(uid) : undefined) ?? uid ?? ''
  }
  // Face: a confirmation names the subject in details.subject_uid; a rejection
  // targets the subject directly (target_uid).
  const subjectUid = detailString(record.details, 'subject_uid') ?? record.target_uid
  const recorded = detailString(record.details, 'subject_name')
  const looked = subjectUid !== null ? subjects.get(subjectUid) : undefined
  return recorded ?? looked ?? subjectUid ?? ''
}

/**
 * Turns one `via=review` audit record into a {@link ReviewDecision}, resolving
 * the target name against the supplied rosters. Returns null for an action
 * outside the four review decisions, so a stray record never renders a blank row.
 */
export function toReviewDecision(
  record: AuditRecord,
  subjects: NameMap,
  labels: NameMap,
): ReviewDecision | null {
  let verdict: Verdict
  let kind: DecisionKind
  switch (record.action) {
    case ACTION_FACE_ASSIGN:
      verdict = 'yes'
      kind = 'face'
      break
    case ACTION_LABEL_ATTACH:
      verdict = 'yes'
      kind = 'label'
      break
    case ACTION_FACE_REJECT:
      verdict = 'no'
      kind = 'face'
      break
    case ACTION_LABEL_REJECT:
      verdict = 'no'
      kind = 'label'
      break
    default:
      return null
  }
  return {
    id: record.id,
    verdict,
    kind,
    photoUid: detailString(record.details, 'photo_uid'),
    faceIndex: detailNumber(record.details, 'face_index'),
    targetName: resolveTargetName(record, kind, subjects, labels),
    createdAt: record.created_at,
  }
}
