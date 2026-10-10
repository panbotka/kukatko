import { beforeEach, describe, expect, it, vi } from 'vitest'

import {
  forgetBatch,
  readInterruptedBatch,
  rememberBatch,
  UPLOAD_BATCH_KEY,
  UPLOAD_BATCH_MAX_AGE_MS,
} from './uploadLinkBatch'

const NOW = 1_800_000_000_000

beforeEach(() => {
  localStorage.clear()
})

describe('uploadLinkBatch', () => {
  it('remembers a running batch for its link and keeps its start', () => {
    rememberBatch('Ab3dEf7h', 3, NOW)
    rememberBatch('Ab3dEf7h', 5, NOW + 1000)
    expect(readInterruptedBatch('Ab3dEf7h', NOW + 2000)).toEqual({
      code: 'Ab3dEf7h',
      count: 5,
      startedAt: NOW,
    })
  })

  it('does not mention another link’s batch', () => {
    rememberBatch('Ab3dEf7h', 3, NOW)
    expect(readInterruptedBatch('Zz9yXw8v', NOW)).toBeNull()
    // A batch through another link starts its own clock.
    rememberBatch('Zz9yXw8v', 2, NOW + 5000)
    expect(readInterruptedBatch('Zz9yXw8v', NOW + 5000)?.startedAt).toBe(NOW + 5000)
  })

  it('forgets a finished batch', () => {
    rememberBatch('Ab3dEf7h', 3, NOW)
    forgetBatch()
    expect(localStorage.getItem(UPLOAD_BATCH_KEY)).toBeNull()
    expect(readInterruptedBatch('Ab3dEf7h', NOW)).toBeNull()
  })

  it('ignores a batch too old to matter, and garbage', () => {
    rememberBatch('Ab3dEf7h', 3, NOW)
    expect(readInterruptedBatch('Ab3dEf7h', NOW + UPLOAD_BATCH_MAX_AGE_MS + 1)).toBeNull()
    localStorage.setItem(UPLOAD_BATCH_KEY, '{"code":"Ab3dEf7h","count":"3"}')
    expect(readInterruptedBatch('Ab3dEf7h', NOW)).toBeNull()
    localStorage.setItem(UPLOAD_BATCH_KEY, 'not json')
    expect(readInterruptedBatch('Ab3dEf7h', NOW)).toBeNull()
  })

  it('survives storage that throws', () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('denied')
    })
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('denied')
    })
    vi.spyOn(Storage.prototype, 'removeItem').mockImplementation(() => {
      throw new Error('denied')
    })
    expect(() => {
      rememberBatch('Ab3dEf7h', 3, NOW)
      forgetBatch()
    }).not.toThrow()
    expect(readInterruptedBatch('Ab3dEf7h', NOW)).toBeNull()
  })
})
