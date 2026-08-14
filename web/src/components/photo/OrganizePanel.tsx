import { useEffect, useMemo, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import { useTranslation } from 'react-i18next'

import { EntityChip } from '../EntityChip'

import { foldedEquals } from '../../lib/text'
import {
  addAlbumPhotos,
  type AlbumCount,
  attachLabel,
  createAlbum,
  createLabel,
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
 * The albums & labels panel: the photo's current album and label chips (each an
 * {@link EntityChip} linking to its scoped list — the same chip the read-only
 * strip above the photo draws), with inline add (a type-to-filter autocomplete
 * over the remaining albums/labels — see {@link AddAutocomplete}) and remove
 * controls for editors. Mutations call the organize API and update the photo's
 * memberships in place. Viewers see the chips read-only.
 *
 * Both fields also create: typing a name nothing carries offers to create the
 * album/label and attach the photo in one action, so a catalog with zero albums
 * or zero labels can still get its first one. The new album takes the defaults
 * the Albums page would give it — a plain, public album with no description —
 * which stay editable there.
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

  // What the photo already carries: held out of the options above, and named
  // here so typing one of them is not offered for creation either.
  const memberAlbumTitles = useMemo(() => photo.albums.map((album) => album.title), [photo.albums])
  const memberLabelNames = useMemo(() => photo.labels.map((label) => label.name), [photo.labels])

  /** Runs a mutation with the busy/error plumbing; reports whether it succeeded. */
  async function run(action: () => Promise<PhotoDetail>): Promise<boolean> {
    setBusy(true)
    setError(false)
    try {
      onChanged(await action())
      return true
    } catch {
      setError(true)
      return false
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

  /**
   * Creates the album `name` and puts the photo in it in one action, then
   * reports success so the field can clear (or keep the text on failure).
   *
   * The field only offers to create a name nothing in the loaded catalog carries,
   * but this does not lean on that: an album of that name that is known here is
   * reused rather than duplicated under a second, colliding slug.
   */
  function createAndAddAlbum(name: string): Promise<boolean> {
    return run(async () => {
      const existing = albums.find((candidate) => foldedEquals(candidate.title, name))
      const album =
        existing ?? (await createAlbum({ title: name, description: '', private: false }))
      if (existing === undefined) {
        setAlbums((current) => [...current, { ...album, photo_count: 0 }])
      }
      if (photo.albums.some((current) => current.uid === album.uid)) {
        return photo
      }
      await addAlbumPhotos(album.uid, [photo.uid])
      return { ...photo, albums: [...photo.albums, { uid: album.uid, title: album.title }] }
    })
  }

  /**
   * Creates the label `name` and attaches it to the photo in one action, then
   * reports success so the field can clear (or keep the text on failure).
   *
   * As with {@link createAndAddAlbum}, a label of that name that is already known
   * here is attached rather than created a second time.
   */
  function createAndAttachLabel(name: string): Promise<boolean> {
    return run(async () => {
      const existing = labels.find((candidate) => foldedEquals(candidate.name, name))
      const label = existing ?? (await createLabel({ name, priority: 0 }))
      if (existing === undefined) {
        setLabels((current) => [...current, { ...label, photo_count: 0 }])
      }
      if (photo.labels.some((current) => current.uid === label.uid)) {
        return photo
      }
      await attachLabel(label.uid, photo.uid)
      return { ...photo, labels: [...photo.labels, { uid: label.uid, name: label.name }] }
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
          <EntityChip
            key={album.uid}
            kind="album"
            to={`/albums/${album.uid}`}
            remove={
              canWrite
                ? {
                    label: t('photo.organize.removeAlbum', { name: album.title }),
                    onRemove: () => {
                      removeAlbum(album.uid)
                    },
                  }
                : undefined
            }
          >
            {album.title}
          </EntityChip>
        ))}
      </div>
      {/* Like the label field, this stays even with no options — it creates. */}
      {canWrite && (
        <AddAutocomplete
          id="organize-add-album"
          label={t('photo.organize.addAlbum')}
          options={albumOptions}
          existingNames={memberAlbumTitles}
          disabled={busy}
          onAdd={addAlbum}
          onCreate={createAndAddAlbum}
        />
      )}

      <div className="small text-secondary mb-1">{t('photo.organize.labels')}</div>
      <div className="d-flex flex-wrap gap-2 mb-2">
        {photo.labels.length === 0 && (
          <span className="text-secondary small">{t('photo.organize.noLabels')}</span>
        )}
        {photo.labels.map((label) => (
          <EntityChip
            key={label.uid}
            kind="tag"
            to={`/labels/${label.uid}`}
            remove={
              canWrite
                ? {
                    label: t('photo.organize.removeLabel', { name: label.name }),
                    onRemove: () => {
                      removeLabel(label.uid)
                    },
                  }
                : undefined
            }
          >
            {label.name}
          </EntityChip>
        ))}
      </div>
      {canWrite && (
        <AddAutocomplete
          id="organize-add-label"
          label={t('photo.organize.addLabel')}
          options={labelOptions}
          existingNames={memberLabelNames}
          disabled={busy}
          onAdd={addLabel}
          onCreate={createAndAttachLabel}
        />
      )}
    </div>
  )
}
