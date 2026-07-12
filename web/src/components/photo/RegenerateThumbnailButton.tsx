import { useState } from 'react'
import Button from 'react-bootstrap/Button'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'

import { ApiError } from '../../services/auth'
import { regenerateThumbnail } from '../../services/photos'
import { Icon } from '../Icon'

/** Props for {@link RegenerateThumbnailButton}. */
export interface RegenerateThumbnailButtonProps {
  /** UID of the photo whose thumbnail (and pHash) is rebuilt. */
  uid: string
  /**
   * Called after a successful regeneration so the parent can cache-bust the
   * displayed image — the derived thumbnail changed under a stable URL, so the
   * `<img>` must be told to refetch.
   */
  onRegenerated: () => void
}

/** The lifecycle of a regeneration request, driving the button and status line. */
type Status =
  | { kind: 'idle' }
  | { kind: 'pending' }
  | { kind: 'success' }
  | { kind: 'error'; message: string }

/**
 * A maintenance control that rebuilds a photo's thumbnail (and perceptual hashes)
 * from its original when the cached thumbnail is missing or stale. It is a service
 * action, not an organize control, so it belongs with the technical details and
 * is only rendered for editors/admins. While the request is in flight it shows a
 * spinner and disables itself; on success it reports the outcome and asks the
 * parent to reload the displayed image; on failure it explains why (a 422 means
 * the original is missing or cannot be decoded). It never touches the original.
 */
export function RegenerateThumbnailButton({ uid, onRegenerated }: RegenerateThumbnailButtonProps) {
  const { t } = useTranslation()
  const [status, setStatus] = useState<Status>({ kind: 'idle' })

  const run = async () => {
    setStatus({ kind: 'pending' })
    try {
      await regenerateThumbnail(uid)
      setStatus({ kind: 'success' })
      onRegenerated()
    } catch (err) {
      const message =
        err instanceof ApiError && err.status === 422
          ? t('photo.technical.regenerateThumbUndecodable')
          : t('photo.technical.regenerateThumbError')
      setStatus({ kind: 'error', message })
    }
  }

  const pending = status.kind === 'pending'
  return (
    <div className="mt-3">
      <Button
        variant="outline-secondary"
        size="sm"
        disabled={pending}
        onClick={() => {
          void run()
        }}
      >
        {pending ? (
          <Spinner animation="border" size="sm" className="me-1" aria-hidden="true" />
        ) : (
          <Icon name="arrow-clockwise" className="me-1" />
        )}
        {pending ? t('photo.technical.regeneratingThumb') : t('photo.technical.regenerateThumb')}
      </Button>
      {status.kind === 'success' && (
        <div className="text-success small mt-1" role="status">
          {t('photo.technical.regenerateThumbSuccess')}
        </div>
      )}
      {status.kind === 'error' && (
        <div className="text-danger small mt-1" role="alert">
          {status.message}
        </div>
      )}
    </div>
  )
}
