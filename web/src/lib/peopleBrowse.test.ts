import { describe, expect, it } from 'vitest'

import {
  browsePeople,
  PEOPLE_DEFAULTS,
  peopleBrowseOptions,
  toPeopleSort,
  toPeopleTab,
} from './peopleBrowse'

import { type SubjectCount, type SubjectType } from '../services/people'

/** A subject with the two fields the index actually browses by. */
function subject(
  name: string,
  { type = 'person', photoCount = 1 }: { type?: SubjectType; photoCount?: number } = {},
): SubjectCount {
  return {
    uid: `su_${name.toLowerCase()}`,
    slug: name.toLowerCase(),
    name,
    type,
    favorite: false,
    private: false,
    notes: '',
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    marker_count: photoCount * 2,
    photo_count: photoCount,
  }
}

/** A library like the real one: mostly people, one dog, one oddity. */
function library(): SubjectCount[] {
  return [
    subject('Anna', { photoCount: 12 }),
    subject('Němcová', { photoCount: 40 }),
    subject('Bedřich', { photoCount: 3 }),
    subject('Rex', { type: 'pet', photoCount: 7 }),
    subject('Chalupa', { type: 'other', photoCount: 5 }),
  ]
}

/** The whole library under the given view patch, in the given language. */
function browse(patch: Partial<typeof PEOPLE_DEFAULTS> = {}, language = 'cs') {
  return browsePeople(library(), peopleBrowseOptions({ ...PEOPLE_DEFAULTS, ...patch }, language))
}

describe('browsePeople', () => {
  it('opens on everybody in alphabetical order', () => {
    const { visible } = browse()
    expect(visible.map((s) => s.name)).toEqual(['Anna', 'Bedřich', 'Chalupa', 'Němcová', 'Rex'])
  })

  it('sorts by the reader’s alphabet, not the database’s', () => {
    // Czech collates Ch after H and Ř after R; a byte-order sort would put
    // "Chalupa" between "Bedřich" and "Němcová" in English too, so the test uses
    // a pair the two locales genuinely disagree about.
    const cs = browse({}, 'cs').visible.map((s) => s.name)
    expect(cs.indexOf('Chalupa')).toBeGreaterThan(cs.indexOf('Bedřich'))
    expect(cs.indexOf('Němcová')).toBeGreaterThan(cs.indexOf('Chalupa'))
  })

  it('orders by the photo count the tile shows, then by name', () => {
    const { visible } = browse({ sort: 'count' })
    expect(visible.map((s) => s.name)).toEqual(['Němcová', 'Anna', 'Rex', 'Chalupa', 'Bedřich'])
  })

  it('breaks a tie in the count order by name, so the grid never reshuffles', () => {
    const tied = [subject('Zuzana', { photoCount: 4 }), subject('Adam', { photoCount: 4 })]
    const { visible } = browsePeople(
      tied,
      peopleBrowseOptions({ ...PEOPLE_DEFAULTS, sort: 'count' }, 'cs'),
    )
    expect(visible.map((s) => s.name)).toEqual(['Adam', 'Zuzana'])
  })

  it('searches names case- and accent-insensitively, as the library facet does', () => {
    expect(browse({ q: 'nemcova' }).visible.map((s) => s.name)).toEqual(['Němcová'])
    expect(browse({ q: 'ANN' }).visible.map((s) => s.name)).toEqual(['Anna'])
  })

  it('narrows to one kind of subject', () => {
    expect(browse({ type: 'pet' }).visible.map((s) => s.name)).toEqual(['Rex'])
    expect(browse({ type: 'other' }).visible.map((s) => s.name)).toEqual(['Chalupa'])
    expect(browse({ type: 'person' }).visible.map((s) => s.name)).toEqual([
      'Anna',
      'Bedřich',
      'Němcová',
    ])
  })

  it('counts what each kind holds under the current search, not the library total', () => {
    // The counts are the answer to "where are my matches?", so they have to move
    // with the search box.
    expect(browse().counts).toEqual({ all: 5, person: 3, pet: 1, other: 1 })
    expect(browse({ q: 'e' }).counts).toEqual({ all: 3, person: 2, pet: 1, other: 0 })
  })

  it('reports how many the search dropped, so the empty state can say why', () => {
    expect(browse().filteredOut).toBe(0)
    expect(browse({ q: 'nemcova' }).filteredOut).toBe(4)
    // A type that empties the grid is not the search dropping anybody: the hint
    // must not blame a search term that is not there.
    expect(browse({ type: 'pet' }).filteredOut).toBe(0)
  })

  it('keeps a subject of an unknown kind visible under "other"', () => {
    // The backend may grow a type this frontend does not know; falling out of
    // every option would make somebody disappear from their own index.
    const exotic = [{ ...subject('Bacil'), type: 'plant' as unknown as SubjectType }]
    const result = browsePeople(exotic, peopleBrowseOptions(PEOPLE_DEFAULTS, 'cs'))
    expect(result.counts.other).toBe(1)
    expect(result.visible.map((s) => s.name)).toEqual(['Bacil'])
  })
})

describe('toPeopleTab / toPeopleSort', () => {
  it('falls back to the defaults for a value the URL made up', () => {
    expect(toPeopleTab('robot')).toBe('all')
    expect(toPeopleTab('')).toBe('all')
    expect(toPeopleSort('newest')).toBe('name')
  })

  it('passes a known value through', () => {
    expect(toPeopleTab('pet')).toBe('pet')
    expect(toPeopleSort('count')).toBe('count')
  })
})
