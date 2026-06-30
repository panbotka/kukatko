import { useEffect, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Badge from 'react-bootstrap/Badge'
import Button from 'react-bootstrap/Button'
import CloseButton from 'react-bootstrap/CloseButton'
import Form from 'react-bootstrap/Form'
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
 * linking to its scoped list), with inline add (a dropdown of the remaining
 * albums/labels) and remove controls for editors. Mutations call the organize API
 * and update the photo's memberships in place. Viewers see the chips read-only.
 */
export function OrganizePanel({ photo, canWrite, onChanged }: OrganizePanelProps) {
  const { t } = useTranslation()
  const [albums, setAlbums] = useState<AlbumCount[]>([])
  const [labels, setLabels] = useState<LabelCount[]>([])
  const [albumChoice, setAlbumChoice] = useState('')
  const [labelChoice, setLabelChoice] = useState('')
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

  const memberAlbumUids = new Set(photo.albums.map((album) => album.uid))
  const memberLabelUids = new Set(photo.labels.map((label) => label.uid))
  const availableAlbums = albums.filter((album) => !memberAlbumUids.has(album.uid))
  const availableLabels = labels.filter((label) => !memberLabelUids.has(label.uid))

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

  function addAlbum() {
    const album = albums.find((candidate) => candidate.uid === albumChoice)
    if (album === undefined) {
      return
    }
    setAlbumChoice('')
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

  function addLabel() {
    const label = labels.find((candidate) => candidate.uid === labelChoice)
    if (label === undefined) {
      return
    }
    setLabelChoice('')
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
      {canWrite && availableAlbums.length > 0 && (
        <div className="d-flex gap-2 mb-3">
          <Form.Select
            size="sm"
            value={albumChoice}
            aria-label={t('photo.organize.addAlbum')}
            onChange={(event) => {
              setAlbumChoice(event.target.value)
            }}
          >
            <option value="">{t('photo.organize.addAlbum')}</option>
            {availableAlbums.map((album) => (
              <option key={album.uid} value={album.uid}>
                {album.title}
              </option>
            ))}
          </Form.Select>
          <Button
            variant="outline-primary"
            size="sm"
            disabled={busy || albumChoice === ''}
            onClick={addAlbum}
          >
            {t('photo.organize.add')}
          </Button>
        </div>
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
      {canWrite && availableLabels.length > 0 && (
        <div className="d-flex gap-2">
          <Form.Select
            size="sm"
            value={labelChoice}
            aria-label={t('photo.organize.addLabel')}
            onChange={(event) => {
              setLabelChoice(event.target.value)
            }}
          >
            <option value="">{t('photo.organize.addLabel')}</option>
            {availableLabels.map((label) => (
              <option key={label.uid} value={label.uid}>
                {label.name}
              </option>
            ))}
          </Form.Select>
          <Button
            variant="outline-primary"
            size="sm"
            disabled={busy || labelChoice === ''}
            onClick={addLabel}
          >
            {t('photo.organize.add')}
          </Button>
        </div>
      )}
    </div>
  )
}
