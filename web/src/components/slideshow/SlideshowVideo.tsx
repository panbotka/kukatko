import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { useHlsPlayback } from '../../hooks/useHlsPlayback'
import { formatDuration } from '../../lib/format'
import { type Photo } from '../../services/photos'

/**
 * How long one video slide may hold the show, however long the clip is.
 *
 * A slideshow is a sequence somebody is watching to the end; a twenty-minute
 * recording of a school play is not a slide, and left to run it would turn the
 * show into that one clip. Cutting every clip short would be the opposite
 * mistake — the ordinary family video is under a minute and deserves to be seen
 * whole. Half a minute is where those two meet: long enough that nearly every
 * clip plays out and ends the slide itself, short enough that the one long
 * recording is a pause in the show rather than the end of it.
 */
export const MAX_VIDEO_SLIDE_MS = 30_000

/**
 * How long the slide waits for playback to actually start before giving up on
 * it. Not every clip can be played by every browser — a codec it cannot decode,
 * an autoplay policy that refuses, a file that will not buffer — and the failure
 * that matters is the silent one: a show that stops dead on the poster of a bad
 * file. A few seconds is long enough for a clip that is merely slow to start,
 * and short enough that a clip which never will is only a slightly long slide.
 */
export const PLAYBACK_GRACE_MS = 5_000

/**
 * Where a video slide is in its life: waiting for playback to begin, playing,
 * finished (the clip ended or {@link MAX_VIDEO_SLIDE_MS} ran out), or unplayable
 * — which is not an error to report but a slide that reverts to being the
 * poster, held for as long as any photograph would be.
 */
type SlideState = 'starting' | 'playing' | 'over' | 'failed'

/** Props for {@link SlideshowVideo}. */
export interface SlideshowVideoProps {
  /** The clip being shown. */
  photo: Photo
  /** The poster frame — the same rendition the stage would paint as a still. */
  poster: string
  /** Whether the show is running: a paused show pauses the clip. */
  playing: boolean
  /**
   * The ordinary photo interval. It times this slide only when the clip cannot
   * be played: the poster is then held exactly as the still it has become.
   */
  intervalMs: number
  /** The slide is over — advance the show (auto-advance, not a manual next). */
  onEnded: () => void
  /** Classes for the element itself, so the stage's transition applies to it. */
  className?: string
}

/**
 * A timeout that runs only while `active`, keeping what is left of its budget
 * across the pauses in between: a slide paused halfway through its cap resumes
 * with half of it left, rather than starting the wait over or firing the moment
 * it is unpaused. Its budget is read once — the deadlines here are decided when
 * the slide begins, and a slide is short.
 *
 * @param active Whether the clock should be running right now.
 * @param budgetMs The total time the timeout is allowed to spend running.
 * @param onExpire Called once the budget is exhausted.
 */
function useBudgetedTimeout(active: boolean, budgetMs: number, onExpire: () => void): void {
  const remaining = useRef(budgetMs)
  const expire = useRef(onExpire)
  expire.current = onExpire

  useEffect(() => {
    if (!active) {
      return undefined
    }
    const startedAt = Date.now()
    const id = window.setTimeout(() => {
      remaining.current = 0
      expire.current()
    }, remaining.current)
    return () => {
      window.clearTimeout(id)
      remaining.current = Math.max(0, remaining.current - (Date.now() - startedAt))
    }
  }, [active])
}

/**
 * One video slide of the slideshow: the clip itself, playing.
 *
 * The stage used to paint a video as its poster frame and move on when the photo
 * interval elapsed, which showed a family's home videos as a row of motionless
 * pictures. Here the clip plays — muted, from the beginning, full-bleed on the
 * same stage — and the show advances when it *ends* rather than when the interval
 * does.
 *
 * **Deliberately not the viewer's player.** `VideoPlayer` is a control bar, a
 * scrub preview, a speed menu and a keyboard map; a slide has a reader sitting
 * back watching, and the slideshow's own controls are the ones on screen. What is
 * shared is the part that matters — {@link useHlsPlayback}, so a clip with an
 * encoded rendition streams here exactly as it does in the viewer, and everything
 * else plays progressively. (A listing does not report `hls`, so in practice a
 * slide plays progressively today and streams the moment a payload says it can.)
 *
 * **Muted, always.** A slideshow that suddenly makes noise is worse than a silent
 * one, and a browser would refuse to autoplay it anyway. There is no sound
 * control here for the same reason there is no scrubber: it is not what this
 * screen is for.
 *
 * **Nothing may stall the show.** Three bounded clocks see to that, and all three
 * stop while the show is paused, so a paused slide is genuinely paused rather
 * than quietly running out: playback must *start* within
 * {@link PLAYBACK_GRACE_MS}, no clip may hold the slide longer than
 * {@link MAX_VIDEO_SLIDE_MS}, and a clip that cannot be played at all falls back
 * to being its poster for one ordinary photo interval. Every one of those ends in
 * the same place — `onEnded`, the auto-advance the interval would have made.
 *
 * The slide says what it is while it is still a picture: the play mark and the
 * clip's length, the very badge the library tile carries, until the moment
 * playback starts and the picture says it for itself.
 */
export function SlideshowVideo({
  photo,
  poster,
  playing,
  intervalMs,
  onEnded,
  className,
}: SlideshowVideoProps) {
  const { t } = useTranslation()
  const videoRef = useRef<HTMLVideoElement>(null)
  const [state, setState] = useState<SlideState>('starting')

  // A fatal streaming error is the same answer as a codec the browser refuses:
  // this clip is not going to play, so the slide becomes its poster.
  const fail = useCallback(() => {
    setState('failed')
  }, [])

  const { src } = useHlsPlayback({
    videoRef,
    uid: photo.uid,
    streaming: photo.hls === true,
    onFatalError: fail,
  })

  // The three clocks, each running only in the state it belongs to and only
  // while the show is: waiting for playback, capping a long clip, and holding
  // the poster of a clip that will not play.
  useBudgetedTimeout(playing && state === 'starting', PLAYBACK_GRACE_MS, fail)
  useBudgetedTimeout(playing && state === 'playing', MAX_VIDEO_SLIDE_MS, () => {
    setState('over')
  })
  useBudgetedTimeout(playing && state === 'failed', intervalMs, () => {
    setState('over')
  })

  // Play and pause follow the show. `play()` may be refused (an autoplay policy,
  // a codec) and answers with a rejected promise — or, in a DOM that implements
  // no playback at all, with nothing; both mean the slide is a poster now.
  useEffect(() => {
    const video = videoRef.current
    if (video === null) {
      return
    }
    if (!playing || state === 'over' || state === 'failed') {
      video.pause()
      return
    }
    Promise.resolve(video.play()).catch(fail)
  }, [playing, state, fail])

  // Asking for the advance from an effect rather than from the `ended` handler
  // is what makes pausing safe: a clip that finishes and is then paused before
  // the next slide is ready asks again on resume, instead of leaving the show
  // sitting on a finished clip with no clock left to move it.
  const ended = useRef(onEnded)
  ended.current = onEnded
  useEffect(() => {
    if (playing && state === 'over') {
      ended.current()
    }
  }, [playing, state])

  // Leaving the show — or stepping to another slide — must not leave a clip
  // playing into an empty room. Dropping the source detaches the download too;
  // hls.js, where it is in play, is destroyed by its own hook.
  useEffect(() => {
    const video = videoRef.current
    return () => {
      if (video === null) {
        return
      }
      video.pause()
      video.removeAttribute('src')
      video.load()
    }
  }, [])

  const duration = photo.duration_ms ?? 0
  const started = state === 'playing' || state === 'over'

  return (
    <>
      <video
        ref={videoRef}
        className={className}
        src={src}
        poster={poster}
        // Muted is not a default here, it is the behaviour: see the component's
        // documentation. `playsInline` keeps an iPhone from taking the clip
        // fullscreen and throwing the reader out of the show.
        muted
        playsInline
        preload="auto"
        aria-label={photo.title || photo.file_name}
        onPlaying={() => {
          setState((current) => (current === 'starting' ? 'playing' : current))
        }}
        onEnded={() => {
          setState('over')
        }}
        onError={fail}
      />
      {!started && (
        <span
          className="slideshow__badge badge text-bg-dark opacity-75 d-inline-flex align-items-center gap-1"
          role="img"
          aria-label={t('slideshow.video')}
        >
          <span aria-hidden="true">▶</span>
          {duration > 0 && <span>{formatDuration(duration)}</span>}
        </span>
      )}
    </>
  )
}
