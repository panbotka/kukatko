import { ApiError } from './auth'
import { type Label } from './organize'
import { type Bbox, type Subject } from './people'
import { type Photo } from './photos'

/**
 * Review-game client, mirroring `internal/reviewapi`: `GET /review/queue` hands
 * the player a batch of one-at-a-time yes/no/skip questions targeted at the
 * uncertainty band, `POST /review/answer` applies one verdict through the
 * existing write paths. The session cookie is sent automatically (same-origin);
 * every call throws {@link ApiError} on a non-OK response.
 */

const API_BASE = '/api/v1'

/** Standard backend error envelope shared by every API group. */
interface ErrorBody {
  error?: string
}

/** Extracts the backend error message from a non-OK response, if present. */
async function readErrorMessage(res: Response): Promise<string> {
  try {
    const body = (await res.json()) as ErrorBody
    if (typeof body.error === 'string' && body.error !== '') {
      return body.error
    }
  } catch {
    // Body was empty or not JSON; fall back to the status text below.
  }
  return res.statusText || `request failed: ${res.status}`
}

/** Issues a GET and parses the JSON body, throwing ApiError on a non-OK status. */
async function getJSON<T>(path: string, signal?: AbortSignal): Promise<T> {
  const res = await fetch(`${API_BASE}${path}`, {
    method: 'GET',
    credentials: 'same-origin',
    signal,
  })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
  return (await res.json()) as T
}

/** Issues a JSON POST and parses the body, throwing ApiError on a non-OK status. */
async function postJSON<T>(path: string, body: unknown, signal?: AbortSignal): Promise<T> {
  const res = await fetch(`${API_BASE}${path}`, {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
    signal,
  })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
  return (await res.json()) as T
}

/**
 * What a question asks about (`review.Kind`):
 *
 * - `face` — is this unnamed face a given person?
 * - `label` — should this photo carry a given label?
 * - `place` — was this photo taken where the geo-estimator guessed?
 * - `duplicate` — are these two near-identical photos the same shot?
 * - `outlier` — is this face, already assigned to X, really X?
 *
 * The last three check work the machine already *did*, so their answers settle
 * something rather than merely teaching a search. None of them destroys
 * anything: the duplicate check records an opinion and never merges.
 */
export type ReviewKind = 'face' | 'label' | 'place' | 'duplicate' | 'outlier'

/** The estimated location a place question is about (`review.PlaceGuess`). */
export interface ReviewPlaceGuess {
  /** The most specific name of the place — never empty. */
  name: string
  country?: string
  city?: string
  place_name?: string
  lat: number
  lng: number
}

/** The player's verdict on one question. */
export type ReviewAnswer = 'yes' | 'no' | 'skip'

/**
 * A face bounding box in both spaces the UI needs (`candidates.FaceBox`):
 * display-relative (0..1, already EXIF-oriented) and display pixels.
 */
export interface ReviewFaceBox {
  relative: Bbox
  pixel: [number, number, number, number]
}

/** One question of the review game (`review.Question`). */
export interface ReviewQuestion {
  /** Stable, content-derived id the answer endpoint takes. */
  id: string
  kind: ReviewKind
  /**
   * Which confidence tier the question was drawn from: `sure` (the answer is
   * almost certainly yes) or `band` (genuinely uncertain). The UI asks the same
   * question either way — it is carried so the mix can be observed.
   */
  tier?: 'sure' | 'band'
  /** The candidate's 0–1 confidence (1 − cosine distance). */
  confidence: number
  /** The full catalog record with media URLs stamped. */
  photo: Photo
  /** The person under question (face and outlier questions). */
  subject?: Subject
  /** The face's per-photo slot (face and outlier questions). */
  face_index?: number
  /** The face's bounding box (face and outlier questions). */
  bbox?: ReviewFaceBox
  /** What confirming would do (face questions only). */
  action?: 'create_marker' | 'assign_person'
  /**
   * The marker the answer acts on: the one a face yes would assign, or the one
   * an outlier no would detach the person from.
   */
  marker_uid?: string
  /** The label under question (label questions only). */
  label?: Label
  /** The estimated location under question (place questions only). */
  place?: ReviewPlaceGuess
  /** The second photo of the pair (duplicate questions only). */
  other?: Photo
  /** The duplicate group the pair belongs to (duplicate questions only). */
  group_id?: string
  /** The face's cosine distance from its person's centroid (outlier questions). */
  distance?: number
}

/**
 * What the game may ask about (`review.Source`): only faces, only labels, or
 * both interleaved. These literal values are the `source` query parameter the
 * backend accepts, and the choice is applied *inside* the queue build — asking
 * for labels does not run the face scan at all.
 */
export type ReviewSource = 'both' | 'people' | 'labels'

/** The sources in the order the toggle presents them; `both` is the default. */
export const REVIEW_SOURCES: readonly ReviewSource[] = ['both', 'people', 'labels']

/** The library has no named people and no labels yet — the game has no sources. */
export const REASON_NO_SOURCES = 'no_people_no_labels'

/** The game is restricted to people, but no one is named yet. */
export const REASON_NO_PEOPLE = 'no_people'

/** The game is restricted to labels, but no label has photos yet. */
export const REASON_NO_LABELS = 'no_labels'

/** Sources exist but no candidate currently falls into the uncertainty band. */
export const REASON_NO_CANDIDATES = 'no_candidates'

/** Response body of `GET /review/queue` (`review.QueueResult`). */
export interface ReviewQueue {
  questions: ReviewQuestion[]
  /** The applied source, echoed back so a stale batch is recognisable. */
  source: ReviewSource
  /** How many questions this session answered so far. */
  answered: number
  /** Rough estimate of how many candidates are still queued. */
  remaining: number
  /**
   * Explains an empty queue: {@link REASON_NO_SOURCES}, {@link REASON_NO_PEOPLE},
   * {@link REASON_NO_LABELS} or {@link REASON_NO_CANDIDATES}.
   */
  reason?: string
}

/** Response body of `POST /review/answer` (`review.AnswerResult`). */
export interface ReviewAnswerResult {
  /**
   * What the answer wrote: assigned, labeled, confirmed, cleared, detached,
   * rejected, skipped, already_answered or gone.
   */
  result: string
  answered: number
  remaining: number
}

/**
 * Fetches the next batch of questions for the signed-in user from `source`
 * (default both). The queue is cached server-side per user *and per source*, so
 * refetching between batches is cheap while a switched source always rebuilds;
 * an omitted limit uses the server's configured batch size.
 */
export async function fetchReviewQueue(
  source: ReviewSource = 'both',
  limit?: number,
  signal?: AbortSignal,
): Promise<ReviewQueue> {
  const params = new URLSearchParams({ source })
  if (limit !== undefined && limit > 0) {
    params.set('limit', String(limit))
  }
  return getJSON<ReviewQueue>(`/review/queue?${params.toString()}`, signal)
}

/**
 * Applies one verdict via `POST /review/answer`. Answers are idempotent
 * server-side (a repeat returns `already_answered` without a second write) and
 * a vanished target returns `gone` rather than an error, so the caller can fire
 * optimistically and simply move on.
 */
export async function answerReview(
  questionId: string,
  answer: ReviewAnswer,
  signal?: AbortSignal,
): Promise<ReviewAnswerResult> {
  return postJSON<ReviewAnswerResult>('/review/answer', { question_id: questionId, answer }, signal)
}

/**
 * The time span the leaderboard aggregates over (`review.LeaderboardWindow`):
 * all-time, the rolling last seven days, or since midnight today. These literal
 * values are the `window` query parameter the backend accepts.
 */
export type LeaderboardWindow = 'all' | '7d' | 'today'

/** The ordered set of windows, in the order the toggle presents them. */
export const LEADERBOARD_WINDOWS: readonly LeaderboardWindow[] = ['all', '7d', 'today']

/**
 * One user's review-decision tally on the leaderboard, mirroring
 * `reviewapi.leaderboardEntry` (a `review.LeaderboardEntry` plus `is_me`). Total
 * is always `yes_count + no_count`, so the board ranks on it directly.
 */
export interface LeaderboardEntry {
  /** The acting user's uid, so the caller's own row is findable. */
  user_uid: string
  /** The user's display name, falling back to their username when blank. */
  display_name: string
  /** Confirmations recorded through the game (face assign + label attach). */
  yes_count: number
  /** Rejections recorded through the game (face reject + label reject). */
  no_count: number
  /** `yes_count + no_count`, the value the board is ranked on. */
  total: number
  /** True for the authenticated caller's own row. */
  is_me: boolean
}

/** Response body of `GET /review/leaderboard` (`reviewapi.leaderboardResponse`). */
export interface Leaderboard {
  /** The window that was applied ("all", "7d" or "today"). */
  window: LeaderboardWindow
  /** The caller's uid, so their row is locatable even with no entries yet. */
  caller_uid: string
  /** The ranked board, highest total first; never null. */
  entries: LeaderboardEntry[]
}

/**
 * Fetches the review competition standings for a window. Any signed-in user may
 * read the board (the backend gates it behind RequireAuth, not RequireWrite),
 * so viewers can watch the game too.
 */
export async function fetchLeaderboard(
  window: LeaderboardWindow,
  signal?: AbortSignal,
): Promise<Leaderboard> {
  return getJSON<Leaderboard>(`/review/leaderboard?window=${encodeURIComponent(window)}`, signal)
}
