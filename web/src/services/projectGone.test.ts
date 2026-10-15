import { describe, expect, it, vi } from 'vitest'

import { ApiError } from './auth'
import { onProjectGone, PROJECT_NOT_FOUND_MESSAGE, reportApiError } from './projectGone'

describe('projectGone', () => {
  it("tells the listeners about the project guard's 404 and nothing else", () => {
    const listener = vi.fn()
    const unsubscribe = onProjectGone(listener)

    reportApiError(404, 'photo not found')
    reportApiError(403, PROJECT_NOT_FOUND_MESSAGE)
    reportApiError(500, 'boom')
    expect(listener).not.toHaveBeenCalled()

    reportApiError(404, PROJECT_NOT_FOUND_MESSAGE)
    expect(listener).toHaveBeenCalledTimes(1)

    unsubscribe()
    reportApiError(404, PROJECT_NOT_FOUND_MESSAGE)
    expect(listener).toHaveBeenCalledTimes(1)
  })

  it('hears every service through the ApiError they all throw', () => {
    const listener = vi.fn()
    const unsubscribe = onProjectGone(listener)

    void new ApiError(404, PROJECT_NOT_FOUND_MESSAGE)
    expect(listener).toHaveBeenCalledTimes(1)
    unsubscribe()
  })
})
