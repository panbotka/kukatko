import { useEffect, useState } from 'react'

import { type QueueItemStatus } from '../../hooks/useUploadQueue'
import { previewKind } from '../../lib/mediaFiles'
import { Icon, type IconName } from '../Icon'

/** Props for {@link UploadThumb}. */
export interface UploadThumbProps {
  /** The picked file, previewed straight from the browser's own copy of it. */
  file: File
  /** The row's lifecycle, which tints the frame and draws the overlay. */
  status: QueueItemStatus
  /** Upload progress in `[0, 1]`, shown over the picture while it is in flight. */
  progress: number
}

/** The placeholder glyph for a file no `<img>` can paint. */
function placeholderIcon(file: File): IconName {
  return previewKind(file) === 'video' ? 'play-fill' : 'image'
}

/**
 * The local preview of one queued file: a square frame holding the picture the
 * browser makes of the `File` itself — no upload, no server, no round trip — so
 * a batch reads as photos rather than as a column of file names.
 *
 * Only what an `<img>` can paint is loaded (`lib/mediaFiles` `previewKind`); a
 * video shows a play glyph, and HEIC/RAW — which no browser decodes — a picture
 * glyph, instead of the broken-image icon a hopeful `<img>` would end at. A file
 * that lies about itself is caught by `onError` and falls back to the same
 * placeholder.
 *
 * The object URL is created on mount and **revoked on unmount**: the list is
 * virtualized, so only the rows on screen hold one, and clearing the queue (or
 * removing a file) unmounts the row and hands the memory back. Nothing else in
 * the app may hold these URLs — a queue of hundreds would otherwise pin every
 * picked file for as long as the tab lives.
 *
 * The frame itself carries the state as colour (`.kk-upload-thumb--<status>`),
 * with the live percentage over an in-flight file and a warning glyph over a
 * failed one, so a long queue can be skimmed without reading a single badge. It
 * is all decorative: the row's badge states the same thing in words.
 */
export function UploadThumb({ file, status, progress }: UploadThumbProps) {
  const [url, setUrl] = useState<string | null>(null)
  const [broken, setBroken] = useState(false)
  const kind = previewKind(file)

  useEffect(() => {
    if (kind !== 'image') {
      return
    }
    const objectUrl = URL.createObjectURL(file)
    setBroken(false)
    setUrl(objectUrl)
    return () => {
      URL.revokeObjectURL(objectUrl)
      setUrl(null)
    }
  }, [file, kind])

  const percent = Math.round(progress * 100)

  return (
    <div className={`kk-upload-thumb kk-upload-thumb--${status}`} data-testid="upload-thumb">
      {url !== null && !broken ? (
        <img
          src={url}
          alt=""
          className="kk-upload-thumb__img"
          // A phone decoding a 12 Mpx camera original for a 72px square must not
          // do it on the main thread while the queue is being scrolled.
          decoding="async"
          onError={() => {
            setBroken(true)
          }}
        />
      ) : (
        <Icon name={placeholderIcon(file)} className="kk-upload-thumb__icon" />
      )}

      {status === 'uploading' && (
        <span className="kk-upload-thumb__overlay" aria-hidden="true">
          {percent}%
        </span>
      )}
      {status === 'error' && (
        <span className="kk-upload-thumb__overlay" aria-hidden="true">
          <Icon name="exclamation-triangle" />
        </span>
      )}
    </div>
  )
}
