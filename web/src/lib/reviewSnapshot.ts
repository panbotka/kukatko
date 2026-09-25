import type { AnsweredQuestion, RoundProgress, SessionTally } from '../hooks/useReviewGame'
import { type Photo } from '../services/photos'
import {
  REVIEW_SOURCES,
  type ReviewAnswer,
  type ReviewBreather,
  type ReviewKind,
  type ReviewQuestion,
  type ReviewSource,
} from '../services/review'

import { type ReviewCard } from './reviewRounds'

/**
 * The review game's run, kept across a round trip away from `/review`.
 *
 * The game holds its whole run in memory, so without this a peek at a photo's
 * own page — and straight back — threw the queue, the round position, the
 * tallies and the combo away. The snapshot is what brings them back: the
 * question payloads exactly as `GET /review/queue` handed them over, plus the
 * counters. Nothing about animation, drag or the DOM, and no image data — the
 * cards re-derive their pictures from the photo uids.
 *
 * Session storage, not local: a run is a sitting, and a half-played round from
 * last week is not where anybody wants to land. It is also scoped to the user
 * who played it, because a shared browser must never hand one account's queue
 * — or its tallies — to another.
 */

/** sessionStorage key holding the run. One key: there is one game per tab. */
export const REVIEW_SNAPSHOT_KEY = 'kukatko.review.run'

/**
 * The snapshot's shape version. Bump it whenever the stored shape changes: a
 * snapshot of any other version is ignored, never half-read.
 */
export const REVIEW_SNAPSHOT_VERSION = 1

/** Everything a resumed run needs, as it is written to storage. */
export interface ReviewSnapshot {
  version: typeof REVIEW_SNAPSHOT_VERSION
  /** The uid of the user who played the run; nobody else may resume it. */
  user: string
  /** The "Na co se ptát" source the queue was built for. */
  source: ReviewSource
  /** The local queue, the card on screen first. */
  queue: ReviewCard[]
  /** The round in progress, including its "daily" flag. */
  round: RoundProgress
  combo: number
  /** The combo an undo would restore; paired with `combo`. */
  comboBefore: number
  /** The session's yes/no count (the header's "answered"). */
  answered: number
  session: SessionTally
  /** The photos decided about this session, for the closing mosaic. */
  touched: Photo[]
  remaining: number
  /** Every question id enqueued this session, so a refill cannot repeat one. */
  seen: string[]
  /** Undone questions whose next yes/no must take the direct write paths. */
  direct: string[]
  /** Marker uids learned during undo, keyed by question id. */
  markers: Record<string, string>
  /** Answers whose request failed and are still waiting for a retry. */
  failed: AnsweredQuestion[]
}

/**
 * The storage the snapshot uses, or undefined where there is none. Merely
 * touching `sessionStorage` throws in a browser with storage disabled.
 */
function safeStorage(): Storage | undefined {
  try {
    return globalThis.sessionStorage
  } catch {
    return undefined
  }
}

/**
 * Stores the run. Best-effort: a full or disabled storage costs the player the
 * resume and nothing else.
 */
export function writeReviewSnapshot(
  snapshot: ReviewSnapshot,
  storage: Storage | undefined = safeStorage(),
): void {
  try {
    storage?.setItem(REVIEW_SNAPSHOT_KEY, JSON.stringify(snapshot))
  } catch {
    // Quota or disabled storage — the next visit simply starts a fresh round.
  }
}

/** Forgets the run: a finished round, an ended session, an explicit exit. */
export function clearReviewSnapshot(storage: Storage | undefined = safeStorage()): void {
  try {
    storage?.removeItem(REVIEW_SNAPSHOT_KEY)
  } catch {
    // Nothing stored that could be read back either.
  }
}

/**
 * The run `user` left behind for `source`, or null when there is none to
 * resume: nothing stored, storage unavailable, a payload that does not parse or
 * does not have the shape this build writes, another user's run, or a run over
 * a different source. Null always means "start a fresh round", so the caller
 * never has to tell the reasons apart.
 */
export function readReviewSnapshot(
  user: string,
  source: ReviewSource,
  storage: Storage | undefined = safeStorage(),
): ReviewSnapshot | null {
  let raw: string | null
  try {
    raw = storage?.getItem(REVIEW_SNAPSHOT_KEY) ?? null
  } catch {
    return null
  }
  if (raw === null || user === '') {
    return null
  }
  let parsed: unknown
  try {
    parsed = JSON.parse(raw)
  } catch {
    return null
  }
  const snapshot = asSnapshot(parsed)
  if (snapshot?.user !== user || snapshot.source !== source) {
    return null
  }
  return snapshot
}

// ---------------------------------------------------------------------------
// Validation. A stored snapshot is untrusted input — an older build's shape, a
// hand edit, a truncated write — and the renderer dereferences what it finds,
// so everything it will read is checked here rather than trusted there.

type Obj = Record<string, unknown>

const KINDS: readonly ReviewKind[] = ['face', 'label', 'place', 'duplicate', 'outlier']
const ANSWERS: readonly ReviewAnswer[] = ['yes', 'no', 'skip']

function isObj(value: unknown): value is Obj {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function isString(value: unknown): value is string {
  return typeof value === 'string'
}

function isNumber(value: unknown): value is number {
  return typeof value === 'number' && Number.isFinite(value)
}

/** A counter: a finite, non-negative integer. */
function isCount(value: unknown): value is number {
  return isNumber(value) && Number.isInteger(value) && value >= 0
}

function isOptional(value: unknown, check: (v: unknown) => boolean): boolean {
  return value === undefined || check(value)
}

function isStringArray(value: unknown): value is string[] {
  return Array.isArray(value) && value.every(isString)
}

/** The fields of a photo the game's cards and the closing mosaic read. */
function isPhoto(value: unknown): value is Photo {
  return (
    isObj(value) &&
    isString(value.uid) &&
    value.uid !== '' &&
    isString(value.file_name) &&
    isNumber(value.file_width) &&
    isNumber(value.file_height) &&
    isOptional(value.file_orientation, isNumber) &&
    isOptional(value.title, isString)
  )
}

function isBbox(value: unknown): boolean {
  return Array.isArray(value) && value.length === 4 && value.every(isNumber)
}

/** A named reference — the subject or label a question is about. */
function isNamed(value: unknown): boolean {
  return isObj(value) && isString(value.uid) && isString(value.name)
}

function isQuestion(value: unknown): value is ReviewQuestion {
  if (!isObj(value)) {
    return false
  }
  const { bbox, place } = value
  return (
    isString(value.id) &&
    value.id !== '' &&
    KINDS.some((kind) => kind === value.kind) &&
    isNumber(value.confidence) &&
    isPhoto(value.photo) &&
    isOptional(value.subject, isNamed) &&
    isOptional(value.label, isNamed) &&
    isOptional(value.face_index, isNumber) &&
    isOptional(bbox, (b) => isObj(b) && isBbox(b.relative)) &&
    isOptional(value.marker_uid, isString) &&
    isOptional(place, (p) => isObj(p) && isString(p.name)) &&
    isOptional(value.other, isPhoto) &&
    isOptional(value.distance, isNumber)
  )
}

function isBreather(value: unknown): value is ReviewBreather {
  return (
    isObj(value) &&
    isPhoto(value.photo) &&
    isString(value.title) &&
    isString(value.reason) &&
    isOptional(value.year, isNumber)
  )
}

function isCard(value: unknown): value is ReviewCard {
  if (!isObj(value) || !isString(value.key)) {
    return false
  }
  switch (value.type) {
    case 'question':
      return isQuestion(value.question)
    case 'breather':
      return isBreather(value.breather)
    default:
      return false
  }
}

function isRound(value: unknown): value is RoundProgress {
  return (
    isObj(value) &&
    isCount(value.index) &&
    isCount(value.size) &&
    isCount(value.played) &&
    isCount(value.confirmed) &&
    isCount(value.rejected) &&
    isCount(value.skipped) &&
    typeof value.daily === 'boolean' &&
    typeof value.last === 'boolean' &&
    value.played <= value.size
  )
}

function isTally(value: unknown): value is SessionTally {
  return (
    isObj(value) && isCount(value.confirmed) && isCount(value.rejected) && isCount(value.skipped)
  )
}

function isAnswered(value: unknown): value is AnsweredQuestion {
  return isObj(value) && isQuestion(value.question) && ANSWERS.some((a) => a === value.answer)
}

function isMarkers(value: unknown): value is Record<string, string> {
  return isObj(value) && Object.values(value).every(isString)
}

/** Narrows a parsed value to a snapshot this build wrote, or null. */
function asSnapshot(value: unknown): ReviewSnapshot | null {
  if (!isObj(value) || value.version !== REVIEW_SNAPSHOT_VERSION) {
    return null
  }
  const valid =
    isString(value.user) &&
    REVIEW_SOURCES.some((source) => source === value.source) &&
    Array.isArray(value.queue) &&
    value.queue.length > 0 &&
    value.queue.every(isCard) &&
    isRound(value.round) &&
    isCount(value.combo) &&
    isCount(value.comboBefore) &&
    isCount(value.answered) &&
    isTally(value.session) &&
    Array.isArray(value.touched) &&
    value.touched.every(isPhoto) &&
    isCount(value.remaining) &&
    isStringArray(value.seen) &&
    isStringArray(value.direct) &&
    isMarkers(value.markers) &&
    Array.isArray(value.failed) &&
    value.failed.every(isAnswered)
  return valid ? (value as unknown as ReviewSnapshot) : null
}
