import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { setCurrentProjectSlug } from './api'
import {
  detachNamelessSubjects,
  fetchNamelessSubjects,
  restoreNamelessSubjects,
} from './maintenance'

/** Stubs fetch with one answer and returns the mock. */
function stubFetch(body: BodyInit, headers: Record<string, string> = {}) {
  const mock = vi.fn().mockResolvedValue(new Response(body, { status: 200, headers }))
  vi.stubGlobal('fetch', mock)
  return mock
}

/** The method and URL of the stub's only call. */
function sent(mock: ReturnType<typeof stubFetch>) {
  const [url, init] = mock.mock.calls[0] as [string, RequestInit]
  return { url, method: init.method }
}

beforeEach(() => {
  // The repair lives on the instance-level page: a maintainer with no
  // membership has no current project, and the calls must not need one.
  setCurrentProjectSlug(null)
})

afterEach(() => {
  vi.unstubAllGlobals()
  setCurrentProjectSlug(null)
})

describe('nameless-subject repair', () => {
  it('reports in the named project, not the current one', async () => {
    setCurrentProjectSlug('v')
    const mock = stubFetch(JSON.stringify({ subjects: [], marker_total: 0, face_total: 0 }))

    await fetchNamelessSubjects('rodina')

    expect(sent(mock)).toEqual({
      url: '/api/v1/p/rodina/maintenance/nameless-subjects',
      method: 'GET',
    })
  })

  it('works without any current project', async () => {
    const mock = stubFetch(JSON.stringify({ subjects: [], marker_total: 0, face_total: 0 }))

    await fetchNamelessSubjects('a b')

    expect(sent(mock).url).toBe('/api/v1/p/a%20b/maintenance/nameless-subjects')
  })

  it('detaches in the named project and reads the undo file headers', async () => {
    vi.stubGlobal('URL', { createObjectURL: () => '#undo', revokeObjectURL: () => undefined })
    const mock = stubFetch('{"subjects":[]}', {
      'Content-Disposition': 'attachment; filename="undo.json"',
      'X-Kukatko-Nameless-Subjects': '1',
      'X-Kukatko-Nameless-Markers': '2',
      'X-Kukatko-Nameless-Faces': '3',
    })

    const undo = await detachNamelessSubjects('rodina')

    expect(sent(mock)).toEqual({
      url: '/api/v1/p/rodina/maintenance/nameless-subjects/detach',
      method: 'POST',
    })
    expect(undo).toEqual({ filename: 'undo.json', subjects: 1, markers: 2, faces: 3 })
  })

  it('restores into the named project', async () => {
    const mock = stubFetch(JSON.stringify({ queued: 4 }))
    const file = new File(['{}'], 'undo.json', { type: 'application/json' })

    const result = await restoreNamelessSubjects('rodina', file)

    expect(sent(mock)).toEqual({
      url: '/api/v1/p/rodina/maintenance/nameless-subjects/restore',
      method: 'POST',
    })
    expect(result.queued).toBe(4)
  })
})
