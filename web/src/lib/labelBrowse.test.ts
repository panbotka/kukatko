import { describe, expect, it } from 'vitest'

import { type LabelCount } from '../services/organize'

import {
  browseLabels,
  familyKey,
  familyPrefix,
  labelBrowseOptions,
  LABEL_FAMILY_MIN,
  LABEL_SORT_DEFAULT,
  LABELS_DEFAULTS,
  openFamilies,
  toggleFamilyOpen,
  toLabelSort,
  withFamilyOpen,
} from './labelBrowse'

/** A label with just the fields the browse rules read. */
function label(name: string, photoCount: number): LabelCount {
  return {
    uid: `lb_${name.toLowerCase().replace(/\s+/g, '-')}`,
    slug: name.toLowerCase(),
    name,
    priority: 0,
    review_enabled: true,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    photo_count: photoCount,
  }
}

/** The options a browse runs under, defaults unless overridden. */
function options(overrides: Partial<Parameters<typeof browseLabels>[1]> = {}) {
  return {
    query: '',
    sort: LABEL_SORT_DEFAULT,
    open: [] as string[],
    language: 'cs',
    ...overrides,
  }
}

/** The names an entry list shows, families rendered as `prefix…`. */
function shown(entries: ReturnType<typeof browseLabels>['entries']): string[] {
  return entries.map((entry) => (entry.kind === 'label' ? entry.label.name : `${entry.prefix}…`))
}

/** Four `Dum` labels — exactly the minimum a family needs to fold. */
function houseNumbers(): LabelCount[] {
  return [label('Dum11', 8), label('Dum12', 5), label('Dum20', 3), label('Dum4', 7)]
}

describe('toLabelSort', () => {
  it('accepts the orderings the selector offers', () => {
    expect(toLabelSort('name')).toBe('name')
    expect(toLabelSort('count')).toBe('count')
  })

  it('falls back to photo count for anything else', () => {
    // A hand-edited URL must not leave the page with no ordering at all.
    expect(toLabelSort('priority')).toBe('count')
    expect(toLabelSort('')).toBe('count')
  })

  it('opens on the most-used labels, not the alphabet', () => {
    // The alphabet is precisely the order that puts the numbered families first.
    expect(LABELS_DEFAULTS.sort).toBe('count')
  })
})

describe('familyPrefix', () => {
  it('splits a numbered name at its trailing digits', () => {
    expect(familyPrefix('Dum11')).toBe('Dum')
    expect(familyPrefix('Dum 12')).toBe('Dum')
    expect(familyPrefix('Dum-20')).toBe('Dum')
    expect(familyPrefix('IMG_20240101')).toBe('IMG')
  })

  it('leaves a name that is not a prefix plus a number alone', () => {
    expect(familyPrefix('Dovolená')).toBeNull()
    // A bare number belongs to no family: there is no prefix to name it after.
    expect(familyPrefix('2024')).toBeNull()
    expect(familyPrefix('Léto 2024 u vody')).toBeNull()
  })
})

describe('familyKey', () => {
  it('folds case and diacritics, so one family survives mixed spellings', () => {
    expect(familyKey('Dum11')).toBe('dum')
    expect(familyKey('DUM 12')).toBe('dum')
    expect(familyKey('dům13')).toBe('dum')
  })

  it('yields nothing for an unnumbered name', () => {
    expect(familyKey('Dovolená')).toBeNull()
  })

  it('drops the separator the URL uses, so a key cannot split into two', () => {
    expect(familyKey('A,B 1')).toBe('ab')
  })
})

describe('the expanded-family URL value', () => {
  it('reads an empty value as nothing expanded', () => {
    expect(openFamilies('')).toEqual([])
  })

  it('round-trips several families', () => {
    expect(openFamilies('dum,img')).toEqual(['dum', 'img'])
  })

  it('toggles a family in and back out', () => {
    expect(toggleFamilyOpen('', 'dum')).toBe('dum')
    expect(toggleFamilyOpen('dum', 'img')).toBe('dum,img')
    expect(toggleFamilyOpen('dum,img', 'dum')).toBe('img')
    expect(toggleFamilyOpen('dum', 'dum')).toBe('')
  })

  it('expands without disturbing the families already open', () => {
    expect(withFamilyOpen('img', 'dum')).toBe('img,dum')
    // Already open: the value is left exactly as it was.
    expect(withFamilyOpen('dum,img', 'dum')).toBe('dum,img')
  })
})

describe('labelBrowseOptions', () => {
  it('decodes the URL view, sanitizing the ordering', () => {
    expect(labelBrowseOptions({ q: 'dum', sort: 'nonsense', open: 'dum,img' }, 'en')).toEqual({
      query: 'dum',
      sort: 'count',
      open: ['dum', 'img'],
      language: 'en',
    })
  })
})

describe('browseLabels', () => {
  it('orders by photo count, breaking ties on the name', () => {
    const labels = [label('Auto', 3), label('Bota', 9), label('Ananas', 3)]

    expect(shown(browseLabels(labels, options()).entries)).toEqual(['Bota', 'Ananas', 'Auto'])
  })

  it('orders alphabetically in the reader’s own language', () => {
    // Czech collates `ch` after `h`; the database's own order does not.
    const labels = [label('Chalupa', 1), label('Ivan', 1), label('Hora', 1)]

    expect(shown(browseLabels(labels, options({ sort: 'name' })).entries)).toEqual([
      'Hora',
      'Chalupa',
      'Ivan',
    ])
  })

  it('searches folded, so an unaccented query finds the accented label', () => {
    const labels = [label('Dovolená', 4), label('Hory', 2)]
    const result = browseLabels(labels, options({ query: 'dovolena' }))

    expect(shown(result.entries)).toEqual(['Dovolená'])
    expect(result.matched).toBe(1)
    expect(result.filteredOut).toBe(1)
  })

  it('folds a numbered family into one entry carrying its members', () => {
    const labels = [...houseNumbers(), label('Dovolená', 4)]
    const { entries } = browseLabels(labels, options())
    const family = entries.find((entry) => entry.kind === 'family')

    expect(shown(entries)).toEqual(['Dum…', 'Dovolená'])
    expect(family?.kind === 'family' && family.labels.map((l) => l.name)).toEqual([
      'Dum11',
      'Dum4',
      'Dum12',
      'Dum20',
    ])
    // Ordered by the family's own total (23), which is what puts it first.
    expect(family?.kind === 'family' && family.photoCount).toBe(23)
    expect(family?.kind === 'family' && family.expanded).toBe(false)
  })

  it('leaves a family below the minimum dissolved', () => {
    // Three chips take less room than the chip that would hide them.
    const labels = houseNumbers().slice(0, LABEL_FAMILY_MIN - 1)

    expect(shown(browseLabels(labels, options({ sort: 'name' })).entries)).toEqual([
      'Dum11',
      'Dum12',
      'Dum20',
    ])
  })

  it('marks the families the URL asks for as expanded', () => {
    const { entries } = browseLabels(houseNumbers(), options({ open: ['dum'] }))

    expect(entries[0].kind === 'family' && entries[0].expanded).toBe(true)
  })

  it('dissolves the families while a search is running', () => {
    // Typing `dum4` asks for `Dum4` itself; answering with a folded chip the
    // reader has to open again would answer a different question.
    const labels = [...houseNumbers(), label('Dovolená', 4)]

    expect(shown(browseLabels(labels, options({ query: 'dum4' })).entries)).toEqual(['Dum4'])
    // Numeric collation, so the house numbers run 4 · 11 · 12 · 20 rather than
    // the string order 11 · 12 · 20 · 4.
    expect(shown(browseLabels(labels, options({ query: 'dum', sort: 'name' })).entries)).toEqual([
      'Dum4',
      'Dum11',
      'Dum12',
      'Dum20',
    ])
  })

  it('keeps one family for members spelled differently', () => {
    const labels = [label('Dum11', 1), label('DUM 12', 1), label('dum-20', 1), label('Dum4', 1)]
    const { entries } = browseLabels(labels, options())

    expect(entries).toHaveLength(1)
    // The prefix shown is the one the first member spells.
    expect(entries[0].kind === 'family' && entries[0].prefix).toBe('Dum')
  })

  it('sorts a family by its total, so folding cannot bury a big one', () => {
    const labels = [label('Dovolená', 20), ...houseNumbers()]

    expect(shown(browseLabels(labels, options()).entries)).toEqual(['Dum…', 'Dovolená'])
  })

  it('sorts a family under its prefix alphabetically', () => {
    const labels = [label('Auto', 1), label('Zeď', 1), ...houseNumbers()]

    expect(shown(browseLabels(labels, options({ sort: 'name' })).entries)).toEqual([
      'Auto',
      'Dum…',
      'Zeď',
    ])
  })

  it('reports an empty library as nothing to draw', () => {
    expect(browseLabels([], options())).toEqual({ entries: [], matched: 0, filteredOut: 0 })
  })
})
