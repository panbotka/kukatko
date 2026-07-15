import { describe, expect, it } from 'vitest'

import { type Candidate } from '../services/faces'
import { type Photo } from '../services/photos'

import {
  buildAssignRequest,
  buildRejection,
  bucketOf,
  candidateKey,
  initialStatus,
  isActionable,
  matchesTab,
  type ReviewItem,
  tabCounts,
  toReviewItems,
} from './candidateReview'

/** makeCandidate builds a candidate with only the fields the review logic reads. */
function makeCandidate(overrides: Partial<Candidate> & { action: Candidate['action'] }): Candidate {
  return {
    photo: { uid: 'p1' } as unknown as Photo,
    face_index: 0,
    bbox: { relative: [0.1, 0.1, 0.2, 0.2], pixel: [10, 10, 20, 20] },
    distance: 0.3,
    match_count: 1,
    ...overrides,
  }
}

/** item wraps a candidate at a given status. */
function item(candidate: Candidate, status: ReviewItem['status'] = 'pending'): ReviewItem {
  return { candidate, status }
}

describe('candidateKey', () => {
  it('combines the photo and face index', () => {
    expect(candidateKey(makeCandidate({ action: 'create_marker', face_index: 3 }))).toBe('p1:3')
  })
})

describe('initialStatus', () => {
  it('starts already-done candidates done and everything else pending', () => {
    expect(initialStatus(makeCandidate({ action: 'already_done' }))).toBe('done')
    expect(initialStatus(makeCandidate({ action: 'create_marker' }))).toBe('pending')
    expect(initialStatus(makeCandidate({ action: 'assign_person' }))).toBe('pending')
  })
})

describe('bucketOf', () => {
  it('buckets by status first, then by action', () => {
    expect(bucketOf(item(makeCandidate({ action: 'create_marker' })))).toBe('new')
    expect(bucketOf(item(makeCandidate({ action: 'assign_person' })))).toBe('assign')
    expect(bucketOf(item(makeCandidate({ action: 'create_marker' }), 'done'))).toBe('done')
    // An errored card still needs its original action.
    expect(bucketOf(item(makeCandidate({ action: 'assign_person' }), 'error'))).toBe('assign')
  })
})

describe('isActionable', () => {
  it('is true for pending and errored, false for done', () => {
    const candidate = makeCandidate({ action: 'create_marker' })
    expect(isActionable(item(candidate, 'pending'))).toBe(true)
    expect(isActionable(item(candidate, 'error'))).toBe(true)
    expect(isActionable(item(candidate, 'done'))).toBe(false)
  })
})

describe('matchesTab / tabCounts', () => {
  const items = [
    item(makeCandidate({ action: 'create_marker', face_index: 0 })),
    item(makeCandidate({ action: 'assign_person', face_index: 1 })),
    item(makeCandidate({ action: 'create_marker', face_index: 2 }), 'done'),
  ]

  it('matches the all tab for everything', () => {
    expect(items.every((it) => matchesTab(it, 'all'))).toBe(true)
  })

  it('scopes each bucket tab', () => {
    expect(items.filter((it) => matchesTab(it, 'new'))).toHaveLength(1)
    expect(items.filter((it) => matchesTab(it, 'assign'))).toHaveLength(1)
    expect(items.filter((it) => matchesTab(it, 'done'))).toHaveLength(1)
  })

  it('counts every tab', () => {
    expect(tabCounts(items)).toEqual({ all: 3, new: 1, assign: 1, done: 1 })
  })
})

describe('buildAssignRequest', () => {
  it('creates a marker at the box for an unmarked face', () => {
    const candidate = makeCandidate({ action: 'create_marker', face_index: 4 })
    expect(buildAssignRequest(candidate, 'su_1')).toEqual({
      action: 'create_marker',
      face_index: 4,
      bbox: [0.1, 0.1, 0.2, 0.2],
      subject_uid: 'su_1',
    })
  })

  it('assigns the person to an existing marker', () => {
    const candidate = makeCandidate({ action: 'assign_person', marker_uid: 'mk_9' })
    expect(buildAssignRequest(candidate, 'su_1')).toEqual({
      action: 'assign_person',
      marker_uid: 'mk_9',
      subject_uid: 'su_1',
    })
  })
})

describe('buildRejection', () => {
  it('names the face and the subject it is rejected for', () => {
    const candidate = makeCandidate({ action: 'create_marker', face_index: 2 })
    expect(buildRejection(candidate, 'su_1')).toEqual({
      photo_uid: 'p1',
      face_index: 2,
      subject_uid: 'su_1',
    })
  })
})

describe('toReviewItems', () => {
  it('seeds each candidate with its initial status', () => {
    const items = toReviewItems([
      makeCandidate({ action: 'create_marker', face_index: 0 }),
      makeCandidate({ action: 'already_done', face_index: 1 }),
    ])
    expect(items.map((it) => it.status)).toEqual(['pending', 'done'])
  })
})
