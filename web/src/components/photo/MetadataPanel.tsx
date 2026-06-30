import { type SyntheticEvent, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import Form from 'react-bootstrap/Form'
import { useTranslation } from 'react-i18next'

import { formatDateTime } from '../../lib/format'
import { type PhotoDetail, type PhotoMetadataUpdate, updatePhoto } from '../../services/photos'

/** Props for {@link MetadataPanel}. */
export interface MetadataPanelProps {
  /** The photo whose metadata is shown and (for editors) edited. */
  photo: PhotoDetail
  /** Whether the current user may edit metadata (editor/admin). */
  canWrite: boolean
  /** Called with the refreshed photo after a successful save. */
  onUpdated: (photo: PhotoDetail) => void
}

/** Formats an ISO timestamp for the value of a `datetime-local` input. */
function toLocalInput(iso: string | undefined): string {
  if (iso === undefined || iso === '') {
    return ''
  }
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) {
    return ''
  }
  // Shift to local time then trim to minutes (YYYY-MM-DDTHH:mm).
  const offset = date.getTimezoneOffset() * 60_000
  return new Date(date.getTime() - offset).toISOString().slice(0, 16)
}

/** A read-only labelled value row, omitted entirely when the value is empty. */
function Field({ label, value }: { label: string; value: string | undefined }) {
  if (value === undefined || value === '') {
    return null
  }
  return (
    <div className="mb-2">
      <div className="small text-secondary">{label}</div>
      <div>{value}</div>
    </div>
  )
}

/**
 * The metadata panel: a read-only summary of the photo's title, description,
 * notes, capture time and camera/lens/EXIF, with an inline edit form for
 * editors that PATCHes the catalogue. Location (lat/lng) is editable here too, so
 * a photo can be geotagged or have its coordinates cleared. All text is i18n.
 */
export function MetadataPanel({ photo, canWrite, onUpdated }: MetadataPanelProps) {
  const { t, i18n } = useTranslation()
  const [editing, setEditing] = useState(false)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState(false)

  const [title, setTitle] = useState(photo.title)
  const [description, setDescription] = useState(photo.description)
  const [notes, setNotes] = useState(photo.notes ?? '')
  const [takenAt, setTakenAt] = useState(toLocalInput(photo.taken_at))
  const [lat, setLat] = useState(photo.lat !== undefined ? String(photo.lat) : '')
  const [lng, setLng] = useState(photo.lng !== undefined ? String(photo.lng) : '')

  function startEditing() {
    setTitle(photo.title)
    setDescription(photo.description)
    setNotes(photo.notes ?? '')
    setTakenAt(toLocalInput(photo.taken_at))
    setLat(photo.lat !== undefined ? String(photo.lat) : '')
    setLng(photo.lng !== undefined ? String(photo.lng) : '')
    setError(false)
    setEditing(true)
  }

  /** Builds the PATCH payload, mapping empty inputs to a cleared (null) value. */
  function buildPatch(): PhotoMetadataUpdate | null {
    const patch: PhotoMetadataUpdate = {
      title: title.trim(),
      description,
      notes,
      taken_at: takenAt === '' ? null : new Date(takenAt).toISOString(),
    }
    if (lat.trim() === '' && lng.trim() === '') {
      patch.lat = null
      patch.lng = null
    } else {
      const latNum = Number(lat)
      const lngNum = Number(lng)
      if (Number.isNaN(latNum) || Number.isNaN(lngNum)) {
        return null
      }
      patch.lat = latNum
      patch.lng = lngNum
    }
    return patch
  }

  async function save(event: SyntheticEvent) {
    event.preventDefault()
    const patch = buildPatch()
    if (patch === null) {
      setError(true)
      return
    }
    setSaving(true)
    setError(false)
    try {
      const updated = await updatePhoto(photo.uid, patch)
      onUpdated(updated)
      setEditing(false)
    } catch {
      setError(true)
    } finally {
      setSaving(false)
    }
  }

  if (editing) {
    return (
      <Form onSubmit={(event) => void save(event)} aria-label={t('photo.metadata.formLabel')}>
        {error && (
          <Alert variant="danger" className="py-2 small">
            {t('photo.metadata.saveError')}
          </Alert>
        )}
        <Form.Group className="mb-2" controlId="photo-title">
          <Form.Label className="small text-secondary mb-1">{t('photo.metadata.title')}</Form.Label>
          <Form.Control
            value={title}
            onChange={(event) => {
              setTitle(event.target.value)
            }}
          />
        </Form.Group>
        <Form.Group className="mb-2" controlId="photo-description">
          <Form.Label className="small text-secondary mb-1">
            {t('photo.metadata.description')}
          </Form.Label>
          <Form.Control
            as="textarea"
            rows={2}
            value={description}
            onChange={(event) => {
              setDescription(event.target.value)
            }}
          />
        </Form.Group>
        <Form.Group className="mb-2" controlId="photo-notes">
          <Form.Label className="small text-secondary mb-1">{t('photo.metadata.notes')}</Form.Label>
          <Form.Control
            as="textarea"
            rows={2}
            value={notes}
            onChange={(event) => {
              setNotes(event.target.value)
            }}
          />
        </Form.Group>
        <Form.Group className="mb-2" controlId="photo-taken-at">
          <Form.Label className="small text-secondary mb-1">
            {t('photo.metadata.takenAt')}
          </Form.Label>
          <Form.Control
            type="datetime-local"
            value={takenAt}
            onChange={(event) => {
              setTakenAt(event.target.value)
            }}
          />
        </Form.Group>
        <div className="d-flex gap-2 mb-2">
          <Form.Group className="flex-fill" controlId="photo-lat">
            <Form.Label className="small text-secondary mb-1">{t('photo.metadata.lat')}</Form.Label>
            <Form.Control
              value={lat}
              onChange={(event) => {
                setLat(event.target.value)
              }}
            />
          </Form.Group>
          <Form.Group className="flex-fill" controlId="photo-lng">
            <Form.Label className="small text-secondary mb-1">{t('photo.metadata.lng')}</Form.Label>
            <Form.Control
              value={lng}
              onChange={(event) => {
                setLng(event.target.value)
              }}
            />
          </Form.Group>
        </div>
        <div className="d-flex gap-2">
          <Button type="submit" variant="primary" size="sm" disabled={saving}>
            {t('photo.metadata.save')}
          </Button>
          <Button
            type="button"
            variant="outline-secondary"
            size="sm"
            disabled={saving}
            onClick={() => {
              setEditing(false)
            }}
          >
            {t('photo.metadata.cancel')}
          </Button>
        </div>
      </Form>
    )
  }

  const exposure =
    photo.exposure !== undefined && photo.exposure !== '' ? `${photo.exposure} s` : undefined
  const focal = photo.focal_length !== undefined ? `${photo.focal_length} mm` : undefined
  const aperture = photo.aperture !== undefined ? `f/${photo.aperture}` : undefined
  const iso = photo.iso !== undefined ? `ISO ${photo.iso}` : undefined

  return (
    <div>
      <Field label={t('photo.metadata.title')} value={photo.title} />
      <Field label={t('photo.metadata.description')} value={photo.description} />
      <Field label={t('photo.metadata.notes')} value={photo.notes} />
      <Field
        label={t('photo.metadata.takenAt')}
        value={
          photo.taken_at !== undefined ? formatDateTime(photo.taken_at, i18n.language) : undefined
        }
      />
      <Field label={t('photo.metadata.camera')} value={photo.camera_model || photo.camera_make} />
      <Field label={t('photo.metadata.lens')} value={photo.lens_model} />
      <Field label={t('photo.metadata.aperture')} value={aperture} />
      <Field label={t('photo.metadata.exposure')} value={exposure} />
      <Field label={t('photo.metadata.focalLength')} value={focal} />
      <Field label={t('photo.metadata.iso')} value={iso} />
      <Field label={t('photo.metadata.fileName')} value={photo.file_name} />

      {canWrite && (
        <Button variant="outline-secondary" size="sm" className="mt-2" onClick={startEditing}>
          {t('photo.metadata.edit')}
        </Button>
      )}
    </div>
  )
}
