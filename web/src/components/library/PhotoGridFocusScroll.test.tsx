import { act, render } from '@testing-library/react'
import { createRef, forwardRef, type ReactNode, useEffect, useImperativeHandle } from 'react'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { type ListRange } from 'react-virtuoso'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'
import { revealAlign } from '../../lib/gridScroll'
import type { Photo } from '../../services/photos'

import { PhotoGrid, type PhotoGridHandle } from './PhotoGrid'

/** Every jump the grid asked virtuoso for, newest last. */
const jumps = vi.fn()
/** The row range the stand-in reports as being on screen. */
let visible: ListRange = { startIndex: 0, endIndex: 0 }

/**
 * A stand-in for react-virtuoso's list that mounts nothing and instead reports a
 * fixed visible *row* range and records the jumps it is asked for. Whether the
 * wall moves at all is the whole point here, so what is asserted is the call —
 * jsdom lays nothing out and could never be asked where a row ended up.
 */
interface MockListProps {
  data: readonly unknown[]
  itemContent: (index: number, row: never) => ReactNode
  rangeChanged?: (range: ListRange) => void
}
vi.mock('react-virtuoso', () => ({
  Virtuoso: forwardRef<unknown, MockListProps>(function MockList({ rangeChanged }, ref) {
    useImperativeHandle(ref, () => ({
      scrollToIndex: (location: unknown) => {
        jumps(location)
      },
      getState: () => undefined,
    }))
    useEffect(() => {
      rangeChanged?.(visible)
    }, [rangeChanged])
    return <div data-testid="grid" />
  }),
}))

// jsdom measures every box as zero-wide, which would lay the whole library into
// one row and leave nothing to scroll between. A real width gives the wall real
// rows, which is what a reveal-only scroll is about.
vi.mock('../../hooks/useElementWidth', () => ({ useElementWidth: () => 1200 }))

/** A minimal photo; only its shape matters to the justified layout. */
function photo(uid: string): Photo {
  return {
    uid,
    file_name: `${uid}.jpg`,
    file_width: 100,
    file_height: 100,
    title: '',
    thumb_url: `/thumb/${uid}`,
  } as unknown as Photo
}

/** Enough photos for the wall to run over many rows. */
const PHOTOS = Array.from({ length: 60 }, (_, index) => photo(`p${String(index)}`))

/** Renders the wall and hands back its imperative handle. */
function renderGrid() {
  const ref = createRef<PhotoGridHandle>()
  render(
    <I18nextProvider i18n={i18n}>
      <MemoryRouter>
        <PhotoGrid
          photos={PHOTOS}
          loadingMore={false}
          moreError={false}
          onEndReached={vi.fn()}
          onRetry={vi.fn()}
          gridRef={ref}
        />
      </MemoryRouter>
    </I18nextProvider>,
  )
  return ref
}

/** The single jump the grid asked for, or undefined when it asked for none. */
function lastJump(): { index: number; align?: string } | undefined {
  return jumps.mock.calls.at(-1)?.[0] as { index: number; align?: string } | undefined
}

beforeEach(() => {
  jumps.mockClear()
  visible = { startIndex: 0, endIndex: 0 }
})

describe('revealAlign', () => {
  it('does not scroll for a row comfortably inside the visible range', () => {
    expect(revealAlign(5, { startIndex: 2, endIndex: 9 })).toBeNull()
  })

  it('aligns a row above the visible range to the top', () => {
    expect(revealAlign(1, { startIndex: 4, endIndex: 9 })).toBe('start')
  })

  it('aligns a row below the visible range to the bottom', () => {
    expect(revealAlign(12, { startIndex: 4, endIndex: 9 })).toBe('end')
  })

  it('still reveals the rows at either edge, which may be half shown', () => {
    expect(revealAlign(4, { startIndex: 4, endIndex: 9 })).toBe('start')
    expect(revealAlign(9, { startIndex: 4, endIndex: 9 })).toBe('end')
  })

  it('aligns to the top before any range has been reported', () => {
    expect(revealAlign(3, null)).toBe('start')
  })
})

describe('PhotoGrid scrollToIndex', () => {
  it('leaves the wall alone when the focused photo is already on screen', () => {
    visible = { startIndex: 0, endIndex: 20 }
    const ref = renderGrid()

    act(() => {
      ref.current?.scrollToIndex(20)
    })

    expect(jumps).not.toHaveBeenCalled()
  })

  it('scrolls down by the shortest hop when focus steps off the bottom', () => {
    visible = { startIndex: 0, endIndex: 2 }
    const ref = renderGrid()

    act(() => {
      ref.current?.scrollToIndex(59)
    })

    expect(lastJump()?.align).toBe('end')
  })

  it('scrolls up when focus steps off the top', () => {
    visible = { startIndex: 8, endIndex: 12 }
    const ref = renderGrid()

    act(() => {
      ref.current?.scrollToIndex(0)
    })

    expect(lastJump()?.align).toBe('start')
  })

  it('honours an explicit alignment even for a photo already on screen', () => {
    visible = { startIndex: 0, endIndex: 20 }
    const ref = renderGrid()

    act(() => {
      ref.current?.scrollToIndex({ index: 20, align: 'start' })
    })

    expect(lastJump()?.align).toBe('start')
  })
})
