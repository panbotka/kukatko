import { render, screen } from '@testing-library/react'
import { type ComponentType, type ReactNode } from 'react'
import { describe, expect, it, vi } from 'vitest'

import { ReviewGrid } from './ReviewGrid'

/** Context the grid threads to its List and Footer components. */
interface GridContext {
  density: number
  footer?: ReactNode
}

/** The props the fake grid reads; the rest is accepted and ignored. */
interface MockGridProps {
  data: string[]
  context: GridContext
  components: {
    List: ComponentType<{ context: GridContext; children: ReactNode }>
    Footer: ComponentType<{ context: GridContext }>
  }
  itemContent: (index: number, item: string) => ReactNode
  computeItemKey: (index: number, item: string) => string
  endReached?: () => void
  increaseViewportBy: { bottom: number; top: number }
  useWindowScroll: boolean
}

/**
 * Minimal stand-in for react-virtuoso's grid: jsdom has no layout, so the real
 * one measures zero and mounts nothing. This renders the items through the real
 * `List` (the element carrying the column template) and mounts only the slice
 * `window` names — the virtualization the component relies on.
 */
const virtuoso = vi.hoisted(() => ({
  window: { start: 0, end: Number.MAX_SAFE_INTEGER },
  props: null as MockGridProps | null,
}))

vi.mock('react-virtuoso', () => ({
  VirtuosoGrid: (props: MockGridProps) => {
    virtuoso.props = props
    const { components, context, data, itemContent, computeItemKey } = props
    return (
      <components.List context={context}>
        {data
          .map((item, index) => ({ index, item }))
          .filter(({ index }) => index >= virtuoso.window.start && index < virtuoso.window.end)
          .map(({ index, item }) => (
            <div key={computeItemKey(index, item)}>{itemContent(index, item)}</div>
          ))}
        <components.Footer context={context} />
      </components.List>
    )
  },
}))

const ITEMS = ['a', 'b', 'c', 'd']

/** Renders a grid of plain cards, one per item. */
function renderGrid(
  props: { density?: number; footer?: ReactNode; onEndReached?: () => void } = {},
) {
  return render(
    <ReviewGrid
      items={ITEMS}
      itemKey={(item) => item}
      renderItem={(item) => <div>card-{item}</div>}
      density={props.density ?? 3}
      footer={props.footer}
      onEndReached={props.onEndReached}
    />,
  )
}

/** The grid element, the one carrying the column template. */
function gridElement(): HTMLElement {
  const el = document.querySelector<HTMLElement>('.kk-review-grid')
  if (el === null) {
    throw new Error('review grid not rendered')
  }
  return el
}

describe('ReviewGrid', () => {
  it('renders a card per item', () => {
    renderGrid()
    expect(screen.getAllByText(/^card-/).map((el) => el.textContent)).toEqual([
      'card-a',
      'card-b',
      'card-c',
      'card-d',
    ])
  })

  it('pins the column count the reader chose', () => {
    renderGrid({ density: 2 })
    const grid = gridElement()
    expect(grid.style.display).toBe('grid')
    expect(grid.style.gridTemplateColumns).toBe('repeat(2, 1fr)')
    expect(grid).toHaveAttribute('data-density', '2')
  })

  it('mounts only the cards in view, keyed so one survives scrolling', () => {
    virtuoso.window = { start: 1, end: 3 }
    try {
      renderGrid()
      expect(screen.getAllByText(/^card-/).map((el) => el.textContent)).toEqual([
        'card-b',
        'card-c',
      ])
      // The whole queue is still handed to virtuoso — only the DOM is trimmed.
      expect(virtuoso.props?.data).toEqual(ITEMS)
      expect(virtuoso.props?.computeItemKey(2, 'c')).toBe('c')
    } finally {
      virtuoso.window = { start: 0, end: Number.MAX_SAFE_INTEGER }
    }
  })

  it('renders the page footer below the last card', () => {
    renderGrid({ footer: <span>loading more</span> })
    expect(screen.getByText('loading more')).toBeInTheDocument()
  })

  it('scrolls with the window, buffers rows and reports reaching the end', () => {
    const onEndReached = vi.fn()
    renderGrid({ onEndReached })
    expect(virtuoso.props?.useWindowScroll).toBe(true)
    expect(virtuoso.props?.increaseViewportBy).toEqual({ top: 200, bottom: 400 })
    virtuoso.props?.endReached?.()
    expect(onEndReached).toHaveBeenCalledTimes(1)
  })
})
