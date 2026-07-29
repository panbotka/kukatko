import { render, screen } from '@testing-library/react'
import { type ComponentType, type ReactNode } from 'react'
import { describe, expect, it, vi } from 'vitest'

import { TileGrid, type TileGridLayout } from './TileGrid'

/**
 * Minimal stand-in for react-virtuoso's grid: jsdom has no layout, so the real
 * one measures zero and mounts nothing. This renders the items through the real
 * `List` component (the element carrying the column template) and mounts only
 * the slice `window` names — the virtualization the component relies on.
 */
interface MockGridProps {
  data: string[]
  context: TileGridLayout
  components: { List: ComponentType<{ context: TileGridLayout; children: ReactNode }> }
  itemContent: (index: number, item: string) => ReactNode
  computeItemKey: (index: number, item: string) => string
  increaseViewportBy: { bottom: number; top: number }
  useWindowScroll: boolean
}

const virtuoso = vi.hoisted(() => ({
  /** Half-open range of item indexes the fake grid mounts; all of them by default. */
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
            <div key={computeItemKey(index, item)} data-index={index}>
              {itemContent(index, item)}
            </div>
          ))}
      </components.List>
    )
  },
}))

const ITEMS = ['a', 'b', 'c', 'd']

/** Renders a grid of plain cards, one per item. */
function renderGrid(props: { minTile?: number; gap?: number } = {}) {
  return render(
    <TileGrid
      items={ITEMS}
      itemKey={(item) => item}
      renderItem={(item) => <div>tile-{item}</div>}
      {...props}
    />,
  )
}

/** The grid element, the one carrying the column template. */
function gridElement(): HTMLElement {
  const el = document.querySelector<HTMLElement>('.kk-tile-grid')
  if (el === null) {
    throw new Error('tile grid not rendered')
  }
  return el
}

describe('TileGrid', () => {
  it('renders a card per item', () => {
    renderGrid()
    expect(screen.getAllByText(/^tile-/).map((el) => el.textContent)).toEqual([
      'tile-a',
      'tile-b',
      'tile-c',
      'tile-d',
    ])
  })

  it('lays the cards out in responsive auto-fill columns', () => {
    renderGrid()
    const grid = gridElement()
    expect(grid.style.display).toBe('grid')
    // `auto-fill` + `minmax` is what reflows the columns: the container width
    // alone decides how many 160px-floor tracks fit (1 at 320px, 2 at 360px…).
    expect(grid.style.gridTemplateColumns).toBe('repeat(auto-fill, minmax(160px, 1fr))')
    expect(grid.style.gap).toBe('12px')
  })

  it('reflows at the tile size the page asks for', () => {
    renderGrid({ minTile: 140, gap: 8 })
    const grid = gridElement()
    expect(grid.style.gridTemplateColumns).toBe('repeat(auto-fill, minmax(140px, 1fr))')
    expect(grid.style.gap).toBe('8px')
  })

  it('mounts only the items in view, keyed so a tile survives scrolling', () => {
    virtuoso.window = { start: 1, end: 3 }
    try {
      renderGrid()
      expect(screen.getAllByText(/^tile-/).map((el) => el.textContent)).toEqual([
        'tile-b',
        'tile-c',
      ])
      // The whole collection is still handed to virtuoso — only the DOM is trimmed.
      expect(virtuoso.props?.data).toEqual(ITEMS)
      expect(virtuoso.props?.computeItemKey(2, 'c')).toBe('c')
    } finally {
      virtuoso.window = { start: 0, end: Number.MAX_SAFE_INTEGER }
    }
  })

  it('scrolls with the window and keeps a buffer of rows around the viewport', () => {
    renderGrid()
    expect(virtuoso.props?.useWindowScroll).toBe(true)
    expect(virtuoso.props?.increaseViewportBy).toEqual({ top: 200, bottom: 400 })
  })
})
