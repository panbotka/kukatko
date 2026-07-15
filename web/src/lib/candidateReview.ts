import { type ParseKeys } from 'i18next'

import { type AssignRequest } from '../services/people'
import { type Candidate } from '../services/faces'
import { type FaceRejection } from '../services/feedback'

/**
 * The pure state model behind the /faces review grid.
 *
 * A candidate moves through a tiny lifecycle as the user works: `pending` (waiting
 * for a verdict), `done` (confirmed optimistically, or it was already this subject),
 * `error` (its confirm failed and can be retried). A rejected candidate is removed
 * from the list entirely — it never comes back.
 *
 * Each card belongs to one of three **buckets**, which drive both the filter tabs
 * and the single colour code shared by the badge, the bounding rectangle and the
 * legend: `new` (needs a new marker), `assign` (needs a person on an existing
 * marker), `done` (nothing left to do). Keeping this logic here — free of React —
 * lets the tab counts, the colours and the "confirm all" scope be tested directly.
 */

/** Where a candidate is in its confirm lifecycle. */
export type CandidateStatus = 'pending' | 'done' | 'error'

/** A candidate paired with its live review status. */
export interface ReviewItem {
  candidate: Candidate
  status: CandidateStatus
}

/** The colour code, keyed by bucket; also the set of filter tabs beyond "all". */
export type Bucket = 'new' | 'assign' | 'done'

/** A filter tab: every bucket, plus the catch-all "all". */
export type FilterTab = 'all' | Bucket

/** The filter tabs, in display order. */
export const FILTER_TABS: readonly FilterTab[] = ['all', 'new', 'assign', 'done']

/**
 * The Bootstrap contextual colour per bucket, used for the `<Badge bg>` variant and
 * (via CSS custom properties) the bounding rectangle and legend swatch. One map, so
 * the three can never disagree.
 */
export const BUCKET_VARIANT: Record<Bucket, string> = {
  new: 'info',
  assign: 'warning',
  done: 'success',
}

/** The i18n label key for each filter tab. */
export const TAB_LABEL_KEY: Record<FilterTab, ParseKeys> = {
  all: 'faceSearch.tabs.all',
  new: 'faceSearch.tabs.new',
  assign: 'faceSearch.tabs.assign',
  done: 'faceSearch.tabs.done',
}

/** The i18n label key for each bucket's legend entry. */
export const BUCKET_LABEL_KEY: Record<Bucket, ParseKeys> = {
  new: 'faceSearch.legend.new',
  assign: 'faceSearch.legend.assign',
  done: 'faceSearch.legend.done',
}

/**
 * candidateKey returns a stable identifier for a candidate (its photo and face),
 * used to address a card across re-renders and filtering without relying on its list
 * index, which shifts as rejected cards leave.
 */
export function candidateKey(candidate: Candidate): string {
  return `${candidate.photo.uid}:${String(candidate.face_index)}`
}

/**
 * initialStatus is the status a freshly-loaded candidate starts in: `already_done`
 * candidates are complete on arrival (a rare stale-cache case), everything else is
 * pending a verdict.
 */
export function initialStatus(candidate: Candidate): CandidateStatus {
  return candidate.action === 'already_done' ? 'done' : 'pending'
}

/** toReviewItems seeds the review list from a fresh search result, nearest first. */
export function toReviewItems(candidates: Candidate[]): ReviewItem[] {
  return candidates.map((candidate) => ({ candidate, status: initialStatus(candidate) }))
}

/**
 * bucketOf places an item in its colour/tab bucket. A confirmed (or already-done)
 * item is `done`; otherwise it is bucketed by what confirming it would do —
 * `assign_person` needs a person, everything else needs a new marker.
 */
export function bucketOf(item: ReviewItem): Bucket {
  if (item.status === 'done') {
    return 'done'
  }
  return item.candidate.action === 'assign_person' ? 'assign' : 'new'
}

/**
 * isActionable reports whether "confirm" still applies to an item: a pending card,
 * or one whose confirm errored and can be retried. Done and in-flight cards are not.
 */
export function isActionable(item: ReviewItem): boolean {
  return item.status === 'pending' || item.status === 'error'
}

/** matchesTab reports whether an item belongs under the given filter tab. */
export function matchesTab(item: ReviewItem, tab: FilterTab): boolean {
  return tab === 'all' || bucketOf(item) === tab
}

/** tabCounts tallies how many items sit under each filter tab. */
export function tabCounts(items: ReviewItem[]): Record<FilterTab, number> {
  const counts: Record<FilterTab, number> = { all: items.length, new: 0, assign: 0, done: 0 }
  for (const item of items) {
    counts[bucketOf(item)] += 1
  }
  return counts
}

/**
 * buildAssignRequest routes the confirm call for a candidate, mirroring the on-photo
 * `useFaces` logic: an existing overlapping marker means assign the person to it; an
 * unmarked face means create a marker at its box and assign there. Sharing this rule
 * keeps a confirm from ever creating a duplicate marker over one that already exists.
 */
export function buildAssignRequest(candidate: Candidate, subjectUid: string): AssignRequest {
  if (candidate.marker_uid !== undefined && candidate.marker_uid !== '') {
    return { action: 'assign_person', marker_uid: candidate.marker_uid, subject_uid: subjectUid }
  }
  return {
    action: 'create_marker',
    face_index: candidate.face_index,
    bbox: candidate.bbox.relative,
    subject_uid: subjectUid,
  }
}

/** buildRejection builds the persisted "not this person" feedback for a candidate. */
export function buildRejection(candidate: Candidate, subjectUid: string): FaceRejection {
  return {
    photo_uid: candidate.photo.uid,
    face_index: candidate.face_index,
    subject_uid: subjectUid,
  }
}
