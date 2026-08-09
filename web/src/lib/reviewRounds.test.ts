import { describe, expect, it } from 'vitest'

import { type Photo } from '../services/photos'
import { type ReviewBreather, type ReviewQuestion, type ReviewReveal } from '../services/review'

import {
  buildRoundCards,
  DAILY_STORAGE_KEY,
  dailyMixDone,
  insertReveal,
  localDayKey,
  markDailyMixDone,
  milestoneCrossed,
  type ReviewCard,
} from './reviewRounds'

/** A photo stub; only the uid is ever read by the round arithmetic. */
function photo(uid: string): Photo {
  return { uid, file_name: `${uid}.jpg` } as unknown as Photo
}

/** A face question with the given id. */
function question(id: string): ReviewQuestion {
  return { id, kind: 'face', confidence: 0.8, photo: photo(`p-${id}`) }
}

/** A breather card's payload. */
function breather(uid: string): ReviewBreather {
  return { kind: 'breather', photo: photo(uid), title: uid, reason: 'favorite' }
}

/** The payoff of a confirmed face. */
function reveal(name = 'Alois'): ReviewReveal {
  return { subject_uid: 's1', name, photo_count: 27, oldest_year: 1962 }
}

/** The card types in order, as a compact string for readable assertions. */
function shape(cards: readonly ReviewCard[]): string {
  return cards.map((card) => card.type.charAt(0)).join('')
}

describe('buildRoundCards', () => {
  it('lays the questions out in the order the backend mixed them', () => {
    const cards = buildRoundCards([question('a'), question('b'), question('c')])
    expect(shape(cards)).toBe('qqq')
    expect(cards.map((card) => card.key)).toEqual(['a', 'b', 'c'])
  })

  it('drops the round’s one breather into the middle', () => {
    // The pause belongs where the run of similar questions starts to feel like a
    // belt — not at the edges, where it is either a splash screen or an outro.
    const questions = Array.from({ length: 10 }, (_v, n) => question(`q${String(n)}`))
    const cards = buildRoundCards(questions, [breather('b1')])
    expect(shape(cards)).toBe('qqqqqbqqqqq')
  })

  it('spaces several breathers evenly instead of stacking them', () => {
    const questions = Array.from({ length: 9 }, (_v, n) => question(`q${String(n)}`))
    const cards = buildRoundCards(questions, [breather('b1'), breather('b2')])
    expect(shape(cards)).toBe('qqqbqqqbqqq')
  })

  it('builds nothing at all from a round with no questions', () => {
    // A page of nothing but breathers is not a round, it is a slideshow — and
    // the game would report progress through a round nobody is playing.
    expect(buildRoundCards([], [breather('b1')])).toEqual([])
  })
})

describe('insertReveal', () => {
  it('takes the next breather slot, so the round keeps its one pause', () => {
    const cards = buildRoundCards([question('a'), question('b')], [breather('b1')])
    expect(shape(cards)).toBe('qbq')
    const withReveal = insertReveal(cards, reveal())
    expect(shape(withReveal)).toBe('qrq')
  })

  it('gets a slot behind the current card when no breather is left', () => {
    const cards = buildRoundCards([question('a'), question('b')])
    const withReveal = insertReveal(cards, reveal())
    // Never in place of what the player is looking at, and never so far ahead
    // that the payoff loses its connection to the answer that earned it.
    expect(shape(withReveal)).toBe('qrq')
  })

  it('never replaces the card on screen, even when that card is the breather', () => {
    const cards: ReviewCard[] = buildRoundCards([question('a')], [breather('b1')])
    // A round whose breather sits first: it is what the player is looking at.
    const onScreenBreather = [cards[0], ...cards.slice(1)]
    const withReveal = insertReveal(onScreenBreather, reveal())
    expect(withReveal[0]).toEqual(cards[0])
  })

  it('rides only once per round', () => {
    const cards = buildRoundCards([question('a'), question('b'), question('c')])
    const once = insertReveal(cards, reveal('Alois'))
    const twice = insertReveal(once, reveal('Bára'))
    expect(twice.filter((card) => card.type === 'reveal')).toHaveLength(1)
  })

  it('drops a reveal that arrives after the round is over', () => {
    expect(insertReveal([], reveal())).toEqual([])
  })
})

describe('milestoneCrossed', () => {
  it('fires exactly on the marker and never again', () => {
    expect(milestoneCrossed(9, 10)).toBe(10)
    expect(milestoneCrossed(10, 11)).toBeNull()
    expect(milestoneCrossed(24, 25)).toBe(25)
    expect(milestoneCrossed(49, 50)).toBe(50)
  })

  it('reports the highest marker a jump crossed', () => {
    // A retried batch of failed answers can land several at once; the player is
    // told where they are, not where they passed.
    expect(milestoneCrossed(0, 30)).toBe(25)
  })

  it('is silent below the first marker and after the last', () => {
    expect(milestoneCrossed(0, 9)).toBeNull()
    expect(milestoneCrossed(60, 61)).toBeNull()
  })
})

describe('the daily mix flag', () => {
  it('keys on the local calendar day, not on UTC', () => {
    // Late on a Czech evening the UTC date has not turned yet (and in summer it
    // is already tomorrow at midnight local) — the player's day is what counts.
    expect(localDayKey(new Date(2026, 7, 9, 23, 30))).toBe('2026-08-09')
    expect(localDayKey(new Date(2026, 0, 1, 0, 5))).toBe('2026-01-01')
  })

  it('remembers a finished mix for the rest of the day only', () => {
    const store = new Map<string, string>()
    const storage = {
      getItem: (key: string) => store.get(key) ?? null,
      setItem: (key: string, value: string) => store.set(key, value),
    } as unknown as Storage
    const today = new Date(2026, 7, 9, 10, 0)

    expect(dailyMixDone(today, storage)).toBe(false)
    markDailyMixDone(today, storage)
    expect(store.get(DAILY_STORAGE_KEY)).toBe('2026-08-09')
    expect(dailyMixDone(today, storage)).toBe(true)
    expect(dailyMixDone(new Date(2026, 7, 10, 10, 0), storage)).toBe(false)
  })

  it('survives storage that refuses to work', () => {
    // Private-mode Safari throws on both reads and writes. A lost flag costs a
    // repeated title; a thrown error would cost the game.
    const hostile = {
      getItem: () => {
        throw new Error('denied')
      },
      setItem: () => {
        throw new Error('denied')
      },
    } as unknown as Storage
    const now = new Date(2026, 7, 9)
    expect(dailyMixDone(now, hostile)).toBe(false)
    expect(() => {
      markDailyMixDone(now, hostile)
    }).not.toThrow()
  })
})
