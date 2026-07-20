import { useCallback, useEffect, useRef, useState } from 'react'
import Button from 'react-bootstrap/Button'
import Dropdown from 'react-bootstrap/Dropdown'
import Modal from 'react-bootstrap/Modal'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'

import { type UseBulkEditResult } from '../../hooks/useBulkEdit'
import { useIsNarrowViewport } from '../../hooks/useIsNarrowViewport'
import { ApiError } from '../../services/auth'
import { type BulkOperations, bulkUpdatePhotos } from '../../services/bulk'
import { type AlbumCount, fetchAlbums, fetchLabels, type LabelCount } from '../../services/organize'
import { Icon, type IconName } from '../Icon'
import { MultiSelect, type MultiSelectOption } from '../MultiSelect'
import { useToast } from '../toast/ToastContext'

import { BulkEditModal } from './BulkEditModal'
import { DownloadZipButton } from './DownloadZipButton'
import { StackSelectedControl } from './StackSelectedControl'

/** Which lightweight picker (if any) is open over the bar. */
type Picker = 'album' | 'label' | null

/** The lazily-loaded album/label options shared by both pickers. */
type OptionsState =
  | { status: 'idle' }
  | { status: 'loading' }
  | { status: 'ready'; albums: MultiSelectOption[]; labels: MultiSelectOption[] }
  | { status: 'error' }

/** Maps an album to a picker option (value = uid, shown by title + count). */
function albumOption(album: AlbumCount): MultiSelectOption {
  return { value: album.uid, label: album.title, count: album.photo_count }
}

/** Maps a label to a picker option (value = uid, shown by name + count). */
function labelOption(label: LabelCount): MultiSelectOption {
  return { value: label.uid, label: label.name, count: label.photo_count }
}

/**
 * A page-specific action merged into the shared bar — "Nastavit obálku" on an
 * album, say. It is described rather than passed as a node so every page's extra
 * looks and behaves like the built-in actions, instead of each page restyling a
 * button of its own.
 */
export interface BatchExtraAction {
  /** Stable identity of the action within its page's list (the React key). */
  id: string
  /** The glyph shown before the label. */
  icon: IconName
  /** The translated, visible label — also the button's title. */
  label: string
  /** Runs the action on the current selection. */
  onClick: () => void
  /** Greys the action out, e.g. one that needs exactly one photo picked. */
  disabled?: boolean
  /** Renders it as destructive (e.g. removing photos from the album). */
  danger?: boolean
}

/** Props for {@link BatchActionBar}. */
export interface BatchActionBarProps {
  /** The bulk-edit state from `useBulkEdit` (hover-select), owned by the page. */
  bulk: UseBulkEditResult
  /** Selects every loaded tile in view; omit to hide the select-all control. */
  onSelectAll?: () => void
  /**
   * Actions only this page can offer, appended after the shared ones so the
   * common vocabulary keeps the same order everywhere. Omit on a page that has
   * none (the library, favorites, search).
   */
  extraActions?: readonly BatchExtraAction[]
}

/**
 * A labelled icon action button styled for the frosted bar. On a phone the bar
 * collapses to save width: `iconOnly` drops the visible label (the glyph carries
 * the meaning) but keeps it reachable to assistive tech via `aria-label`, and the
 * `title` tooltip stays in both modes.
 */
function BarAction({
  icon,
  label,
  onClick,
  disabled,
  danger,
  iconOnly = false,
}: {
  icon: IconName
  label: string
  onClick: () => void
  disabled?: boolean
  danger?: boolean
  iconOnly?: boolean
}) {
  return (
    <Button
      variant={danger === true ? 'outline-danger' : 'outline-light'}
      size="sm"
      onClick={onClick}
      disabled={disabled}
      title={label}
      aria-label={iconOnly ? label : undefined}
    >
      <Icon name={icon} className={iconOnly ? undefined : 'me-1'} />
      {!iconOnly && <span>{label}</span>}
    </Button>
  )
}

/**
 * The floating batch action bar: a frosted command bar that rises from the
 * bottom edge whenever a library selection exists, showing the live count and
 * the batch actions — add to album, add/remove labels, favorite, archive,
 * download, plus stacking and the full editor. Each metadata action applies the
 * whole batch in a single `POST /photos/bulk` request; a success or failure is
 * surfaced as a toast. A successful apply clears the selection and reloads the
 * grid; a failed one leaves the selection intact so it can be retried. Escape
 * (handled by the grid's keyboard navigation) clears the selection and hides the
 * bar.
 *
 * The bar renders only while something is selected — the page mounts it under
 * that condition — so it never appears empty.
 *
 * Every photo list shows this same bar, so the batch vocabulary does not change
 * from page to page; a page that owns actions of its own (an album's set-cover /
 * remove-from-album) hands them over as `extraActions` and they join the bar
 * instead of forcing a second toolbar next to it.
 */
export function BatchActionBar({ bulk, onSelectAll, extraActions }: BatchActionBarProps) {
  const { t } = useTranslation()
  const { show } = useToast()
  const [busy, setBusy] = useState(false)
  const [picker, setPicker] = useState<Picker>(null)
  const [options, setOptions] = useState<OptionsState>({ status: 'idle' })
  // True once the option lists have loaded successfully; kept in a ref so the
  // effect below can reuse the cache without depending on `options` (see the
  // effect comment). A retry via `reloadOptions` re-runs the fetch.
  const optionsLoaded = useRef(false)
  const [reloadOptions, setReloadOptions] = useState(0)
  const [addAlbums, setAddAlbums] = useState<string[]>([])
  const [addLabels, setAddLabels] = useState<string[]>([])
  const [removeLabels, setRemoveLabels] = useState<string[]>([])
  // On a phone the ~10 labelled actions cannot share one row, so the bar folds
  // the secondary ones into a "…" overflow menu and shows the rest icon-only.
  const narrow = useIsNarrowViewport()

  // Load albums and labels the first time a picker opens and cache them for the
  // session. Keyed on `picker` (and the retry counter) — deliberately NOT on
  // `options.status` — so writing the `loading`/`ready` result never re-runs
  // this effect and aborts its own in-flight fetch. Reading the "already
  // loaded" guard from `optionsLoaded.current` keeps that state out of the deps
  // too, mirroring OrganizePanel / BulkEditModal. The cleanup still aborts the
  // fetch on a genuine unmount or picker close.
  useEffect(() => {
    if (picker === null || optionsLoaded.current) {
      return
    }
    const controller = new AbortController()
    setOptions({ status: 'loading' })
    Promise.all([fetchAlbums(controller.signal), fetchLabels(controller.signal)])
      .then(([albums, labels]) => {
        optionsLoaded.current = true
        setOptions({
          status: 'ready',
          albums: albums.map(albumOption),
          labels: labels.map(labelOption),
        })
      })
      .catch((err: unknown) => {
        if (err instanceof DOMException && err.name === 'AbortError') {
          return
        }
        setOptions({ status: 'error' })
      })
    return () => {
      controller.abort()
    }
  }, [picker, reloadOptions])

  // Publish the bar's live rendered height so a photo grid can reserve exactly
  // that much bottom clearance (the CSS `--kk-batch-clearance` var adds the dock's
  // own offset on top) and its last row always scrolls clear of the floating bar —
  // however the bar wraps or collapses. A hard-coded constant under-reserved on
  // phones, where the bar is taller, and hid the bottom photos behind it.
  const barRef = useRef<HTMLDivElement>(null)
  useEffect(() => {
    const bar = barRef.current
    if (bar === null) {
      return
    }
    const root = document.documentElement
    const publish = (): void => {
      root.style.setProperty('--kk-batch-bar-height', `${bar.getBoundingClientRect().height}px`)
    }
    publish()
    // ResizeObserver is absent in jsdom; the one-off publish above still runs there.
    const observer =
      typeof ResizeObserver === 'function'
        ? new ResizeObserver(() => {
            publish()
          })
        : null
    observer?.observe(bar)
    return () => {
      observer?.disconnect()
      root.style.removeProperty('--kk-batch-bar-height')
    }
  }, [])

  // Retry after a load error: `optionsLoaded` is still false (only a success
  // sets it), so bumping the counter re-runs the effect and fetches again.
  const reloadPickerOptions = useCallback(() => {
    setReloadOptions((n) => n + 1)
  }, [])

  const resetPickerFields = useCallback(() => {
    setAddAlbums([])
    setAddLabels([])
    setRemoveLabels([])
  }, [])

  const closePicker = useCallback(() => {
    setPicker(null)
    resetPickerFields()
  }, [resetPickerFields])

  // Applies one operation set to the whole selection in a single request. On
  // success it reports the count, clears the selection and reloads the grid; on
  // failure it surfaces the server's reason and leaves the selection untouched.
  const applyOps = useCallback(
    async (ops: BulkOperations) => {
      setBusy(true)
      try {
        const result = await bulkUpdatePhotos(bulk.photoUids, ops)
        show({ variant: 'success', message: t('batch.applied', { count: result.counts.updated }) })
        setPicker(null)
        resetPickerFields()
        bulk.finish()
      } catch (err) {
        const message =
          err instanceof ApiError && err.message.trim() !== '' ? err.message : t('batch.error')
        show({ variant: 'danger', message })
      } finally {
        setBusy(false)
      }
    },
    [bulk, resetPickerFields, show, t],
  )

  const applyPicker = useCallback(() => {
    if (picker === 'album') {
      void applyOps({ add_to_albums: addAlbums })
      return
    }
    if (picker === 'label') {
      const ops: BulkOperations = {}
      if (addLabels.length > 0) {
        ops.add_labels = addLabels
      }
      if (removeLabels.length > 0) {
        ops.remove_labels = removeLabels
      }
      void applyOps(ops)
    }
  }, [picker, applyOps, addAlbums, addLabels, removeLabels])

  const pickerHasChanges =
    picker === 'album'
      ? addAlbums.length > 0
      : picker === 'label'
        ? addLabels.length > 0 || removeLabels.length > 0
        : false

  // The bar's controls, built once and then placed either all inline (desktop) or
  // split into inline primaries + an overflow menu (phone). Clear and the count
  // stay pinned in the wrapper below; these are the actions that move.
  const selectAllControl =
    onSelectAll !== undefined ? (
      <Button variant="outline-light" size="sm" onClick={onSelectAll} disabled={busy}>
        <Icon name="ui-checks" className="me-1" />
        <span>{t('batch.selectAll')}</span>
      </Button>
    ) : null
  const albumAction = (
    <BarAction
      icon="collection"
      label={t('batch.album')}
      iconOnly={narrow}
      onClick={() => {
        setPicker('album')
      }}
      disabled={busy}
    />
  )
  const labelAction = (
    <BarAction
      icon="tags"
      label={t('batch.label')}
      iconOnly={narrow}
      onClick={() => {
        setPicker('label')
      }}
      disabled={busy}
    />
  )
  const favoriteAction = (
    <BarAction
      icon="heart"
      label={t('batch.favorite')}
      onClick={() => {
        void applyOps({ set_favorite: true })
      }}
      disabled={busy}
    />
  )
  const archiveAction = (
    <BarAction
      icon="archive"
      label={t('batch.archive')}
      danger
      onClick={() => {
        void applyOps({ archive: true })
      }}
      disabled={busy}
    />
  )
  const downloadControl = <DownloadZipButton photoUids={bulk.photoUids} variant="outline-light" />
  const stackControl = <StackSelectedControl bulk={bulk} variant="outline-light" />
  const moreAction = (
    <BarAction icon="sliders" label={t('batch.more')} onClick={bulk.open} disabled={busy} />
  )
  const extras = extraActions?.map((action) => (
    <BarAction
      key={action.id}
      icon={action.icon}
      label={action.label}
      onClick={action.onClick}
      disabled={busy || action.disabled === true}
      danger={action.danger}
    />
  ))

  return (
    <div className="kk-batch-dock">
      <div className="kk-batch-bar" ref={barRef} role="toolbar" aria-label={t('batch.bar')}>
        <Button
          variant="outline-light"
          size="sm"
          onClick={bulk.selection.clear}
          aria-label={t('batch.clear')}
          title={t('batch.clear')}
        >
          <Icon name="x-lg" />
        </Button>
        <span className="fw-semibold kk-batch-count me-auto" aria-live="polite">
          {t('selection.count', { count: bulk.selection.count })}
        </span>
        {narrow ? (
          <>
            {albumAction}
            {labelAction}
            <Dropdown drop="up" align="end" className="kk-batch-overflow">
              <Dropdown.Toggle
                variant="outline-light"
                size="sm"
                id="batch-overflow"
                className="kk-batch-overflow-toggle"
                aria-label={t('batch.overflow')}
                title={t('batch.overflow')}
                disabled={busy}
              >
                <Icon name="three-dots" />
              </Dropdown.Toggle>
              <Dropdown.Menu className="kk-batch-overflow-menu">
                <div className="d-grid gap-1">
                  {selectAllControl}
                  {favoriteAction}
                  {archiveAction}
                  {downloadControl}
                  {stackControl}
                  {moreAction}
                  {extras}
                </div>
              </Dropdown.Menu>
            </Dropdown>
          </>
        ) : (
          <>
            {selectAllControl}
            {albumAction}
            {labelAction}
            {favoriteAction}
            {archiveAction}
            {downloadControl}
            {stackControl}
            {moreAction}
            {extras}
          </>
        )}
      </div>

      {/* No `scrollable`: the picker is short, and its `overflow: auto` body clipped
          the MultiSelect suggestion overlay. `fullscreen="sm-down"` gives a phone the
          whole screen so the field and its (in-flow) suggestions clear the keyboard. */}
      <Modal show={picker !== null} onHide={closePicker} centered fullscreen="sm-down">
        <Modal.Header closeButton>
          <Modal.Title>{picker === 'label' ? t('batch.label') : t('batch.album')}</Modal.Title>
        </Modal.Header>
        <Modal.Body>
          {options.status === 'loading' && (
            <div className="d-flex justify-content-center py-3">
              <Spinner animation="border" role="status" size="sm">
                <span className="visually-hidden">{t('bulkEdit.loading')}</span>
              </Spinner>
            </div>
          )}
          {options.status === 'error' && (
            <div className="d-flex align-items-center justify-content-between gap-2">
              <span className="text-danger">{t('batch.optionsError')}</span>
              <Button variant="outline-secondary" size="sm" onClick={reloadPickerOptions}>
                {t('batch.retry')}
              </Button>
            </div>
          )}
          {options.status === 'ready' && picker === 'album' && (
            <MultiSelect
              id="batch-add-albums"
              label={t('batch.albumField')}
              options={options.albums}
              selected={addAlbums}
              onChange={setAddAlbums}
              placeholder={t('batch.albumPlaceholder')}
              disabled={busy}
            />
          )}
          {options.status === 'ready' && picker === 'label' && (
            <>
              <MultiSelect
                id="batch-add-labels"
                label={t('batch.labelAddField')}
                options={options.labels}
                selected={addLabels}
                onChange={setAddLabels}
                placeholder={t('batch.labelPlaceholder')}
                disabled={busy}
              />
              <div className="mt-3">
                <MultiSelect
                  id="batch-remove-labels"
                  label={t('batch.labelRemoveField')}
                  options={options.labels}
                  selected={removeLabels}
                  onChange={setRemoveLabels}
                  placeholder={t('batch.labelPlaceholder')}
                  disabled={busy}
                  destructive
                />
              </div>
            </>
          )}
        </Modal.Body>
        <Modal.Footer>
          <Button variant="secondary" onClick={closePicker} disabled={busy}>
            {t('batch.cancel')}
          </Button>
          <Button variant="primary" onClick={applyPicker} disabled={busy || !pickerHasChanges}>
            {busy && <Spinner animation="border" size="sm" className="me-1" aria-hidden="true" />}
            {t('batch.apply')}
          </Button>
        </Modal.Footer>
      </Modal>

      <BulkEditModal
        show={bulk.editing}
        photoUids={bulk.photoUids}
        onHide={bulk.close}
        onDone={bulk.finish}
      />
    </div>
  )
}
