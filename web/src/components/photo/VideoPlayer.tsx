import {
  type KeyboardEvent as ReactKeyboardEvent,
  type ReactNode,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from 'react'
import Button from 'react-bootstrap/Button'
import Dropdown from 'react-bootstrap/Dropdown'
import { useTranslation } from 'react-i18next'

import { useHlsPlayback } from '../../hooks/useHlsPlayback'
import { useKeyboardShortcuts, type ShortcutMap } from '../../hooks/useKeyboardShortcuts'
import { useStoryboard } from '../../hooks/useStoryboard'
import { containedRect, type PaintedRect } from '../../lib/faceGeometry'
import { isTypingElement } from '../../lib/ratingHotkeys'
import { shortcutToken } from '../../lib/shortcuts'
import { encodeInProgress, videoFallback } from '../../lib/videoEncode'
import {
  ARROW_SECONDS,
  DEFAULT_PLAYBACK_RATE,
  FRAME_SECONDS,
  PLAYBACK_RATES,
  SKIP_SECONDS,
  formatPlaybackTime,
  readPlaybackRate,
  seekTarget,
  stepPlaybackRate,
  tenthPosition,
  writePlaybackRate,
  type PlaybackRate,
} from '../../lib/videoPlayback'
import { type PhotoProcessing } from '../../services/photos'
import { Icon } from '../Icon'

import { VideoScrubber } from './VideoScrubber'

/**
 * What the player currently claims of the keyboard, reported to the page around
 * it so the two never act on one press. The page owns the same letters, arrows
 * and digits for browsing, favouriting and rating; the player owns them only
 * while it is the thing the reader is using.
 */
export interface VideoKeyboardScope {
  /**
   * The player owns the video keys right now: it holds the focus, the clip is
   * playing, or it is filling the screen. The page must stand aside for the keys
   * they share (the arrows, `f`, `m`, the digits) while this is true.
   */
  readonly active: boolean
  /**
   * The player is what fullscreen is showing, so Escape belongs to leaving it —
   * and the viewer around it must not read the same press as "close the photo".
   */
  readonly fullscreen: boolean
}

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
  /**
   * Whether the photo has an encoded HLS rendition (`photo.hls`). When it has,
   * the clip is streamed — natively or through hls.js; when it has not, the
   * original file is played from the range endpoint as it always was.
   */
  streaming?: boolean
  /**
   * Where the clip's streaming encode stands — the `hls_transcode` row of the
   * photo's processing report, or `null`/absent when the detail carries no
   * report at all. It never gates playback; it is what turns the dead end the
   * player used to show into "this is still being prepared, come back in a
   * moment", and what puts a quiet note under a clip playing progressively
   * while a better version is on its way.
   */
  encode?: PhotoProcessing | null
  /**
   * Drawn over the **poster frame**, in a layer that is exactly the rectangle the
   * poster paints in — the face boxes of the clip, whose detection ran on that
   * one frame. It is mounted only while the poster is what is on screen, i.e.
   * until the clip is first played; see {@link VideoPlayer} for why.
   */
  overlay?: ReactNode
  /**
   * Called whenever what the player claims of the keyboard changes, and once
   * with everything cleared when it unmounts. The page uses it to hand over the
   * keys the two share; see {@link VideoKeyboardScope}.
   */
  onKeyboardScope?: (scope: VideoKeyboardScope) => void
  /**
   * The picture's aspect ratio (width ÷ height) to place {@link overlay} against
   * until the element reports its own — the catalogue row's dimensions, which
   * hold the layer still for the moment before the metadata arrives. Ignored
   * once the element knows better.
   */
  posterRatio?: number
}

/**
 * The video player on the photo detail page: an HTML5 `<video>` with Kukátko's
 * own control bar over it — play/pause, ±10 s skips, a scrubbable timeline with
 * frame previews, a playback-speed menu, mute and fullscreen.
 *
 * **Where the bytes come from** is `useHlsPlayback`'s business, not this
 * component's. A clip with an encoded rendition is streamed — the browser demuxes
 * the playlist itself where it can, otherwise hls.js is lazily loaded and attached
 * to this very element — and everything else is played from the range-capable
 * `/video` endpoint, exactly as before. The element stays a plain `<video>` either
 * way, so the controls, the scrub preview, the shortcuts and Picture-in-Picture
 * neither know nor care. The download link always points at the original file: the
 * renditions exist to be played, not to be kept.
 *
 * The controls are ours rather than the browser's for one reason: the scrub
 * preview. A native timeline exposes no hover position, so the storyboard
 * thumbnails — the point of the feature — could not be placed against it.
 * Everything the native controls did is reimplemented here, and the `<video>`
 * stays a plain element so the browser's own decoding, buffering and Picture-in-
 * Picture keep working.
 *
 * **Keyboard.** The set everyone already knows from the big players: Space and
 * `K` play/pause, the arrows seek ±5 s and `J`/`L` ±10 s, `0`–`9` open the clip
 * at that tenth of it, `M` mutes, `F` fills the screen, `<`/`>` step the speed
 * and `,`/`.` nudge a paused clip by about a frame. Every one of them is scoped
 * rather than claimed: they answer only while the player is *active* — it
 * contains the focused element, the clip is playing, or it is fullscreen — and
 * at every other moment they keep the meaning the page gives them (the arrows
 * page between photos, `f` favourites, `m` shows the faces, the digits award
 * stars). The page is told which of the two states it is in through
 * `onKeyboardScope`; without that the two would both act on one press.
 *
 * Two keys need more than a map entry. **Space** is handled on the container
 * because the shared hook hands it to a focused button, and after a click on
 * Play that button holds the focus — one press has to be one toggle, not a
 * second activation and not a scroll. **Escape** is bound only while fullscreen,
 * where it means "leave fullscreen"; outside it the key stays the viewer's way
 * back out.
 *
 * **Touch.** Every control is a real button with the app's finger-sized tap
 * target, and the timeline is draggable. The hover preview is desktop-only: it
 * is driven by mouse pointer events, because a finger on the timeline covers the
 * very frame the preview would show.
 *
 * **The poster carries the faces.** Face detection on a clip only ever looks at
 * the poster frame, so that one frame is where its boxes belong — and the player
 * hands an `overlay` a layer that is exactly the rectangle the poster paints in,
 * bars excluded. The layer stands down the moment the clip is first played: from
 * then on the element shows the video, not the poster, and boxes measured on the
 * poster would sit on the wrong picture. It never comes back for that clip;
 * turning the faces view off and on again is not what the reader wants mid-clip,
 * and stepping to another video mounts a fresh element with its own poster.
 *
 * **When the browser cannot decode the codec** the player says what that means
 * *for this clip*, which the streaming encode's state decides: an encode still
 * queued or running makes it a wait ("being prepared for playback, come back in
 * a moment"), a failed one names the error and points at the Processing panel,
 * and only a clip nothing more will happen to keeps the old "cannot be played,
 * download it instead". The download link is offered in every one of those. The
 * same fact, while the clip *does* play progressively, is a single quiet line
 * under the control bar — a better version is coming, nothing to do about it.
 */
export function VideoPlayer({
  uid,
  title,
  poster,
  downloadHref,
  token,
  streaming = false,
  encode = null,
  overlay,
  onKeyboardScope,
  posterRatio,
}: VideoPlayerProps) {
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
  // Whether this player is what the screen is filled with. It is read from the
  // document rather than from our own request, because fullscreen can be left in
  // ways this component never hears about — the browser's own Escape among them.
  const [fullscreen, setFullscreen] = useState(false)
  // The storyboard is asked for only once playback has started: the request is
  // what schedules the render, so a video nobody watches never costs a decode.
  const [started, setStarted] = useState(false)
  const storyboard = useStoryboard(uid, started)
  // The picture's own aspect ratio, as the element reports it once it has read
  // the clip's metadata. It is the *video's*, which is the poster's too — the
  // poster is a frame of that clip — and it beats the caller's estimate, which
  // comes from a catalogue row that can be wrong. Until then the estimate stands.
  const [frameRatio, setFrameRatio] = useState<number | null>(null)
  // Where the poster actually paints inside the element, relative to the player
  // — the box the overlay sits on. Null while there is nothing to draw.
  const [posterRect, setPosterRect] = useState<PaintedRect | null>(null)
  // The overlay belongs to the poster, so it stands down the moment the clip has
  // been played: from then on the element paints a frame of the video, and boxes
  // measured on the poster would be boxes on the wrong picture.
  const showOverlay = overlay !== undefined && !started

  // Measure the painted poster. The element is sized by the stage, the poster is
  // fitted into it (`object-fit: contain`), and the leftover is black bars — so a
  // layer that simply covered the element would put every box off its face by the
  // width of one bar.
  useEffect(() => {
    const video = videoRef.current
    if (!showOverlay || video === null) {
      setPosterRect(null)
      return
    }
    const measure = (): void => {
      const painted = containedRect(
        { width: video.offsetWidth, height: video.offsetHeight },
        frameRatio ?? posterRatio ?? 0,
      )
      // Relative to `.kk-video`, which is the element's offset parent and the
      // layer's own containing block.
      const next: PaintedRect = {
        left: video.offsetLeft + painted.left,
        top: video.offsetTop + painted.top,
        width: painted.width,
        height: painted.height,
      }
      setPosterRect((current) =>
        current !== null &&
        current.left === next.left &&
        current.top === next.top &&
        current.width === next.width &&
        current.height === next.height
          ? current
          : next,
      )
    }
    measure()
    const observer = typeof ResizeObserver === 'function' ? new ResizeObserver(measure) : null
    observer?.observe(video)
    // Without a ResizeObserver the window's own resize is the only signal there
    // is — better than a layer frozen at mount.
    if (observer === null) {
      window.addEventListener('resize', measure)
    }
    return () => {
      observer?.disconnect()
      if (observer === null) {
        window.removeEventListener('resize', measure)
      }
    }
  }, [showOverlay, frameRatio, posterRatio, uid])

  // A fatal streaming error is the same dead end as an undecodable codec: the
  // element will not play this clip, so the player says so and offers the file.
  const failPlayback = useCallback((): void => {
    setFailed(true)
  }, [])
  // Where the bytes come from — a playlist the browser demuxes itself, hls.js
  // attached to this very element, or the original file. Everything below this
  // line is written as if there were only ever one plain `<video>`.
  const { src } = useHlsPlayback({ videoRef, uid, streaming, token, onFatalError: failPlayback })

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
    setFrameRatio(null)
  }, [uid])

  // Follow fullscreen rather than assume it: the browser's own Escape, the F11
  // key and another element going fullscreen all change this without asking.
  useEffect(() => {
    const sync = (): void => {
      setFullscreen(containerRef.current?.contains(document.fullscreenElement) === true)
    }
    sync()
    document.addEventListener('fullscreenchange', sync)
    return () => {
      document.removeEventListener('fullscreenchange', sync)
    }
  }, [])

  // The player owns the shared keys while the reader is using it: it holds the
  // focus, the clip is running, or it is filling the screen. Everywhere else
  // those keys mean what they have always meant on the page around it.
  const active = focused || playing || fullscreen

  // Report the scope up so the page can stand aside for exactly these keys. Held
  // in a ref so a caller that re-creates the callback every render does not turn
  // this into a render loop — and so the unmount below can still reach it.
  const scopeRef = useRef(onKeyboardScope)
  scopeRef.current = onKeyboardScope
  useEffect(() => {
    scopeRef.current?.({ active, fullscreen })
  }, [active, fullscreen])
  useEffect(
    () => () => {
      // A player that is gone claims nothing: paging to a still photo must give
      // the arrows and the digits straight back to the page.
      scopeRef.current?.({ active: false, fullscreen: false })
    },
    [],
  )

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

  // A frame step is a paused-only act: nudging a running clip by 1/25 s is
  // invisible, and the reader who reached for `,` wanted to look at one picture.
  const stepFrame = useCallback((direction: 1 | -1): void => {
    const video = videoRef.current
    if (video?.paused !== true) {
      return
    }
    video.currentTime = seekTarget(video.currentTime, direction * FRAME_SECONDS, video.duration)
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
    // The tracked state, not a fresh read of the document: it is the same fact,
    // and it is the one the Escape binding and the reported scope agree with.
    if (fullscreen) {
      void document.exitFullscreen().catch(() => undefined)
      return
    }
    void container.requestFullscreen().catch(() => undefined)
  }

  // `0`–`9` open the clip at that tenth of its length. Built once per seek
  // closure rather than spelled out ten times, and kept out of the map literal
  // below so the transport keys stay readable.
  const tenthShortcuts = useMemo<ShortcutMap>(
    () =>
      Object.fromEntries(
        Array.from({ length: 10 }, (_unused, digit) => [
          String(digit),
          () => {
            const video = videoRef.current
            if (video !== null) {
              seekTo(tenthPosition(digit, video.duration))
            }
          },
        ]),
      ),
    [seekTo],
  )

  // The player's keys, and only while it is the thing being used: focused,
  // playing, or filling the screen. Outside that scope every one of them is the
  // page's — the arrows page between photos, `f` favourites, `m` shows the faces
  // and the digits award stars — and the page is what stands aside, not us.
  useKeyboardShortcuts(
    {
      ...tenthShortcuts,
      ' ': (event) => {
        // A press with the focus inside the player was already dealt with on the
        // container (see `onKeyDown` there); this is the one arriving from the
        // page while the clip simply plays.
        if (event.target instanceof Node && containerRef.current?.contains(event.target) === true) {
          return
        }
        togglePlay()
      },
      k: togglePlay,
      ArrowLeft: () => {
        seekBy(-ARROW_SECONDS)
      },
      ArrowRight: () => {
        seekBy(ARROW_SECONDS)
      },
      j: () => {
        seekBy(-SKIP_SECONDS)
      },
      l: () => {
        seekBy(SKIP_SECONDS)
      },
      m: toggleMute,
      f: toggleFullscreen,
      '<': () => {
        applyRate(stepPlaybackRate(rate, -1))
      },
      '>': () => {
        applyRate(stepPlaybackRate(rate, 1))
      },
      ',': () => {
        stepFrame(-1)
      },
      '.': () => {
        stepFrame(1)
      },
      // Escape is bound only while fullscreen, because outside it the key is the
      // viewer's way back out and a player that swallowed it would trap the
      // reader on a photo.
      ...(fullscreen
        ? {
            Escape: () => {
              void document.exitFullscreen().catch(() => undefined)
            },
          }
        : {}),
    },
    { enabled: active },
  )

  /**
   * The space bar, for every press whose focus is inside the player. It cannot
   * be left to the shared hook: that hook deliberately hands Space to a focused
   * button, and after a click on Play the Play button *is* the focused button —
   * so the press would re-activate it rather than reach a shortcut. Handling it
   * here, with the default prevented, turns one press into exactly one toggle,
   * never two and never a scroll of the page behind the viewer.
   */
  const onContainerKeyDown = (event: ReactKeyboardEvent<HTMLDivElement>): void => {
    if (shortcutToken(event.key) !== ' ') {
      return
    }
    if (event.ctrlKey || event.metaKey || event.altKey || isTypingElement(event.target)) {
      return
    }
    event.preventDefault()
    togglePlay()
  }

  // The clip has no rendition yet and one is on its way. Progressive playback
  // carries on — the note is a note, never a gate — but it explains why seeking
  // is coarse and says a smoother version is coming.
  const preparing = !streaming && encodeInProgress(encode)

  if (failed) {
    // The browser will not play this clip. Whether that is a dead end or a wait
    // is the encode's business, not the element's: a rendition still owed means
    // this very clip becomes playable on its own in a few minutes.
    const fallback = videoFallback(encode)
    return (
      <div className="d-flex flex-column align-items-center justify-content-center text-light p-4 gap-2">
        <p className="mb-0 text-center">{t(`photo.video.${fallback}`)}</p>
        {fallback === 'preparationFailed' && (
          <>
            {encode?.error !== undefined && encode.error !== '' && (
              <p className="small text-center text-break mb-0">{encode.error}</p>
            )}
            <p className="small text-center mb-0">{t('photo.video.preparationFailedHint')}</p>
          </>
        )}
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
      onKeyDown={onContainerKeyDown}
    >
      <video
        ref={videoRef}
        playsInline
        preload="metadata"
        poster={poster}
        src={src}
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
        onLoadedMetadata={(event) => {
          const { videoWidth, videoHeight } = event.currentTarget
          setFrameRatio(videoWidth > 0 && videoHeight > 0 ? videoWidth / videoHeight : null)
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

      {showOverlay && posterRect !== null && (
        /* Exactly the painted poster, so a layer positioning its children in
           percentages of it lands them on the picture rather than on the bars. */
        <div
          className="kk-video__overlay"
          style={{
            left: `${String(posterRect.left)}px`,
            top: `${String(posterRect.top)}px`,
            width: `${String(posterRect.width)}px`,
            height: `${String(posterRect.height)}px`,
          }}
        >
          {overlay}
        </div>
      )}

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

      {preparing && (
        /* One quiet line under the bar, never a dialog: the clip is playing, and
           this only says why it plays the way it does. */
        <p className="kk-video__notice mb-0" role="status">
          <Icon name="clock-history" className="me-1" aria-hidden="true" />
          {t('photo.video.preparingHint')}
        </p>
      )}
    </div>
  )
}
