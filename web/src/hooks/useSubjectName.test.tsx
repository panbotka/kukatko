import { renderHook, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { type Subject, type SubjectCount } from '../services/people'

import { useSubjectName } from './useSubjectName'

vi.mock('../services/people', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/people')>()
  return { ...actual, fetchSubject: vi.fn() }
})

const { fetchSubject } = await import('../services/people')
const fetchMock = vi.mocked(fetchSubject)

/** listed builds a subject as the page's own list carries it. */
function listed(uid: string, name: string): SubjectCount {
  return { uid, name, marker_count: 2, photo_count: 2 } as unknown as SubjectCount
}

beforeEach(() => {
  fetchMock.mockReset()
})

describe('useSubjectName', () => {
  it('names a subject from the loaded list without asking the API', () => {
    const { result } = renderHook(() => useSubjectName('su_1', [listed('su_1', 'Alice')], false))

    expect(result.current).toBe('Alice')
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('waits for the list rather than fetching a subject that may be in it', () => {
    renderHook(() => useSubjectName('su_1', [], true))

    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('fetches the one subject the loaded list turned out not to carry', async () => {
    fetchMock.mockResolvedValue({ uid: 'su_1', name: 'Alice' } as unknown as Subject)
    const { result } = renderHook(() => useSubjectName('su_1', [listed('su_2', 'Bob')], false))

    await waitFor(() => {
      expect(result.current).toBe('Alice')
    })
    expect(fetchMock).toHaveBeenCalledWith('su_1', expect.anything())
  })

  it('stays unknown when the lookup fails', async () => {
    fetchMock.mockRejectedValue(new Error('404'))
    const { result } = renderHook(() => useSubjectName('su_gone', [], false))

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalled()
    })
    expect(result.current).toBeNull()
  })

  it('does not label a newly picked subject with the previous answer', async () => {
    fetchMock.mockResolvedValue({ uid: 'su_1', name: 'Alice' } as unknown as Subject)
    const { result, rerender } = renderHook(
      ({ uid }: { uid: string }) => useSubjectName(uid, [], false),
      { initialProps: { uid: 'su_1' } },
    )
    await waitFor(() => {
      expect(result.current).toBe('Alice')
    })

    fetchMock.mockReturnValue(new Promise(() => undefined))
    rerender({ uid: 'su_2' })

    expect(result.current).toBeNull()
  })

  it('names nobody when nothing is selected', () => {
    const { result } = renderHook(() => useSubjectName(null, [listed('su_1', 'Alice')], false))

    expect(result.current).toBeNull()
    expect(fetchMock).not.toHaveBeenCalled()
  })
})
