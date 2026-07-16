import { describe, expect, it } from 'vitest'

import type { DuplicateGroup } from '../services/duplicates'
import type { PhotoDetail } from '../services/photos'

import {
  type ComparePhoto,
  buildDiffRows,
  buildPairQueue,
  countDiffering,
  dropPairsTouching,
  pairId,
  pairIndexInGroup,
  pairsInGroup,
} from './duplicateCompare'

/** A minimal group member; only the fields the queue reads matter. */
function member(uid: string) {
  return {
    uid,
    title: '',
    file_name: `${uid}.jpg`,
    file_width: 100,
    file_height: 100,
    file_size: 1000,
    media_type: 'image',
    is_keeper: false,
  }
}

/** A group with the first uid as the suggested keeper. */
function group(id: string, keeper: string, ...others: string[]): DuplicateGroup {
  return {
    id,
    reason: 'phash',
    keeper_uid: keeper,
    members: [member(keeper), ...others.map(member)],
  }
}

/** A photo detail with only the compared fields set; the rest take neutral values. */
function photo(over: Partial<PhotoDetail> = {}): PhotoDetail {
  return {
    uid: 'ph1',
    file_hash: 'h',
    file_name: 'a.jpg',
    file_size: 1000,
    file_mime: 'image/jpeg',
    file_width: 4000,
    file_height: 3000,
    taken_at_source: 'exif',
    // Present in the base so the spread of `over` keeps them `string` rather than
    // `string | undefined`, matching the fixture convention in PhotoLocation.test.
    thumb_url: '/api/v1/photos/x/thumb/fit_1920',
    download_url: '/api/v1/photos/x/download?original=true',
    title: '',
    description: '',
    camera_make: '',
    camera_model: '',
    lens_model: '',
    created_at: '2024-01-01T00:00:00Z',
    updated_at: '2024-01-01T00:00:00Z',
    files: [],
    albums: [],
    labels: [],
    ...over,
  }
}

/** Wraps a photo as a compare side with the given people. */
function side(over: Partial<PhotoDetail> = {}, people: string[] = []): ComparePhoto {
  return { photo: photo(over), people }
}

/** Deterministic formatters, so the assertions do not depend on the runtime locale. */
const fmt = {
  bytes: (n: number) => `${String(n)} B`,
  dateTime: (iso: string) => iso,
}

describe('buildPairQueue', () => {
  it('yields the single pair of a two-member group', () => {
    const pairs = buildPairQueue([group('g1', 'a', 'b')])
    expect(pairs).toHaveLength(1)
    expect(pairs[0].leftUid).toBe('a')
    expect(pairs[0].rightUid).toBe('b')
  })

  it('compares a larger group pairwise against the keeper, never member-to-member', () => {
    const pairs = buildPairQueue([group('g1', 'k', 'a', 'b')])
    expect(pairs.map((p) => [p.leftUid, p.rightUid])).toEqual([
      ['k', 'a'],
      ['k', 'b'],
    ])
  })

  it('offers every non-keeper member exactly once, so none is silently ignored', () => {
    const pairs = buildPairQueue([group('g1', 'k', 'a', 'b', 'c')])
    expect(pairs.map((p) => p.rightUid).sort()).toEqual(['a', 'b', 'c'])
  })

  it('flattens several groups into one queue', () => {
    const pairs = buildPairQueue([group('g1', 'a', 'b'), group('g2', 'c', 'd')])
    expect(pairs).toHaveLength(2)
  })

  it('skips a group whose keeper is not among its members rather than guessing', () => {
    const broken: DuplicateGroup = { ...group('g1', 'a', 'b'), keeper_uid: 'missing' }
    expect(buildPairQueue([broken])).toEqual([])
  })

  it('gives a pair an order-independent id, matching the backend canonical form', () => {
    expect(pairId('b', 'a')).toBe(pairId('a', 'b'))
  })
})

describe('pair counting', () => {
  it("counts a group's pairs as its members minus the keeper", () => {
    expect(pairsInGroup(group('g1', 'k', 'a', 'b'))).toBe(2)
  })

  it('numbers a pair within its group for the caption', () => {
    const pairs = buildPairQueue([group('g1', 'k', 'a', 'b')])
    expect(pairIndexInGroup(pairs[0])).toBe(1)
    expect(pairIndexInGroup(pairs[1])).toBe(2)
  })
})

describe('dropPairsTouching', () => {
  it('removes every pair naming an archived photo', () => {
    const pairs = buildPairQueue([group('g1', 'k', 'a', 'b')])
    const left = dropPairsTouching(pairs, 'a')
    expect(left.map((p) => p.rightUid)).toEqual(['b'])
  })

  it('empties the queue when the archived photo is the keeper of every pair', () => {
    const pairs = buildPairQueue([group('g1', 'k', 'a', 'b')])
    expect(dropPairsTouching(pairs, 'k')).toEqual([])
  })
})

describe('buildDiffRows', () => {
  it('marks exactly the differing rows and leaves identical ones unmarked', () => {
    const rows = buildDiffRows(
      side({ file_width: 4000, file_height: 3000, file_size: 5_000_000 }),
      side({ file_width: 1000, file_height: 750, file_size: 200_000 }),
      fmt,
    )
    const differing = rows.filter((r) => r.differs).map((r) => r.key)
    expect(differing.sort()).toEqual(['dimensions', 'fileSize'])
    // Everything else is genuinely identical and must stay unmarked — the marking
    // is the whole signal, so a false positive is as bad as a miss.
    expect(rows.filter((r) => !r.differs).map((r) => r.key)).toContain('format')
    expect(rows.filter((r) => !r.differs).map((r) => r.key)).toContain('camera')
  })

  it('marks nothing when the two photos match on every compared field', () => {
    const rows = buildDiffRows(side(), side(), fmt)
    expect(countDiffering(rows)).toBe(0)
    expect(rows.every((r) => !r.differs)).toBe(true)
  })

  it('reports dimensions with megapixels', () => {
    const rows = buildDiffRows(side(), side(), fmt)
    const dims = rows.find((r) => r.key === 'dimensions')
    expect(dims?.left).toBe('4000 × 3000 (12.0 Mpx)')
  })

  it('marks a capture-date difference the formatting would otherwise hide', () => {
    // Both render as the same minute; the underlying values differ, and it is the
    // values that decide, so the row must still be marked.
    const rows = buildDiffRows(
      side({ taken_at: '2024-05-01T10:00:01Z' }),
      side({ taken_at: '2024-05-01T10:00:45Z' }),
      { bytes: fmt.bytes, dateTime: () => '1 May 2024 10:00' },
    )
    const taken = rows.find((r) => r.key === 'takenAt')
    expect(taken?.differs).toBe(true)
    expect(taken?.left).toBe(taken?.right)
  })

  it('compares people, albums and labels as sets, ignoring API order', () => {
    const rows = buildDiffRows(side({}, ['Anna', 'Bob']), side({}, ['Bob', 'Anna']), fmt)
    expect(rows.find((r) => r.key === 'people')?.differs).toBe(false)
  })

  it('marks curation the other copy does not have — the reason to look before merging', () => {
    const rows = buildDiffRows(
      side({ albums: [{ uid: 'al1', title: 'Holiday' }] }, ['Anna']),
      side({}, []),
      fmt,
    )
    expect(rows.find((r) => r.key === 'albums')?.differs).toBe(true)
    expect(rows.find((r) => r.key === 'albums')?.left).toBe('Holiday')
    expect(rows.find((r) => r.key === 'people')?.differs).toBe(true)
  })

  it('joins the camera make and model, and marks a camera difference', () => {
    const rows = buildDiffRows(
      side({ camera_make: 'Canon', camera_model: 'EOS R6' }),
      side({ camera_make: '', camera_model: '' }),
      fmt,
    )
    const camera = rows.find((r) => r.key === 'camera')
    expect(camera?.left).toBe('Canon EOS R6')
    expect(camera?.right).toBe('')
    expect(camera?.differs).toBe(true)
  })

  it('prefers a resolved place name over raw coordinates for the location row', () => {
    const rows = buildDiffRows(
      side({
        lat: 50.1,
        lng: 14.4,
        place: { country: 'Czechia', region: '', city: 'Prague', place_name: '' },
      }),
      side({ lat: 48.9, lng: 2.3 }),
      fmt,
    )
    const location = rows.find((r) => r.key === 'location')
    expect(location?.left).toBe('Prague, Czechia')
    expect(location?.right).toBe('48.9000, 2.3000')
    expect(location?.differs).toBe(true)
  })

  it('counts the differing rows for the summary line', () => {
    const rows = buildDiffRows(side({ file_size: 1 }), side({ file_size: 2 }), fmt)
    expect(countDiffering(rows)).toBe(1)
  })
})
