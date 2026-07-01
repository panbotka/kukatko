import { act, renderHook, waitFor } from '@testing-library/react'
import { type VirtuosoGridHandle } from 'react-virtuoso'
import { describe, expect, it, vi } from 'vitest'

import { useGridJump, type UseGridJumpOptions } from './useGridJump'

/** A fake grid handle capturing the imperative scroll calls. */
function fakeGrid(): {
  ref: { current: VirtuosoGridHandle }
  scrollToIndex: ReturnType<typeof vi.fn>
} {
  const scrollToIndex = vi.fn()
  return {
    ref: { current: { scrollToIndex, scrollTo: vi.fn(), scrollBy: vi.fn() } },
    scrollToIndex,
  }
}

describe('useGridJump', () => {
  it('scrolls straight to the index when it is already loaded', async () => {
    const grid = fakeGrid()
    const loadMore = vi.fn()
    const { result } = renderHook(() =>
      useGridJump({
        gridRef: grid.ref,
        loadedCount: 5,
        hasMore: false,
        loadingMore: false,
        loadMore,
      }),
    )

    act(() => {
      result.current(2)
    })

    await waitFor(() => {
      expect(grid.scrollToIndex).toHaveBeenCalledWith({ index: 2, align: 'start' })
    })
    expect(loadMore).not.toHaveBeenCalled()
  })

  it('loads pages until the target index is reachable, then scrolls to it', async () => {
    const grid = fakeGrid()
    const loadMore = vi.fn()
    const { result, rerender } = renderHook((props: UseGridJumpOptions) => useGridJump(props), {
      initialProps: {
        gridRef: grid.ref,
        loadedCount: 1,
        hasMore: true,
        loadingMore: false,
        loadMore,
      },
    })

    // The target sits beyond the loaded page, so the hook drives the loader.
    act(() => {
      result.current(3)
    })
    await waitFor(() => {
      expect(loadMore).toHaveBeenCalled()
    })
    expect(grid.scrollToIndex).not.toHaveBeenCalled()

    // The next page arrives; now the target is reachable and the grid scrolls.
    rerender({ gridRef: grid.ref, loadedCount: 4, hasMore: true, loadingMore: false, loadMore })
    await waitFor(() => {
      expect(grid.scrollToIndex).toHaveBeenCalledWith({ index: 3, align: 'start' })
    })
  })

  it('clamps to the last loaded item when no more pages remain', async () => {
    const grid = fakeGrid()
    const loadMore = vi.fn()
    const { result } = renderHook(() =>
      useGridJump({
        gridRef: grid.ref,
        loadedCount: 2,
        hasMore: false,
        loadingMore: false,
        loadMore,
      }),
    )

    act(() => {
      result.current(9)
    })

    await waitFor(() => {
      expect(grid.scrollToIndex).toHaveBeenCalledWith({ index: 1, align: 'start' })
    })
    expect(loadMore).not.toHaveBeenCalled()
  })
})
