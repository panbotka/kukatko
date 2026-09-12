import { describe, expect, it } from 'vitest'

import { matchedByNickname, matchesSubjectName, nicknameTag, withNickname } from './nickname'

describe('nicknameTag', () => {
  it('wraps a nickname in Czech quotation marks', () => {
    expect(nicknameTag('Bohouš')).toBe('„Bohouš"')
  })

  it('trims before quoting, so a stray space does not widen the quotes', () => {
    expect(nicknameTag('  Bohouš  ')).toBe('„Bohouš"')
  })

  it.each([
    ['an empty string', ''],
    ['whitespace only', '   '],
    ['null', null],
    ['undefined', undefined],
  ])('yields null for %s, so nothing is drawn', (_label, value) => {
    expect(nicknameTag(value)).toBeNull()
  })
})

describe('withNickname', () => {
  it('appends the nickname to the name', () => {
    expect(withNickname('Bohumil Nečas', 'Bohouš')).toBe('Bohumil Nečas („Bohouš")')
  })

  it('leaves the name alone when there is no nickname', () => {
    expect(withNickname('Anna Nováková', '')).toBe('Anna Nováková')
  })
})

describe('matchesSubjectName', () => {
  it('matches the name', () => {
    expect(matchesSubjectName('Bohumil Nečas', 'Bohouš', 'necas')).toBe(true)
  })

  it('matches the nickname, accent- and case-insensitively', () => {
    expect(matchesSubjectName('Bohumil Nečas', 'Bohouš', 'BOHOUS')).toBe(true)
  })

  it('matches part of a nickname', () => {
    expect(matchesSubjectName('Bohumil Nečas', 'Bohouš', 'ouš')).toBe(true)
  })

  // The trap an OR over a mostly-empty column sets: if an empty nickname matched,
  // every query would return the whole library.
  it('does not match a subject whose nickname is empty on an unrelated query', () => {
    expect(matchesSubjectName('Anna Nováková', '', 'bohous')).toBe(false)
  })

  it('matches everything on an empty query, as a filter with nothing typed must', () => {
    expect(matchesSubjectName('Anna Nováková', '', '')).toBe(true)
  })
})

describe('matchedByNickname', () => {
  it('is true when only the nickname contains the query', () => {
    expect(matchedByNickname('Bohumil Nečas', 'Bohouš', 'bohous')).toBe(true)
  })

  it('is false when the name contains it too — the row needs no explanation', () => {
    expect(matchedByNickname('Bohumil Nečas', 'Bohumil', 'bohum')).toBe(false)
  })

  it('is false for a query matching neither', () => {
    expect(matchedByNickname('Bohumil Nečas', 'Bohouš', 'pepa')).toBe(false)
  })

  it('is false for an empty query, which singled nobody out', () => {
    expect(matchedByNickname('Bohumil Nečas', 'Bohouš', '  ')).toBe(false)
  })
})
