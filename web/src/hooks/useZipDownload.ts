import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ApiError } from '../services/auth'
import { downloadPhotosZip } from '../services/photos'

/** What to pack: explicit photos (a selection) or a whole album. */
export interface ZipDownloadTarget {
  /** Explicit photo UIDs to pack (a library selection). */
  photoUids?: string[]
  /** An album UID to download whole (expanded to its live photos server-side). */
  albumUid?: string
  /** Base archive name without extension (e.g. an album title). */
  name?: string
}

/** The state of a ZIP download and the function that starts one. */
export interface ZipDownload {
  /** Starts the download; never rejects — a failure lands in `error`. */
  run: () => Promise<void>
  /** The archive is being built and streamed. */
  pending: boolean
  /** Why the last attempt failed, ready to show; `null` when it did not. */
  error: string | null
  /** Nothing to pack: no album and an empty selection. */
  empty: boolean
}

/**
 * The lifecycle of a ZIP-of-originals download, apart from any control: the
 * toolbar's `DownloadZipButton` and the album header's "…" menu item both drive
 * it, the latter showing the failure outside a menu that has closed by then.
 * A 413 means the request was over the per-download cap, anything else is a
 * generic failure. The browser download itself is triggered by the service.
 */
export function useZipDownload({ photoUids, albumUid, name }: ZipDownloadTarget): ZipDownload {
  const { t } = useTranslation()
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const run = async () => {
    setPending(true)
    setError(null)
    try {
      await downloadPhotosZip({ photoUids, albumUid, name })
    } catch (err) {
      setError(
        err instanceof ApiError && err.status === 413
          ? t('download.zipTooMany')
          : t('download.zipError'),
      )
    } finally {
      setPending(false)
    }
  }

  const empty = albumUid === undefined && (photoUids === undefined || photoUids.length === 0)
  return { run, pending, error, empty }
}
