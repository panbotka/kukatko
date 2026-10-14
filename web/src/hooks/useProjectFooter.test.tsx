import { act, renderHook, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { announceFooterChanged, useProjectFooter } from './useProjectFooter'

/** Answers each project's footer request with that project's line. */
const LINES: Record<string, string> = { v: 'Provozuje SDH Veselice', rodina: 'Rodina Nováků' }

let fetchMock: ReturnType<typeof vi.fn>

beforeEach(() => {
  fetchMock = vi.fn((input: RequestInfo | URL) => {
    const url = input instanceof Request ? input.url : String(input)
    const slug = /\/p\/([^/]+)\/settings\/footer$/.exec(url)?.[1] ?? ''
    if (slug === 'broken') {
      return Promise.resolve(new Response('{"error":"boom"}', { status: 500 }))
    }
    return Promise.resolve(
      new Response(JSON.stringify({ footer_text: LINES[slug] ?? '' }), { status: 200 }),
    )
  })
  vi.stubGlobal('fetch', fetchMock)
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('useProjectFooter', () => {
  it("reads the named project's line", async () => {
    const { result } = renderHook(() => useProjectFooter('v'))
    await waitFor(() => {
      expect(result.current).toBe('Provozuje SDH Veselice')
    })
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/p/v/settings/footer', expect.anything())
  })

  it('asks nothing and answers nothing for no project', () => {
    const { result } = renderHook(() => useProjectFooter(null))
    expect(result.current).toBe('')
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it("never shows the previous project's line under the next one", async () => {
    let resolveRodina: (response: Response) => void = () => undefined
    const { result, rerender } = renderHook(({ slug }) => useProjectFooter(slug), {
      initialProps: { slug: 'v' },
    })
    await waitFor(() => {
      expect(result.current).toBe('Provozuje SDH Veselice')
    })
    fetchMock.mockImplementationOnce(
      () =>
        new Promise<Response>((resolve) => {
          resolveRodina = resolve
        }),
    )
    rerender({ slug: 'rodina' })
    // Still on its way: nothing rather than v's sentence.
    expect(result.current).toBe('')
    await act(async () => {
      resolveRodina(new Response(JSON.stringify({ footer_text: 'Rodina Nováků' }), { status: 200 }))
      await Promise.resolve()
    })
    await waitFor(() => {
      expect(result.current).toBe('Rodina Nováků')
    })
  })

  it('shows no line when the read fails', async () => {
    const { result } = renderHook(() => useProjectFooter('broken'))
    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalled()
    })
    expect(result.current).toBe('')
  })

  it("takes a saved line at once, and only its own project's", async () => {
    const { result } = renderHook(() => useProjectFooter('v'))
    await waitFor(() => {
      expect(result.current).toBe('Provozuje SDH Veselice')
    })
    act(() => {
      announceFooterChanged('rodina', 'Cizí text')
    })
    expect(result.current).toBe('Provozuje SDH Veselice')
    act(() => {
      announceFooterChanged('v', 'Nový text')
    })
    expect(result.current).toBe('Nový text')
  })
})
