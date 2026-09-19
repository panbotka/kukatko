import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { ApiError } from './auth'
import {
  addTaskPhotos,
  answerOptions,
  createTask,
  deleteTask,
  fetchTask,
  fetchTasks,
  isClosedState,
  OPEN_TASK_STATES,
  REVIEW_APPROVE,
  REVIEW_RETURN,
  TASK_STATES,
  updateTask,
} from './tasks'

/** The URL the last fetch was issued against. */
function lastUrl(): string {
  const target = vi.mocked(fetch).mock.calls[0]?.[0]
  return typeof target === 'string' ? target : ''
}

/** The parsed body of the last fetch. */
function lastBody(): unknown {
  const body = vi.mocked(fetch).mock.calls[0]?.[1]?.body
  return typeof body === 'string' ? JSON.parse(body) : undefined
}

/** A Response carrying JSON, as the endpoints answer with. */
function jsonResponse(payload: unknown, status = 200): Response {
  return new Response(JSON.stringify(payload), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

beforeEach(() => {
  vi.stubGlobal('fetch', vi.fn())
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('task states', () => {
  it('knows which states end a task', () => {
    expect(isClosedState('done')).toBe(true)
    expect(isClosedState('rejected')).toBe(true)
    expect(isClosedState('question')).toBe(false)
  })

  it('keeps the open list and the predicate in step', () => {
    for (const state of OPEN_TASK_STATES) {
      expect(isClosedState(state)).toBe(false)
    }
    const open = TASK_STATES.filter((state) => !isClosedState(state))
    expect(open).toEqual([...OPEN_TASK_STATES])
  })
})

describe('answerOptions', () => {
  it('offers a task its own options, in order', () => {
    expect(answerOptions({ state: 'question', options: ['1936', '1938'] })).toEqual([
      { text: '1936' },
      { text: '1938' },
    ])
  })

  it('gives a review with no options the built-in pair, and only a review', () => {
    expect(answerOptions({ state: 'review', options: [] })).toEqual([
      { text: REVIEW_APPROVE, builtin: 'approve' },
      { text: REVIEW_RETURN, builtin: 'sendBack' },
    ])
    expect(answerOptions({ state: 'question', options: [] })).toEqual([])
    expect(answerOptions({ state: 'done', options: [] })).toEqual([])
  })

  it('posts fixed Czech strings for the built-in pair, whatever the UI language', () => {
    // The agent matches these verbatim; they must never be localised.
    expect(REVIEW_APPROVE).toBe('Schvaluji.')
    expect(REVIEW_RETURN).toBe('Vrátit k přepracování.')
  })

  it('lets a review with its own options keep them', () => {
    expect(answerOptions({ state: 'review', options: ['ano, ale 1937'] })).toEqual([
      { text: 'ano, ale 1937' },
    ])
  })
})

describe('fetchTasks', () => {
  it('sends no parameters for the bare listing', async () => {
    vi.mocked(fetch).mockResolvedValue(jsonResponse({ tasks: [], total: 0, limit: 50, offset: 0 }))
    await fetchTasks()
    expect(lastUrl()).toBe('/api/v1/tasks')
  })

  it('joins the states and carries the flags', async () => {
    vi.mocked(fetch).mockResolvedValue(jsonResponse({ tasks: [], total: 0, limit: 50, offset: 0 }))
    await fetchTasks({ states: ['question', 'review'], answered: true, q: 'dům', offset: 20 })

    const url = lastUrl()
    expect(url).toContain('state=question%2Creview')
    expect(url).toContain('answered=true')
    expect(url).toContain('q=d%C5%AFm')
    expect(url).toContain('offset=20')
  })

  it('prefers an explicit state list over the open shorthand', async () => {
    vi.mocked(fetch).mockResolvedValue(jsonResponse({ tasks: [], total: 0, limit: 50, offset: 0 }))
    await fetchTasks({ states: ['done'], open: true })

    expect(lastUrl()).toContain('state=done')
    expect(lastUrl()).not.toContain('open=true')
  })

  it('throws the backend message on a failure', async () => {
    vi.mocked(fetch).mockResolvedValue(jsonResponse({ error: 'unknown state "parked"' }, 400))
    await expect(fetchTasks({ states: ['done'] })).rejects.toThrow('unknown state "parked"')
  })
})

describe('task mutations', () => {
  it('escapes the uid in the path', async () => {
    vi.mocked(fetch).mockResolvedValue(jsonResponse({ uid: 'tk/1' }))
    await fetchTask('tk/1')
    expect(lastUrl()).toBe('/api/v1/tasks/tk%2F1')
  })

  it('sends the whole input when opening a task', async () => {
    vi.mocked(fetch).mockResolvedValue(jsonResponse({ uid: 'tk1' }, 201))
    await createTask({ title: 'Kdy?', photo_uids: ['ph1'] })

    expect(lastBody()).toEqual({ title: 'Kdy?', photo_uids: ['ph1'] })
  })

  it('sends only the fields an edit names', async () => {
    vi.mocked(fetch).mockResolvedValue(jsonResponse({ uid: 'tk1' }))
    await updateTask('tk1', { state: 'done', resolution: '1987' })

    expect(lastBody()).toEqual({ state: 'done', resolution: '1987' })
  })

  it("surfaces a viewer's refusal as a 403", async () => {
    vi.mocked(fetch).mockResolvedValue(jsonResponse({ error: 'forbidden' }, 403))
    await expect(updateTask('tk1', { state: 'done' })).rejects.toBeInstanceOf(ApiError)
  })

  it('resolves a delete with no body', async () => {
    vi.mocked(fetch).mockResolvedValue(new Response(null, { status: 204 }))
    await expect(deleteTask('tk1')).resolves.toBeUndefined()
  })

  it('posts the membership change as a list', async () => {
    vi.mocked(fetch).mockResolvedValue(jsonResponse({ changed: 1, task: { uid: 'tk1' } }))
    await addTaskPhotos('tk1', ['ph1', 'ph2'])

    expect(lastUrl()).toBe('/api/v1/tasks/tk1/photos')
    expect(lastBody()).toEqual({ photo_uids: ['ph1', 'ph2'] })
  })
})
