import { act, renderHook } from '@testing-library/react'
import { type ReactNode } from 'react'
import { MemoryRouter, useNavigate, useSearchParams } from 'react-router-dom'
import { describe, expect, it } from 'vitest'

import { useQueryHistory } from './useQueryHistory'

/**
 * The hook beside the two things a page does with it: write the query into the
 * URL with the options it hands out, and navigate some other way. Mounted under
 * the plain (non-data) router the app itself runs on.
 */
function useHarness() {
  const history = useQueryHistory()
  const [params, setParams] = useSearchParams()
  const navigate = useNavigate()
  return { history, query: params.get('q') ?? '', setParams, navigate }
}

function wrapper({ children }: { children: ReactNode }) {
  return <MemoryRouter initialEntries={['/']}>{children}</MemoryRouter>
}

/** Writes `q` the way a query box does: with the options the hook returns. */
function write(
  result: { current: ReturnType<typeof useHarness> },
  q: string,
  how: 'typed' | 'submitted',
) {
  act(() => {
    result.current.setParams({ q }, result.current.history[how]())
  })
}

function back(result: { current: ReturnType<typeof useHarness> }) {
  act(() => {
    void result.current.navigate(-1)
  })
}

describe('useQueryHistory', () => {
  it('pushes the first pause of an edit and rewrites that entry with the rest', () => {
    const { result } = renderHook(useHarness, { wrapper })

    write(result, 'sv', 'typed')
    write(result, 'svat', 'typed')
    write(result, 'svatba', 'submitted')
    expect(result.current.query).toBe('svatba')

    back(result)
    expect(result.current.query).toBe('')
  })

  it('starts a new entry for the edit after a submit', () => {
    const { result } = renderHook(useHarness, { wrapper })

    write(result, 'svatba', 'submitted')
    write(result, 'hory', 'typed')

    back(result)
    expect(result.current.query).toBe('svatba')
  })

  it('ends the edit on a navigation it did not make', () => {
    const { result } = renderHook(useHarness, { wrapper })

    write(result, 'svatba', 'typed')
    // Somewhere else and back again, as Back/Forward or a sort change would.
    act(() => {
      void result.current.navigate('/?q=svatba&sort=oldest')
    })
    write(result, 'svatba 2024', 'typed')

    back(result)
    expect(result.current.query).toBe('svatba')
  })

  it('writes nothing on end, and the next edit pushes', () => {
    const { result } = renderHook(useHarness, { wrapper })

    write(result, 'svatba', 'typed')
    act(() => {
      result.current.history.end()
    })
    write(result, 'hory', 'typed')

    back(result)
    expect(result.current.query).toBe('svatba')
    back(result)
    expect(result.current.query).toBe('')
  })

  it('keeps one identity across navigations, so it may sit in effect dependencies', () => {
    const { result } = renderHook(useHarness, { wrapper })
    const first = result.current.history

    write(result, 'svatba', 'typed')
    expect(result.current.history).toBe(first)
  })
})
