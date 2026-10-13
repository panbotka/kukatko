import { describe, expect, it } from 'vitest'

import { generateRegistrationSecret } from './registrationSecret'

describe('generateRegistrationSecret', () => {
  it('makes two halves of four unambiguous letters', () => {
    const word = generateRegistrationSecret()
    expect(word).toMatch(/^[a-hjkmnp-z2-9]{4}-[a-hjkmnp-z2-9]{4}$/)
  })

  it('draws a new word every time', () => {
    const words = new Set(Array.from({ length: 20 }, () => generateRegistrationSecret()))
    expect(words.size).toBe(20)
  })

  it('throws away the bytes that would favour the first letters', () => {
    // 248 is the first byte past the largest multiple of 31; a source that
    // answers it first must not turn it into a letter.
    let call = 0
    const word = generateRegistrationSecret((bytes) => {
      bytes.fill(call === 0 ? 248 : 0)
      call += 1
    })
    expect(call).toBe(2)
    expect(word).toBe('aaaa-aaaa')
  })
})
