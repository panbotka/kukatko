import { useCallback, useEffect, useRef, useState } from 'react'
import Button from 'react-bootstrap/Button'
import Dropdown from 'react-bootstrap/Dropdown'
import { useTranslation } from 'react-i18next'

import { useKeyboardShortcuts } from '../../hooks/useKeyboardShortcuts'
import { useStoryboard } from '../../hooks/useStoryboard'
import {
  DEFAULT_PLAYBACK_RATE,
  PLAYBACK_RATES,
  SKIP_SECONDS,
  formatPlaybackTime,
  readPlaybackRate,
  seekTarget,
  stepPlaybackRate,
  writePlaybackRate,
  type PlaybackRate,
} from '../../lib/videoPlayback'
import { videoUrl } from '../../services/photos'
import { Icon } from '../Icon'

import { VideoScrubber } from './VideoScrubber'

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
  /** Download token appended to the stream and sprite URLs for cookie-less contexts. */
  token?: string | null
}

/**
 * The video player on the photo detail page: an HTML5 `<video>` streamed from
 * the range-capable backend endpoint (so the browser can seek), with Kukátko's
 * own control bar over it — play/pause, ±10 s skips, a scrubbable timeline with
 * frame previews, a playback-speed menu, mute and fullscreen.
 *
 * The controls are ours rather than the browser's for one reason: the scrub
 * preview. A native timeline exposes no hover position, so the storyboard
 * thumbnails — the point of the feature — could not be placed against it.
 * Everything the native controls did is reimplemented here, and the `<video>`
 * stays a plain element so the browser's own decoding, buffering and Picture-in-
 * Picture keep working.
 *
 * **Keyboard.** `K` toggles playback, `J`/`L` skip ±10 s and `<`/`>` step the
 * speed — but only while the player is *active*: it contains the focused element,
 * or the clip is playing. The arrow keys are deliberately left alone; on this
 * page they page between photos, and a video that hijacked them would break
 * browsing to satisfy a control that has its own buttons. Click the player (or
 * tab into it) and the video keys are yours.
 *
 * **Touch.** Every control is a real button with the app's finger-sized tap
 * target, and the timeline is draggable. The hover preview is desktop-only: it
 * is driven by mouse pointer events, because a finger on the timeline covers the
 * very frame the preview would show.
 *
 * When the browser cannot decode the codec — and on-the-fly transcoding is off —
 * the player surfaces a download fallback so the user can still retrieve the file.
 */
export function VideoPlayer({ uid, title, poster, downloadHref, token }: VideoPlayerProps) {
  const { t } = useTranslation()
  const videoRef = useRef<HTMLVideoElement>(null)
  const containerRef = useRef<HTMLDivElement>(null)
  const [failed, setFailed] = useState(false)
  const [playing, setPlaying] = useState(false)
  const [muted, setMuted] = useState(false)
  const [position, setPosition] = useState(0)
  const [duration, setDuration] = useState(0)
  const [rate, setRate] = useState<PlaybackRate>(DEFAULT_PLAYBACK_RATE)
  const [focused, setFocused] = useState(false)
  // The storyboard is asked for only once playback has started: the request is
  // what schedules the render, so a video nobody watches never costs a decode.
  const [started, setStarted] = useState(false)
  const storyboard = useStoryboard(uid, started)

  // Restore the rate remembered for this session, and re-apply it whenever the
  // element is replaced (a new clip): `playbackRate` is element state, not React
  // state, so it resets to 1 with every fresh `<video>`.
  useEffect(() => {
    const remembered = readPlaybackRate()
    setRate(remembered)
    if (videoRef.current !== null) {
      videoRef.current.playbackRate = remembered
    }
  }, [uid])

  // Moving to another clip resets the transport UI: the old position and the
  // "has been played" flag describe a video that is no longer on screen.
  useEffect(() => {
    setPlaying(false)
    setStarted(false)
    setPosition(0)
    setDuration(0)
    setFailed(false)
  }, [uid])

  const applyRate = (next: PlaybackRate): void => {
    setRate(next)
    writePlaybackRate(next)
    if (videoRef.current !== null) {
      videoRef.current.playbackRate = next
    }
  }

  const togglePlay = useCallback((): void => {
    const video = videoRef.current
    if (video === null) {
      return
    }
    if (video.paused) {
      void video.play().catch(() => {
        // Autoplay policies can refuse; the button simply does nothing.
      })
      return
    }
    video.pause()
  }, [])

  const seekBy = useCallback((delta: number): void => {
    const video = videoRef.current
    if (video === null) {
      return
    }
    video.currentTime = seekTarget(video.currentTime, delta, video.duration)
  }, [])

  const seekTo = useCallback((seconds: number): void => {
    const video = videoRef.current
    if (video === null) {
      return
    }
    video.currentTime = seekTarget(seconds, 0, video.duration)
    setPosition(video.currentTime)
  }, [])

  const toggleMute = (): void => {
    const video = videoRef.current
    if (video === null) {
      return
    }
    video.muted = !video.muted
    setMuted(video.muted)
  }

  const toggleFullscreen = (): void => {
    const container = containerRef.current
    if (container === null) {
      return
    }
    if (document.fullscreenElement !== null) {
      void document.exitFullscreen().catch(() => undefined)
      return
    }
    void container.requestFullscreen().catch(() => undefined)
  }

  // Video shortcuts are scoped: they only fire while the player holds focus or
  // the clip is running, so `j`/`k`/`l` stay free for the rest of the page.
  useKeyboardShortcuts(
    {
      k: togglePlay,
      j: () => {
        seekBy(-SKIP_SECONDS)
      },
      l: () => {
        seekBy(SKIP_SECONDS)
      },
      '<': () => {
        applyRate(stepPlaybackRate(rate, -1))
      },
      '>': () => {
        applyRate(stepPlaybackRate(rate, 1))
      },
    },
    { enabled: focused || playing },
  )

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
    <div
      ref={containerRef}
      className="kk-video"
      // A focusable wrapper is what scopes the keyboard shortcuts: clicking the
      // picture or tabbing to a control puts focus inside it, and only then do
      // J/K/L answer.
      onFocus={() => {
        setFocused(true)
      }}
      onBlur={(event) => {
        if (!event.currentTarget.contains(event.relatedTarget)) {
          setFocused(false)
        }
      }}
    >
      <video
        ref={videoRef}
        playsInline
        preload="metadata"
        poster={poster}
        src={videoUrl(uid, token)}
        aria-label={`${t('photo.video.label')}: ${title}`}
        className="kk-video__media"
        onClick={togglePlay}
        onPlay={() => {
          setPlaying(true)
          setStarted(true)
        }}
        onPause={() => {
          setPlaying(false)
        }}
        onTimeUpdate={(event) => {
          setPosition(event.currentTarget.currentTime)
        }}
        onDurationChange={(event) => {
          setDuration(
            Number.isFinite(event.currentTarget.duration) ? event.currentTarget.duration : 0,
          )
        }}
        onVolumeChange={(event) => {
          setMuted(event.currentTarget.muted)
        }}
        onError={() => {
          setFailed(true)
        }}
      >
        {t('photo.video.unsupported')}
      </video>

      <div className="kk-video__controls">
        <VideoScrubber
          uid={uid}
          position={position}
          duration={duration}
          storyboard={storyboard}
          token={token}
          onSeek={seekTo}
        />
        <div className="kk-video__buttons">
          <button
            type="button"
            className="kk-video__button kukatko-tap-target"
            aria-label={playing ? t('photo.video.pause') : t('photo.video.play')}
            title={playing ? t('photo.video.pause') : t('photo.video.play')}
            onClick={togglePlay}
          >
            <Icon name={playing ? 'pause-fill' : 'play-fill'} />
          </button>
          <button
            type="button"
            className="kk-video__button kukatko-tap-target"
            aria-label={t('photo.video.skipBack', { seconds: SKIP_SECONDS })}
            title={t('photo.video.skipBack', { seconds: SKIP_SECONDS })}
            onClick={() => {
              seekBy(-SKIP_SECONDS)
            }}
          >
            <Icon name="skip-backward-fill" />
          </button>
          <button
            type="button"
            className="kk-video__button kukatko-tap-target"
            aria-label={t('photo.video.skipForward', { seconds: SKIP_SECONDS })}
            title={t('photo.video.skipForward', { seconds: SKIP_SECONDS })}
            onClick={() => {
              seekBy(SKIP_SECONDS)
            }}
          >
            <Icon name="skip-forward-fill" />
          </button>
          <span className="kk-video__time" aria-hidden="true">
            {formatPlaybackTime(position)} / {formatPlaybackTime(duration)}
          </span>
          <span className="flex-grow-1" />
          <Dropdown align="end" drop="up">
            <Dropdown.Toggle
              variant="link"
              size="sm"
              id={`video-speed-${uid}`}
              className="kk-video__speed kukatko-tap-target"
              aria-label={t('photo.video.speed')}
              title={t('photo.video.speed')}
            >
              {t('photo.video.rate', { rate })}
            </Dropdown.Toggle>
            <Dropdown.Menu>
              {PLAYBACK_RATES.map((option) => (
                <Dropdown.Item
                  key={option}
                  active={option === rate}
                  onClick={() => {
                    applyRate(option)
                  }}
                >
                  {t('photo.video.rate', { rate: option })}
                </Dropdown.Item>
              ))}
            </Dropdown.Menu>
          </Dropdown>
          <button
            type="button"
            className="kk-video__button kukatko-tap-target"
            aria-label={muted ? t('photo.video.unmute') : t('photo.video.mute')}
            title={muted ? t('photo.video.unmute') : t('photo.video.mute')}
            onClick={toggleMute}
          >
            <Icon name={muted ? 'volume-mute-fill' : 'volume-up-fill'} />
          </button>
          <button
            type="button"
            className="kk-video__button kukatko-tap-target"
            aria-label={t('photo.video.fullscreen')}
            title={t('photo.video.fullscreen')}
            onClick={toggleFullscreen}
          >
            <Icon name="arrows-fullscreen" />
          </button>
        </div>
      </div>
    </div>
  )
}
