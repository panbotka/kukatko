import { useState } from 'react'
import Button from 'react-bootstrap/Button'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'

import { ApiError } from '../../services/auth'
import { downloadPhotosZip } from '../../services/photos'
import { Icon } from '../Icon'

/** Props for {@link DownloadZipButton}. */
export interface DownloadZipButtonProps {
  /** Explicit photo UIDs to pack (a library selection). */
  photoUids?: string[]
  /** An album UID to download whole (expanded to its live photos server-side). */
  albumUid?: string
  /** Base archive name without extension (e.g. an album title). */
  name?: string
  /** Extra disabling condition from the parent (e.g. an empty album). */
  disabled?: boolean
  /** Bootstrap button variant; defaults to a subtle outline suited to a toolbar. */
  variant?: string
}

/** The lifecycle of a ZIP download request, driving the button and status line. */
type Status = { kind: 'idle' } | { kind: 'pending' } | { kind: 'error'; message: string }

/**
 * A toolbar action that downloads a selection of photos — or a whole album — as a
 * single ZIP of originals. While the archive streams it shows a spinner and
 * disables itself; on failure it explains why (a 413 means the request was over
 * the per-download cap, anything else a generic failure). The browser download is
 * triggered inside the service, so this component only reflects pending/error
 * state. It disables itself when there is nothing to download.
 */
export function DownloadZipButton({
  photoUids,
  albumUid,
  name,
  disabled,
  variant,
}: DownloadZipButtonProps) {
  const { t } = useTranslation()
  const [status, setStatus] = useState<Status>({ kind: 'idle' })

  const run = async () => {
    setStatus({ kind: 'pending' })
    try {
      await downloadPhotosZip({ photoUids, albumUid, name })
      setStatus({ kind: 'idle' })
    } catch (err) {
      const message =
        err instanceof ApiError && err.status === 413
          ? t('download.zipTooMany')
          : t('download.zipError')
      setStatus({ kind: 'error', message })
    }
  }

  const pending = status.kind === 'pending'
  const nothingToDownload =
    albumUid === undefined && (photoUids === undefined || photoUids.length === 0)

  return (
    <>
      <Button
        variant={variant ?? 'outline-light'}
        size="sm"
        disabled={disabled === true || pending || nothingToDownload}
        onClick={() => {
          void run()
        }}
      >
        {pending ? (
          <Spinner animation="border" size="sm" className="me-1" aria-hidden="true" />
        ) : (
          <Icon name="box-arrow-in-down" className="me-1" />
        )}
        {pending ? t('download.zipPending') : t('download.zip')}
      </Button>
      {status.kind === 'error' && (
        <span className="text-danger small ms-2" role="alert">
          {status.message}
        </span>
      )}
    </>
  )
}
