import type { TFunction } from 'i18next'
import Button from 'react-bootstrap/Button'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'

import { type ShareError, type ShareStatus, usePhotoShare } from '../../hooks/usePhotoShare'
import { Icon } from '../Icon'

/** Props for {@link SharePhotosButton}. */
export interface SharePhotosButtonProps {
  /** The photos to hand over — a library selection, or a single photo's UID. */
  photoUids: string[]
  /** Extra disabling condition from the parent. */
  disabled?: boolean
  /** Bootstrap button variant; defaults to the outline suited to the batch bar. */
  variant?: string
}

/** The button's own label for each step of the sequence. */
function shareLabel(status: ShareStatus, t: TFunction): string {
  switch (status.kind) {
    case 'manifest':
      return t('sharePhotos.preparing')
    case 'fetching':
      // Originals over a mobile connection are not instant, so the count is the
      // difference between "it is working" and "it is stuck".
      return t('sharePhotos.fetching', { done: status.done, total: status.total })
    case 'sharing':
      return t('sharePhotos.handing')
    case 'waiting':
      return t('sharePhotos.nextBatch', { index: status.batch, total: status.batches })
    case 'idle':
      return t('sharePhotos.action')
  }
}

/** The reader-facing sentence for a share that did not (fully) happen. */
function shareMessage(error: ShareError, t: TFunction): string {
  switch (error.kind) {
    case 'tooMany':
      return t('sharePhotos.tooManyError')
    case 'fetch':
      return t('sharePhotos.fileError', { name: error.name, count: error.count })
    case 'sheet':
      return t('sharePhotos.sheetError')
    case 'manifest':
      return t('sharePhotos.error')
  }
}

/**
 * A toolbar action that hands the selected photos to the phone's own share sheet,
 * where iOS offers "Save Images" into Apple Photos and Android offers Google Photos
 * — the answer to "someone else uploaded these and I want mine in my phone".
 *
 * It renders **nothing at all** where the browser cannot do it (desktop Firefox,
 * Linux, an insecure origin): `navigator.canShare({files})` is asked with a real
 * probe file, and where it says no the ZIP download beside it stays the answer. A
 * button that throws on tap would be worse than no button.
 *
 * A selection too big for one handoff is shared in several, and each one needs its
 * own tap — a share sheet may only be opened from a fresh user gesture, so the label
 * turns into "Share batch 2 of 5" and waits. Everything about that sequence lives in
 * `usePhotoShare`; this component only says what it is doing.
 */
export function SharePhotosButton({ photoUids, disabled, variant }: SharePhotosButtonProps) {
  const { t } = useTranslation()
  const { supported, status, error, busy, share } = usePhotoShare(photoUids)

  if (!supported) {
    return null
  }

  const multiBatch = (status.kind === 'waiting' || status.kind === 'fetching') && status.batches > 1
  return (
    <>
      <Button
        variant={variant ?? 'outline-light'}
        size="sm"
        disabled={disabled === true || busy || photoUids.length === 0}
        onClick={share}
      >
        {busy ? (
          <Spinner animation="border" size="sm" className="me-1" aria-hidden="true" />
        ) : (
          <Icon name="share" className="me-1" />
        )}
        {shareLabel(status, t)}
      </Button>
      {multiBatch && (
        <span className="small ms-2" aria-live="polite">
          {t('sharePhotos.progress', { done: status.batch - 1, total: status.batches })}
        </span>
      )}
      {error !== null && (
        <span className="text-danger small ms-2" role="alert">
          {shareMessage(error, t)}
        </span>
      )}
    </>
  )
}
