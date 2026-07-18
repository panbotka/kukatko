import { useCallback, useEffect, useState } from 'react'
import Button from 'react-bootstrap/Button'
import Modal from 'react-bootstrap/Modal'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'

import { type UseBulkEditResult } from '../../hooks/useBulkEdit'
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

/** Props for {@link BatchActionBar}. */
export interface BatchActionBarProps {
  /** The bulk-edit state from `useBulkEdit` (hover-select), owned by the page. */
  bulk: UseBulkEditResult
  /** Selects every loaded tile in view; omit to hide the select-all control. */
  onSelectAll?: () => void
}

/** A labelled icon action button styled for the frosted bar. */
function BarAction({
  icon,
  label,
  onClick,
  disabled,
  danger,
}: {
  icon: IconName
  label: string
  onClick: () => void
  disabled?: boolean
  danger?: boolean
}) {
  return (
    <Button
      variant={danger === true ? 'outline-danger' : 'outline-light'}
      size="sm"
      onClick={onClick}
      disabled={disabled}
      title={label}
    >
      <Icon name={icon} className="me-1" />
      <span>{label}</span>
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
 */
export function BatchActionBar({ bulk, onSelectAll }: BatchActionBarProps) {
  const { t } = useTranslation()
  const { show } = useToast()
  const [busy, setBusy] = useState(false)
  const [picker, setPicker] = useState<Picker>(null)
  const [options, setOptions] = useState<OptionsState>({ status: 'idle' })
  const [addAlbums, setAddAlbums] = useState<string[]>([])
  const [addLabels, setAddLabels] = useState<string[]>([])
  const [removeLabels, setRemoveLabels] = useState<string[]>([])

  // Load albums and labels the first time a picker opens; keep them cached for
  // the session. A retry resets the state to `idle`, which re-runs this effect.
  useEffect(() => {
    if (picker === null || options.status !== 'idle') {
      return
    }
    const controller = new AbortController()
    setOptions({ status: 'loading' })
    Promise.all([fetchAlbums(controller.signal), fetchLabels(controller.signal)])
      .then(([albums, labels]) => {
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
  }, [picker, options.status])

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

  return (
    <div className="kk-batch-dock">
      <div className="kk-batch-bar" role="toolbar" aria-label={t('batch.bar')}>
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
        {onSelectAll !== undefined && (
          <Button variant="outline-light" size="sm" onClick={onSelectAll} disabled={busy}>
            <Icon name="ui-checks" className="me-1" />
            <span>{t('batch.selectAll')}</span>
          </Button>
        )}
        <BarAction
          icon="collection"
          label={t('batch.album')}
          onClick={() => {
            setPicker('album')
          }}
          disabled={busy}
        />
        <BarAction
          icon="tags"
          label={t('batch.label')}
          onClick={() => {
            setPicker('label')
          }}
          disabled={busy}
        />
        <BarAction
          icon="heart"
          label={t('batch.favorite')}
          onClick={() => {
            void applyOps({ set_favorite: true })
          }}
          disabled={busy}
        />
        <BarAction
          icon="archive"
          label={t('batch.archive')}
          danger
          onClick={() => {
            void applyOps({ archive: true })
          }}
          disabled={busy}
        />
        <DownloadZipButton photoUids={bulk.photoUids} variant="outline-light" />
        <StackSelectedControl bulk={bulk} variant="outline-light" />
        <BarAction icon="sliders" label={t('batch.more')} onClick={bulk.open} disabled={busy} />
      </div>

      <Modal show={picker !== null} onHide={closePicker} centered scrollable>
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
              <Button
                variant="outline-secondary"
                size="sm"
                onClick={() => {
                  setOptions({ status: 'idle' })
                }}
              >
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
