import { useRef, useState, type PointerEvent as ReactPointerEvent } from 'react'
import { useTranslation } from 'react-i18next'

import {
  formatPlaybackTime,
  playbackFraction,
  positionFromPointer,
  previewOffset,
  storyboardTileIndex,
  storyboardTileStyle,
  type StoryboardSpec,
} from '../../lib/videoPlayback'
import { storyboardSpriteUrl, type Storyboard } from '../../services/photos'

/** Props for {@link VideoScrubber}. */
export interface VideoScrubberProps {
  /** UID of the photo being played, used to address its storyboard sprite. */
  uid: string
  /** Current playback position in seconds. */
  position: number
  /** Clip length in seconds; 0 while the metadata has not loaded. */
  duration: number
  /** The clip's storyboard, or null when it has none (yet or ever). */
  storyboard: Storyboard | null
  /** Download token appended to the sprite URL for cookie-less contexts. */
  token?: string | null
  /** Seeks playback to the given position in seconds. */
  onSeek: (seconds: number) => void
}

/**
 * The video player's timeline: a progress bar you can click or drag to seek,
 * with a frame preview that follows the cursor.
 *
 * The preview is drawn from the clip's storyboard sprite — one JPEG holding a
 * grid of frames — by offsetting it as a CSS background, so moving along the
 * timeline costs no request at all. A clip with no storyboard (not generated
 * yet, or never) simply shows the time bubble instead: the preview degrades, the
 * scrubbing does not.
 *
 * The preview is deliberately **mouse-only**. It is driven by pointer events and
 * ignores anything whose `pointerType` is not `mouse`, because on a touchscreen
 * the finger doing the scrubbing sits exactly where the preview would appear —
 * showing it there would hide the frame it is meant to reveal. Touch keeps the
 * full drag-to-seek behaviour.
 *
 * It is a slider in the accessibility tree, so a keyboard user can seek with the
 * arrow keys once it has focus (the browser's native `<input type="range">`
 * behaviour) without the player having to claim those keys globally.
 */
export function VideoScrubber({
  uid,
  position,
  duration,
  storyboard,
  token,
  onSeek,
}: VideoScrubberProps) {
  const { t } = useTranslation()
  const trackRef = useRef<HTMLDivElement>(null)
  const [hover, setHover] = useState<{ x: number; seconds: number } | null>(null)
  const [dragging, setDragging] = useState(false)

  const progress = playbackFraction(position, duration) * 100
  const spec = readySpec(storyboard)

  /** Maps a pointer event to the timeline position it points at, in seconds. */
  const positionAt = (event: ReactPointerEvent<HTMLDivElement>): { x: number; seconds: number } => {
    const rect = event.currentTarget.getBoundingClientRect()
    const x = event.clientX - rect.left
    return { x, seconds: positionFromPointer(x, rect.width, duration) }
  }

  const onPointerDown = (event: ReactPointerEvent<HTMLDivElement>): void => {
    if (duration <= 0) {
      return
    }
    event.currentTarget.setPointerCapture(event.pointerId)
    setDragging(true)
    onSeek(positionAt(event).seconds)
  }

  const onPointerMove = (event: ReactPointerEvent<HTMLDivElement>): void => {
    const at = positionAt(event)
    if (dragging) {
      onSeek(at.seconds)
    }
    // Touch and pen never get a preview: the pointer itself covers the frame.
    if (event.pointerType === 'mouse') {
      setHover(at)
    }
  }

  const endDrag = (event: ReactPointerEvent<HTMLDivElement>): void => {
    if (event.currentTarget.hasPointerCapture(event.pointerId)) {
      event.currentTarget.releasePointerCapture(event.pointerId)
    }
    setDragging(false)
  }

  return (
    <div className="kk-video__timeline">
      {hover !== null && duration > 0 && (
        <div
          className="kk-video__preview"
          style={{
            left: `${String(previewOffset(hover.x, trackWidth(trackRef.current), previewWidth(spec)))}px`,
          }}
          aria-hidden="true"
        >
          {spec !== null && (
            <span
              className="kk-video__preview-frame"
              style={storyboardTileStyle(
                spec,
                storyboardSpriteUrl(uid, token),
                storyboardTileIndex(spec, hover.seconds),
              )}
            />
          )}
          <span className="kk-video__preview-time">{formatPlaybackTime(hover.seconds)}</span>
        </div>
      )}
      <div
        ref={trackRef}
        className="kk-video__track"
        role="slider"
        tabIndex={0}
        aria-label={t('photo.video.seek')}
        aria-valuemin={0}
        aria-valuemax={Math.max(Math.round(duration), 0)}
        aria-valuenow={Math.round(position)}
        aria-valuetext={`${formatPlaybackTime(position)} / ${formatPlaybackTime(duration)}`}
        onPointerDown={onPointerDown}
        onPointerMove={onPointerMove}
        onPointerUp={endDrag}
        onPointerCancel={endDrag}
        onPointerLeave={() => {
          setHover(null)
        }}
        onKeyDown={(event) => {
          const step = seekStep(event.key)
          if (step === 0) {
            return
          }
          event.preventDefault()
          event.stopPropagation()
          onSeek(position + step)
        }}
      >
        <span className="kk-video__progress" style={{ width: `${String(progress)}%` }} />
      </div>
    </div>
  )
}

/**
 * The storyboard grid to draw previews from, or null when there is none to draw:
 * a storyboard that is pending, unavailable, or ready but missing a geometry
 * field is not something a preview can be placed against.
 */
function readySpec(storyboard: Storyboard | null): StoryboardSpec | null {
  if (storyboard?.status !== 'ready') {
    return null
  }
  const {
    columns,
    rows,
    count,
    tile_width: width,
    tile_height: height,
    interval_ms: interval,
  } = storyboard
  if (
    columns === undefined ||
    rows === undefined ||
    count === undefined ||
    width === undefined ||
    height === undefined ||
    interval === undefined
  ) {
    return null
  }
  return { columns, rows, count, tile_width: width, tile_height: height, interval_ms: interval }
}

/** How wide the hover preview is: the sprite tile, or the bare time bubble. */
function previewWidth(spec: StoryboardSpec | null): number {
  return spec === null ? 56 : spec.tile_width
}

/**
 * The rendered width of the timeline track, or 0 before it has been laid out —
 * which only clamps the preview to the centre, never throws.
 */
function trackWidth(track: HTMLDivElement | null): number {
  return track?.getBoundingClientRect().width ?? 0
}

/**
 * How far an arrow key seeks from the focused timeline: five seconds per press,
 * a minute with Page Up/Down. Any other key returns 0, meaning "not mine" — the
 * handler then leaves the event alone so the page's own shortcuts still work.
 */
function seekStep(key: string): number {
  switch (key) {
    case 'ArrowRight':
      return 5
    case 'ArrowLeft':
      return -5
    case 'PageUp':
      return 60
    case 'PageDown':
      return -60
    default:
      return 0
  }
}
