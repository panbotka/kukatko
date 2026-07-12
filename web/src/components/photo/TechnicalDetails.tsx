import { useState } from 'react'
import Button from 'react-bootstrap/Button'
import { useTranslation } from 'react-i18next'

import { type PhotoDetail } from '../../services/photos'
import { Icon } from '../Icon'

import { MetaField } from './MetaField'
import { RegenerateThumbnailButton } from './RegenerateThumbnailButton'

/** Props for {@link TechnicalDetails}. */
export interface TechnicalDetailsProps {
  /** The photo whose capture settings and file facts are listed. */
  photo: PhotoDetail
  /**
   * Whether the current user may run maintenance actions (editors/admins). Gates
   * the regenerate-thumbnail service button; viewers never see it.
   */
  canWrite: boolean
  /** Called after the thumbnail is regenerated so the page can reload the image. */
  onThumbnailRegenerated: () => void
}

/** The DOM id of the collapsible region, referenced by `aria-controls`. */
const REGION_ID = 'photo-technical-details'

/**
 * The camera/lens/EXIF, file facts and uploader of a photo, collapsed behind an
 * expander that is closed on first render. These are intrinsic, read-only
 * reference facts, so they stay out of the way of the organize block and caption
 * that lead the detail page — one click brings them back.
 */
export function TechnicalDetails({
  photo,
  canWrite,
  onThumbnailRegenerated,
}: TechnicalDetailsProps) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)

  const exposure =
    photo.exposure !== undefined && photo.exposure !== '' ? `${photo.exposure} s` : undefined
  const focal = photo.focal_length !== undefined ? `${photo.focal_length} mm` : undefined
  const aperture = photo.aperture !== undefined ? `f/${photo.aperture}` : undefined
  const iso = photo.iso !== undefined ? `ISO ${photo.iso}` : undefined
  const dimensions =
    photo.file_width > 0 && photo.file_height > 0
      ? `${photo.file_width} × ${photo.file_height} px`
      : undefined

  return (
    <section aria-label={t('photo.technical.title')}>
      <Button
        variant="link"
        size="sm"
        className="px-0 text-decoration-none"
        aria-expanded={open}
        aria-controls={REGION_ID}
        onClick={() => {
          setOpen(!open)
        }}
      >
        <Icon name={open ? 'chevron-down' : 'chevron-right'} className="me-1" />
        {t('photo.technical.title')}
      </Button>
      {open && (
        <div id={REGION_ID} className="mt-2">
          <MetaField
            label={t('photo.metadata.camera')}
            value={photo.camera_model || photo.camera_make}
          />
          <MetaField label={t('photo.metadata.lens')} value={photo.lens_model} />
          <MetaField label={t('photo.metadata.aperture')} value={aperture} />
          <MetaField label={t('photo.metadata.exposure')} value={exposure} />
          <MetaField label={t('photo.metadata.focalLength')} value={focal} />
          <MetaField label={t('photo.metadata.iso')} value={iso} />
          <MetaField label={t('photo.metadata.fileName')} value={photo.file_name} />
          <MetaField label={t('photo.technical.dimensions')} value={dimensions} />
          <MetaField
            label={t('photo.metadata.uploadedBy')}
            value={
              photo.uploader !== undefined && photo.uploader.name !== ''
                ? photo.uploader.name
                : t('photo.metadata.uploaderUnknown')
            }
          />
          {canWrite && (
            <RegenerateThumbnailButton uid={photo.uid} onRegenerated={onThumbnailRegenerated} />
          )}
        </div>
      )}
    </section>
  )
}
