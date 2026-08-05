import { describe, expect, it } from 'vitest'

import { type DuplicateMarkerGroup } from '../services/dupmarkers'

import { dropMarker, groupKey, MIN_GROUP_SIZE, removeGroup } from './duplicateMarkers'

/** group builds a finding with `count` markers of `subject` on `photo`. */
function group(photo: string, subject: string, count: number): DuplicateMarkerGroup {
  return {
    photo_uid: photo,
    photo_title: '',
    width: 4000,
    height: 3000,
    orientation: 1,
    subject_uid: subject,
    subject_name: subject,
    markers: Array.from({ length: count }, (_, i) => ({
      uid: `${photo}-${subject}-m${String(i + 1)}`,
      bbox: [0.1 * (i + 1), 0.2, 0.1, 0.1] as [number, number, number, number],
      score: 0,
      reviewed: false,
    })),
  }
}

describe('groupKey', () => {
  it('identifies a finding by its (photo, person) pair', () => {
    expect(groupKey({ photo_uid: 'p1', subject_uid: 's1' })).toBe('p1:s1')
  })

  it('tells the same person on two photos apart', () => {
    expect(groupKey({ photo_uid: 'p1', subject_uid: 's1' })).not.toBe(
      groupKey({ photo_uid: 'p2', subject_uid: 's1' }),
    )
  })
})

describe('removeGroup', () => {
  it('drops only the named finding', () => {
    const groups = [group('p1', 's1', 2), group('p2', 's1', 3)]

    const left = removeGroup(groups, 'p1:s1')

    expect(left).toHaveLength(1)
    expect(left[0].photo_uid).toBe('p2')
  })

  it('leaves the list alone for an unknown key', () => {
    const groups = [group('p1', 's1', 2)]
    expect(removeGroup(groups, 'nope:nope')).toHaveLength(1)
  })
})

describe('dropMarker', () => {
  it('keeps a three-marker finding standing at two', () => {
    const groups = [group('p1', 's1', 3)]

    const left = dropMarker(groups, 'p1:s1', 'p1-s1-m2')

    expect(left).toHaveLength(1)
    expect(left[0].markers.map((m) => m.uid)).toEqual(['p1-s1-m1', 'p1-s1-m3'])
  })

  it('drops the finding once it falls below the minimum', () => {
    const groups = [group('p1', 's1', MIN_GROUP_SIZE)]

    expect(dropMarker(groups, 'p1:s1', 'p1-s1-m1')).toHaveLength(0)
  })

  it('touches no other finding', () => {
    const groups = [group('p1', 's1', 2), group('p2', 's1', 2)]

    const left = dropMarker(groups, 'p1:s1', 'p1-s1-m1')

    expect(left).toHaveLength(1)
    expect(left[0].photo_uid).toBe('p2')
    expect(left[0].markers).toHaveLength(2)
  })

  it('does not mutate the input', () => {
    const groups = [group('p1', 's1', 3)]

    dropMarker(groups, 'p1:s1', 'p1-s1-m2')

    expect(groups[0].markers).toHaveLength(3)
  })
})
