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

import { useGridDensity } from '../../hooks/useGridDensity'
import { GRID_GAP_PX, gridTemplateColumns } from '../../lib/gridDensity'
import { type Photo } from '../../services/photos'
import { Skeleton } from '../Skeleton'

import { PhotoTile } from './PhotoTile'

/** State the footer needs, threaded to the virtuoso components via `context`. */
interface GridContext {
  loadingMore: boolean
  moreError: boolean
  onRetry: () => void
}

/**
 * CSS-grid list honouring the user's density preference: it renders exactly the
 * chosen number of columns (`repeat(n, 1fr)`) on every viewport — the user picks
 * a concrete count in 1..GRID_COLUMNS_MAX, so there is no width-driven "auto"
 * fallback to defer to.
 *
 * Changing the density only restyles this element — virtuoso re-measures the
 * resized tiles and keeps the scroll position, and the page keeps its selection.
 */
const List = forwardRef<
  HTMLDivElement,
  { style?: React.CSSProperties; className?: string; children?: React.ReactNode }
>(function List({ style, className, children, ...props }, ref) {
  const { density } = useGridDensity()
  return (
    <div
      ref={ref}
      {...props}
      // The class lets the page measure the live column count (for row-wise
      // keyboard navigation) from the rendered grid's computed `grid-template`,
      // which now always resolves to exactly the chosen number of columns.
      className={`kukatko-photo-grid${className ? ` ${className}` : ''}`}
      data-density={String(density)}
      style={{
        display: 'grid',
        gridTemplateColumns: gridTemplateColumns(density),
        gap: `${GRID_GAP_PX}px`,
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

/**
 * Stand-in for a photo the grid knows exists but has not loaded yet — a slot the
 * windowed library list leaves empty until the page covering it arrives. It is
 * exactly a tile's shape (a square of the same radius), so the row it sits in has
 * the right height from the outset and nothing shifts when the photo lands.
 */
function TilePlaceholder() {
  return <Skeleton radius="var(--kk-radius-tile)" style={{ aspectRatio: '1 / 1' }} />
}

/** Selection wiring for a grid that offers multi-select. */
export interface PhotoGridSelection {
  /**
   * Explicit selection mode: every tile is a selection target from the outset
   * (the "Select" flow used by the album/label/search grids). When false the
   * grid may still be selectable on hover — see {@link PhotoGridSelection.hoverSelect}.
   */
  active: boolean
  /**
   * Hover-select mode: tiles stay links but reveal a corner checkmark on hover,
   * and the moment anything is selected the grid behaves selection-first (tiles
   * toggle, checkmarks stay shown). The library uses this so multi-select needs
   * no explicit "enter selection mode" step.
   */
  hoverSelect?: boolean
  /** The currently selected photo UIDs. */
  selected: Set<string>
  /** Toggles a photo's selection. */
  onToggle: (uid: string) => void
  /**
   * Selects the contiguous range between the selection anchor and `uid`
   * (Shift+click), given the grid's current photo order. When omitted a
   * Shift+click behaves as a plain toggle.
   */
  onToggleRange?: (uid: string, orderedUids: string[]) => void
}

/** Props for {@link PhotoGrid}. */
export interface PhotoGridProps {
  /**
   * The photos to render, in order. A slot may be `undefined`: the windowed
   * library list sizes this array to the *whole* result and fills only the pages
   * it has loaded, so an index here is a photo's absolute position and a hole is
   * a photo whose page is still on its way. Holes render as
   * {@link TilePlaceholder}; a plain `Photo[]` (every other grid) has none.
   */
  photos: readonly (Photo | undefined)[]
  loadingMore: boolean
  moreError: boolean
  /**
   * Called when the reader reaches the end of the loaded list. Omit it for a
   * windowed list, which loads from the visible range
   * ({@link PhotoGridProps.onRangeChanged}) rather than from its end.
   */
  onEndReached?: () => void
  onRetry: () => void
  /** Optional selection mode; when omitted the grid is a plain link grid. */
  selection?: PhotoGridSelection
  /**
   * When true each tile shows a favorite heart overlay (a personal toggle). The
   * heart is suppressed while a tile is a selection target. Defaults false.
   */
  favoritable?: boolean
  /**
   * Called with a photo's new favorite state whenever its heart flips, so a page
   * that also favorites from elsewhere (the library's `f` shortcut) keeps one
   * baseline per photo rather than two that drift apart.
   */
  onFavoriteChange?: (uid: string, favorite: boolean) => void
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
  /**
   * Per-photo overlays stamped onto each tile (see `PhotoTileProps.extras`) —
   * e.g. the /expand page's similarity badge and reject button. Called for every
   * rendered tile; return null for photos that need none.
   */
  tileExtras?: (photo: Photo) => React.ReactNode
}

/**
 * Virtualized grid of photo tiles. Only the visible rows are mounted
 * (react-virtuoso) and it scrolls with the window, so the page behaves like a
 * normal document. The footer surfaces load-more progress and errors.
 *
 * It serves both loading shapes. A grid that grows by appending pages requests
 * the next one via `onEndReached`. A *windowed* grid (the library) instead hands
 * over an array as long as the whole result with holes where pages are not
 * loaded, watches `onRangeChanged` to fetch what came into view, and leaves
 * `onEndReached` off — that is what lets it jump straight to any position
 * instead of paging its way there.
 */
export function PhotoGrid({
  photos,
  loadingMore,
  moreError,
  onEndReached,
  onRetry,
  selection,
  favoritable = false,
  onFavoriteChange,
  detailQuery,
  gridRef,
  onRangeChanged,
  focusedIndex = -1,
  tileExtras,
}: PhotoGridProps) {
  // Shift+click selects the contiguous range between the anchor and the clicked
  // tile; the grid supplies its own photo order so pages need no extra wiring.
  // Only loaded photos can take part — a range cannot select a tile whose uid
  // nobody knows yet.
  const toggleSelect = (uid: string, shiftKey?: boolean) => {
    if (shiftKey === true && selection?.onToggleRange !== undefined) {
      selection.onToggleRange(
        uid,
        photos.filter((p) => p !== undefined).map((p) => p.uid),
      )
      return
    }
    selection?.onToggle(uid)
  }
  // Whether the grid offers selection at all (a corner checkmark), and whether a
  // tile-body click toggles rather than navigates. In hover-select mode the tile
  // stays a link until the first pick, then turns selection-first so a run can be
  // gathered quickly; in explicit mode it is selection-first throughout.
  const anySelected = selection !== undefined && selection.selected.size > 0
  const selectable = selection !== undefined && (selection.active || selection.hoverSelect === true)
  const selectFirst =
    selection !== undefined && (selection.active || (selection.hoverSelect === true && anySelected))
  return (
    <VirtuosoGrid
      ref={gridRef}
      useWindowScroll
      data={photos}
      context={{ loadingMore, moreError, onRetry }}
      endReached={onEndReached}
      rangeChanged={onRangeChanged}
      components={gridComponents}
      itemContent={(index, photo) =>
        photo === undefined ? (
          <TilePlaceholder />
        ) : (
          <PhotoTile
            photo={photo}
            selectable={selectable}
            selectFirst={selectFirst}
            selected={selection?.selected.has(photo.uid) ?? false}
            anySelected={anySelected}
            onToggleSelect={selection === undefined ? undefined : toggleSelect}
            favoritable={favoritable}
            onFavoriteChange={onFavoriteChange}
            detailQuery={detailQuery}
            focused={index === focusedIndex}
            extras={tileExtras?.(photo)}
          />
        )
      }
      // A not-yet-loaded slot keys on its index, so the placeholder is replaced
      // (not reordered) the moment its photo arrives.
      computeItemKey={(index, photo) => photo?.uid ?? `slot-${String(index)}`}
      style={{ minHeight: '50vh' }}
    />
  )
}
