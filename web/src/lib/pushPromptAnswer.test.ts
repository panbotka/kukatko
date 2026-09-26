import { afterEach, describe, expect, it, vi } from 'vitest'

import { hasAnsweredPushPrompt, recordPushPromptAnswer } from './pushPromptAnswer'

afterEach(() => {
  window.localStorage.clear()
})

describe('pushPromptAnswer', () => {
  it('remembers an answer per account', () => {
    expect(hasAnsweredPushPrompt('u1')).toBe(false)

    recordPushPromptAnswer('u1', new Date('2026-09-26T10:00:00Z'))

    expect(hasAnsweredPushPrompt('u1')).toBe(true)
    expect(hasAnsweredPushPrompt('u2')).toBe(false)
    expect(window.localStorage.getItem('kukatko.pushPrompt.answered.u1')).toBe(
      '2026-09-26T10:00:00.000Z',
    )
  })

  it('counts unreadable storage as answered, so the prompt cannot nag', () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('SecurityError')
    })

    expect(hasAnsweredPushPrompt('u1')).toBe(true)
  })

  it('swallows a failing write', () => {
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('QuotaExceededError')
    })

    expect(() => {
      recordPushPromptAnswer('u1')
    }).not.toThrow()
  })
})
