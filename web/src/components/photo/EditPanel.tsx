import { useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import Card from 'react-bootstrap/Card'
import Form from 'react-bootstrap/Form'
import { useTranslation } from 'react-i18next'

import { hasCrop, NEUTRAL_EDIT, rotateRight } from '../../lib/photoEdit'
import { type PhotoEdit, saveEdit } from '../../services/photos'
import { Icon } from '../Icon'

/** Props for {@link EditPanel}. */
export interface EditPanelProps {
  /** The photo being edited. */
  uid: string
  /** The in-progress edit the controls show — and the photo beside them previews. */
  edit: PhotoEdit
  /** Reports every adjustment, so the page can preview it on the photo. */
  onChange: (edit: PhotoEdit) => void
  /** Called with the persisted edit after a successful save. */
  onSaved: (edit: PhotoEdit) => void
  /** Closes the panel, discarding whatever is unsaved. */
  onClose: () => void
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
 * and an optional normalised crop. Saving writes to `photo_edits` via the edit
 * API; the original file is never modified. Editor/admin only (the page gates
 * rendering).
 *
 * It sits beside the photo (below it on a phone) exactly like `FacesPanel`, whose
 * shape it mirrors: a card whose header names it and carries the close button.
 * Crucially it renders NO image of its own — it is a controlled component, and
 * the page previews the reported edit on the one photo the detail already shows,
 * live and CSS-only (matching how the backend renders the download).
 */
export function EditPanel({ uid, edit, onChange, onSaved, onClose }: EditPanelProps) {
  const { t } = useTranslation()
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState(false)

  const cropOn = hasCrop(edit)

  function toggleCrop(enabled: boolean) {
    onChange(enabled ? { ...edit, ...DEFAULT_CROP } : withoutCrop(edit))
  }

  function setCrop(field: 'crop_x' | 'crop_y' | 'crop_w' | 'crop_h', value: number) {
    onChange({ ...edit, [field]: value })
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
    <Card>
      <Card.Header className="d-flex justify-content-between align-items-center">
        <span>{t('photo.edit.title')}</span>
        <Button
          variant="link"
          size="sm"
          className="p-0 text-reset text-decoration-none"
          aria-label={t('photo.edit.closePanel')}
          onClick={onClose}
        >
          <Icon name="x-lg" />
        </Button>
      </Card.Header>
      <Card.Body style={{ maxHeight: '80vh', overflowY: 'auto' }}>
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
              onChange({ ...edit, rotation: rotateRight(edit.rotation) })
            }}
          >
            {t('photo.edit.rotateRight')}
          </Button>
          <span className="small">{edit.rotation}°</span>
        </div>

        <Form.Group className="mb-3" controlId="photo-edit-brightness">
          <Form.Label className="small text-secondary mb-1">
            {t('photo.edit.brightness')}
          </Form.Label>
          <Form.Range
            min={-1}
            max={1}
            step={0.05}
            value={edit.brightness}
            aria-label={t('photo.edit.brightness')}
            onChange={(event) => {
              onChange({ ...edit, brightness: Number(event.target.value) })
            }}
          />
        </Form.Group>

        <Form.Group className="mb-3" controlId="photo-edit-contrast">
          <Form.Label className="small text-secondary mb-1">{t('photo.edit.contrast')}</Form.Label>
          <Form.Range
            min={-1}
            max={1}
            step={0.05}
            value={edit.contrast}
            aria-label={t('photo.edit.contrast')}
            onChange={(event) => {
              onChange({ ...edit, contrast: Number(event.target.value) })
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
                  value={edit[field] ?? 0}
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
          <Button variant="primary" size="sm" disabled={saving} onClick={() => void persist(edit)}>
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
      </Card.Body>
    </Card>
  )
}
