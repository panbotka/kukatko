import { useCallback, useImperativeHandle, useMemo, useRef } from 'react'
import Button from 'react-bootstrap/Button'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'
import { type ListRange, type StateSnapshot, Virtuoso, type VirtuosoHandle } from 'react-virtuoso'

import { useElementWidth } from '../../hooks/useElementWidth'
import { useGridDensity } from '../../hooks/useGridDensity'
import { useLongPressSelect } from '../../hooks/useLongPressSelect'
import { type GridDensityScope, LIBRARY_GRID_SCOPE } from '../../lib/gridDensity'
import { revealAlign } from '../../lib/gridScroll'
import {
  DEFAULT_TILE_RATIO,
  type JustifiedRow,
  justifiedRows,
  rowHeightForColumns,
  rowOfTile,
  tileRatio,
} from '../../lib/justifiedLayout'
import { type Photo } from '../../services/photos'
import { Skeleton } from '../Skeleton'

import { PhotoTile } from './PhotoTile'

/** State the footer needs, threaded to the virtuoso components via `context`. */
interface GridContext {
  loadingMore: boolean
  moreError: boolean
  onRetry: () => void
}

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

/**
 * Stand-in for a photo the grid knows exists but has not loaded yet — a slot the
 * windowed library list leaves empty until the page covering it arrives. It fills
 * the box the row laid out for it, so the row it sits in has the right height
 * from the outset and nothing shifts when the photo lands.
 */
function TilePlaceholder() {
  return <Skeleton radius="var(--kk-radius-tile)" style={{ width: '100%', height: '100%' }} />
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
  /**
   * Adds several photos to the selection at once, leaving anything already
   * picked alone. It is what the **touch long-press drag** paints: press and
   * hold a tile, keep the finger down, and every tile it crosses joins the
   * selection. Omitting it leaves the grid without that gesture — the checkmark
   * and the taps still select — so a caller that has no additive selection to
   * offer simply does not wire it.
   */
  onSelectMany?: (uids: string[]) => void
}

/**
 * Where a caller may ask the grid to scroll — the same shape virtuoso takes,
 * narrowed to what the pages use, and always in **photo** indices: the grid
 * itself knows which row a photo ended up in.
 */
export type PhotoGridScrollTarget =
  | number
  | { index: number; align?: 'start' | 'center' | 'end'; behavior?: 'auto' | 'smooth' }

/** Imperative handle a page holds on the grid (`PhotoGridProps.gridRef`). */
export interface PhotoGridHandle {
  /**
   * Scrolls the photo at this absolute index into view. A bare index *reveals*
   * it — the wall does not move while it is already on screen — which is what
   * keyboard focus wants; an explicit `align` positions the row, which is what a
   * timeline jump wants.
   */
  scrollToIndex: (target: PhotoGridScrollTarget) => void
}

/** Props for {@link PhotoGrid}. */
export interface PhotoGridProps {
  /**
   * The photos to render, in order. A slot may be `undefined`: the windowed
   * library list sizes this array to the *whole* result and fills only the pages
   * it has loaded, so an index here is a photo's absolute position and a hole is
   * a photo whose page is still on its way. Holes render as
   * {@link TilePlaceholder} in a default-shaped box; a plain `Photo[]` (every
   * other grid) has none.
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
   * Imperative handle to the grid, exposing `scrollToIndex` (in photo indices)
   * so the timeline scrubber and the keyboard navigation can jump to a photo.
   */
  gridRef?: React.Ref<PhotoGridHandle>
  /**
   * Called with the visible **photo** range each time it changes, letting the
   * scrubber highlight the month owning the first visible photo and the windowed
   * list fetch what came into view. The grid translates its own row range back
   * into photo indices, so a caller never sees the layout.
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
  /**
   * A position this grid was left at, restored as it mounts: the offset plus the
   * measurements needed to lay the rows out at it before anything is on screen,
   * so returning from a photo lands on the tile it was opened from instead of at
   * the top. Read once, when the grid mounts — see `useGridScrollMemory`.
   */
  restoreStateFrom?: StateSnapshot
  /**
   * Reports the grid's position (and the measurements that give it meaning)
   * whenever it changes, so the page can remember where the reader was.
   */
  onStateChanged?: (state: StateSnapshot) => void
  /**
   * Which stored column count (and gutter) this grid follows. Defaults to the
   * photo library's — pass `REVIEW_GRID_SCOPE` where the grid is a review
   * workspace rather than a browsing wall, so it moves with the other review
   * tools instead of re-densifying the library on the way back.
   */
  scope?: GridDensityScope
}

/**
 * Virtualized justified wall of photo tiles.
 *
 * Photos keep their own proportions: the tiles are laid into rows that share one
 * height and fill the width edge to edge (`lib/justifiedLayout`), so a panorama
 * is wide, a portrait is tall and nothing is cropped to a square on the way in.
 * The density control still says how many photos go across — it is read as "that
 * many *landscape* photos", which is what a row of mixed shapes then works out
 * to. The layout needs a real width, so the grid measures its own box and lays
 * itself out again whenever that moves.
 *
 * Virtualization is by **row**: only the visible rows are mounted
 * (react-virtuoso) and it scrolls with the window, so the page behaves like a
 * normal document. Rows have different heights, which is exactly what the list
 * virtualizer is for. Everything a page passes and receives is still in *photo*
 * indices — the range it watches, the index it scrolls to — because which row a
 * photo landed in is the grid's business and nobody else's.
 *
 * It serves both loading shapes. A grid that grows by appending pages requests
 * the next one via `onEndReached`. A *windowed* grid (the library) instead hands
 * over an array as long as the whole result with holes where pages are not
 * loaded, watches `onRangeChanged` to fetch what came into view, and leaves
 * `onEndReached` off — that is what lets it jump straight to any position
 * instead of paging its way there.
 *
 * Where the reader is in it is not the grid's business to remember, but it is the
 * grid's to report and to restore: `onStateChanged` hands the page a position to
 * keep and `restoreStateFrom` puts the grid back at one, which is what makes Back
 * out of a photo land on the tile it was opened from.
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
  restoreStateFrom,
  onStateChanged,
  scope = LIBRARY_GRID_SCOPE,
}: PhotoGridProps) {
  const { density } = useGridDensity(scope)
  const wrapRef = useRef<HTMLDivElement>(null)
  const width = useElementWidth(wrapRef)
  const gap = scope.gapPx

  // The layout, in three steps so each is memoized on what actually moves it:
  // the photos' shapes (a new page), the target height (density or width) and
  // the rows themselves.
  const ratios = useMemo(
    () =>
      photos.map((photo) =>
        photo === undefined
          ? DEFAULT_TILE_RATIO
          : tileRatio(photo.file_width, photo.file_height, photo.file_orientation ?? 0),
      ),
    [photos],
  )
  const targetHeight = rowHeightForColumns(width, density, gap)
  const rows = useMemo(
    () => justifiedRows(ratios, { containerWidth: width, targetRowHeight: targetHeight, gap }),
    [ratios, width, targetHeight, gap],
  )
  // The current layout, for the imperative calls (which run outside render) and
  // for translating virtuoso's row range back into photo indices.
  const rowsRef = useRef(rows)
  rowsRef.current = rows

  const virtuosoRef = useRef<VirtuosoHandle>(null)
  // The rows virtuoso last reported as being on screen, so a reveal-only scroll
  // can tell "already visible" from "off screen" without measuring anything.
  const visibleRowsRef = useRef<ListRange | null>(null)
  useImperativeHandle(
    gridRef,
    () => ({
      scrollToIndex: (target: PhotoGridScrollTarget) => {
        const index = typeof target === 'number' ? target : target.index
        const row = rowOfTile(rowsRef.current, index)
        if (row < 0) {
          return
        }
        if (typeof target !== 'number') {
          virtuosoRef.current?.scrollToIndex({ ...target, index: row })
          return
        }
        // A bare index is the keyboard focus asking to be revealed: stepping to
        // the tile next door must not yank the wall, only a move off screen may
        // scroll — and then by the shortest hop, top or bottom.
        const align = revealAlign(row, visibleRowsRef.current)
        if (align === null) {
          return
        }
        virtuosoRef.current?.scrollToIndex({ index: row, align })
      },
    }),
    [],
  )

  // Virtuoso's list has no `stateChanged` prop (only the grid had one), so the
  // position is read off the handle: whenever the visible rows change and once
  // more when the scroll settles. The page debounces what it does with it.
  const captureState = useCallback(() => {
    if (onStateChanged === undefined) {
      return
    }
    virtuosoRef.current?.getState(onStateChanged)
  }, [onStateChanged])

  const handleRangeChanged = useCallback(
    (range: ListRange) => {
      visibleRowsRef.current = range
      captureState()
      if (onRangeChanged === undefined) {
        return
      }
      // `.at` rather than an index: a range virtuoso reported against a layout
      // that has since been redone can point past the end, and a row that is not
      // there is a range not worth reporting.
      const first = rowsRef.current.at(range.startIndex)
      const last = rowsRef.current.at(range.endIndex)
      if (first === undefined || last === undefined) {
        return
      }
      onRangeChanged({
        startIndex: first.start,
        endIndex: last.start + last.tiles.length - 1,
      })
    },
    [captureState, onRangeChanged],
  )

  const handleIsScrolling = useCallback(
    (scrolling: boolean) => {
      if (!scrolling) {
        captureState()
      }
    },
    [captureState],
  )

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

  // Press and hold a tile on a touch screen, then drag: every tile the finger
  // crosses joins the selection (the count in the bulk bar climbs as it goes,
  // since each addition is an ordinary selection update). The gesture is offered
  // wherever the grid is selectable at all and the caller has an additive
  // handler to take it; on a mouse it is inert by construction.
  const selectMany = selection?.onSelectMany
  const addToSelection = useCallback(
    (uids: string[]) => {
      selectMany?.(uids)
    },
    [selectMany],
  )
  const { dragging } = useLongPressSelect({
    target: wrapRef,
    enabled: selectable && selectMany !== undefined,
    onSelect: addToSelection,
  })

  const renderRow = (row: JustifiedRow) => (
    // The gutter below the row is padding, not a margin: virtuoso sizes an item
    // from its box, and a margin it cannot see would let the rows overlap.
    <div
      className="kukatko-photo-row d-flex"
      style={{
        gap: `${String(gap)}px`,
        height: `${String(row.height + gap)}px`,
        paddingBottom: `${String(gap)}px`,
        boxSizing: 'border-box',
      }}
    >
      {row.tiles.map((tile) => {
        const photo = photos[tile.index]
        return (
          <div
            key={photo?.uid ?? `slot-${String(tile.index)}`}
            style={{ width: `${String(tile.width)}px`, flex: '0 0 auto' }}
          >
            {photo === undefined ? (
              <TilePlaceholder />
            ) : (
              <PhotoTile
                photo={photo}
                fill
                tileWidth={tile.width}
                selectable={selectable}
                selectFirst={selectFirst}
                selected={selection?.selected.has(photo.uid) ?? false}
                anySelected={anySelected}
                onToggleSelect={selection === undefined ? undefined : toggleSelect}
                favoritable={favoritable}
                onFavoriteChange={onFavoriteChange}
                detailQuery={detailQuery}
                focused={tile.index === focusedIndex}
                extras={tileExtras?.(photo)}
              />
            )}
          </div>
        )
      })}
    </div>
  )

  return (
    // The measured box *and* the class the wall's styling hangs off. It stays
    // outside virtuoso so it is a plain block element of the page's own width —
    // measuring virtuoso's own scroller would measure something it is itself
    // sizing.
    <div
      ref={wrapRef}
      className="kukatko-photo-grid"
      data-density={String(density)}
      // While a long-press drag is painting a selection the wall must not scroll
      // or start selecting text under the finger. `preventDefault` on the move
      // already stops the scroll for this gesture; the attribute is what stops
      // the next one from starting mid-drag (and marks the state for tests).
      data-dragging={dragging ? 'true' : undefined}
    >
      <Virtuoso
        ref={virtuosoRef}
        useWindowScroll
        data={rows}
        context={{ loadingMore, moreError, onRetry }}
        endReached={onEndReached}
        rangeChanged={handleRangeChanged}
        isScrolling={handleIsScrolling}
        restoreStateFrom={restoreStateFrom}
        components={{ Footer: GridFooter }}
        itemContent={(_index, row) => renderRow(row)}
        // A row keys on the photo that opens it, so re-laying the wall out (a
        // resize, a density step) reuses the rows whose first photo did not move.
        computeItemKey={(index, row) => `row-${String(row.start)}-${String(index)}`}
        style={{ minHeight: '50vh' }}
      />
    </div>
  )
}
