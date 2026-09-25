import { beforeEach, describe, expect, it } from 'vitest'

import { type Photo } from '../services/photos'
import { type ReviewQuestion } from '../services/review'

import { questionCard } from './reviewRounds'
import {
  clearReviewSnapshot,
  readReviewSnapshot,
  REVIEW_SNAPSHOT_KEY,
  REVIEW_SNAPSHOT_VERSION,
  type ReviewSnapshot,
  writeReviewSnapshot,
} from './reviewSnapshot'

function photo(uid: string): Photo {
  return {
    uid,
    file_name: `${uid}.jpg`,
    file_width: 1200,
    file_height: 800,
    file_orientation: 1,
    title: '',
  } as unknown as Photo
}

function question(id: string): ReviewQuestion {
  return {
    id,
    kind: 'face',
    confidence: 0.7,
    photo: photo(`p-${id}`),
    subject: { uid: `s-${id}`, name: 'Alice' } as ReviewQuestion['subject'],
    face_index: 0,
    bbox: { relative: [0.1, 0.1, 0.2, 0.2], pixel: [1, 2, 3, 4] },
    marker_uid: `m-${id}`,
  }
}

/** A run two answers into a round of four, with one failed answer pending. */
function snapshot(overrides: Partial<ReviewSnapshot> = {}): ReviewSnapshot {
  return {
    version: REVIEW_SNAPSHOT_VERSION,
    user: 'u1',
    source: 'both',
    queue: [questionCard(question('q3')), questionCard(question('q4'))],
    round: {
      index: 1,
      size: 4,
      played: 2,
      confirmed: 1,
      rejected: 1,
      skipped: 0,
      daily: true,
      last: false,
    },
    combo: 2,
    comboBefore: 1,
    answered: 2,
    session: { confirmed: 1, rejected: 1, skipped: 0 },
    touched: [photo('p-q1'), photo('p-q2')],
    remaining: 7,
    seen: ['q1', 'q2', 'q3', 'q4'],
    direct: [],
    markers: {},
    failed: [{ question: question('q2'), answer: 'no' }],
    ...overrides,
  }
}

/** Stores a raw value under the snapshot key, bypassing the writer. */
function storeRaw(value: unknown) {
  window.sessionStorage.setItem(
    REVIEW_SNAPSHOT_KEY,
    typeof value === 'string' ? value : JSON.stringify(value),
  )
}

beforeEach(() => {
  window.sessionStorage.clear()
})

describe('review snapshot', () => {
  it('reads back exactly what was written, for the same user and source', () => {
    const run = snapshot()
    writeReviewSnapshot(run)

    expect(readReviewSnapshot('u1', 'both')).toEqual(run)
  })

  it('lives in sessionStorage, never in localStorage', () => {
    writeReviewSnapshot(snapshot())

    expect(window.sessionStorage.getItem(REVIEW_SNAPSHOT_KEY)).not.toBeNull()
    expect(window.localStorage.getItem(REVIEW_SNAPSHOT_KEY)).toBeNull()
  })

  it('never hands one user’s run to another account', () => {
    writeReviewSnapshot(snapshot({ user: 'u1' }))

    expect(readReviewSnapshot('u2', 'both')).toBeNull()
    expect(readReviewSnapshot('', 'both')).toBeNull()
  })

  it('ignores a run taken for a different source', () => {
    writeReviewSnapshot(snapshot({ source: 'people' }))

    expect(readReviewSnapshot('u1', 'labels')).toBeNull()
    expect(readReviewSnapshot('u1', 'people')).not.toBeNull()
  })

  it('returns null when nothing is stored, and after a clear', () => {
    expect(readReviewSnapshot('u1', 'both')).toBeNull()

    writeReviewSnapshot(snapshot())
    clearReviewSnapshot()
    expect(readReviewSnapshot('u1', 'both')).toBeNull()
  })

  it.each([
    ['unparseable JSON', '{"version":1,'],
    ['a bare string', '"hello"'],
    ['null', 'null'],
    ['an array', '[]'],
  ])('rejects %s', (_name, raw) => {
    storeRaw(raw)
    expect(readReviewSnapshot('u1', 'both')).toBeNull()
  })

  it.each<[string, (run: Record<string, unknown>) => void]>([
    ['another version', (run) => (run.version = 2)],
    ['a missing version', (run) => delete run.version],
    ['an unknown source', (run) => (run.source = 'everything')],
    ['an empty queue', (run) => (run.queue = [])],
    ['a queue that is not an array', (run) => (run.queue = { 0: 'x' })],
    ['an unknown card type', (run) => (run.queue = [{ type: 'ad', key: 'x' }])],
    [
      'a question of an unknown kind',
      (run) => (run.queue = [questionCard({ ...question('q3'), kind: 'dance' as 'face' })]),
    ],
    [
      'a question without a photo',
      (run) => (run.queue = [{ type: 'question', key: 'q3', question: { id: 'q3' } }]),
    ],
    [
      'a photo without dimensions',
      (run) =>
        (run.queue = [
          questionCard({ ...question('q3'), photo: { uid: 'p' } as unknown as Photo }),
        ]),
    ],
    [
      'a malformed bbox',
      (run) =>
        (run.queue = [
          questionCard({
            ...question('q3'),
            bbox: { relative: [1, 2] as unknown as [number, number, number, number], pixel: [] },
          } as unknown as ReviewQuestion),
        ]),
    ],
    ['a negative counter', (run) => (run.combo = -1)],
    ['a fractional counter', (run) => (run.answered = 1.5)],
    ['a non-numeric counter', (run) => (run.remaining = '7')],
    [
      'a round without its daily flag',
      (run) => delete (run.round as Record<string, unknown>).daily,
    ],
    ['a round played past its size', (run) => ((run.round as Record<string, unknown>).played = 9)],
    ['a tally with a missing field', (run) => (run.session = { confirmed: 1 })],
    ['seen ids that are not strings', (run) => (run.seen = [1, 2])],
    ['markers that are not strings', (run) => (run.markers = { q1: 5 })],
    [
      'a failed answer with an unknown verdict',
      (run) => (run.failed = [{ question: question('q2'), answer: 'maybe' }]),
    ],
    ['a touched entry that is not a photo', (run) => (run.touched = ['p-q1'])],
  ])('rejects a snapshot with %s', (_name, corrupt) => {
    const run = JSON.parse(JSON.stringify(snapshot())) as Record<string, unknown>
    corrupt(run)
    storeRaw(run)

    expect(readReviewSnapshot('u1', 'both')).toBeNull()
  })

  it('treats storage that throws as no storage at all', () => {
    const broken = {
      getItem: () => {
        throw new Error('SecurityError')
      },
      setItem: () => {
        throw new Error('QuotaExceededError')
      },
      removeItem: () => {
        throw new Error('SecurityError')
      },
    } as unknown as Storage

    expect(() => {
      writeReviewSnapshot(snapshot(), broken)
    }).not.toThrow()
    expect(() => {
      clearReviewSnapshot(broken)
    }).not.toThrow()
    expect(readReviewSnapshot('u1', 'both', broken)).toBeNull()
  })
})
