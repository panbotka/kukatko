import { useCallback, useEffect, useRef, useState } from 'react'
import Button from 'react-bootstrap/Button'
import Card from 'react-bootstrap/Card'
import Form from 'react-bootstrap/Form'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'

import { usePrefersReducedMotion } from '../../hooks/usePrefersReducedMotion'
import { formatDuration, slideshowRemainingMs } from '../../lib/duration'
import { kenBurnsStyle } from '../../lib/kenBurns'
import {
  SLIDESHOW_EFFECTS,
  SLIDESHOW_INTERVALS_MS,
  type SlideshowEffect,
  type SlideshowSettings,
} from '../../lib/slideshowSettings'
import { type Photo, thumbUrl } from '../../services/photos'

import './slideshow.css'

/**
 * Preview size for the slideshow stage: a large fit-to-box preview, not a tile.
 * Exported because the page preloads upcoming slides, and a prefetch at any
 * other size would warm the wrong image and leave the stage waiting anyway.
 */
export const SLIDESHOW_PREVIEW_SIZE = 'fit_1920'

/** Minimum horizontal travel (px) for a touch swipe to count as next/prev. */
const SWIPE_THRESHOLD = 50

/** Props for {@link Slideshow}. */
export interface SlideshowProps {
  /** The loaded photos to show, in playback order. */
  photos: Photo[]
  /** Index of the currently shown photo (from {@link import('../../hooks/useSlideshow').useSlideshow}). */
  index: number
  /**
   * How many photos the show will play in total — the server's count for the
   * query, which may exceed the loaded `photos` while further pages stream in.
   * It drives the progress and remaining-time readout. Defaults to the loaded
   * count when the caller has no total.
   */
  total?: number
  /** Whether the slideshow is auto-advancing. */
  playing: boolean
  /** The active effect / speed settings. */
  settings: SlideshowSettings
  /** Advance to the next photo. */
  onNext: () => void
  /** Go to the previous photo. */
  onPrev: () => void
  /** Toggle play / pause. */
  onToggle: () => void
  /** Leave the slideshow (returns to the prior view). */
  onExit: () => void
  /** Change the transition effect (persisted by the caller). */
  onEffectChange: (effect: SlideshowEffect) => void
  /** Change the auto-advance interval, in ms (persisted by the caller). */
  onIntervalChange: (intervalMs: number) => void
  /** Whether a further page is being loaded in the background (shows a spinner). */
  loadingMore?: boolean
}

/** The CSS class animating each effect; `none` (and a stilled slide) get no class. */
const EFFECT_CLASS: Readonly<Record<SlideshowEffect, string>> = {
  fade: 'slideshow__image--fade',
  slide: 'slideshow__image--slide',
  kenburns: 'slideshow__image--kenburns',
  none: '',
}

/** True when a keyboard event originates from a form control we should not hijack. */
function isFormControl(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) {
    return false
  }
  const tag = target.tagName
  return tag === 'INPUT' || tag === 'SELECT' || tag === 'TEXTAREA'
}

/**
 * The fullscreen slideshow stage: shows the current photo with the configured
 * transition, an always-available control bar (previous / play-pause / next /
 * fullscreen / settings) and a close button. It wires keyboard (← → for nav,
 * space to play/pause, Esc to exit or leave fullscreen) and touch (horizontal
 * swipe) controls, and exposes the effect/speed pickers. All controls and labels
 * are translated; the photo set, index and playback state are owned by the
 * caller — including the preloading of upcoming slides, which the page drives so
 * it can hold the advance until the next image has decoded.
 */
export function Slideshow({
  photos,
  index,
  total,
  playing,
  settings,
  onNext,
  onPrev,
  onToggle,
  onExit,
  onEffectChange,
  onIntervalChange,
  loadingMore = false,
}: SlideshowProps) {
  const { t } = useTranslation()
  const reducedMotion = usePrefersReducedMotion()
  const containerRef = useRef<HTMLDivElement>(null)
  const [showSettings, setShowSettings] = useState(false)
  const [isFullscreen, setIsFullscreen] = useState(false)
  const touchStart = useRef<{ x: number; y: number } | null>(null)

  // The page only mounts the stage with a non-empty set and the controller hook
  // keeps `index` within range, so the current photo is always present. Clamp
  // defensively against a transient over-index while a page is still loading.
  const current = photos[Math.min(index, photos.length - 1)]

  // Count the whole show, not just the pages loaded so far: "7 of 40" must not
  // read "7 of 7" while the second page is still in flight. A total behind the
  // loaded set (never expected) would do the same, so take the larger.
  const playCount = Math.max(total ?? 0, photos.length)

  const toggleFullscreen = useCallback(() => {
    const el = containerRef.current
    if (el === null) {
      return
    }
    // `document.fullscreenElement` is an Element when fullscreen, else null /
    // undefined (jsdom): a truthy check covers both. The Fullscreen API may be
    // absent (jsdom / older browsers), so feature-detect before calling.
    if (document.fullscreenElement) {
      if (typeof document.exitFullscreen === 'function') {
        void document.exitFullscreen()
      }
    } else if (typeof el.requestFullscreen === 'function') {
      void el.requestFullscreen()
    }
  }, [])

  // Track native fullscreen changes (e.g. the browser's own Esc) so the toggle
  // button label stays in sync.
  useEffect(() => {
    const onChange = (): void => {
      setIsFullscreen(Boolean(document.fullscreenElement))
    }
    document.addEventListener('fullscreenchange', onChange)
    return () => {
      document.removeEventListener('fullscreenchange', onChange)
    }
  }, [])

  // Keyboard controls. Esc leaves fullscreen first (if active), otherwise exits.
  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent): void => {
      if (isFormControl(event.target)) {
        return
      }
      switch (event.key) {
        case 'ArrowLeft':
          event.preventDefault()
          onPrev()
          break
        case 'ArrowRight':
          event.preventDefault()
          onNext()
          break
        case ' ':
        case 'Spacebar':
          event.preventDefault()
          onToggle()
          break
        case 'Escape':
          if (document.fullscreenElement && typeof document.exitFullscreen === 'function') {
            void document.exitFullscreen()
          } else {
            onExit()
          }
          break
        case 'f':
        case 'F':
          event.preventDefault()
          toggleFullscreen()
          break
        default:
          break
      }
    }
    window.addEventListener('keydown', onKeyDown)
    return () => {
      window.removeEventListener('keydown', onKeyDown)
    }
  }, [onNext, onPrev, onToggle, onExit, toggleFullscreen])

  const onTouchStart = useCallback((event: React.TouchEvent): void => {
    const touch = event.changedTouches[0]
    touchStart.current = { x: touch.clientX, y: touch.clientY }
  }, [])

  const onTouchEnd = useCallback(
    (event: React.TouchEvent): void => {
      const start = touchStart.current
      touchStart.current = null
      if (start === null) {
        return
      }
      const touch = event.changedTouches[0]
      const dx = touch.clientX - start.x
      const dy = touch.clientY - start.y
      if (Math.abs(dx) < SWIPE_THRESHOLD || Math.abs(dx) <= Math.abs(dy)) {
        return
      }
      if (dx < 0) {
        onNext()
      } else {
        onPrev()
      }
    },
    [onNext, onPrev],
  )

  // Ken Burns pans across the photo itself, so it only makes sense for stills:
  // a video slide keeps its previous, motionless framing. A reduced-motion user
  // gets the same static slide rather than a shortened pan.
  const isVideo = current.file_mime.startsWith('video/')
  const kenBurns = settings.effect === 'kenburns' && !reducedMotion && !isVideo
  const appliedEffect: SlideshowEffect =
    settings.effect === 'kenburns' && !kenBurns ? 'none' : settings.effect
  const effectClass = EFFECT_CLASS[appliedEffect]

  return (
    <div
      ref={containerRef}
      className="slideshow"
      role="region"
      aria-label={t('slideshow.title')}
      onTouchStart={onTouchStart}
      onTouchEnd={onTouchEnd}
    >
      <Button
        variant="dark"
        size="sm"
        className="slideshow__close"
        aria-label={t('slideshow.close')}
        onClick={onExit}
      >
        ✕
      </Button>

      <div className="slideshow__caption">
        <span className="text-truncate">{current.title || current.file_name}</span>
        <span className="flex-shrink-0 text-nowrap">
          {t('slideshow.progress', {
            current: index + 1,
            total: playCount,
            remaining: formatDuration(
              slideshowRemainingMs(index, playCount, settings.intervalMs),
              t,
            ),
          })}
        </span>
      </div>

      <div className="slideshow__stage">
        <img
          key={current.uid}
          className={`slideshow__image ${effectClass}`}
          src={thumbUrl(current.uid, SLIDESHOW_PREVIEW_SIZE)}
          alt={current.title || current.file_name}
          data-effect={settings.effect}
          style={kenBurns ? kenBurnsStyle(current.uid, settings.intervalMs) : undefined}
          draggable={false}
        />
        {loadingMore && (
          <Spinner
            animation="border"
            role="status"
            size="sm"
            className="position-absolute top-50 start-50 text-light"
          />
        )}
      </div>

      {showSettings && (
        <Card bg="dark" text="light" className="slideshow__settings">
          <Card.Body className="d-grid gap-3">
            <Form.Group controlId="slideshow-effect">
              <Form.Label className="small mb-1">{t('slideshow.effect.label')}</Form.Label>
              <Form.Select
                size="sm"
                value={settings.effect}
                onChange={(e) => {
                  onEffectChange(e.target.value as SlideshowEffect)
                }}
              >
                {SLIDESHOW_EFFECTS.map((effect) => (
                  <option key={effect} value={effect}>
                    {t(`slideshow.effect.${effect}`)}
                  </option>
                ))}
              </Form.Select>
            </Form.Group>
            <Form.Group controlId="slideshow-speed">
              <Form.Label className="small mb-1">{t('slideshow.speed.label')}</Form.Label>
              <Form.Select
                size="sm"
                value={String(settings.intervalMs)}
                onChange={(e) => {
                  onIntervalChange(Number(e.target.value))
                }}
              >
                {SLIDESHOW_INTERVALS_MS.map((ms) => (
                  <option key={ms} value={ms}>
                    {t('slideshow.speed.seconds', { seconds: Math.round(ms / 1000) })}
                  </option>
                ))}
              </Form.Select>
            </Form.Group>
          </Card.Body>
        </Card>
      )}

      <div className="slideshow__controls">
        <Button
          variant="dark"
          size="sm"
          aria-label={t('slideshow.prev')}
          onClick={onPrev}
          disabled={photos.length === 0}
        >
          ‹
        </Button>
        <Button
          variant="light"
          size="sm"
          aria-label={playing ? t('slideshow.pause') : t('slideshow.play')}
          onClick={onToggle}
          disabled={photos.length === 0}
        >
          {playing ? '❚❚' : '▶'}
        </Button>
        <Button
          variant="dark"
          size="sm"
          aria-label={t('slideshow.next')}
          onClick={onNext}
          disabled={photos.length === 0}
        >
          ›
        </Button>
        <Button
          variant="dark"
          size="sm"
          aria-label={isFullscreen ? t('slideshow.exitFullscreen') : t('slideshow.fullscreen')}
          onClick={toggleFullscreen}
        >
          ⛶
        </Button>
        <Button
          variant={showSettings ? 'secondary' : 'dark'}
          size="sm"
          aria-label={t('slideshow.settings')}
          aria-pressed={showSettings}
          onClick={() => {
            setShowSettings((s) => !s)
          }}
        >
          ⚙
        </Button>
      </div>
    </div>
  )
}
