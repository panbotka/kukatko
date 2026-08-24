import { describe, expect, it } from 'vitest'

import { isPlaceholderEmail, isWaitingForApproval } from './account'

describe('isPlaceholderEmail', () => {
  it('recognises the reserved .invalid domain the backend mints placeholders in', () => {
    expect(isPlaceholderEmail('ada@kukatko.invalid')).toBe(true)
    // The check is on the last label, so a future placeholder domain still counts…
    expect(isPlaceholderEmail('ada@somewhere.else.invalid')).toBe(true)
    // …and the domain is compared case-insensitively, as DNS is.
    expect(isPlaceholderEmail('ada@KUKATKO.INVALID')).toBe(true)
  })

  it('leaves a real address alone', () => {
    expect(isPlaceholderEmail('ada@example.com')).toBe(false)
    // "invalid" inside another label is not the reserved TLD.
    expect(isPlaceholderEmail('ada@invalid.example.com')).toBe(false)
    expect(isPlaceholderEmail('invalid@example.com')).toBe(false)
    // Nothing that is not an address at all is a placeholder either.
    expect(isPlaceholderEmail('')).toBe(false)
    expect(isPlaceholderEmail('invalid')).toBe(false)
  })
})

describe('isWaitingForApproval', () => {
  it('reads an explicit null as "nobody has let this account in"', () => {
    expect(isWaitingForApproval({ approved_at: null })).toBe(true)
    expect(isWaitingForApproval({ approved_at: '2026-08-01T10:00:00Z' })).toBe(false)
  })

  it('does not call an account waiting just because the payload is silent', () => {
    // The backend always sends the key, so undefined means "this payload does
    // not say" — badging such a row as waiting would invent an errand.
    expect(isWaitingForApproval({})).toBe(false)
  })
})
