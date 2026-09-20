import { describe, expect, it, vi } from 'vitest'

import { notifyTaskChanged, subscribeTaskChanges } from './taskChanges'

describe('taskChanges', () => {
  it('tells every subscriber, and stops after unsubscribing', () => {
    const first = vi.fn()
    const second = vi.fn()
    const stopFirst = subscribeTaskChanges(first)
    const stopSecond = subscribeTaskChanges(second)

    notifyTaskChanged()
    expect(first).toHaveBeenCalledTimes(1)
    expect(second).toHaveBeenCalledTimes(1)

    stopFirst()
    notifyTaskChanged()
    expect(first).toHaveBeenCalledTimes(1)
    expect(second).toHaveBeenCalledTimes(2)
    stopSecond()
  })

  it('is harmless with nobody listening', () => {
    expect(() => {
      notifyTaskChanged()
    }).not.toThrow()
  })
})
