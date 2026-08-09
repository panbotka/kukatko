import { describe, expect, it } from 'vitest'

import { AVATAR_TONE_COUNT, avatarInitial, avatarTone } from './avatarIdentity'

describe('avatarInitial', () => {
  it('takes the first letter of the name, upper-cased', () => {
    expect(avatarInitial('Jarmila')).toBe('J')
    expect(avatarInitial('jarmila kozáková')).toBe('J')
  })

  it('upper-cases a diacritic letter without losing the diacritic', () => {
    expect(avatarInitial('šárka')).toBe('Š')
    expect(avatarInitial('Řehoř')).toBe('Ř')
  })

  it('ignores surrounding whitespace', () => {
    expect(avatarInitial('  Petr  ')).toBe('P')
  })

  it('falls back to a question mark for a name that is not there', () => {
    expect(avatarInitial('')).toBe('?')
    expect(avatarInitial('   ')).toBe('?')
  })

  it('keeps a whole character, never half a surrogate pair', () => {
    // An astral-plane character is one grapheme, not two UTF-16 code units.
    expect(avatarInitial('𝒥ana')).toBe('𝒥')
  })
})

describe('avatarTone', () => {
  it('is deterministic — the same name always gets the same tone', () => {
    expect(avatarTone('Jarmila')).toBe(avatarTone('Jarmila'))
  })

  it('folds case and whitespace, so one person is one colour', () => {
    expect(avatarTone('  JARMILA ')).toBe(avatarTone('jarmila'))
  })

  it('stays inside the palette for anything thrown at it', () => {
    for (const name of ['', 'A', 'Jarmila', 'Řehoř Novotný', '𝒥ana', 'x'.repeat(500)]) {
      const tone = avatarTone(name)
      expect(Number.isInteger(tone)).toBe(true)
      expect(tone).toBeGreaterThanOrEqual(0)
      expect(tone).toBeLessThan(AVATAR_TONE_COUNT)
    }
  })

  it('spreads a handful of real names over several tones', () => {
    const names = ['Jarmila', 'Petr', 'Anna', 'Tomáš', 'Marie', 'Josef', 'Eva', 'Karel']
    const tones = new Set(names.map((name) => avatarTone(name)))
    // Not a guarantee of no collisions — a hash has them — but a palette that
    // collapsed every name onto one colour would be worthless, so assert it does not.
    expect(tones.size).toBeGreaterThan(3)
  })
})
