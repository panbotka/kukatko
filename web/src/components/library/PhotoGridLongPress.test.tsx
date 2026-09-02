import { act, fireEvent, render, screen } from '@testing-library/react'
import { type ReactNode } from 'react'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'
import { useSelection } from '../../hooks/useSelection'
import { LONG_PRESS_MS, LONG_PRESS_SLOP } from '../../lib/longPressSelect'
import type { Photo } from '../../services/photos'

import { PhotoGrid } from './PhotoGrid'

// Same stand-in as the other grid tests: jsdom lays nothing out, so virtuoso
// mounts no rows of its own and the tiles under test would never exist.
interface MockListProps {
  data: unknown[]
  itemContent: (index: number, row: never) => ReactNode
}
vi.mock('react-virtuoso', () => ({
  Virtuoso: ({ data, itemContent }: MockListProps) => (
    <div data-testid="grid">
      {data.map((row, index) => (
        <div key={index}>{itemContent(index, row as never)}</div>
      ))}
    </div>
  ),
}))

const UIDS = ['a', 'b', 'c', 'd']

/** Builds a minimal Photo whose tile is findable by its file name. */
function photo(uid: string): Photo {
  return {
    uid,
    file_name: `${uid}.jpg`,
    title: '',
    thumb_url: `/thumb/${uid}`,
  } as unknown as Photo
}

const PHOTOS = UIDS.map(photo)

/** A point in the shape a TouchEvent's touch list carries. */
function pt(x: number, y: number): { clientX: number; clientY: number } {
  return { clientX: x, clientY: y }
}

/**
 * Stands in for the layout jsdom does not do: the four tiles sit left to right,
 * 100 px apart, and anything past them is the gutter beyond the wall.
 */
function stubHitTest(): void {
  const at = (x: number, _y: number): Element | null => {
    const uid = UIDS.at(Math.floor(x / 100))
    return uid === undefined ? null : document.querySelector(`[data-photo-uid="${uid}"]`)
  }
  Object.defineProperty(document, 'elementFromPoint', {
    value: at,
    configurable: true,
    writable: true,
  })
}

/**
 * The grid as a page actually wires it: a real `useSelection` behind it and the
 * live selection count beside it, which is what the bulk actions bar shows.
 */
function Harness() {
  const selection = useSelection()
  return (
    <>
      <output data-testid="count">{selection.count}</output>
      <PhotoGrid
        photos={PHOTOS}
        loadingMore={false}
        moreError={false}
        onRetry={vi.fn()}
        selection={{
          active: false,
          hoverSelect: true,
          selected: selection.selected,
          onToggle: selection.toggle,
          onToggleRange: selection.toggleRange,
          onSelectMany: selection.selectMany,
        }}
      />
    </>
  )
}

/** Renders the harness inside the providers a tile needs. */
function renderGrid() {
  return render(
    <I18nextProvider i18n={i18n}>
      <MemoryRouter>
        <Harness />
      </MemoryRouter>
    </I18nextProvider>,
  )
}

/** The tile box for a photo — what the gesture reads its UID off. */
function tile(uid: string): HTMLElement {
  const found = document.querySelector(`[data-photo-uid="${uid}"]`)
  if (!(found instanceof HTMLElement)) {
    throw new Error(`no tile for ${uid}`)
  }
  return found
}

/** The live selection count, as the bulk actions bar shows it. */
function count(): string {
  return screen.getByTestId('count').textContent
}

beforeAll(async () => {
  await i18n.changeLanguage('en')
})

beforeEach(() => {
  vi.useFakeTimers()
  Object.defineProperty(window.navigator, 'maxTouchPoints', { value: 1, configurable: true })
  stubHitTest()
})

afterEach(() => {
  vi.useRealTimers()
  Reflect.deleteProperty(document, 'elementFromPoint')
  Object.defineProperty(window.navigator, 'maxTouchPoints', { value: 0, configurable: true })
})

describe('PhotoGrid long-press selection', () => {
  it('starts a selection on a long press and extends it as the finger drags', () => {
    renderGrid()
    // Before the gesture the wall is a plain link grid: nothing is picked.
    expect(count()).toBe('0')
    expect(screen.getByRole('link', { name: 'b.jpg' })).toBeInTheDocument()

    const start = tile('b')
    fireEvent.touchStart(start, { touches: [pt(150, 20)] })
    act(() => {
      vi.advanceTimersByTime(LONG_PRESS_MS)
    })
    // The press alone selects its photo — which is what puts the grid into its
    // selection-first mode, with no "enter selection mode" step to take.
    expect(count()).toBe('1')
    expect(screen.getByRole('button', { name: 'b.jpg' })).toHaveAttribute('aria-pressed', 'true')

    // Dragging on adds every tile the finger crosses, and the count climbs with
    // it rather than at the end of the stroke.
    act(() => {
      fireEvent.touchMove(start, { touches: [pt(250, 20)] })
    })
    expect(count()).toBe('2')
    act(() => {
      fireEvent.touchMove(start, { touches: [pt(350, 20)] })
    })
    expect(count()).toBe('3')
    // Back over a tile already in the batch: the gesture is additive, so the
    // count holds instead of dropping one.
    act(() => {
      fireEvent.touchMove(start, { touches: [pt(250, 20)] })
    })
    expect(count()).toBe('3')

    fireEvent.touchEnd(start, { touches: [], changedTouches: [pt(250, 20)] })
    expect(count()).toBe('3')
    for (const uid of ['b', 'c', 'd']) {
      expect(screen.getByRole('button', { name: `${uid}.jpg` })).toHaveAttribute(
        'aria-pressed',
        'true',
      )
    }
    expect(screen.getByRole('button', { name: 'a.jpg' })).toHaveAttribute('aria-pressed', 'false')
  })

  it('leaves an ordinary scroll alone', () => {
    renderGrid()
    const start = tile('b')
    fireEvent.touchStart(start, { touches: [pt(150, 20)] })
    act(() => {
      fireEvent.touchMove(start, { touches: [pt(150, 20 + LONG_PRESS_SLOP + 10)] })
      vi.advanceTimersByTime(LONG_PRESS_MS * 2)
    })
    expect(count()).toBe('0')
    // Still a link grid: nothing was selected, so nothing turned selection-first.
    expect(screen.getByRole('link', { name: 'b.jpg' })).toBeInTheDocument()
  })

  it('goes on toggling by tap once the selection has been started', () => {
    renderGrid()
    const start = tile('a')
    fireEvent.touchStart(start, { touches: [pt(50, 20)] })
    act(() => {
      vi.advanceTimersByTime(LONG_PRESS_MS)
    })
    fireEvent.touchEnd(start, { touches: [], changedTouches: [pt(50, 20)] })
    expect(count()).toBe('1')

    // A plain tap on another tile adds it, and a second tap on it takes it back
    // off — exactly what a click did before the gesture existed.
    fireEvent.click(screen.getByRole('button', { name: 'c.jpg' }))
    expect(count()).toBe('2')
    fireEvent.click(screen.getByRole('button', { name: 'c.jpg' }))
    expect(count()).toBe('1')
  })

  it('offers no gesture where the grid has no selection to add to', () => {
    render(
      <I18nextProvider i18n={i18n}>
        <MemoryRouter>
          <PhotoGrid photos={PHOTOS} loadingMore={false} moreError={false} onRetry={vi.fn()} />
        </MemoryRouter>
      </I18nextProvider>,
    )
    const start = tile('b')
    fireEvent.touchStart(start, { touches: [pt(150, 20)] })
    act(() => {
      vi.advanceTimersByTime(LONG_PRESS_MS * 2)
    })
    // A viewer's grid stays a wall of links; the long press does nothing at all.
    expect(screen.getByRole('link', { name: 'b.jpg' })).toBeInTheDocument()
  })
})
