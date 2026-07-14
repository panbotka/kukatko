import { type ReactNode, useState } from 'react'
import Badge from 'react-bootstrap/Badge'
import Button from 'react-bootstrap/Button'
import { useTranslation } from 'react-i18next'

import { formatByteCount, formatBytes, formatDateTime, formatDuration } from '../../lib/format'
import {
  aspectRatio,
  formatMime,
  megapixels,
  orientation,
  shortHash,
  splitKeywords,
  takenAtSource,
} from '../../lib/photoFacts'
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

/** Whether a formatted value has anything to show. */
function has(value: string | undefined): boolean {
  return value !== undefined && value !== ''
}

/** Formats a number in the active locale, with at most `digits` decimals. */
function formatNumber(value: number, locale: string, digits: number): string {
  return new Intl.NumberFormat(locale, { maximumFractionDigits: digits }).format(value)
}

/**
 * One titled group of the card — the definition list its {@link MetaField} rows
 * fill. Label and value sit side by side on a wide viewport and stack on a narrow
 * one. A group is only rendered when it has something in it: the caller decides,
 * because a heading over an empty list is the same blank noise as an empty row.
 */
function MetaGroup({ title, children }: { title: string; children: ReactNode }) {
  return (
    <div className="mb-3">
      <h3 className="small text-uppercase text-secondary fw-semibold mb-2">{title}</h3>
      <dl className="row mb-0">{children}</dl>
    </div>
  )
}

/**
 * Everything the app knows about a photo — capture settings, the IPTC/XMP credit
 * block, the file's own technicals, its cached place, a video's streams and where
 * an imported photo came from — grouped and collapsed behind an expander that is
 * closed on first render. These are intrinsic, read-only reference facts, so they
 * stay out of the way of the organize block and caption that lead the detail page;
 * one click brings them back. Editing happens in `MetadataPanel`, never here.
 *
 * A field the photo has no value for is not rendered at all, and nor is a group
 * that would hold only such fields: a scan with no EXIF shows a handful of rows
 * rather than a wall of dashes.
 */
export function TechnicalDetails({
  photo,
  canWrite,
  onThumbnailRegenerated,
}: TechnicalDetailsProps) {
  const { t, i18n } = useTranslation()
  const [open, setOpen] = useState(false)
  const [copied, setCopied] = useState(false)
  const locale = i18n.language

  // Photo — how the image was captured, and what its maker says about it.
  const exposure =
    photo.exposure !== undefined && photo.exposure !== '' ? `${photo.exposure} s` : undefined
  const focal = photo.focal_length !== undefined ? `${photo.focal_length} mm` : undefined
  const aperture = photo.aperture !== undefined ? `f/${photo.aperture}` : undefined
  const iso = photo.iso !== undefined ? `ISO ${photo.iso}` : undefined
  const camera = photo.camera_model || photo.camera_make
  const source = takenAtSource(photo.taken_at_source)
  const sourceLabel =
    source === undefined ? undefined : t(`photo.technical.takenAtSourceValue.${source}`)
  const keywords = splitKeywords(photo.keywords)
  const flagged = photo.private === true || photo.scan === true

  // File — the bytes on disk, and what the app derives from them.
  const dimensions =
    photo.file_width > 0 && photo.file_height > 0
      ? `${photo.file_width} × ${photo.file_height} px`
      : undefined
  const ratio = aspectRatio(photo.file_width, photo.file_height, locale)
  const pixels = megapixels(photo.file_width, photo.file_height, locale)
  const resolution =
    pixels === undefined ? undefined : `${pixels} ${t('photo.technical.megapixelUnit')}`
  const format = formatMime(photo.file_mime)
  const size = photo.file_size > 0 ? formatBytes(photo.file_size, locale) : undefined
  const exif = orientation(photo.file_orientation)
  const orientationLabel =
    exif === undefined ? undefined : t(`photo.technical.orientationValue.${exif}`)
  // The pre-ingest name is only worth a row when it is not the name the file
  // already carries in the storage layout.
  const originalName =
    photo.original_name !== undefined &&
    photo.original_name !== '' &&
    photo.original_name !== photo.file_name
      ? photo.original_name
      : undefined

  // Location — the coordinate, and the place the `places` job resolved it into.
  const coordinates =
    photo.lat !== undefined && photo.lng !== undefined
      ? `${photo.lat.toFixed(5)}, ${photo.lng.toFixed(5)}`
      : undefined
  const altitude =
    photo.altitude !== undefined ? `${formatNumber(photo.altitude, locale, 0)} m` : undefined
  const place = photo.place

  // Video — only for a clip or the motion half of a live photo.
  const isVideo = photo.media_type === 'video' || photo.media_type === 'live'
  const duration = photo.duration_ms !== undefined ? formatDuration(photo.duration_ms) : undefined
  const audio = isVideo
    ? t(photo.has_audio === true ? 'photo.technical.hasAudioYes' : 'photo.technical.hasAudioNo')
    : undefined
  const fps = photo.fps !== undefined ? `${formatNumber(photo.fps, locale, 2)} fps` : undefined

  // Origin — who put the photo here, and which library it came from.
  const uploader =
    photo.uploader !== undefined && photo.uploader.name !== ''
      ? photo.uploader.name
      : t('photo.metadata.uploaderUnknown')

  const hasPhotoGroup =
    [
      camera,
      photo.lens_model,
      aperture,
      exposure,
      focal,
      iso,
      photo.camera_serial,
      photo.software,
      sourceLabel,
      photo.subject,
      photo.artist,
      photo.copyright,
      photo.license,
      photo.projection,
    ].some(has) ||
    keywords.length > 0 ||
    flagged
  const hasFileGroup = [
    photo.file_name,
    originalName,
    format,
    size,
    dimensions,
    ratio,
    resolution,
    orientationLabel,
    photo.color_profile,
    photo.image_codec,
    photo.file_hash,
  ].some(has)
  const hasLocationGroup = has(coordinates) || has(altitude) || place !== undefined
  const hasVideoGroup =
    isVideo && [duration, photo.video_codec, photo.audio_codec, audio, fps].some(has)

  async function copyHash() {
    try {
      await navigator.clipboard.writeText(photo.file_hash)
      setCopied(true)
    } catch {
      // The clipboard can be denied (an insecure context, a withheld permission).
      // The full hash is in the row's tooltip either way, so there is nothing to
      // report and nothing to undo.
    }
  }

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
          {hasPhotoGroup && (
            <MetaGroup title={t('photo.technical.groups.photo')}>
              {flagged && (
                <MetaField label={t('photo.technical.flags')}>
                  <span className="d-flex gap-1 flex-wrap">
                    {photo.private === true && (
                      <Badge bg="warning" text="dark">
                        {t('photo.technical.private')}
                      </Badge>
                    )}
                    {photo.scan === true && (
                      <Badge bg="secondary">{t('photo.technical.scan')}</Badge>
                    )}
                  </span>
                </MetaField>
              )}
              <MetaField label={t('photo.metadata.camera')} value={camera} />
              <MetaField label={t('photo.metadata.lens')} value={photo.lens_model} />
              <MetaField label={t('photo.metadata.aperture')} value={aperture} />
              <MetaField label={t('photo.metadata.exposure')} value={exposure} />
              <MetaField label={t('photo.metadata.focalLength')} value={focal} />
              <MetaField label={t('photo.metadata.iso')} value={iso} />
              <MetaField label={t('photo.technical.cameraSerial')} value={photo.camera_serial} />
              <MetaField label={t('photo.technical.software')} value={photo.software} />
              <MetaField label={t('photo.technical.takenAtSource')} value={sourceLabel} />
              <MetaField label={t('photo.technical.subject')} value={photo.subject} />
              {keywords.length > 0 && (
                <MetaField label={t('photo.technical.keywords')}>
                  <span className="d-flex gap-1 flex-wrap">
                    {keywords.map((keyword) => (
                      <Badge key={keyword} bg="secondary" className="fw-normal">
                        {keyword}
                      </Badge>
                    ))}
                  </span>
                </MetaField>
              )}
              <MetaField label={t('photo.technical.artist')} value={photo.artist} />
              <MetaField label={t('photo.technical.copyright')} value={photo.copyright} />
              <MetaField label={t('photo.technical.license')} value={photo.license} />
              <MetaField label={t('photo.technical.projection')} value={photo.projection} />
            </MetaGroup>
          )}

          {hasFileGroup && (
            <MetaGroup title={t('photo.technical.groups.file')}>
              <MetaField label={t('photo.metadata.fileName')} value={photo.file_name} />
              <MetaField label={t('photo.technical.originalName')} value={originalName} />
              <MetaField label={t('photo.technical.format')} value={format} />
              <MetaField
                label={t('photo.technical.fileSize')}
                value={size}
                title={formatByteCount(photo.file_size, locale)}
              />
              <MetaField label={t('photo.technical.dimensions')} value={dimensions} />
              <MetaField label={t('photo.technical.aspectRatio')} value={ratio} />
              <MetaField label={t('photo.technical.resolution')} value={resolution} />
              <MetaField label={t('photo.technical.orientation')} value={orientationLabel} />
              <MetaField label={t('photo.technical.colorProfile')} value={photo.color_profile} />
              <MetaField label={t('photo.technical.imageCodec')} value={photo.image_codec} />
              {photo.file_hash !== '' && (
                <MetaField label={t('photo.technical.fileHash')} title={photo.file_hash}>
                  <span className="d-inline-flex align-items-center gap-2">
                    <code className="text-break">{shortHash(photo.file_hash)}</code>
                    <Button
                      variant="link"
                      size="sm"
                      className="p-0 lh-1 text-decoration-none"
                      aria-label={t('photo.technical.copyHash')}
                      onClick={() => {
                        void copyHash()
                      }}
                    >
                      <Icon name={copied ? 'check-lg' : 'clipboard'} />
                    </Button>
                  </span>
                </MetaField>
              )}
              <MetaField
                label={t('photo.technical.createdAt')}
                value={formatDateTime(photo.created_at, locale)}
              />
              <MetaField
                label={t('photo.technical.updatedAt')}
                value={formatDateTime(photo.updated_at, locale)}
              />
            </MetaGroup>
          )}

          {hasLocationGroup && (
            <MetaGroup title={t('photo.technical.groups.location')}>
              <MetaField label={t('photo.metadata.coordinates')} value={coordinates} />
              <MetaField label={t('photo.technical.altitude')} value={altitude} />
              <MetaField label={t('photo.technical.country')} value={place?.country} />
              <MetaField label={t('photo.technical.region')} value={place?.region} />
              <MetaField label={t('photo.technical.city')} value={place?.city} />
              <MetaField label={t('photo.technical.placeName')} value={place?.place_name} />
            </MetaGroup>
          )}

          {hasVideoGroup && (
            <MetaGroup title={t('photo.technical.groups.video')}>
              <MetaField label={t('photo.technical.duration')} value={duration} />
              <MetaField label={t('photo.technical.videoCodec')} value={photo.video_codec} />
              <MetaField label={t('photo.technical.audioCodec')} value={photo.audio_codec} />
              <MetaField label={t('photo.technical.hasAudio')} value={audio} />
              <MetaField label={t('photo.technical.fps')} value={fps} />
            </MetaGroup>
          )}

          <MetaGroup title={t('photo.technical.groups.origin')}>
            <MetaField label={t('photo.metadata.uploadedBy')} value={uploader} />
            <MetaField
              label={t('photo.technical.photoprismUid')}
              value={photo.photoprism_uid}
              title={photo.photoprism_uid}
            />
            <MetaField
              label={t('photo.technical.photosorterUid')}
              value={photo.photosorter_uid}
              title={photo.photosorter_uid}
            />
          </MetaGroup>

          {canWrite && (
            <RegenerateThumbnailButton uid={photo.uid} onRegenerated={onThumbnailRegenerated} />
          )}
        </div>
      )}
    </section>
  )
}
