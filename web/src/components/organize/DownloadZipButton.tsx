import Button from 'react-bootstrap/Button'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'

import { useZipDownload } from '../../hooks/useZipDownload'
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

/**
 * A toolbar action that downloads a selection of photos — or a whole album — as a
 * single ZIP of originals. While the archive streams it shows a spinner and
 * disables itself; on failure it explains why (see `useZipDownload`). It disables
 * itself when there is nothing to download.
 */
export function DownloadZipButton({
  photoUids,
  albumUid,
  name,
  disabled,
  variant,
}: DownloadZipButtonProps) {
  const { t } = useTranslation()
  const { run, pending, error, empty } = useZipDownload({ photoUids, albumUid, name })

  return (
    <>
      <Button
        variant={variant ?? 'outline-light'}
        size="sm"
        disabled={disabled === true || pending || empty}
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
      {error !== null && (
        <span className="text-danger small ms-2" role="alert">
          {error}
        </span>
      )}
    </>
  )
}
