import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import { useTranslation } from 'react-i18next'
import { Link, useNavigate } from 'react-router-dom'
import { type ListRange, type VirtuosoGridHandle } from 'react-virtuoso'

import { useAuth } from '../auth/AuthContext'
import { EmptyState } from '../components/EmptyState'
import { FilterBar } from '../components/library/FilterBar'
import { GridSkeleton } from '../components/library/GridSkeleton'
import { PhotoGrid } from '../components/library/PhotoGrid'
import { TimelineScrubber } from '../components/library/TimelineScrubber'
import { BulkEditModal } from '../components/organize/BulkEditModal'
import { SelectionBar } from '../components/organize/SelectionBar'
import { SaveSearchModal } from '../components/savedsearch/SaveSearchModal'
import { useGridJump } from '../hooks/useGridJump'
import { useGridKeyboardNavigation } from '../hooks/useGridKeyboardNavigation'
import { usePhotoLibrary } from '../hooks/usePhotoLibrary'
import { useSelection } from '../hooks/useSelection'
import { detailQueryString } from '../lib/detailView'
import { LIBRARY_DEFAULTS, type LibraryView, viewToParams } from '../lib/libraryView'
import { slideshowHref } from '../lib/slideshowView'
import { useUrlState } from '../lib/urlState'
import { favoritePhoto } from '../services/photos'

/**
 * The main photo library: a filter/sort bar over a virtualized, infinite-scroll
 * thumbnail grid. The entire view (filters, sort) lives in the URL, so Back /
 * Forward restore the exact view and sharing the URL reproduces it. The grid
 * pages through the API as the user scrolls. Every tile carries a favorite heart
 * (a personal toggle for all roles); editors can additionally enter selection
 * mode to bulk-edit a multi-photo selection (albums, labels, description,
 * location, private, archive, favorite) via the bulk API.
 */
export function LibraryPage() {
  const { t } = useTranslation()
  const { canWrite } = useAuth()
  const navigate = useNavigate()
  const [view, setView] = useUrlState<LibraryView>(LIBRARY_DEFAULTS)
  const selection = useSelection()
  const [editing, setEditing] = useState(false)
  const [savingView, setSavingView] = useState(false)

  // Memoise the API params so the data hook only reloads when the query changes.
  const params = useMemo(() => viewToParams(view), [view])
  // The detail link carries this view so prev/next and Back respect the order.
  const detailQuery = useMemo(
    () => detailQueryString({ ...view, album: '', label: '', favorite: '' }),
    [view],
  )
  const { photos, total, status, loadingMore, moreError, hasMore, loadMore, retry } =
    usePhotoLibrary(params)

  // Optimistic per-photo favorite overrides for the `f` keyboard shortcut on the
  // focused tile: the flip is applied to the displayed photos immediately (each
  // tile's own useFavorite resyncs from the prop) and rolled back if the request
  // fails. Cleared when the view changes so overrides never outlive their list.
  const [favOverrides, setFavOverrides] = useState<ReadonlyMap<string, boolean>>(new Map())
  useEffect(() => {
    setFavOverrides(new Map())
  }, [detailQuery])
  const displayPhotos = useMemo(() => {
    if (favOverrides.size === 0) {
      return photos
    }
    return photos.map((p) =>
      favOverrides.has(p.uid) ? { ...p, is_favorite: favOverrides.get(p.uid) } : p,
    )
  }, [photos, favOverrides])

  // Timeline scrubber wiring: a ref to the grid to scroll it, the first visible
  // index to highlight the current month, and a jump that loads pages first when
  // the target month lies ahead of the infinite-scroll cursor. The scrubber is
  // only meaningful for the default newest-first date order (the timeline is
  // always date-grouped), so it is hidden for other sorts and in selection mode.
  const gridRef = useRef<VirtuosoGridHandle>(null)
  const [rangeStart, setRangeStart] = useState(0)
  const jumpTo = useGridJump({
    gridRef,
    loadedCount: photos.length,
    hasMore,
    loadingMore,
    loadMore,
  })
  const onRangeChanged = useCallback((range: ListRange) => {
    setRangeStart(range.startIndex)
  }, [])
  const showScrubber = view.sort === LIBRARY_DEFAULTS.sort && !selection.active

  // Keyboard navigation over the grid: a visible focus highlight moved by the
  // arrow keys / hjkl, with Enter/x/f/Escape acting on the focused tile. Row-wise
  // moves need the live column count, read from the rendered grid's computed
  // template so it tracks the responsive `auto-fill` layout.
  const gridWrapRef = useRef<HTMLDivElement>(null)
  const getColumns = useCallback(() => {
    const el = gridWrapRef.current?.querySelector<HTMLElement>('.kukatko-photo-grid')
    if (!el) {
      return 1
    }
    const tracks = getComputedStyle(el)
      .gridTemplateColumns.split(' ')
      .filter((track) => track.trim() !== '')
    return tracks.length > 0 ? tracks.length : 1
  }, [])
  const scrollFocusIntoView = useCallback((index: number) => {
    gridRef.current?.scrollToIndex(index)
  }, [])
  const openPhoto = useCallback(
    (index: number) => {
      const p = displayPhotos.at(index)
      if (!p) {
        return
      }
      void navigate(detailQuery === '' ? `/photos/${p.uid}` : `/photos/${p.uid}?${detailQuery}`)
    },
    [displayPhotos, detailQuery, navigate],
  )
  const selectPhoto = useCallback(
    (index: number) => {
      const p = displayPhotos.at(index)
      if (!p || !canWrite) {
        return
      }
      if (!selection.active) {
        selection.enable()
      }
      selection.toggle(p.uid)
    },
    [displayPhotos, canWrite, selection],
  )
  const toggleFavorite = useCallback(
    (index: number) => {
      const p = displayPhotos.at(index)
      if (!p) {
        return
      }
      const current = favOverrides.get(p.uid) ?? p.is_favorite ?? false
      const next = !current
      setFavOverrides((m) => new Map(m).set(p.uid, next))
      favoritePhoto(p.uid, next).catch(() => {
        setFavOverrides((m) => new Map(m).set(p.uid, current))
      })
    },
    [displayPhotos, favOverrides],
  )
  const { focusedIndex } = useGridKeyboardNavigation({
    count: displayPhotos.length,
    enabled: status === 'ready' && displayPhotos.length > 0,
    resetKey: detailQuery,
    getColumns,
    scrollToIndex: scrollFocusIntoView,
    onOpen: openPhoto,
    onToggleSelect: selectPhoto,
    onToggleFavorite: toggleFavorite,
    hasSelection: selection.count > 0,
    onClearSelection: selection.clear,
  })

  return (
    <>
      <div className="d-flex justify-content-between align-items-center mb-3 flex-wrap gap-2">
        <h1 className="kk-page-title mb-0">{t('library.title')}</h1>
        {!selection.active && (
          <div className="d-flex gap-1 flex-wrap">
            {status === 'ready' && photos.length > 0 && (
              <Link to={slideshowHref({}, view)} className="btn btn-outline-secondary btn-sm">
                {t('slideshow.start')}
              </Link>
            )}
            <Button
              variant="outline-secondary"
              size="sm"
              onClick={() => {
                setSavingView(true)
              }}
            >
              {t('savedSearches.saveView')}
            </Button>
            {canWrite && (
              <Button variant="outline-secondary" size="sm" onClick={selection.enable}>
                {t('library.select')}
              </Button>
            )}
          </div>
        )}
      </div>

      {selection.active && (
        <SelectionBar count={selection.count} onCancel={selection.disable}>
          <Button
            variant="outline-secondary"
            size="sm"
            disabled={photos.length === 0}
            onClick={() => {
              selection.selectMany(displayPhotos.map((p) => p.uid))
            }}
          >
            {t('library.selectAll')}
          </Button>
          <Button
            variant="primary"
            size="sm"
            disabled={selection.count === 0}
            onClick={() => {
              setEditing(true)
            }}
          >
            {t('library.bulkEdit')}
          </Button>
        </SelectionBar>
      )}

      <FilterBar view={view} onChange={setView} total={total} />

      {status === 'loading' && <GridSkeleton />}

      {status === 'error' && (
        <Alert variant="danger" className="d-flex align-items-center justify-content-between">
          <span>{t('library.error.load')}</span>
          <Button variant="outline-light" size="sm" onClick={retry}>
            {t('library.error.retry')}
          </Button>
        </Alert>
      )}

      {status === 'ready' && photos.length === 0 && (
        <EmptyState title={t('library.empty.title')} hint={t('library.empty.hint')} />
      )}

      {status === 'ready' && photos.length > 0 && (
        <>
          <div ref={gridWrapRef}>
            <PhotoGrid
              photos={displayPhotos}
              loadingMore={loadingMore}
              moreError={moreError}
              onEndReached={loadMore}
              onRetry={retry}
              selection={
                selection.active
                  ? { active: true, selected: selection.selected, onToggle: selection.toggle }
                  : undefined
              }
              favoritable={!selection.active}
              ratable={!selection.active}
              detailQuery={detailQuery}
              gridRef={gridRef}
              onRangeChanged={onRangeChanged}
              focusedIndex={focusedIndex}
            />
          </div>
          {showScrubber && (
            <TimelineScrubber params={params} activeIndex={rangeStart} onJump={jumpTo} />
          )}
        </>
      )}

      {canWrite && (
        <BulkEditModal
          show={editing}
          photoUids={[...selection.selected]}
          onHide={() => {
            setEditing(false)
          }}
          onDone={() => {
            setEditing(false)
            selection.disable()
          }}
        />
      )}

      <SaveSearchModal
        show={savingView}
        params={view}
        onHide={() => {
          setSavingView(false)
        }}
        onSaved={() => {
          setSavingView(false)
        }}
      />
    </>
  )
}
