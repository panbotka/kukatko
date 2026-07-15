import { act, renderHook } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { useSelection } from './useSelection'

/** The grid order the range tests select over. */
const ORDER = ['a', 'b', 'c', 'd', 'e']

describe('useSelection', () => {
  it('toggles single items on and off', () => {
    const { result } = renderHook(() => useSelection())
    act(() => {
      result.current.toggle('b')
    })
    expect([...result.current.selected]).toEqual(['b'])
    act(() => {
      result.current.toggle('b')
    })
    expect(result.current.count).toBe(0)
  })

  it('selects the contiguous range between the anchor and the shift-clicked item', () => {
    const { result } = renderHook(() => useSelection())
    act(() => {
      result.current.toggle('b')
    })
    act(() => {
      result.current.toggleRange('d', ORDER)
    })
    expect([...result.current.selected].sort()).toEqual(['b', 'c', 'd'])
  })

  it('selects a range walked backwards from the anchor', () => {
    const { result } = renderHook(() => useSelection())
    act(() => {
      result.current.toggle('d')
    })
    act(() => {
      result.current.toggleRange('b', ORDER)
    })
    expect([...result.current.selected].sort()).toEqual(['b', 'c', 'd'])
  })

  it('only ever adds when ranging: an unselected gap inside stays selected after', () => {
    const { result } = renderHook(() => useSelection())
    act(() => {
      result.current.toggle('a')
    })
    act(() => {
      result.current.toggleRange('c', ORDER)
    })
    act(() => {
      result.current.toggleRange('e', ORDER)
    })
    // The anchor is still 'a' (only a plain toggle moves it), so the second
    // shift-click extends the same range rather than starting a new one.
    expect([...result.current.selected].sort()).toEqual(['a', 'b', 'c', 'd', 'e'])
  })

  it('degrades to a plain toggle without an anchor', () => {
    const { result } = renderHook(() => useSelection())
    act(() => {
      result.current.toggleRange('c', ORDER)
    })
    expect([...result.current.selected]).toEqual(['c'])
  })

  it('degrades to a plain toggle when the anchor left the grid', () => {
    const { result } = renderHook(() => useSelection())
    act(() => {
      result.current.toggle('gone')
    })
    act(() => {
      result.current.toggleRange('c', ORDER)
    })
    expect([...result.current.selected].sort()).toEqual(['c', 'gone'])
  })

  it('clear drops the anchor along with the selection', () => {
    const { result } = renderHook(() => useSelection())
    act(() => {
      result.current.toggle('b')
    })
    act(() => {
      result.current.clear()
    })
    act(() => {
      result.current.toggleRange('d', ORDER)
    })
    expect([...result.current.selected]).toEqual(['d'])
  })
})
