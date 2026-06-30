import { useState } from 'react'
import Button from 'react-bootstrap/Button'
import { useTranslation } from 'react-i18next'

import { videoUrl } from '../../services/photos'

/** Props for {@link VideoPlayer}. */
export interface VideoPlayerProps {
  /** UID of the photo whose video is streamed. */
  uid: string
  /** Accessible label / title for the player. */
  title: string
  /** Poster image URL shown before playback starts. */
  poster: string
  /** URL to download the original video as a fallback when it cannot be played. */
  downloadHref: string
  /** Download token appended to the stream URL for cookie-less contexts. */
  token?: string | null
}

/**
 * An HTML5 video player for the photo detail page. It streams the video from the
 * range-capable backend endpoint (so the browser can seek), shows the photo's
 * poster until playback starts, and exposes the native controls (play/pause,
 * seek, volume, fullscreen, keyboard and touch). When the browser cannot decode
 * the codec — and on-the-fly transcoding is off — the player surfaces a download
 * fallback so the user can still retrieve the file.
 */
export function VideoPlayer({ uid, title, poster, downloadHref, token }: VideoPlayerProps) {
  const { t } = useTranslation()
  const [failed, setFailed] = useState(false)

  if (failed) {
    return (
      <div className="d-flex flex-column align-items-center justify-content-center text-light p-4 gap-2">
        <p className="mb-0 text-center">{t('photo.video.unsupported')}</p>
        <Button as="a" href={downloadHref} variant="light" size="sm" download>
          {t('photo.video.downloadInstead')}
        </Button>
      </div>
    )
  }

  return (
    <video
      controls
      playsInline
      preload="metadata"
      poster={poster}
      src={videoUrl(uid, token)}
      aria-label={`${t('photo.video.label')}: ${title}`}
      className="mw-100"
      style={{ maxHeight: '70vh' }}
      onError={() => {
        setFailed(true)
      }}
    >
      {t('photo.video.unsupported')}
    </video>
  )
}
