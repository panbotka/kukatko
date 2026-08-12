import { useCallback, useEffect, useRef, useState } from 'react'

/**
 * How many slides ahead of the cursor trigger a background page load, so the
 * next photos are fetched well before the slideshow reaches the loaded end.
 * It is also how far ahead {@link preloadWindow} preloads their images.
 */
export const PRELOAD_AHEAD = 5

/**
 * How many slides behind the cursor stay preloaded, so stepping back with ← is
 * as instant as stepping forward.
 */
export const PRELOAD_BEHIND = 1

/**
 * How long the show may hold on the current slide waiting for the next one to
 * decode. Past this the slide advances anyway: a stuck download must degrade
 * into a brief flash of an empty stage, never into a show that stops playing.
 */
export const MAX_HOLD_MS = 10_000

/**
 * Whether the image of a given slide can be painted: `ready` once it is fetched
 * *and decoded*, `error` when it will never load (the slide is skipped), and
 * `pending` while it is still on its way.
 */
export type SlideReadiness = 'pending' | 'ready' | 'error'

/** The readiness of a slideshow that does not track its images: play at will. */
const ALWAYS_READY = (): SlideReadiness => 'ready'

/** Options for {@link useSlideshow}. */
export interface UseSlideshowOptions {
  /**
   * The number of photos currently loaded; the cursor ranges over `[0, length)`.
   * It may grow as further pages load — advancing past the end then continues
   * into the newly loaded photos rather than wrapping prematurely.
   */
  length: number
  /** Whether more pages remain behind the loaded set (gates wrap-around). */
  hasMore?: boolean
  /** Auto-advance interval, in milliseconds. */
  intervalMs: number
  /**
   * Whether the show starts again at the first photo once the last has played.
   * Defaults to false: auto-advance then stops on the last slide, leaving it on
   * screen and the show paused. It governs auto-advance only — a manual next
   * from the end still wraps, as prev from the start always has, because that is
   * the reader steering rather than the show running on.
   */
  repeat?: boolean
  /** Whether playback starts automatically. Defaults to true. */
  autoPlay?: boolean
  /** Requests the next page when the cursor nears the loaded end. */
  onLoadMore?: () => void
  /**
   * Reports whether the slide at an index is ready to paint. Auto-advance waits
   * on it (see {@link UseSlideshowResult.holding}); manual navigation never
   * does. Defaults to treating every slide as ready.
   *
   * Its identity must change whenever a slide's readiness changes, so the hook
   * can advance the instant the next image lands.
   */
  readiness?: (index: number) => SlideReadiness
  /** The bounded wait for an undecoded slide. Defaults to {@link MAX_HOLD_MS}. */
  maxHoldMs?: number
}

/** Result of {@link useSlideshow}: the cursor, playback state and controls. */
export interface UseSlideshowResult {
  /** The index of the currently shown photo within the loaded set. */
  index: number
  /** Whether the slideshow is auto-advancing. */
  playing: boolean
  /** True while the interval has elapsed but the next slide is not ready yet. */
  holding: boolean
  /**
   * How many times the show has wrapped from the last photo back to the first —
   * i.e. which pass through the list is running. It changes only on a forward
   * wrap, so a caller keeping per-pass bookkeeping (which photos have been seen
   * *this* time round) knows exactly when to start it over.
   */
  pass: number
  /** Advances to the next photo (loads more / wraps to the first at the end). */
  next: () => void
  /** Goes back to the previous photo (wraps to the last loaded at the start). */
  prev: () => void
  /** Starts auto-advancing. */
  play: () => void
  /** Pauses auto-advancing. */
  pause: () => void
  /** Toggles play / pause. */
  toggle: () => void
  /** Jumps to a specific index (clamped to the loaded set). */
  goTo: (index: number) => void
}

/**
 * The indices whose images are worth having decoded while the cursor sits at
 * `index`: {@link PRELOAD_AHEAD} ahead, {@link PRELOAD_BEHIND} behind, the
 * current slide first so it wins the bandwidth. Offsets wrap, which both keeps
 * the window whole for a set smaller than itself and readies the first photos
 * for the wrap-around at the end. The result is deduplicated and in priority
 * order.
 */
export function preloadWindow(index: number, length: number): number[] {
  if (length <= 0) {
    return []
  }
  const wrap = (i: number): number => ((i % length) + length) % length
  const window = new Set<number>()
  for (let offset = 0; offset <= PRELOAD_AHEAD; offset++) {
    window.add(wrap(index + offset))
  }
  for (let offset = 1; offset <= PRELOAD_BEHIND; offset++) {
    window.add(wrap(index - offset))
  }
  return [...window]
}

/**
 * The slide auto-advance should move to, skipping over images that failed to
 * load — a broken slide must not block the show, and holding for one would only
 * burn the bounded wait. Bounded by the set size, so an all-broken set settles
 * back on the current slide instead of looping forever.
 */
function advanceTarget(
  index: number,
  length: number,
  readiness: (index: number) => SlideReadiness,
): number {
  let target = (index + 1) % length
  for (let step = 0; step < length - 1 && readiness(target) === 'error'; step++) {
    target = (target + 1) % length
  }
  return target
}

/**
 * Drives a slideshow over a (possibly still-growing) list of photos: it owns the
 * current index and play/pause state, auto-advances on the configured interval,
 * and supports manual next/prev with wrap-around.
 *
 * Auto-advance is gated on readiness. When the interval elapses the hook does
 * not jump straight to the next slide — it starts *holding*, and moves the
 * instant `readiness` reports the next image as decoded and paintable, which on
 * a fast connection is the same tick. That is what keeps a slow slide from
 * showing as a blank stage for its first second. A hold is bounded by
 * `maxHoldMs`, after which the show advances regardless, and a slide whose image
 * failed is skipped rather than waited for. Manual next/prev/goTo cancel any
 * hold and switch immediately; pausing cancels it too, so resuming starts a
 * fresh interval.
 *
 * To handle large sets without loading everything up front, it calls
 * `onLoadMore` as the cursor nears the loaded end ({@link PRELOAD_AHEAD} slides
 * ahead) and, when it reaches the very end with more pages pending, waits for
 * them instead of wrapping. A manual next/prev resets the auto-advance timer,
 * and a set of fewer than two photos neither holds nor advances.
 *
 * What happens at the very end is the reader's choice: with `repeat` the show
 * wraps to the first photo and {@link UseSlideshowResult.pass} counts the round,
 * and without it auto-advance stops there, holding the last photo on screen with
 * playback paused.
 */
export function useSlideshow(options: UseSlideshowOptions): UseSlideshowResult {
  const {
    length,
    hasMore = false,
    intervalMs,
    repeat = false,
    autoPlay = true,
    onLoadMore,
    readiness = ALWAYS_READY,
    maxHoldMs = MAX_HOLD_MS,
  } = options

  const [index, setIndex] = useState(0)
  const [playing, setPlaying] = useState(autoPlay)
  // Which pass through the list is running; bumped on every forward wrap.
  const [pass, setPass] = useState(0)
  // `holding`: the interval elapsed and we are waiting on the next image.
  // `timedOut`: that wait hit `maxHoldMs`, so the next check advances anyway.
  const [holding, setHolding] = useState(false)
  const [timedOut, setTimedOut] = useState(false)

  // Refs let the stable next/prev callbacks read the latest values without being
  // re-created on every change (which would needlessly re-arm the timer).
  const lengthRef = useRef(length)
  lengthRef.current = length
  const indexRef = useRef(index)
  indexRef.current = index
  const hasMoreRef = useRef(hasMore)
  hasMoreRef.current = hasMore
  const repeatRef = useRef(repeat)
  repeatRef.current = repeat
  const onLoadMoreRef = useRef(onLoadMore)
  onLoadMoreRef.current = onLoadMore

  const endHold = useCallback(() => {
    setHolding(false)
    setTimedOut(false)
  }, [])

  // Manual next reads the cursor from the ref rather than through a functional
  // update, because wrapping is not only a new index: it starts a new pass, and
  // that has to be announced outside the updater rather than as a side effect
  // inside one. Discrete events (click, keydown) flush before the next arrives,
  // so the ref is current for each of them.
  const next = useCallback(() => {
    endHold()
    const len = lengthRef.current
    if (len === 0) {
      return
    }
    const i = indexRef.current
    if (i + 1 < len) {
      setIndex(i + 1)
      return
    }
    // At the loaded end: wait for more pages if any, otherwise wrap around.
    // A manual next wraps whatever `repeat` says — that is the reader steering,
    // not the show running on past its end.
    if (hasMoreRef.current) {
      onLoadMoreRef.current?.()
      return
    }
    setIndex(0)
    setPass((p) => p + 1)
  }, [endHold])

  const prev = useCallback(() => {
    endHold()
    setIndex((i) => {
      const len = lengthRef.current
      if (len === 0) {
        return 0
      }
      return i - 1 >= 0 ? i - 1 : len - 1
    })
  }, [endHold])

  const goTo = useCallback(
    (target: number) => {
      endHold()
      setIndex(() => {
        const len = lengthRef.current
        if (len === 0) {
          return 0
        }
        return Math.min(len - 1, Math.max(0, target))
      })
    },
    [endHold],
  )

  const play = useCallback(() => {
    setPlaying(true)
  }, [])
  const pause = useCallback(() => {
    setPlaying(false)
  }, [])
  const toggle = useCallback(() => {
    setPlaying((p) => !p)
  }, [])

  // Pausing drops any pending advance: resuming starts a fresh interval rather
  // than jumping the moment the held image lands.
  useEffect(() => {
    if (!playing) {
      endHold()
    }
  }, [playing, endHold])

  // The interval elapsing does not advance — it starts the readiness gate below.
  // Depending on `index` re-arms the timer after every advance, so a manual
  // next/prev also resets the countdown; depending on `length` picks up freshly
  // loaded pages when playback was waiting at the end. It stays disarmed while
  // holding, so changing the interval mid-hold neither restarts the bounded wait
  // nor leaves a second timer behind.
  useEffect(() => {
    if (!playing || holding || length <= 1) {
      return
    }
    const id = window.setTimeout(() => {
      const i = indexRef.current
      if (i + 1 >= lengthRef.current) {
        // At the loaded end with more pages coming, ask for them and stay put;
        // this effect re-arms once `length` grows.
        if (hasMoreRef.current) {
          onLoadMoreRef.current?.()
          return
        }
        // The last photo of a show that does not repeat: stop here, leaving it on
        // screen. Pausing rather than freezing means the play button restarts it.
        if (!repeatRef.current) {
          setPlaying(false)
          return
        }
      }
      setHolding(true)
    }, intervalMs)
    return () => {
      window.clearTimeout(id)
    }
  }, [playing, holding, intervalMs, length, index])

  // The bounded wait. Its deps deliberately exclude `readiness` and `intervalMs`
  // so neither an image settling nor a speed change can push the deadline out.
  useEffect(() => {
    if (!playing || !holding) {
      return
    }
    const id = window.setTimeout(() => {
      setTimedOut(true)
    }, maxHoldMs)
    return () => {
      window.clearTimeout(id)
    }
  }, [playing, holding, maxHoldMs])

  // The readiness gate: re-runs on every readiness change, so the show advances
  // the instant the next image is decoded — or right away when it already was.
  useEffect(() => {
    if (!playing || !holding) {
      return
    }
    if (length <= 1) {
      endHold()
      return
    }
    const target = advanceTarget(index, length, readiness)
    if (!timedOut && readiness(target) === 'pending') {
      return
    }
    setIndex(target)
    // Landing back on the first photo from anywhere else is the wrap: a new pass.
    if (target === 0 && index !== 0) {
      setPass((p) => p + 1)
    }
    endHold()
  }, [playing, holding, timedOut, index, length, readiness, endHold])

  // Prefetch upcoming pages before the cursor reaches the loaded end.
  useEffect(() => {
    if (hasMore && index >= length - PRELOAD_AHEAD) {
      onLoadMore?.()
    }
  }, [hasMore, index, length, onLoadMore])

  // Keep the cursor in range if the loaded set ever shrinks.
  useEffect(() => {
    if (index > 0 && index >= length) {
      setIndex(length === 0 ? 0 : length - 1)
    }
  }, [index, length])

  return { index, playing, holding, pass, next, prev, play, pause, toggle, goTo }
}
