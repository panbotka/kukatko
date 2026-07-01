import { forwardRef } from 'react'
import Button from 'react-bootstrap/Button'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'
import {
  type GridComponents,
  type ListRange,
  VirtuosoGrid,
  type VirtuosoGridHandle,
} from 'react-virtuoso'

import { type Photo } from '../../services/photos'

import { PhotoTile } from './PhotoTile'

/** State the footer needs, threaded to the virtuoso components via `context`. */
interface GridContext {
  loadingMore: boolean
  moreError: boolean
  onRetry: () => void
}

/**
 * Responsive CSS-grid list: columns adapt to the viewport via `auto-fill` so the
 * tile count per row follows the available width on mobile, tablet and desktop.
 */
const List = forwardRef<
  HTMLDivElement,
  { style?: React.CSSProperties; className?: string; children?: React.ReactNode }
>(function List({ style, className, children, ...props }, ref) {
  return (
    <div
      ref={ref}
      {...props}
      // The class lets the page measure the live column count (for row-wise
      // keyboard navigation) from the rendered grid's computed `grid-template`.
      className={`kukatko-photo-grid${className ? ` ${className}` : ''}`}
      style={{
        display: 'grid',
        gridTemplateColumns: 'repeat(auto-fill, minmax(140px, 1fr))',
        gap: '6px',
        ...style,
      }}
    >
      {children}
    </div>
  )
})

/** Footer slot: a spinner while a page loads, or a retry control if one failed. */
function GridFooter({ context }: { context?: GridContext }) {
  const { t } = useTranslation()
  if (!context) {
    return null
  }
  if (context.moreError) {
    return (
      <div className="d-flex align-items-center justify-content-center gap-2 py-4">
        <span className="text-secondary">{t('library.error.more')}</span>
        <Button size="sm" variant="outline-light" onClick={context.onRetry}>
          {t('library.error.retry')}
        </Button>
      </div>
    )
  }
  if (context.loadingMore) {
    return (
      <div className="d-flex justify-content-center py-4">
        <Spinner animation="border" role="status" size="sm">
          <span className="visually-hidden">{t('library.loadingMore')}</span>
        </Spinner>
      </div>
    )
  }
  return null
}

const gridComponents: GridComponents<GridContext> = {
  List,
  Footer: GridFooter,
}

/** Selection wiring for a grid in selection mode. */
export interface PhotoGridSelection {
  /** When true, tiles become selection targets instead of detail links. */
  active: boolean
  /** The currently selected photo UIDs. */
  selected: Set<string>
  /** Toggles a photo's selection. */
  onToggle: (uid: string) => void
}

/** Props for {@link PhotoGrid}. */
export interface PhotoGridProps {
  photos: Photo[]
  loadingMore: boolean
  moreError: boolean
  onEndReached: () => void
  onRetry: () => void
  /** Optional selection mode; when omitted the grid is a plain link grid. */
  selection?: PhotoGridSelection
  /**
   * When true each tile shows a favorite heart overlay (a personal toggle). The
   * heart is suppressed while a tile is a selection target. Defaults false.
   */
  favoritable?: boolean
  /**
   * When true each tile shows a compact star/flag overlay and supports rating
   * hotkeys on the focused tile. Suppressed in selection mode. Defaults false.
   */
  ratable?: boolean
  /**
   * Query string appended to each tile's detail link so the detail page inherits
   * this list's order and scope (for prev/next and Back).
   */
  detailQuery?: string
  /**
   * Imperative handle to the underlying virtuoso grid, exposing `scrollToIndex`
   * so the timeline scrubber can jump to a photo index.
   */
  gridRef?: React.Ref<VirtuosoGridHandle>
  /**
   * Called with the visible item range each time it changes, letting the
   * scrubber highlight the month owning the first visible photo.
   */
  onRangeChanged?: (range: ListRange) => void
  /**
   * Index of the tile carrying the keyboard focus highlight, or -1 for none.
   * Drives the visible highlight for arrow/`hjkl` grid navigation.
   */
  focusedIndex?: number
}

/**
 * Virtualized, infinite-scroll grid of photo tiles. Only the visible rows are
 * mounted (react-virtuoso), and reaching the end requests the next page via
 * `onEndReached`. It scrolls with the window so the page behaves like a normal
 * document. The footer surfaces load-more progress and errors.
 */
export function PhotoGrid({
  photos,
  loadingMore,
  moreError,
  onEndReached,
  onRetry,
  selection,
  favoritable = false,
  ratable = false,
  detailQuery,
  gridRef,
  onRangeChanged,
  focusedIndex = -1,
}: PhotoGridProps) {
  return (
    <VirtuosoGrid
      ref={gridRef}
      useWindowScroll
      data={photos}
      context={{ loadingMore, moreError, onRetry }}
      endReached={onEndReached}
      rangeChanged={onRangeChanged}
      components={gridComponents}
      itemContent={(index, photo) => (
        <PhotoTile
          photo={photo}
          selectable={selection?.active ?? false}
          selected={selection?.selected.has(photo.uid) ?? false}
          onToggleSelect={selection?.onToggle}
          favoritable={favoritable}
          ratable={ratable}
          detailQuery={detailQuery}
          focused={index === focusedIndex}
        />
      )}
      computeItemKey={(_index, photo) => photo.uid}
      style={{ minHeight: '50vh' }}
    />
  )
}
