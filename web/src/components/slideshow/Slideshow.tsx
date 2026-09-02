import { type CSSProperties, useCallback, useEffect, useRef, useState } from 'react'
import Button from 'react-bootstrap/Button'
import Card from 'react-bootstrap/Card'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'

import { useIdleChrome } from '../../hooks/useIdleChrome'
import { usePrefersReducedMotion } from '../../hooks/usePrefersReducedMotion'
import { useViewportBox, type ViewportBox } from '../../hooks/useViewportBox'
import { formatDuration, slideshowRemainingMs } from '../../lib/duration'
import { kenBurnsStyle } from '../../lib/kenBurns'
import { stageRenditionName } from '../../lib/rendition'
import { isActivatableElement } from '../../lib/shortcuts'
import {
  type SlideshowEffect,
  type SlideshowSettings,
  transitionDurationMs,
} from '../../lib/slideshowSettings'
import { type Photo, thumbUrl } from '../../services/photos'
import { Icon } from '../Icon'

import { SlideshowCaption } from './SlideshowCaption'
import { SlideshowSettingsForm } from './SlideshowSettingsForm'

import './slideshow.css'

/**
 * Where the slideshow stage fetches one slide from: the smallest `fit_*` rung
 * that covers the photograph as this viewport paints it, full-bleed.
 *
 * Exported because the page preloads upcoming slides, and a prefetch at any
 * other size would warm the wrong image and leave the stage waiting anyway — so
 * the two must agree, which they do by both deriving the size from the same
 * viewport box and the same photograph rather than by sharing a constant.
 */
export function slideshowSlideSrc(photo: Photo, viewport: ViewportBox): string {
  const size = stageRenditionName(
    viewport,
    { width: photo.file_width, height: photo.file_height },
    viewport.dpr,
  )
  return thumbUrl(photo.uid, size)
}

/** Minimum horizontal travel (px) for a touch swipe to count as next/prev. */
const SWIPE_THRESHOLD = 50

/**
 * How far a finger may travel and still count as a tap rather than a drag. A
 * touch screen registers a few pixels of movement under any thumb, so a tap is
 * "did not really go anywhere", not "did not move at all". Between this and
 * {@link SWIPE_THRESHOLD} lies a deliberate dead zone: a half-hearted drag is an
 * unclear intention and does nothing, which is better than guessing wrong.
 */
const TAP_SLOP = 10

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
  /**
   * Change any of the settings (persisted by the caller). One handler for the
   * whole form: every change — speed, captions, shuffle — applies to the show
   * that is running, from the current slide onward, and none of them restarts it.
   */
  onSettingsChange: (patch: Partial<SlideshowSettings>) => void
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

/** The root's style, which carries the transition duration the CSS reads. */
type StageStyle = CSSProperties & Record<'--slideshow-transition', string>

/** True when a keyboard event originates from a form control we should not hijack. */
function isFormControl(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) {
    return false
  }
  const tag = target.tagName
  return tag === 'INPUT' || tag === 'SELECT' || tag === 'TEXTAREA'
}

/** True when the touch started on the controls rather than on the photograph. */
function isChrome(target: EventTarget | null): boolean {
  return target instanceof Element && target.closest('.slideshow__chrome') !== null
}

/**
 * Whether this browser can take an arbitrary element fullscreen. iOS Safari
 * cannot (it only fullscreens a `<video>`), and a control that visibly does
 * nothing when pressed is worse than no control at all — so the player leaves it
 * out there rather than offering a dead button.
 */
function fullscreenSupported(): boolean {
  // Two separate answers: whether the API exists at all (it does not on an
  // iPhone, and not in jsdom), and whether this document is allowed to use it
  // (an iframe without the permission reports `false`).
  return typeof Element.prototype.requestFullscreen === 'function' && document.fullscreenEnabled
}

/**
 * Runs a Fullscreen API call, swallowing the rejection it may answer with. A
 * refused request (no user gesture, a policy that forbids it) is not something
 * to report to a reader watching photographs, but an unhandled rejection would
 * still reach the console.
 */
function tryFullscreen(request: Promise<void> | undefined): void {
  void request?.catch(() => undefined)
}

/**
 * The fullscreen slideshow stage: the current photo with the configured
 * transition, and — over it, only while somebody is actually there — a control
 * bar (previous / play-pause / next / fullscreen / settings), a close button and
 * the position in the show.
 *
 * **The chrome is a guest on the photograph.** It shows itself when the show
 * starts, and three seconds later, if nothing has happened, it fades out
 * ({@link useIdleChrome}) and takes the mouse cursor with it, leaving the picture
 * alone on the screen — which on a television or a projector is the whole point
 * of the screen. It comes back the moment there is anyone to come back for: a
 * mouse movement, a key, or (on a touch screen, where there is no movement to
 * report) a tap on the picture. It never hides while it is being used — a panel
 * open, the pointer resting on it, focus inside it — and while it is away it is
 * `inert`, so the Tab key cannot land on a button nobody can see.
 *
 * Controls: keyboard (← → / PageUp PageDown for nav, space for play-pause, F for
 * fullscreen, Esc peeling back one layer at a time — the settings panel, then
 * fullscreen, then the show), and touch (a horizontal swipe changes the photo, a
 * tap asks for the controls or puts them away). All controls and labels are
 * translated; the photo set, index and playback state are owned by the caller —
 * including the preloading of upcoming slides, which the page drives so it can
 * hold the advance until the next image has decoded.
 *
 * Its settings panel is the same {@link SlideshowSettingsForm} the start dialog
 * shows, so everything the reader could choose before the show they can change
 * during it, and the two cannot drift apart. Changing anything resumes rather
 * than restarts: the photo on screen stays, the position in the list is kept, and
 * the new setting applies from here on.
 *
 * What each photo *is* — its title, description and date — is laid over the
 * picture by {@link SlideshowCaption}, deliberately outside this component's
 * chrome, so the fade above cannot take it: a show whose photos stop saying what
 * they are the moment the mouse goes still would be worse than one that never
 * said anything. The header bar keeps only the position in the show, which is a
 * fact about the show rather than about the photograph, and fades with the rest.
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
  onSettingsChange,
  loadingMore = false,
}: SlideshowProps) {
  const { t } = useTranslation()
  const reducedMotion = usePrefersReducedMotion()
  // The stage is full-bleed, so the viewport *is* the box the slide is fitted
  // into — which is what decides how many pixels of it are worth fetching.
  const viewport = useViewportBox()
  const containerRef = useRef<HTMLDivElement>(null)
  const [showSettings, setShowSettings] = useState(false)
  const [isFullscreen, setIsFullscreen] = useState(false)
  // The two ways the chrome can be in use, tracked apart: a pointer resting on
  // it and keyboard focus inside it. One flag for both would let the mouse
  // leaving cancel a hold the keyboard still needs — and the chrome would then
  // go `inert` under the focused button, dropping the focus.
  const [chromeHovered, setChromeHovered] = useState(false)
  const [chromeFocused, setChromeFocused] = useState(false)
  const touchStart = useRef<{ x: number; y: number } | null>(null)

  const {
    visible: chromeVisible,
    wake,
    toggle: toggleChrome,
  } = useIdleChrome({ held: showSettings || chromeHovered || chromeFocused })

  // The page only mounts the stage with a non-empty set and the controller hook
  // keeps `index` within range, so the current photo is always present. Clamp
  // defensively against a transient over-index while a page is still loading.
  const current = photos[Math.min(index, photos.length - 1)]

  // Count the whole show, not just the pages loaded so far: "7 of 40" must not
  // read "7 of 7" while the second page is still in flight. A total behind the
  // loaded set (never expected) would do the same, so take the larger.
  const playCount = Math.max(total ?? 0, photos.length)

  // The running-time readout shown beside the speed control: how much of the show
  // is still to come at the current speed. It follows `index` (so it counts down
  // as the show advances and freezes at its value while paused) and
  // `settings.intervalMs` (so changing the speed updates it at once), and is
  // measured against `playCount` — the stable server total — so it does not
  // flicker between values as further pages page in.
  const remaining = formatDuration(slideshowRemainingMs(index, playCount, settings.intervalMs), t)

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
        tryFullscreen(document.exitFullscreen())
      }
    } else if (typeof el.requestFullscreen === 'function') {
      tryFullscreen(el.requestFullscreen())
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

  // Keyboard controls. Any key also counts as "somebody is there", so the chrome
  // is back before the reader looks for it.
  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent): void => {
      if (isFormControl(event.target)) {
        return
      }
      const wasHidden = !chromeVisible
      wake()
      switch (event.key) {
        case 'Tab':
          // Tab moves through the chrome, and while the chrome is away there is
          // nothing to move to. The first press therefore only brings it back —
          // the next one steps into a bar that is on screen, which is the only
          // way a keyboard reaches the controls at all once they can hide.
          if (wasHidden) {
            event.preventDefault()
          }
          break
        case 'ArrowLeft':
        case 'PageUp':
          // PageUp/PageDown because that is what a presentation remote sends,
          // and a family show on a projector is exactly what those are for.
          event.preventDefault()
          onPrev()
          break
        case 'ArrowRight':
        case 'PageDown':
          event.preventDefault()
          onNext()
          break
        case ' ':
        case 'Spacebar':
          if (isActivatableElement(event.target)) {
            break
          }
          event.preventDefault()
          onToggle()
          break
        case 'Escape':
          // One layer at a time: the panel, then fullscreen, then the show. Esc
          // used to close the whole slideshow from under an open settings panel,
          // which is not what anybody means by it.
          if (showSettings) {
            setShowSettings(false)
          } else if (document.fullscreenElement && typeof document.exitFullscreen === 'function') {
            tryFullscreen(document.exitFullscreen())
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
  }, [onNext, onPrev, onToggle, onExit, toggleFullscreen, showSettings, chromeVisible, wake])

  // A mouse being moved is the classic "somebody is there". Touch is deliberately
  // not wired here: a finger dragging across the picture is steering the show,
  // not asking for the buttons, and it has the tap below for that.
  const onPointerMove = useCallback(
    (event: React.PointerEvent): void => {
      if (event.pointerType === 'mouse') {
        wake()
      }
    },
    [wake],
  )

  const onTouchStart = useCallback((event: React.TouchEvent): void => {
    // A second finger means a pinch or a fumble, not a swipe; a touch that
    // starts on the controls belongs to the control it started on.
    if (event.touches.length > 1 || isChrome(event.target)) {
      touchStart.current = null
      return
    }
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
      if (Math.abs(dx) >= SWIPE_THRESHOLD && Math.abs(dx) > Math.abs(dy)) {
        if (dx < 0) {
          onNext()
        } else {
          onPrev()
        }
        return
      }
      // Not a swipe. A finger that stayed put is a tap, which is a touch screen's
      // only way of asking for the controls — and, pressed again, of dismissing
      // them. Anything in between is an unclear drag and does nothing.
      if (Math.hypot(dx, dy) <= TAP_SLOP) {
        toggleChrome()
      }
    },
    [onNext, onPrev, toggleChrome],
  )

  const onTouchCancel = useCallback((): void => {
    touchStart.current = null
  }, [])

  // Ken Burns pans across the photo itself, so it only makes sense for stills:
  // a video slide keeps its previous, motionless framing. A reduced-motion user
  // gets the same static slide rather than a shortened pan.
  const isVideo = current.file_mime.startsWith('video/')
  const kenBurns = settings.effect === 'kenburns' && !reducedMotion && !isVideo
  const appliedEffect: SlideshowEffect =
    settings.effect === 'kenburns' && !kenBurns ? 'none' : settings.effect
  const effectClass = EFFECT_CLASS[appliedEffect]

  // The transition is capped against the chosen speed, so the fastest show does
  // not spend most of every slide fading. Published as a custom property on the
  // root, where the animation rules inherit it and the `<img>` keeps its style
  // attribute for the Ken Burns properties alone.
  const stageStyle: StageStyle = {
    '--slideshow-transition': `${transitionDurationMs(appliedEffect, settings.intervalMs)}ms`,
  }

  const playLabel = playing ? t('slideshow.pause') : t('slideshow.play')
  const fullscreenLabel = isFullscreen ? t('slideshow.exitFullscreen') : t('slideshow.fullscreen')

  return (
    <div
      ref={containerRef}
      className={`slideshow${chromeVisible ? '' : ' slideshow--idle'}`}
      style={stageStyle}
      role="region"
      aria-label={t('slideshow.title')}
      onPointerMove={onPointerMove}
      onTouchStart={onTouchStart}
      onTouchEnd={onTouchEnd}
      onTouchCancel={onTouchCancel}
    >
      <div className="slideshow__stage">
        <img
          key={current.uid}
          className={`slideshow__image ${effectClass}`}
          src={slideshowSlideSrc(current, viewport)}
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

      <SlideshowCaption photo={current} settings={settings} />

      {/* Everything that is not the photograph, in one element: one thing to
          fade, one thing to make `inert`, one scope for "the reader is using
          this". The caption is deliberately not in here. */}
      <div
        className="slideshow__chrome"
        inert={!chromeVisible}
        onPointerEnter={(event) => {
          // A mouse resting on the bar is a reader about to press something. A
          // finger is not resting on anything — it lifts — and a touch pointer
          // that entered and never left would pin the chrome for ever.
          if (event.pointerType === 'mouse') {
            setChromeHovered(true)
          }
        }}
        onPointerLeave={() => {
          setChromeHovered(false)
        }}
        onFocus={() => {
          setChromeFocused(true)
        }}
        onBlur={() => {
          setChromeFocused(false)
        }}
      >
        <Button
          variant="dark"
          size="sm"
          className="slideshow__close"
          aria-label={t('slideshow.close')}
          title={t('slideshow.close')}
          onClick={onExit}
        >
          <Icon name="x-lg" />
        </Button>

        {/* The header bar answers "where am I in the show?" and nothing else.
            What the photo is belongs to the photo, not to the chrome. */}
        <div className="slideshow__progress">
          <span className="flex-shrink-0 text-nowrap ms-auto">
            {t('slideshow.progress', { current: index + 1, total: playCount })}
          </span>
        </div>

        {showSettings && (
          <Card bg="dark" text="light" className="slideshow__settings">
            <Card.Body>
              <SlideshowSettingsForm
                settings={settings}
                onChange={onSettingsChange}
                idPrefix="slideshow-player"
                speedNote={t('slideshow.remaining', { remaining })}
              />
            </Card.Body>
          </Card>
        )}

        <div className="slideshow__controls">
          <Button
            variant="dark"
            size="sm"
            aria-label={t('slideshow.prev')}
            title={t('slideshow.prev')}
            onClick={onPrev}
            disabled={photos.length === 0}
          >
            <Icon name="chevron-left" />
          </Button>
          <Button
            variant="light"
            size="sm"
            className="slideshow__play"
            aria-label={playLabel}
            title={playLabel}
            onClick={onToggle}
            disabled={photos.length === 0}
          >
            <Icon name={playing ? 'pause-fill' : 'play-fill'} />
          </Button>
          <Button
            variant="dark"
            size="sm"
            aria-label={t('slideshow.next')}
            title={t('slideshow.next')}
            onClick={onNext}
            disabled={photos.length === 0}
          >
            <Icon name="chevron-right" />
          </Button>
          {fullscreenSupported() && (
            <Button
              variant="dark"
              size="sm"
              aria-label={fullscreenLabel}
              title={fullscreenLabel}
              onClick={toggleFullscreen}
            >
              <Icon name={isFullscreen ? 'arrows-angle-contract' : 'arrows-fullscreen'} />
            </Button>
          )}
          <Button
            variant={showSettings ? 'secondary' : 'dark'}
            size="sm"
            aria-label={t('slideshow.settings')}
            title={t('slideshow.settings')}
            aria-pressed={showSettings}
            onClick={() => {
              setShowSettings((s) => !s)
            }}
          >
            <Icon name="sliders" />
          </Button>
        </div>
      </div>
    </div>
  )
}
