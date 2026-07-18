import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { type VirtuosoGridHandle } from 'react-virtuoso'

import { EmptyState } from '../components/EmptyState'
import { ErrorState } from '../components/ErrorState'
import { ExpandResults } from '../components/expand/ExpandResults'
import { ExpandSearchForm } from '../components/expand/ExpandSearchForm'
import { Icon } from '../components/Icon'
import { BulkEditControl } from '../components/organize/BulkEditControl'
import { type BulkEditOutcome, type BulkEditPrefill } from '../components/organize/BulkEditModal'
import { SelectionBar } from '../components/organize/SelectionBar'
import { SelectionStart } from '../components/organize/SelectionStart'
import { useBulkEdit } from '../hooks/useBulkEdit'
import { useGridKeyboardNavigation } from '../hooks/useGridKeyboardNavigation'
import {
  clampExpandLimit,
  clampExpandThresholdPercent,
  EXPAND_LIMIT_DEFAULT,
  EXPAND_THRESHOLD_DEFAULT_PERCENT,
  expandSources,
  expandThresholdDistance,
} from '../lib/expandSearch'
import { type ExpandKind, type ExpandResult, searchSimilar } from '../services/expand'
import { rejectLabel } from '../services/feedback'
import { fetchAlbums, fetchLabels } from '../services/organize'

/** The page's fetch state for the expansion search. */
type SearchState =
  | { status: 'idle' }
  | { status: 'loading' }
  | { status: 'error' }
  | { status: 'ready'; result: ExpandResult }

/** The pickable albums and labels, loaded once when the page mounts. */
type SourcesState =
  | { status: 'loading' }
  | { status: 'error' }
  | {
      status: 'ready'
      albums: { uid: string; name: string; photoCount: number }[]
      labels: { uid: string; name: string; photoCount: number }[]
    }

/** Reads the collection kind from a URL parameter, defaulting to album. */
function kindFromParam(value: string | null): ExpandKind {
  return value === 'label' ? 'label' : 'album'
}

/**
 * ExpandPage is the "grow a collection" workspace: pick an album or a label,
 * and the backend finds the photos that look like the ones already in it but
 * are not members yet. The results use the standard library grid and selection
 * model, and adding goes through the ordinary bulk edit — pre-filled with the
 * collection being expanded — so this page introduces no second write path.
 * Added photos leave the grid in place (no refetch, no scroll jump), and for
 * labels a per-tile ✗ persists a rejection so repeated passes converge instead
 * of re-offering the same wrong photos. It is editor-only. See
 * docs/FRONTEND.md.
 */
export function ExpandPage() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()

  // Config lives in local state, seeded from the URL so a reload or Back
  // restores the last search; it is written back only when a search runs.
  const [kind, setKind] = useState<ExpandKind>(() => kindFromParam(searchParams.get('type')))
  const [sourceUid, setSourceUid] = useState<string | null>(() => searchParams.get('source'))
  const [thresholdPercent, setThresholdPercent] = useState(() =>
    clampExpandThresholdPercent(
      Number(searchParams.get('threshold') ?? EXPAND_THRESHOLD_DEFAULT_PERCENT),
    ),
  )
  const [limit, setLimit] = useState(() =>
    clampExpandLimit(Number(searchParams.get('limit') ?? EXPAND_LIMIT_DEFAULT)),
  )

  // The pickable collections, fetched once; the picker derives from `kind`.
  const [sources, setSources] = useState<SourcesState>({ status: 'loading' })
  useEffect(() => {
    const controller = new AbortController()
    Promise.all([fetchAlbums(controller.signal), fetchLabels(controller.signal)])
      .then(([albums, labels]) => {
        setSources({
          status: 'ready',
          albums: albums.map((a) => ({ uid: a.uid, name: a.title, photoCount: a.photo_count })),
          labels: labels.map((l) => ({ uid: l.uid, name: l.name, photoCount: l.photo_count })),
        })
      })
      .catch((err: unknown) => {
        if (err instanceof DOMException && err.name === 'AbortError') {
          return
        }
        setSources({ status: 'error' })
      })
    return () => {
      controller.abort()
    }
  }, [])
  const pickerSources = useMemo(
    () =>
      sources.status === 'ready'
        ? expandSources(kind === 'album' ? sources.albums : sources.labels)
        : [],
    [sources, kind],
  )

  const [state, setState] = useState<SearchState>({ status: 'idle' })
  const abortRef = useRef<AbortController | null>(null)
  // Photos already added or rejected leave the grid without a refetch: the
  // displayed list is the response minus this set, so the scroll never jumps.
  const [removedUids, setRemovedUids] = useState<ReadonlySet<string>>(new Set())
  const [rejectError, setRejectError] = useState(false)

  const runSearch = useCallback((k: ExpandKind, uid: string, percent: number, cap: number) => {
    abortRef.current?.abort()
    const controller = new AbortController()
    abortRef.current = controller
    setState({ status: 'loading' })
    setRejectError(false)
    searchSimilar(
      k,
      uid,
      { threshold: expandThresholdDistance(percent), limit: cap },
      controller.signal,
    )
      .then((result) => {
        setRemovedUids(new Set())
        setState({ status: 'ready', result })
      })
      .catch((err: unknown) => {
        if (err instanceof DOMException && err.name === 'AbortError') {
          return
        }
        setState({ status: 'error' })
      })
  }, [])

  // Abort an in-flight search when leaving the page.
  useEffect(() => () => abortRef.current?.abort(), [])

  // Restore a search from the URL on first load, so Back and refresh work.
  const autoRan = useRef(false)
  useEffect(() => {
    if (autoRan.current) {
      return
    }
    autoRan.current = true
    const uid = searchParams.get('source')
    if (uid !== null && uid !== '') {
      runSearch(kindFromParam(searchParams.get('type')), uid, thresholdPercent, limit)
    }
  }, [searchParams, thresholdPercent, limit, runSearch])

  const handleSearch = useCallback(() => {
    if (sourceUid === null || sourceUid === '') {
      return
    }
    setSearchParams(
      (prev) => {
        const next = new URLSearchParams(prev)
        next.set('type', kind)
        next.set('source', sourceUid)
        next.set('threshold', String(thresholdPercent))
        next.set('limit', String(limit))
        return next
      },
      { replace: true },
    )
    runSearch(kind, sourceUid, thresholdPercent, limit)
  }, [kind, sourceUid, thresholdPercent, limit, runSearch, setSearchParams])

  const handleKindChange = useCallback((next: ExpandKind) => {
    setKind(next)
    // An album UID means nothing among labels (and vice versa): start unpicked.
    setSourceUid(null)
  }, [])

  // Everything below acts on the collection the *results* belong to — the
  // response's own kind and UID — never the picker's current value, so changing
  // the form after a search cannot misdirect an add or a rejection.
  const result = state.status === 'ready' ? state.result : null
  const candidates = useMemo(
    () => (result === null ? [] : result.candidates.filter((c) => !removedUids.has(c.photo.uid))),
    [result, removedUids],
  )

  // A successful bulk apply that added photos to the expanded collection makes
  // them members: they leave the grid (errored ones stay). Any other bulk edit
  // changes no membership here, so the grid keeps its results.
  const handleEdited = useCallback(
    (outcome?: BulkEditOutcome) => {
      if (outcome === undefined || result === null) {
        return
      }
      const added =
        result.kind === 'album' ? outcome.operations.add_to_albums : outcome.operations.add_labels
      if (added?.includes(result.collection_uid) !== true) {
        return
      }
      const done = outcome.result.results
        .filter((r) => r.status !== 'error')
        .map((r) => r.photo_uid)
      if (done.length > 0) {
        setRemovedUids((prev) => new Set([...prev, ...done]))
      }
    },
    [result],
  )
  const bulk = useBulkEdit({ onEdited: handleEdited })
  const selection = bulk.selection

  // The bulk dialog opens with the expanded collection pre-selected: adding the
  // picked photos to it is the action in 99 % of cases, and everything else the
  // dialog can do stays available.
  const prefill = useMemo<BulkEditPrefill>(() => {
    if (result === null) {
      return {}
    }
    return result.kind === 'album'
      ? { addAlbums: [result.collection_uid] }
      : { addLabels: [result.collection_uid] }
  }, [result])

  // ✗ persists a "not this label" rejection and drops the tile optimistically —
  // the endpoint is idempotent — restoring it (with an alert) if the write
  // fails. Albums have no rejection model, so the ✗ exists only for labels.
  const handleReject = useCallback(
    (photoUid: string) => {
      if (result?.kind !== 'label') {
        return
      }
      setRemovedUids((prev) => new Set(prev).add(photoUid))
      if (selection.selected.has(photoUid)) {
        selection.toggle(photoUid)
      }
      rejectLabel({ photo_uid: photoUid, label_uid: result.collection_uid }).catch(() => {
        setRejectError(true)
        setRemovedUids((prev) => {
          const next = new Set(prev)
          next.delete(photoUid)
          return next
        })
      })
    },
    [result, selection],
  )

  // Keyboard navigation identical to the library grid: arrows/hjkl move the
  // highlight, Enter opens the photo, x selects, Escape clears the selection.
  const gridRef = useRef<VirtuosoGridHandle>(null)
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
      const candidate = candidates.at(index)
      if (candidate !== undefined) {
        void navigate(`/photos/${candidate.photo.uid}`)
      }
    },
    [candidates, navigate],
  )
  const selectPhoto = useCallback(
    (index: number) => {
      const candidate = candidates.at(index)
      if (candidate === undefined || !bulk.canBulkEdit) {
        return
      }
      if (!selection.active) {
        selection.enable()
      }
      selection.toggle(candidate.photo.uid)
    },
    [candidates, bulk.canBulkEdit, selection],
  )
  const noopFavorite = useCallback(() => {
    // Tiles on this page carry match badges instead of the favorite heart.
  }, [])
  const searchKey =
    result === null ? '' : `${result.kind}:${result.collection_uid}:${String(result.threshold)}`
  const { focusedIndex } = useGridKeyboardNavigation({
    count: candidates.length,
    enabled: state.status === 'ready' && candidates.length > 0,
    resetKey: searchKey,
    getColumns,
    scrollToIndex: scrollFocusIntoView,
    onOpen: openPhoto,
    onToggleSelect: selectPhoto,
    onToggleFavorite: noopFavorite,
    hasSelection: selection.count > 0,
    onClearSelection: selection.clear,
  })

  // The backend names why a result is structurally empty; a grid emptied by the
  // user's own adds/rejections keeps its summary instead (the counts update).
  const emptyReason = result !== null && result.result_count === 0 ? result.reason : undefined

  return (
    <>
      <div className="d-flex justify-content-between align-items-center mb-1 flex-wrap gap-2">
        <h1 className="kk-page-title mb-0">{t('expand.title')}</h1>
        {!selection.active && candidates.length > 0 && <SelectionStart bulk={bulk} />}
      </div>
      <p className="text-secondary">{t('expand.subtitle')}</p>

      <ExpandSearchForm
        kind={kind}
        sources={pickerSources}
        sourcesLoading={sources.status === 'loading'}
        sourcesError={sources.status === 'error'}
        sourceUid={sourceUid}
        thresholdPercent={thresholdPercent}
        limit={limit}
        loading={state.status === 'loading'}
        onKindChange={handleKindChange}
        onSourceChange={setSourceUid}
        onThresholdChange={setThresholdPercent}
        onLimitChange={setLimit}
        onSearch={handleSearch}
      />

      {selection.active && (
        <SelectionBar count={selection.count} onCancel={selection.disable}>
          <Button
            variant="outline-secondary"
            size="sm"
            disabled={candidates.length === 0}
            onClick={() => {
              selection.selectMany(candidates.map((c) => c.photo.uid))
            }}
          >
            {t('library.selectAll')}
          </Button>
          <BulkEditControl bulk={bulk} prefill={prefill} />
        </SelectionBar>
      )}

      {rejectError && (
        <Alert
          variant="danger"
          dismissible
          onClose={() => {
            setRejectError(false)
          }}
        >
          {t('expand.error.reject')}
        </Alert>
      )}

      {state.status === 'idle' && (
        <EmptyState
          icon={<Icon name="search" />}
          title={t('expand.idle.title')}
          hint={t('expand.idle.hint')}
        />
      )}

      {state.status === 'loading' && (
        <div className="d-flex align-items-center gap-2 py-5 justify-content-center text-secondary">
          <Spinner animation="border" role="status" size="sm" />
          <span>{t('expand.loading')}</span>
        </div>
      )}

      {state.status === 'error' && (
        <ErrorState title={t('expand.error.search')} onRetry={handleSearch} />
      )}

      {/* A collection whose photos have no embeddings yet is the common
          confusing case: a generic "no results" would send the user hunting a
          bug that is not there, so it gets its own explanation. */}
      {emptyReason === 'no_source_embeddings' && (
        <EmptyState
          icon={<Icon name="images" />}
          title={t('expand.empty.noEmbeddings.title')}
          hint={t('expand.empty.noEmbeddings.hint')}
        />
      )}

      {emptyReason === 'empty_collection' && (
        <EmptyState
          icon={<Icon name="images" />}
          title={t('expand.empty.emptyCollection.title')}
          hint={t('expand.empty.emptyCollection.hint')}
        />
      )}

      {result !== null && result.result_count === 0 && emptyReason === undefined && (
        <EmptyState
          icon={<Icon name="search" />}
          title={t('expand.empty.noResults.title')}
          hint={t('expand.empty.noResults.hint')}
        />
      )}

      {result !== null && result.result_count > 0 && (
        <div ref={gridWrapRef}>
          <ExpandResults
            result={result}
            candidates={candidates}
            selection={bulk.gridSelection}
            onReject={result.kind === 'label' ? handleReject : undefined}
            gridRef={gridRef}
            focusedIndex={focusedIndex}
          />
        </div>
      )}
    </>
  )
}
