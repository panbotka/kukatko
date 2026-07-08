import { useEffect, useMemo, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Badge from 'react-bootstrap/Badge'
import CloseButton from 'react-bootstrap/CloseButton'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import {
  addAlbumPhotos,
  type AlbumCount,
  attachLabel,
  detachLabel,
  fetchAlbums,
  fetchLabels,
  type LabelCount,
  removeAlbumPhotos,
} from '../../services/organize'
import { type PhotoDetail } from '../../services/photos'
import { AddAutocomplete } from './AddAutocomplete'

/** Props for {@link OrganizePanel}. */
export interface OrganizePanelProps {
  /** The photo whose album/label memberships are shown and edited. */
  photo: PhotoDetail
  /** Whether the current user may add/remove memberships (editor/admin). */
  canWrite: boolean
  /** Called with the photo whose albums/labels arrays were updated in place. */
  onChanged: (photo: PhotoDetail) => void
}

/**
 * The albums & labels panel: the photo's current album and label chips (each
 * linking to its scoped list), with inline add (a type-to-filter autocomplete
 * over the remaining albums/labels — see {@link AddAutocomplete}) and remove
 * controls for editors. Mutations call the organize API and update the photo's
 * memberships in place. Viewers see the chips read-only.
 */
export function OrganizePanel({ photo, canWrite, onChanged }: OrganizePanelProps) {
  const { t } = useTranslation()
  const [albums, setAlbums] = useState<AlbumCount[]>([])
  const [labels, setLabels] = useState<LabelCount[]>([])
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState(false)

  // Only editors need the full album/label lists for the add dropdowns.
  useEffect(() => {
    if (!canWrite) {
      return
    }
    const controller = new AbortController()
    Promise.all([fetchAlbums(controller.signal), fetchLabels(controller.signal)])
      .then(([albumList, labelList]) => {
        setAlbums(albumList)
        setLabels(labelList)
      })
      .catch(() => undefined)
    return () => {
      controller.abort()
    }
  }, [canWrite])

  // Offer only albums/labels the photo is not already in, mapped to the
  // autocomplete's option shape.
  const albumOptions = useMemo(() => {
    const members = new Set(photo.albums.map((album) => album.uid))
    return albums
      .filter((album) => !members.has(album.uid))
      .map((album) => ({ uid: album.uid, label: album.title }))
  }, [albums, photo.albums])
  const labelOptions = useMemo(() => {
    const members = new Set(photo.labels.map((label) => label.uid))
    return labels
      .filter((label) => !members.has(label.uid))
      .map((label) => ({ uid: label.uid, label: label.name }))
  }, [labels, photo.labels])

  async function run(action: () => Promise<PhotoDetail>) {
    setBusy(true)
    setError(false)
    try {
      onChanged(await action())
    } catch {
      setError(true)
    } finally {
      setBusy(false)
    }
  }

  function addAlbum(uid: string) {
    const album = albums.find((candidate) => candidate.uid === uid)
    if (album === undefined) {
      return
    }
    void run(async () => {
      await addAlbumPhotos(album.uid, [photo.uid])
      return { ...photo, albums: [...photo.albums, { uid: album.uid, title: album.title }] }
    })
  }

  function removeAlbum(uid: string) {
    void run(async () => {
      await removeAlbumPhotos(uid, [photo.uid])
      return { ...photo, albums: photo.albums.filter((album) => album.uid !== uid) }
    })
  }

  function addLabel(uid: string) {
    const label = labels.find((candidate) => candidate.uid === uid)
    if (label === undefined) {
      return
    }
    void run(async () => {
      await attachLabel(label.uid, photo.uid)
      return { ...photo, labels: [...photo.labels, { uid: label.uid, name: label.name }] }
    })
  }

  function removeLabel(uid: string) {
    void run(async () => {
      await detachLabel(uid, photo.uid)
      return { ...photo, labels: photo.labels.filter((label) => label.uid !== uid) }
    })
  }

  return (
    <div>
      {error && (
        <Alert variant="danger" className="py-2 small">
          {t('photo.organize.error')}
        </Alert>
      )}

      <div className="small text-secondary mb-1">{t('photo.organize.albums')}</div>
      <div className="d-flex flex-wrap gap-2 mb-2">
        {photo.albums.length === 0 && (
          <span className="text-secondary small">{t('photo.organize.noAlbums')}</span>
        )}
        {photo.albums.map((album) => (
          <Badge key={album.uid} bg="secondary" className="d-inline-flex align-items-center gap-1">
            <Link to={`/albums/${album.uid}`} className="text-white text-decoration-none">
              {album.title}
            </Link>
            {canWrite && (
              <CloseButton
                variant="white"
                aria-label={t('photo.organize.removeAlbum', { name: album.title })}
                onClick={() => {
                  removeAlbum(album.uid)
                }}
              />
            )}
          </Badge>
        ))}
      </div>
      {canWrite && albumOptions.length > 0 && (
        <AddAutocomplete
          id="organize-add-album"
          label={t('photo.organize.addAlbum')}
          options={albumOptions}
          disabled={busy}
          onAdd={addAlbum}
        />
      )}

      <div className="small text-secondary mb-1">{t('photo.organize.labels')}</div>
      <div className="d-flex flex-wrap gap-2 mb-2">
        {photo.labels.length === 0 && (
          <span className="text-secondary small">{t('photo.organize.noLabels')}</span>
        )}
        {photo.labels.map((label) => (
          <Badge key={label.uid} bg="info" className="d-inline-flex align-items-center gap-1">
            <Link to={`/labels/${label.uid}`} className="text-white text-decoration-none">
              {label.name}
            </Link>
            {canWrite && (
              <CloseButton
                variant="white"
                aria-label={t('photo.organize.removeLabel', { name: label.name })}
                onClick={() => {
                  removeLabel(label.uid)
                }}
              />
            )}
          </Badge>
        ))}
      </div>
      {canWrite && labelOptions.length > 0 && (
        <AddAutocomplete
          id="organize-add-label"
          label={t('photo.organize.addLabel')}
          options={labelOptions}
          disabled={busy}
          onAdd={addLabel}
        />
      )}
    </div>
  )
}
