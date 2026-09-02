import type { TFunction } from 'i18next'
import { useCallback, useEffect, useRef, useState } from 'react'
import Button from 'react-bootstrap/Button'
import Dropdown from 'react-bootstrap/Dropdown'
import Modal from 'react-bootstrap/Modal'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'

import { useAuth } from '../../auth/AuthContext'
import { type UseBulkEditResult } from '../../hooks/useBulkEdit'
import { useIsNarrowViewport } from '../../hooks/useIsNarrowViewport'
import { pendingOptions, pendingValue, resolvePending } from '../../lib/pendingCreate'
import { ApiError } from '../../services/auth'
import { type BulkOperations, bulkUpdatePhotos } from '../../services/bulk'
import {
  type AlbumCount,
  createAlbum,
  createLabel,
  fetchAlbums,
  fetchLabels,
  type LabelCount,
} from '../../services/organize'
import { Icon, type IconName } from '../Icon'
import { MultiSelect, type MultiSelectOption } from '../MultiSelect'
import { useToast } from '../toast/ToastContext'

import { BulkEditModal } from './BulkEditModal'
import { DownloadZipButton } from './DownloadZipButton'
import { SetLocationControl } from './SetLocationControl'
import { SharePhotosButton } from './SharePhotosButton'
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
 * The toast text for an album/label the picker could not create. The server
 * usually says why — a title already taken, write access withdrawn — and that
 * reason is what the reader can act on, so it is quoted next to the name; a
 * silent failure (a network drop, a 5xx with no body) falls back to asking for
 * a retry.
 */
function createFailureMessage(name: string, error: unknown, t: TFunction): string {
  return error instanceof ApiError && error.message.trim() !== ''
    ? t('batch.createError', { name, message: error.message })
    : t('batch.createErrorUnknown', { name })
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
 * The album and label pickers create as well as pick: a name no album or label
 * carries yet offers to be created, which is how a new album usually starts
 * ("these forty photos are Ostatky 2022"). Creation is deferred to the apply, so
 * closing the picker never leaves an empty album behind, and only an editor is
 * offered it — a viewer picks from what exists.
 *
 * Every photo list shows this same bar, so the batch vocabulary does not change
 * from page to page; a page that owns actions of its own (an album's set-cover /
 * remove-from-album) hands them over as `extraActions` and they join the bar
 * instead of forcing a second toolbar next to it.
 */
export function BatchActionBar({ bulk, onSelectAll, extraActions }: BatchActionBarProps) {
  const { t } = useTranslation()
  const { show } = useToast()
  const { canWrite } = useAuth()
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

  // Sends one operation set for the whole selection in a single request. On
  // success it reports the count, clears the selection and reloads the grid; on
  // failure it surfaces the server's reason and leaves the selection untouched.
  // It never rejects — every outcome is a toast — so a caller only has to await
  // it. Busy is the caller's, because a picker apply is busy from the first
  // creation, not from this request.
  const sendOps = useCallback(
    async (ops: BulkOperations) => {
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
      }
    },
    [bulk, resetPickerFields, show, t],
  )

  // The bar's one-click actions (favorite, archive): nothing to resolve first.
  const applyOps = useCallback(
    (ops: BulkOperations) => {
      void (async () => {
        setBusy(true)
        try {
          await sendOps(ops)
        } finally {
          setBusy(false)
        }
      })()
    },
    [sendOps],
  )

  /**
   * Creates the album `name` and folds it into the cached options, so its chip
   * reads by name and the field stops offering to create it again. Answers with
   * the fresh UID for {@link resolvePending}.
   */
  const createAlbumNamed = useCallback(async (name: string): Promise<string> => {
    const album = await createAlbum({ title: name, description: '', private: false })
    setOptions((prev) =>
      prev.status === 'ready'
        ? { ...prev, albums: [...prev.albums, albumOption({ ...album, photo_count: 0 })] }
        : prev,
    )
    return album.uid
  }, [])

  /** The label counterpart of {@link createAlbumNamed}. */
  const createLabelNamed = useCallback(async (name: string): Promise<string> => {
    const label = await createLabel({ name, priority: 0 })
    setOptions((prev) =>
      prev.status === 'ready'
        ? { ...prev, labels: [...prev.labels, labelOption({ ...label, photo_count: 0 })] }
        : prev,
    )
    return label.uid
  }, [])

  /**
   * Applies the open picker. Names typed into the add fields that no album or
   * label carries yet are held as `create:` markers, so they are created first —
   * the bulk endpoint only accepts identifiers that exist. A creation failure
   * names the entry, stops before the batch and keeps everything as it was: the
   * picker open, the typed names in place (with the ones already created swapped
   * to their real UIDs, so a retry never makes a second album of that name) and
   * the photo selection untouched.
   */
  const applyPicker = useCallback(() => {
    void (async () => {
      setBusy(true)
      try {
        if (picker === 'album') {
          const albums = await resolvePending(addAlbums, createAlbumNamed)
          setAddAlbums(albums.values)
          if (albums.status === 'failed') {
            show({ variant: 'danger', message: createFailureMessage(albums.name, albums.error, t) })
            return
          }
          await sendOps({ add_to_albums: albums.values })
          return
        }
        if (picker === 'label') {
          const labels = await resolvePending(addLabels, createLabelNamed)
          setAddLabels(labels.values)
          if (labels.status === 'failed') {
            show({ variant: 'danger', message: createFailureMessage(labels.name, labels.error, t) })
            return
          }
          const ops: BulkOperations = {}
          if (labels.values.length > 0) {
            ops.add_labels = labels.values
          }
          if (removeLabels.length > 0) {
            ops.remove_labels = removeLabels
          }
          await sendOps(ops)
        }
      } finally {
        setBusy(false)
      }
    })()
  }, [
    picker,
    sendOps,
    show,
    t,
    addAlbums,
    addLabels,
    removeLabels,
    createAlbumNamed,
    createLabelNamed,
  ])

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
        applyOps({ set_favorite: true })
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
        applyOps({ archive: true })
      }}
      disabled={busy}
    />
  )
  // The map picker for a whole selection: one pin for a box of scans from the
  // same place. It owns its own dialog (and the count of photos that already
  // have a location), so the bar only places the trigger.
  const locationControl = <SetLocationControl bulk={bulk} variant="outline-light" />
  const downloadControl = <DownloadZipButton photoUids={bulk.photoUids} variant="outline-light" />
  // Beside the ZIP, and gated the same way (both go through RequireDownload), but
  // only where the browser can actually hand files to a share sheet — on a desktop
  // it renders nothing and the ZIP stays the answer.
  const shareControl = <SharePhotosButton photoUids={bulk.photoUids} variant="outline-light" />
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
                  {locationControl}
                  {archiveAction}
                  {downloadControl}
                  {shareControl}
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
            {locationControl}
            {archiveAction}
            {downloadControl}
            {shareControl}
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
              // The pending creations join the options so their chips read as the
              // typed name instead of a raw `create:` marker.
              options={[...options.albums, ...pendingOptions(addAlbums)]}
              selected={addAlbums}
              onChange={setAddAlbums}
              placeholder={t('batch.albumPlaceholder')}
              disabled={busy}
              onCreate={
                canWrite
                  ? (name) => {
                      setAddAlbums((prev) => [...prev, pendingValue(name)])
                    }
                  : undefined
              }
            />
          )}
          {options.status === 'ready' && picker === 'label' && (
            <>
              <MultiSelect
                id="batch-add-labels"
                label={t('batch.labelAddField')}
                options={[...options.labels, ...pendingOptions(addLabels)]}
                selected={addLabels}
                onChange={setAddLabels}
                placeholder={t('batch.labelPlaceholder')}
                disabled={busy}
                onCreate={
                  canWrite
                    ? (name) => {
                        setAddLabels((prev) => [...prev, pendingValue(name)])
                      }
                    : undefined
                }
              />
              <div className="mt-3">
                {/* No create here: a label that does not exist cannot be removed. */}
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
