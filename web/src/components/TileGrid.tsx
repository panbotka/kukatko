import { forwardRef, type CSSProperties, type ReactNode } from 'react'
import { type GridComponents, VirtuosoGrid } from 'react-virtuoso'

/** Geometry of a tile grid, threaded to the list element via virtuoso's `context`. */
export interface TileGridLayout {
  /** Minimum tile width in px — the `minmax` floor that decides the column count. */
  minTile: number
  /** Gap between tiles in px. */
  gap: number
}

/** The card-grid geometry a page gets unless it asks for another one. */
const DEFAULT_LAYOUT: TileGridLayout = { minTile: 160, gap: 12 }

/**
 * The grid element itself: the very same responsive
 * `repeat(auto-fill, minmax(<minTile>px, 1fr))` track list the plain card grids
 * and `TileGridSkeleton` draw, so virtualizing a page moves no tile — the column
 * count still follows the container width (one column on a 320px phone, two at
 * 360px, more as it widens).
 *
 * Virtuoso's own style goes last on purpose: it carries the padding that stands
 * in for the rows that are not mounted, and it must win over anything here.
 */
const List = forwardRef<
  HTMLDivElement,
  { context?: TileGridLayout; style?: CSSProperties; className?: string; children?: ReactNode }
>(function TileGridList({ context, style, className, children, ...props }, ref) {
  const { minTile, gap } = context ?? DEFAULT_LAYOUT
  return (
    <div
      ref={ref}
      {...props}
      // A selector hook only — the tiles keep their own `.kk-tile` card styling,
      // unlike the photo wall's `.kukatko-photo-grid`, which restyles what it holds.
      className={`kk-tile-grid${className ? ` ${className}` : ''}`}
      style={{
        display: 'grid',
        gridTemplateColumns: `repeat(auto-fill, minmax(${String(minTile)}px, 1fr))`,
        gap: `${String(gap)}px`,
        ...style,
      }}
    >
      {children}
    </div>
  )
})

const gridComponents: GridComponents<TileGridLayout> = { List }

/** Props for {@link TileGrid}. */
export interface TileGridProps<T> {
  /** The whole collection — only the visible slice of it is ever mounted. */
  items: T[]
  /** Stable identity of an item, so a tile survives scrolling as the same node. */
  itemKey: (item: T) => string
  /** Renders one card. */
  renderItem: (item: T) => ReactNode
  /** Minimum tile width in px; pass the same value to `TileGridSkeleton`. */
  minTile?: number
  /** Gap between tiles in px. */
  gap?: number
}

/**
 * A virtualized responsive grid of cards (albums today, any tile list tomorrow):
 * only the rows in view plus a small buffer are in the DOM, so a large
 * collection neither inflates the document nor starts hundreds of cover loads.
 *
 * It scrolls with the window — the page stays a normal document, and the browser
 * keeps its own scrolling, so navigating away and back behaves as before. The
 * layout is intentionally identical to the plain CSS grid it replaces: the same
 * `minmax` columns, the same gap, the same tiles.
 */
export function TileGrid<T>({
  items,
  itemKey,
  renderItem,
  minTile = DEFAULT_LAYOUT.minTile,
  gap = DEFAULT_LAYOUT.gap,
}: TileGridProps<T>) {
  return (
    <VirtuosoGrid
      useWindowScroll
      data={items}
      context={{ minTile, gap }}
      components={gridComponents}
      // The buffer: a row's covers start loading just before it scrolls in, and a
      // short scroll back finds its tiles still mounted instead of re-fading them.
      increaseViewportBy={{ top: 200, bottom: 400 }}
      itemContent={(_index, item) => renderItem(item)}
      computeItemKey={(_index, item) => itemKey(item)}
    />
  )
}
