import { useEffect, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import Form from 'react-bootstrap/Form'
import { useTranslation } from 'react-i18next'

import { editPreviewStyle, hasCrop, NEUTRAL_EDIT, rotateRight } from '../../lib/photoEdit'
import { type PhotoEdit, saveEdit, thumbUrl } from '../../services/photos'

/** Props for {@link EditPanel}. */
export interface EditPanelProps {
  /** The photo being edited. */
  uid: string
  /** The currently stored edit (seeds the controls). */
  edit: PhotoEdit
  /** Called with the persisted edit after a successful save. */
  onSaved: (edit: PhotoEdit) => void
}

/** A sensible default crop (a centred 80% box) when crop is first enabled. */
const DEFAULT_CROP = { crop_x: 0.1, crop_y: 0.1, crop_w: 0.8, crop_h: 0.8 }

/**
 * The crop sliders, each paired with its explicit i18n key (the typed i18n config
 * rejects a dynamically-built key, so the literals are spelled out here).
 */
const CROP_FIELDS = [
  { field: 'crop_x', label: 'photo.edit.crop_x' },
  { field: 'crop_y', label: 'photo.edit.crop_y' },
  { field: 'crop_w', label: 'photo.edit.crop_w' },
  { field: 'crop_h', label: 'photo.edit.crop_h' },
] as const

/** Strips the crop fields from an edit, leaving rotation and colour intact. */
function withoutCrop(edit: PhotoEdit): PhotoEdit {
  const { crop_x, crop_y, crop_w, crop_h, ...rest } = edit
  void crop_x
  void crop_y
  void crop_w
  void crop_h
  return rest
}

/**
 * The non-destructive edit panel: rotate (quarter turns), brightness, contrast
 * and an optional normalised crop, with a live preview that reflects the
 * in-progress adjustments via CSS (matching how the backend renders the download).
 * Saving writes to `photo_edits` via the edit API; the original file is never
 * modified. Editor/admin only (the page gates rendering).
 */
export function EditPanel({ uid, edit, onSaved }: EditPanelProps) {
  const { t } = useTranslation()
  const [working, setWorking] = useState<PhotoEdit>(edit)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState(false)

  // Re-seed when the stored edit changes (e.g. after a save or photo switch).
  useEffect(() => {
    setWorking(edit)
  }, [edit])

  const cropOn = hasCrop(working)

  function toggleCrop(enabled: boolean) {
    setWorking((prev) => (enabled ? { ...prev, ...DEFAULT_CROP } : withoutCrop(prev)))
  }

  function setCrop(field: 'crop_x' | 'crop_y' | 'crop_w' | 'crop_h', value: number) {
    setWorking((prev) => ({ ...prev, [field]: value }))
  }

  async function persist(next: PhotoEdit) {
    setSaving(true)
    setError(false)
    try {
      const saved = await saveEdit(uid, next)
      onSaved(saved)
    } catch {
      setError(true)
    } finally {
      setSaving(false)
    }
  }

  return (
    <div aria-label={t('photo.edit.title')}>
      <div
        className="rounded overflow-hidden mb-3 bg-dark d-flex justify-content-center"
        style={{ maxHeight: '40vh' }}
      >
        <img
          src={thumbUrl(uid, 'fit_1280')}
          alt={t('photo.edit.previewAlt')}
          aria-label={t('photo.edit.previewAlt')}
          className="mw-100"
          style={{ objectFit: 'contain', maxHeight: '40vh', ...editPreviewStyle(working) }}
        />
      </div>

      {error && (
        <Alert variant="danger" className="py-2 small">
          {t('photo.edit.error')}
        </Alert>
      )}

      <div className="d-flex align-items-center gap-2 mb-3">
        <span className="small text-secondary">{t('photo.edit.rotation')}</span>
        <Button
          variant="outline-secondary"
          size="sm"
          onClick={() => {
            setWorking((prev) => ({ ...prev, rotation: rotateRight(prev.rotation) }))
          }}
        >
          {t('photo.edit.rotateRight')}
        </Button>
        <span className="small">{working.rotation}°</span>
      </div>

      <Form.Group className="mb-3" controlId="photo-edit-brightness">
        <Form.Label className="small text-secondary mb-1">{t('photo.edit.brightness')}</Form.Label>
        <Form.Range
          min={-1}
          max={1}
          step={0.05}
          value={working.brightness}
          aria-label={t('photo.edit.brightness')}
          onChange={(event) => {
            setWorking((prev) => ({ ...prev, brightness: Number(event.target.value) }))
          }}
        />
      </Form.Group>

      <Form.Group className="mb-3" controlId="photo-edit-contrast">
        <Form.Label className="small text-secondary mb-1">{t('photo.edit.contrast')}</Form.Label>
        <Form.Range
          min={-1}
          max={1}
          step={0.05}
          value={working.contrast}
          aria-label={t('photo.edit.contrast')}
          onChange={(event) => {
            setWorking((prev) => ({ ...prev, contrast: Number(event.target.value) }))
          }}
        />
      </Form.Group>

      <Form.Check
        type="checkbox"
        id="photo-edit-crop"
        className="mb-2"
        label={t('photo.edit.crop')}
        checked={cropOn}
        onChange={(event) => {
          toggleCrop(event.target.checked)
        }}
      />
      {cropOn && (
        <div className="row g-2 mb-3">
          {CROP_FIELDS.map(({ field, label }) => (
            <div className="col-6" key={field}>
              <Form.Label className="small text-secondary mb-1">{t(label)}</Form.Label>
              <Form.Range
                min={0}
                max={1}
                step={0.01}
                value={working[field] ?? 0}
                aria-label={t(label)}
                onChange={(event) => {
                  setCrop(field, Number(event.target.value))
                }}
              />
            </div>
          ))}
        </div>
      )}

      <div className="d-flex gap-2">
        <Button variant="primary" size="sm" disabled={saving} onClick={() => void persist(working)}>
          {t('photo.edit.save')}
        </Button>
        <Button
          variant="outline-secondary"
          size="sm"
          disabled={saving}
          onClick={() => void persist(NEUTRAL_EDIT)}
        >
          {t('photo.edit.reset')}
        </Button>
      </div>
    </div>
  )
}
