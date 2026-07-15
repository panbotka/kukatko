import { type Candidate } from '../services/faces'
import { type Subject } from '../services/people'

import { candidateKey, isActionable, type ReviewItem } from './candidateReview'

/**
 * Pure helpers behind the /recognition sweep page: the confidence slider's range, and
 * the flat keyboard-focus list that stitches every person's actionable cards into one
 * navigable sequence. Keeping this free of React lets the maths and the focus order be
 * tested directly, the same way {@link import('./faceThreshold')} does for /faces.
 *
 * The sweep is deliberately a tight, high-confidence dial: this page is for the easy
 * wins, so the slider starts high and never drops into exploration territory.
 */

/** Lowest confidence the sweep slider offers. */
export const SWEEP_MIN_PERCENT = 50
/** Highest confidence the sweep slider offers. */
export const SWEEP_MAX_PERCENT = 95
/** Slider granularity, in percentage points. */
export const SWEEP_STEP_PERCENT = 1
/** Where the slider starts before the user touches it — a tight default. */
export const SWEEP_DEFAULT_PERCENT = 75
/** Default per-person candidate cap the page offers. */
export const SWEEP_DEFAULT_LIMIT = 50

/**
 * clampConfidencePercent keeps a confidence inside the slider's supported range,
 * guarding against an out-of-range value arriving from a URL query parameter.
 */
export function clampConfidencePercent(percent: number): number {
  if (!Number.isFinite(percent)) {
    return SWEEP_DEFAULT_PERCENT
  }
  if (percent < SWEEP_MIN_PERCENT) {
    return SWEEP_MIN_PERCENT
  }
  if (percent > SWEEP_MAX_PERCENT) {
    return SWEEP_MAX_PERCENT
  }
  return Math.round(percent)
}

/** A person and its live review list, one card on the sweep page. */
export interface PersonState {
  subject: Subject
  items: ReviewItem[]
}

/** How many of a person's candidates still need a decision. */
export function personActionableCount(person: PersonState): number {
  return person.items.filter(isActionable).length
}

/**
 * hasActionable reports whether a person still has any candidate to decide. A person
 * with none has been fully cleared and its card should disappear — the shrinking list
 * is the reward loop.
 */
export function hasActionable(person: PersonState): boolean {
  return person.items.some(isActionable)
}

/** One target in the flat keyboard-focus sequence across all person cards. */
export interface FocusEntry {
  subjectUid: string
  key: string
  candidate: Candidate
}

/**
 * focusKey builds the page-global key for a candidate under a subject, so focus can be
 * addressed unambiguously even when the same face appears under two people.
 */
export function focusKey(subjectUid: string, candidate: Candidate): string {
  return `${subjectUid}::${candidateKey(candidate)}`
}

/**
 * focusSequence flattens every person's actionable candidates, in display order, into
 * the single list the arrow keys move through. Done and in-flight cards are skipped:
 * they are not decision targets.
 */
export function focusSequence(people: PersonState[]): FocusEntry[] {
  const entries: FocusEntry[] = []
  for (const person of people) {
    for (const item of person.items) {
      if (isActionable(item)) {
        entries.push({
          subjectUid: person.subject.uid,
          key: focusKey(person.subject.uid, item.candidate),
          candidate: item.candidate,
        })
      }
    }
  }
  return entries
}

/**
 * nextFocusKey returns the key of the next focus target after the given one, so the
 * flow can advance to the next decision after a confirm or reject. It returns the
 * first entry when the current key is absent (a just-cleared card), and null when the
 * sequence is empty.
 */
export function nextFocusKey(sequence: FocusEntry[], currentKey: string | null): string | null {
  if (sequence.length === 0) {
    return null
  }
  const index = sequence.findIndex((entry) => entry.key === currentKey)
  if (index === -1) {
    return sequence[0].key
  }
  if (index + 1 < sequence.length) {
    return sequence[index + 1].key
  }
  if (index - 1 >= 0) {
    return sequence[index - 1].key
  }
  return null
}
