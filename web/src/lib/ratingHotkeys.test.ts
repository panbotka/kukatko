import { describe, expect, it } from 'vitest'

import { isTypingElement, ratingHotkey } from './ratingHotkeys'

describe('ratingHotkey', () => {
  it('maps number keys 0–5 to a rating action', () => {
    for (let n = 0; n <= 5; n += 1) {
      expect(ratingHotkey(String(n))).toEqual({ kind: 'rating', value: n })
    }
  })

  it('maps p / r / v (any case) to pick / reject / eye flags', () => {
    expect(ratingHotkey('p')).toEqual({ kind: 'flag', value: 'pick' })
    expect(ratingHotkey('P')).toEqual({ kind: 'flag', value: 'pick' })
    expect(ratingHotkey('r')).toEqual({ kind: 'flag', value: 'reject' })
    expect(ratingHotkey('R')).toEqual({ kind: 'flag', value: 'reject' })
    expect(ratingHotkey('v')).toEqual({ kind: 'flag', value: 'eye' })
    expect(ratingHotkey('V')).toEqual({ kind: 'flag', value: 'eye' })
  })

  it('returns null for keys outside the rating range and other keys', () => {
    expect(ratingHotkey('6')).toBeNull()
    expect(ratingHotkey('9')).toBeNull()
    expect(ratingHotkey('a')).toBeNull()
    expect(ratingHotkey('Enter')).toBeNull()
  })
})

describe('isTypingElement', () => {
  it('is true for text-entry elements', () => {
    expect(isTypingElement(document.createElement('input'))).toBe(true)
    expect(isTypingElement(document.createElement('textarea'))).toBe(true)
    expect(isTypingElement(document.createElement('select'))).toBe(true)
    const editable = document.createElement('div')
    editable.setAttribute('contenteditable', 'true')
    expect(isTypingElement(editable)).toBe(true)
  })

  it('is false for non-text elements and null', () => {
    expect(isTypingElement(document.createElement('div'))).toBe(false)
    expect(isTypingElement(document.createElement('a'))).toBe(false)
    expect(isTypingElement(null)).toBe(false)
  })
})
