import { forwardRef, type CSSProperties, type ReactNode } from 'react'
import { type GridComponents, VirtuosoGrid } from 'react-virtuoso'

import { gridTemplateColumns, REVIEW_GRID_SCOPE } from '../../lib/gridDensity'

/** What the grid element needs to draw itself, threaded via virtuoso's context. */
interface ReviewGridContext {
  /** The column count the reader pinned with the shared review density stepper. */
  density: number
  /** Rendered below the last card: the "loading more" line, or nothing. */
  footer?: ReactNode
}

/**
 * The grid element itself — the very same `repeat(n, 1fr)` track list and
 * `kk-review-grid` class the plain review card grids draw, so virtualizing a
 * page moves no card. `kk-review-grid` is what keeps the pinned column count
 * honest: a review card carries a name field and a button, and without it the
 * `1fr` tracks would grow to their content and run off the side of a phone.
 *
 * Virtuoso's own style goes last on purpose: it carries the padding standing in
 * for the rows that are not mounted, and it must win over anything here.
 */
const List = forwardRef<
  HTMLDivElement,
  { context?: ReviewGridContext; style?: CSSProperties; className?: string; children?: ReactNode }
>(function ReviewGridList({ context, style, className, children, ...props }, ref) {
  const density = context?.density ?? 1
  return (
    <div
      ref={ref}
      {...props}
      className={`kk-review-grid${className === undefined ? '' : ` ${className}`}`}
      data-density={density}
      style={{
        display: 'grid',
        gridTemplateColumns: gridTemplateColumns(density),
        gap: `${String(REVIEW_GRID_SCOPE.gapPx)}px`,
        ...style,
      }}
    >
      {children}
    </div>
  )
})

/** Renders whatever the page put in the context, below the last mounted card. */
function Footer({ context }: { context?: ReviewGridContext }) {
  return <>{context?.footer}</>
}

const gridComponents: GridComponents<ReviewGridContext> = { List, Footer }

/** Props for {@link ReviewGrid}. */
export interface ReviewGridProps<T> {
  /** The cards loaded so far — only the rows in view are ever mounted. */
  items: T[]
  /** Stable identity of an item, so a card survives scrolling as the same node. */
  itemKey: (item: T) => string
  /** Renders one card. */
  renderItem: (item: T) => ReactNode
  /** The pinned column count from the shared review density stepper. */
  density: number
  /** Called when the reader reaches the last cards: ask for the next page. */
  onEndReached?: () => void
  /** Rendered below the last card — the "loading more" line and its retry. */
  footer?: ReactNode
}

/**
 * A virtualized grid of review cards: only the rows in view plus a small buffer
 * are in the DOM, so a queue of thousands of groups neither inflates the
 * document nor starts thousands of face crops loading at once.
 *
 * It scrolls with the window — the page stays a normal document — and it asks
 * for the next page as the reader reaches the end, so a long queue arrives
 * incrementally instead of all at once.
 */
export function ReviewGrid<T>({
  items,
  itemKey,
  renderItem,
  density,
  onEndReached,
  footer,
}: ReviewGridProps<T>) {
  return (
    <VirtuosoGrid
      useWindowScroll
      data={items}
      context={{ density, footer }}
      components={gridComponents}
      endReached={onEndReached}
      // The buffer: a row's face crops start loading just before it scrolls in,
      // and a short scroll back finds its cards still mounted.
      increaseViewportBy={{ top: 200, bottom: 400 }}
      itemContent={(_index, item) => renderItem(item)}
      computeItemKey={(_index, item) => itemKey(item)}
    />
  )
}
