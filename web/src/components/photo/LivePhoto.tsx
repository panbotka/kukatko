import { useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { videoUrl } from '../../services/photos'

/** Props for {@link LivePhoto}. */
export interface LivePhotoProps {
  /** UID of the live photo whose motion clip is played on demand. */
  uid: string
  /** Accessible label / title for the live photo. */
  title: string
  /** Poster image (the still) shown when the motion clip is not playing. */
  poster: string
  /** Download token appended to the stream URL for cookie-less contexts. */
  token?: string | null
}

/**
 * A live photo on the detail page: the still image with a "Live" badge that
 * plays its short motion clip while the user hovers (desktop), presses and holds
 * (touch), or keyboard-focuses it. Releasing returns to the still. The clip is
 * muted and looped so it behaves like Apple/Google live photos. The motion video
 * sits over the still and cross-fades in, so there is no layout shift.
 */
export function LivePhoto({ uid, title, poster, token }: LivePhotoProps) {
  const { t } = useTranslation()
  const videoRef = useRef<HTMLVideoElement>(null)
  const [playing, setPlaying] = useState(false)

  const start = () => {
    const video = videoRef.current
    if (video === null) {
      return
    }
    video.currentTime = 0
    void video.play().then(
      () => {
        setPlaying(true)
      },
      () => {
        // Autoplay can be rejected (e.g. before any user gesture); stay on the
        // still rather than surfacing an error for a best-effort preview.
        setPlaying(false)
      },
    )
  }

  const stop = () => {
    const video = videoRef.current
    if (video !== null) {
      video.pause()
    }
    setPlaying(false)
  }

  return (
    <div
      className="position-relative d-inline-block"
      role="button"
      tabIndex={0}
      aria-label={`${t('photo.video.live')}: ${title}. ${t('photo.video.livePlay')}`}
      onMouseEnter={start}
      onMouseLeave={stop}
      onTouchStart={start}
      onTouchEnd={stop}
      onFocus={start}
      onBlur={stop}
    >
      <img
        src={poster}
        alt={title}
        className="mw-100 d-block"
        style={{ maxHeight: '70vh', objectFit: 'contain', opacity: playing ? 0 : 1 }}
      />
      <video
        ref={videoRef}
        src={videoUrl(uid, token)}
        muted
        loop
        playsInline
        preload="metadata"
        aria-hidden="true"
        className="position-absolute top-0 start-0 w-100 h-100"
        style={{
          objectFit: 'contain',
          opacity: playing ? 1 : 0,
          transition: 'opacity var(--kk-duration-fast) var(--kk-ease-standard)',
        }}
      />
      <span className="position-absolute top-0 start-0 m-1 badge text-bg-dark opacity-75">
        {t('photo.video.live')}
      </span>
    </div>
  )
}
