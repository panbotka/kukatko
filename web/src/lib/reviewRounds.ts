import { type Photo } from '../services/photos'
import { type ReviewBreather, type ReviewQuestion, type ReviewReveal } from '../services/review'

/**
 * The round arithmetic of the review game, kept DOM-free so the decisions that
 * actually shape a session — where a breather lands, when a milestone fires,
 * whether today's mix is already done — are unit-testable without simulating a
 * whole game. The React plumbing lives in `useReviewGame`.
 */

/**
 * One card on the stage. A round is not a flat list of questions any more: it
 * carries the odd non-question card, which needs no answer and counts toward
 * nothing (see {@link buildRoundCards}).
 *
 * `key` is unique within a round, so it can key a React list and identify a card
 * without narrowing the union first.
 */
export type ReviewCard =
  | { type: 'question'; key: string; question: ReviewQuestion }
  | { type: 'breather'; key: string; breather: ReviewBreather }
  | { type: 'reveal'; key: string; reveal: ReviewReveal }

/** Wraps a question as the card that asks it. */
export function questionCard(question: ReviewQuestion): ReviewCard {
  return { type: 'question', key: question.id, question }
}

/** Wraps a backend breather as the card that shows it. */
export function breatherCard(breather: ReviewBreather): ReviewCard {
  return { type: 'breather', key: `breather:${breather.photo.uid}`, breather }
}

/** Wraps the payoff of a confirmed face as the card that reveals it. */
export function revealCard(reveal: ReviewReveal): ReviewCard {
  return {
    type: 'reveal',
    key: `reveal:${reveal.subject_uid}:${String(reveal.photo_count)}`,
    reveal,
  }
}

/** The photo a card shows, for preloading and for the session mosaic. */
export function cardPhoto(card: ReviewCard): Photo | undefined {
  switch (card.type) {
    case 'question':
      return card.question.photo
    case 'breather':
      return card.breather.photo
    case 'reveal':
      return undefined
  }
}

/**
 * Lays a round out as cards: the questions in the order the backend mixed them,
 * with the round's breathers spaced evenly between them.
 *
 * The spacing is `n · (i+1) / (k+1)` questions before breather `i`, so the usual
 * case — one breather in a round of ten — lands in the middle, where the run of
 * similar questions starts to feel like a belt. A round with no questions gets no
 * cards at all: a page of nothing but breathers is not a round, it is a
 * slideshow.
 */
export function buildRoundCards(
  questions: readonly ReviewQuestion[],
  breathers: readonly ReviewBreather[] = [],
): ReviewCard[] {
  if (questions.length === 0) {
    return []
  }
  const positions = new Map<number, ReviewBreather>()
  for (const [index, breather] of breathers.entries()) {
    const at = Math.ceil((questions.length * (index + 1)) / (breathers.length + 1))
    // Two breathers landing on one slot would stack into a double pause; the
    // second simply waits for the next question instead.
    let slot = at
    while (positions.has(slot) && slot < questions.length) {
      slot += 1
    }
    if (slot < questions.length) {
      positions.set(slot, breather)
    }
  }
  const cards: ReviewCard[] = []
  for (const [index, question] of questions.entries()) {
    const breather = positions.get(index)
    if (breather !== undefined) {
      cards.push(breatherCard(breather))
    }
    cards.push(questionCard(question))
  }
  return cards
}

/**
 * Places a freshly earned reveal into a round in flight.
 *
 * A reveal is the payoff of a confirmed face, so it takes the round's next
 * breather slot when there is one — a card the player has already been promised a
 * pause at, now carrying something they did rather than a stock photo. With no
 * slot left it gets one of its own, right *behind* the card on screen: never
 * replacing what the player is looking at, and never so far ahead that the
 * connection to the answer is lost.
 *
 * At most one reveal rides in a round; a run of confirmations is a good session,
 * not a reason to interrupt it ten times.
 */
export function insertReveal(cards: readonly ReviewCard[], reveal: ReviewReveal): ReviewCard[] {
  const next = [...cards]
  if (next.length === 0 || next.some((card) => card.type === 'reveal')) {
    return next
  }
  const slot = next.findIndex((card, index) => index > 0 && card.type === 'breather')
  if (slot > 0) {
    next[slot] = revealCard(reveal)
    return next
  }
  next.splice(1, 0, revealCard(reveal))
  return next
}

/**
 * The answer counts a session celebrates. They are spaced so the next one is
 * always plausibly within reach and never routine — a marker every ten answers
 * would stop being a moment by the third one.
 */
export const SESSION_MILESTONES: readonly number[] = [10, 25, 50]

/**
 * The milestone an answer just crossed, or null. Counts can jump by more than
 * one (a retried batch of failed answers), so the *highest* one crossed is
 * reported rather than the nearest — the player is told where they are, not where
 * they passed.
 */
export function milestoneCrossed(before: number, after: number): number | null {
  let reached: number | null = null
  for (const milestone of SESSION_MILESTONES) {
    if (before < milestone && after >= milestone) {
      reached = milestone
    }
  }
  return reached
}

/** Where the "today's mix is done" flag lives; one key, one date string. */
export const DAILY_STORAGE_KEY = 'kukatko.review.daily'

/**
 * The local calendar day as `YYYY-MM-DD`. Local, not UTC: "today" is the
 * player's day, and a UTC key would flip the daily mix over in the middle of a
 * Czech evening.
 */
export function localDayKey(now: Date): string {
  const month = String(now.getMonth() + 1).padStart(2, '0')
  const day = String(now.getDate()).padStart(2, '0')
  return `${String(now.getFullYear())}-${month}-${day}`
}

/**
 * The storage the daily flag uses, or undefined where there is none. Reading
 * `localStorage` throws in a browser with storage disabled, so every access here
 * is guarded — a lost flag costs a repeated "Dnešní mix" title, which is not
 * worth crashing the game over.
 */
function safeStorage(): Storage | undefined {
  try {
    return globalThis.localStorage
  } catch {
    return undefined
  }
}

/** Whether today's mix has already been finished on this device. */
export function dailyMixDone(now: Date, storage: Storage | undefined = safeStorage()): boolean {
  try {
    return storage?.getItem(DAILY_STORAGE_KEY) === localDayKey(now)
  } catch {
    return false
  }
}

/** Records that today's mix has been finished. */
export function markDailyMixDone(now: Date, storage: Storage | undefined = safeStorage()): void {
  try {
    storage?.setItem(DAILY_STORAGE_KEY, localDayKey(now))
  } catch {
    // A device that cannot remember simply gets today's mix offered again.
  }
}
