import { useMemo } from 'react'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'

import { type OrganizeLoadState } from '../../hooks/useUploadOrganize'
import { pendingOptions, pendingValue } from '../../lib/pendingCreate'
import { type AlbumCount, type LabelCount } from '../../services/organize'
import { MultiSelect, type MultiSelectOption } from '../MultiSelect'

/** Props for {@link UploadOrganize}. */
export interface UploadOrganizeProps {
  /** Fetch lifecycle of the album/label catalogs. */
  load: OrganizeLoadState
  /** Chosen album values (real UIDs or `create:` markers). */
  albums: string[]
  /** Chosen label values (real UIDs or `create:` markers). */
  labels: string[]
  /** Replaces the chosen albums. */
  onAlbums: (values: string[]) => void
  /** Replaces the chosen labels. */
  onLabels: (values: string[]) => void
  /** Locks the selectors while an upload/assignment is in flight. */
  disabled: boolean
  /** Whether the acting user may create albums/labels inline. */
  allowCreate: boolean
}

/** Maps an album to a {@link MultiSelect} option, counted by its photo total. */
function albumOption(album: AlbumCount): MultiSelectOption {
  return { value: album.uid, label: album.title, count: album.photo_count }
}

/** Maps a label to a {@link MultiSelect} option, counted by its photo total. */
function labelOption(label: LabelCount): MultiSelectOption {
  return { value: label.uid, label: label.name, count: label.photo_count }
}

/**
 * The upload page's batch-wide album and label picker: two searchable
 * multi-selects that (optionally) tag every photo of the upload. Both reuse the
 * catalog {@link MultiSelect}, so typing a name that matches nothing offers to
 * create it inline — the pick is held as a `create:` marker and only turned into
 * a real album/label when the finished batch is assigned. Empty by default, so
 * an upload with nothing chosen behaves exactly as before.
 */
export function UploadOrganize({
  load,
  albums,
  labels,
  onAlbums,
  onLabels,
  disabled,
  allowCreate,
}: UploadOrganizeProps) {
  const { t } = useTranslation()

  const albumOptions = useMemo(
    () =>
      load.status === 'ready' ? [...load.albums.map(albumOption), ...pendingOptions(albums)] : [],
    [load, albums],
  )
  const labelOptions = useMemo(
    () =>
      load.status === 'ready' ? [...load.labels.map(labelOption), ...pendingOptions(labels)] : [],
    [load, labels],
  )

  return (
    <div className="kk-surface p-3 mb-4">
      <h2 className="kk-text-eyebrow text-secondary mb-1">{t('upload.organize.heading')}</h2>
      <p className="kk-text-caption text-secondary mb-3">{t('upload.organize.hint')}</p>

      {load.status === 'loading' && (
        <div className="d-flex align-items-center gap-2 text-secondary kk-text-caption">
          <Spinner animation="border" role="status" size="sm">
            <span className="visually-hidden">{t('upload.organize.heading')}</span>
          </Spinner>
          <span>{t('upload.organize.heading')}</span>
        </div>
      )}

      {load.status === 'error' && (
        <p className="text-danger kk-text-caption mb-0">{t('upload.organize.loadError')}</p>
      )}

      {load.status === 'ready' && (
        <div className="row g-3">
          <div className="col-12 col-md-6">
            <MultiSelect
              id="upload-albums"
              label={t('upload.organize.albums')}
              placeholder={t('upload.organize.albumsPlaceholder')}
              options={albumOptions}
              selected={albums}
              disabled={disabled}
              onChange={onAlbums}
              onCreate={
                allowCreate
                  ? (name) => {
                      onAlbums([...albums, pendingValue(name)])
                    }
                  : undefined
              }
            />
          </div>
          <div className="col-12 col-md-6">
            <MultiSelect
              id="upload-labels"
              label={t('upload.organize.labels')}
              placeholder={t('upload.organize.labelsPlaceholder')}
              options={labelOptions}
              selected={labels}
              disabled={disabled}
              onChange={onLabels}
              onCreate={
                allowCreate
                  ? (name) => {
                      onLabels([...labels, pendingValue(name)])
                    }
                  : undefined
              }
            />
          </div>
        </div>
      )}
    </div>
  )
}
