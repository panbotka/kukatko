import {
  type CSSProperties,
  type RefObject,
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from 'react'
import { useTranslation } from 'react-i18next'

import { useTimeline } from '../../hooks/useTimeline'
import { formatMonth } from '../../lib/format'
import { type PhotoListParams, type TimelineBucket } from '../../services/photos'

import {
  anchorOf,
  bucketKey,
  buildRail,
  fractionForRank,
  rankForFraction,
  rankForIndex,
  spanMonths,
} from './timelineRail'

/**
 * How far a pointer has to travel before a press turns into a drag. Below it the
 * press stays a click, so a click on a tick lands on that tick's exact month
 * instead of on whatever month sits under the pointer.
 */
const DRAG_THRESHOLD_PX = 3

/**
 * How long the rail stays "awake" after the last sign of activity — a scroll of
 * the grid, or a touch of the rail itself. Awake it is fully opaque, carries a
 * backing plate so its labels stay legible over the photographs it overlays, and
 * shows the month bubble; asleep it fades back to a hint of a scale. On desktop
 * the state changes nothing (the rail sits in its own margin and is always
 * readable), so this only shapes the phone rail.
 */
const IDLE_MS = 1600

/**
 * The style the rail carries: the measured top edge of the grid, published as a
 * custom property the phone stylesheet starts the rail at (see
 * {@link useGridTop}).
 */
type RailStyle = CSSProperties & Record<'--kukatko-timeline-top', string>

/**
 * Tracks the grid's top edge in viewport coordinates, so the fixed rail can start
 * where the grid does instead of at a constant offset.
 *
 * The constant was the bug: the phone rail began 6 rem below the navbar, which is
 * the height of a filter row and nothing else. Anything the page renders above
 * the grid — the „Co je nového" digest on the first visit of the day, an
 * instance-wide announcement (permanently, while it is published), a filter row
 * that wrapped onto another line — pushes the filter row down *under* the rail,
 * and then a tap aimed at **Filtry** lands on a year tick and throws the grid a
 * hundred thousand pixels away. Measuring removes the assumption: whatever
 * precedes the grid, the rail begins below it.
 *
 * The answer is clamped at 0 rather than allowed to go negative — once the header
 * has scrolled off the top the rail is free to run up to its floor (which the
 * stylesheet, not this hook, decides: the sticky navbar), and the clamp is also
 * what stops every further scroll frame from re-rendering the rail with a more
 * negative number. Measurement itself is coalesced into an animation frame.
 *
 * Returns null until the first measurement, and for a rail rendered without a
 * grid to measure; the stylesheet then falls back to the old constant.
 */
function useGridTop(gridWrapRef: RefObject<HTMLElement | null>): number | null {
  const [top, setTop] = useState<number | null>(null)
  // A layout effect, so the first measurement is published before the browser
  // paints: a rail that is briefly in the wrong place is a rail that can briefly
  // take the wrong tap.
  useLayoutEffect(() => {
    const grid = gridWrapRef.current
    if (grid === null) {
      return
    }
    let frame = 0
    const measure = (): void => {
      frame = 0
      setTop(Math.max(0, Math.round(grid.getBoundingClientRect().top)))
    }
    const schedule = (): void => {
      if (frame === 0) {
        frame = window.requestAnimationFrame(measure)
      }
    }
    measure()
    window.addEventListener('scroll', schedule, { passive: true })
    window.addEventListener('resize', schedule)
    // What moves the grid is what is rendered *above* it, not the grid itself, so
    // the page body is what has to be watched: the digest arriving, an
    // announcement being published or dismissed, the filter row re-wrapping.
    // (ResizeObserver is absent in jsdom; the measurement above still runs.)
    const observer = typeof ResizeObserver === 'function' ? new ResizeObserver(schedule) : null
    observer?.observe(document.body)
    return () => {
      if (frame !== 0) {
        window.cancelAnimationFrame(frame)
      }
      window.removeEventListener('scroll', schedule)
      window.removeEventListener('resize', schedule)
      observer?.disconnect()
    }
  }, [gridWrapRef])
  return top
}

/** A month the rail asks the grid to jump to. */
export interface TimelineJump {
  /**
   * The grid index of the month's first photo — its bucket's `cumulative`. The
   * grid can scroll straight to it: the index is absolute in the whole result,
   * not relative to what happens to be loaded.
   */
  index: number
  /** The month, as `YYYY-MM`, for the view's URL anchor. */
  month: string
  /**
   * True for a jump that should not leave a history entry behind: the steps of a
   * drag (which crosses a month at a time) and the restore of an anchor that is
   * already in the URL. A deliberate click on a tick pushes.
   */
  replace: boolean
}

/** Props for {@link TimelineScrubber}. */
export interface TimelineScrubberProps {
  /** The active library filters; the timeline is fetched with these and refetched on change. */
  params: PhotoListParams
  /** The first visible photo index in the grid, used to highlight the current month. */
  activeIndex: number
  /**
   * The element wrapping the grid the rail scrubs. The rail is `position: fixed`
   * and on a phone it lies over the page rather than in a margin beside it, so it
   * has to know where the page's own header ends. It publishes this element's
   * measured top edge as `--kukatko-timeline-top` and the phone stylesheet starts
   * the rail there — see {@link useGridTop}. Required, so that a page cannot
   * render the rail without saying what it must stay below.
   */
  gridWrapRef: RefObject<HTMLElement | null>
  /**
   * The month (`YYYY-MM`) the view is anchored to, from the URL. Once the
   * timeline is loaded the rail resolves it to a grid index and jumps there
   * once, which is what makes Back — and a shared link — land on the month the
   * reader was looking at. Empty for an un-anchored view.
   */
  anchor?: string
  /**
   * Render nothing unless the result spans at least this many calendar months
   * ({@link spanMonths}). A rail is a way across *time*, so on a list that is all
   * one season it is a control offering nothing — an album of a single afternoon
   * does not need a scale of months laid over its photographs. The library passes
   * nothing (0): it is the whole archive, and it always spans it.
   */
  minSpanMonths?: number
  /** Jumps the grid to a month. */
  onJump: (jump: TimelineJump) => void
}

/**
 * A thin, fixed vertical date rail beside the library grid. It is a scale of the
 * months the library holds: every month bucket owns an equal slice of the rail
 * (`fractionForRank`), the rail is thinned to what its measured height can show
 * (`buildRail` — ticks no closer than a few pixels, year labels only where one
 * clears the previous), and a click or a drag jumps the grid to a month via
 * {@link TimelineScrubberProps.onJump} using that bucket's `cumulative` as the
 * scroll index. That index is the month's absolute position in the whole result
 * — the database counted it — so the grid scrolls straight to it and fetches the
 * page that lands there. The jump costs the same whether the month is the second
 * one or the ten-thousandth.
 *
 * Positions used to be proportional to `cumulative / total`, i.e. to photo
 * counts. On a real long-tailed archive (121 years, ~98 % of the photos in the
 * last two decades) that squeezed six decades into a couple of pixels and
 * stacked 103 year labels on top of each other. Per-month slices fix both, and
 * because drag reads the same mapping backwards (`rankForFraction`), the rail
 * and the grid still agree on where a position lands.
 *
 * As the grid scrolls, the tick owning the visible range start is highlighted
 * and a floating bubble names its month. The rail overlays the viewport
 * (`position: fixed`), so a loading or empty timeline simply renders nothing and
 * never shifts the grid layout.
 *
 * It runs whichever way its grid does. The backend returns the histogram in the
 * grid's own order, so an album read oldest-first (its resting state) gets a rail
 * whose top is its first month — nothing here assumes newest-first beyond the
 * order the buckets arrive in. What a *shorter* list gets is nothing at all: see
 * {@link TimelineScrubberProps.minSpanMonths}.
 *
 * **The phone gets the same rail, narrowed and dimmed.** It used to be hidden
 * below 576 px, which left a phone — where photos are actually browsed — with
 * nothing but scrolling to cross a 369 000 px long list. So there the rail keeps
 * only its year labels, in a strip along the right edge, and it sleeps: at rest
 * it is a faint scale over the photographs, and any sign of activity — the grid
 * scrolling under it, or a finger on it — wakes it for {@link IDLE_MS}, opaque,
 * plated for legibility and showing the month bubble. That awake/asleep state is
 * the `is-active` class; everything it means is CSS, and on desktop it means
 * nothing at all. Where the phone rail *starts* is measured, not assumed: it
 * publishes the grid's own top edge and begins there, so nothing the page renders
 * above the grid can end up underneath it — see {@link useGridTop}.
 */
export function TimelineScrubber({
  params,
  activeIndex,
  gridWrapRef,
  anchor = '',
  minSpanMonths = 0,
  onJump,
}: TimelineScrubberProps) {
  const { t, i18n } = useTranslation()
  const { buckets, total, status } = useTimeline(params)
  const gridTop = useGridTop(gridWrapRef)
  // The rail element is state, not a ref: its height decides how much of the
  // timeline can be drawn, so mounting it has to trigger a measuring render.
  const [rail, setRail] = useState<HTMLElement | null>(null)
  const [railHeight, setRailHeight] = useState(0)
  // The last bucket a drag jumped to, so a continuous drag only fires a new jump
  // when it crosses into a different month.
  const lastJumpedRef = useRef<string | null>(null)
  const draggingRef = useRef(false)
  const draggedRef = useRef(false)
  const dragOriginRef = useRef(0)

  // Awake or asleep — see {@link IDLE_MS}. `bump` restarts the countdown, so a
  // continuous scroll or drag keeps the rail up for as long as it lasts.
  const [awake, setAwake] = useState(false)
  const idleTimerRef = useRef<number | undefined>(undefined)
  const bump = useCallback(() => {
    setAwake(true)
    window.clearTimeout(idleTimerRef.current)
    idleTimerRef.current = window.setTimeout(() => {
      setAwake(false)
    }, IDLE_MS)
  }, [])
  // The grid's visible range moving *is* the scroll signal — no listener of our
  // own — and the first run doubles as the rail introducing itself on arrival.
  useEffect(() => {
    bump()
  }, [activeIndex, bump])
  useEffect(
    () => () => {
      window.clearTimeout(idleTimerRef.current)
    },
    [],
  )

  useEffect(() => {
    if (rail === null) {
      return
    }
    const measure = (): void => {
      setRailHeight(rail.getBoundingClientRect().height)
    }
    measure()
    // ResizeObserver is absent in jsdom; the one-off measure above still runs
    // there, and `buildRail` falls back to a nominal height when it reads 0.
    const observer =
      typeof ResizeObserver === 'function'
        ? new ResizeObserver(() => {
            measure()
          })
        : null
    observer?.observe(rail)
    return () => {
      observer?.disconnect()
    }
  }, [rail])

  const ticks = useMemo(() => buildRail(buckets, railHeight), [buckets, railHeight])
  const activeRank = useMemo(() => rankForIndex(buckets, activeIndex), [buckets, activeIndex])
  const activeBucket = activeRank >= 0 ? buckets[activeRank] : undefined

  // The anchor this rail has already acted on. A jump the rail itself made is
  // recorded here too, so the URL it writes does not bounce straight back as an
  // anchor to restore.
  const appliedAnchorRef = useRef<string | null>(null)
  const emitJump = useCallback(
    (bucket: TimelineBucket, replace: boolean) => {
      const month = anchorOf(bucket)
      appliedAnchorRef.current = month
      onJump({ index: bucket.cumulative, month, replace })
    },
    [onJump],
  )

  // Maps a pointer Y position on the rail to the month it lands in and jumps to
  // it, de-duplicating repeated jumps to the same month during a drag.
  const jumpToPointer = useCallback(
    (clientY: number) => {
      if (rail === null || buckets.length === 0) {
        return
      }
      const rect = rail.getBoundingClientRect()
      if (rect.height <= 0) {
        return
      }
      const fraction = Math.min(1, Math.max(0, (clientY - rect.top) / rect.height))
      const bucket = buckets[rankForFraction(fraction, buckets.length)]
      const key = bucketKey(bucket)
      if (key !== lastJumpedRef.current) {
        lastJumpedRef.current = key
        // A drag sweeps through months; each step replaces the anchor rather
        // than pushing, so Back returns to where the drag started, not to every
        // month it passed.
        emitJump(bucket, draggedRef.current)
      }
    },
    [rail, buckets, emitJump],
  )

  const handlePointerDown = useCallback(
    (event: React.PointerEvent<HTMLElement>) => {
      draggingRef.current = true
      draggedRef.current = false
      dragOriginRef.current = event.clientY
      lastJumpedRef.current = null
      bump()
      // The pointer is deliberately NOT captured here. Capturing on press
      // retargets the compatibility mouse events — and with them the `click` —
      // to the capturing element, so a tick would never see its own click and
      // pressing one would do nothing. Capture is taken on the first real move
      // instead, once the gesture is unambiguously a drag.
      //
      // A press on a tick only arms the drag: releasing it fires the tick's own
      // click, which lands on that tick's exact month. A press on the bare rail
      // has no tick to defer to and jumps straight away. Ticks must not swallow
      // the drag — once the rail is dense they cover most of its surface.
      if ((event.target as HTMLElement).closest('button') === null) {
        jumpToPointer(event.clientY)
      }
    },
    [jumpToPointer, bump],
  )

  const handlePointerMove = useCallback(
    (event: React.PointerEvent<HTMLElement>) => {
      if (!draggingRef.current) {
        return
      }
      bump()
      if (!draggedRef.current) {
        if (Math.abs(event.clientY - dragOriginRef.current) < DRAG_THRESHOLD_PX) {
          return
        }
        draggedRef.current = true
        // Now that this is a drag, capture the pointer so it keeps tracking
        // outside the rail's bounds.
        rail?.setPointerCapture(event.pointerId)
      }
      jumpToPointer(event.clientY)
    },
    [rail, jumpToPointer, bump],
  )

  const endDrag = useCallback(
    (event: React.PointerEvent<HTMLElement>) => {
      draggingRef.current = false
      lastJumpedRef.current = null
      if (rail?.hasPointerCapture(event.pointerId) === true) {
        rail.releasePointerCapture(event.pointerId)
      }
    },
    [rail],
  )

  const handleTickClick = useCallback(
    (event: React.MouseEvent<HTMLButtonElement>, bucket: TimelineBucket) => {
      // Ignore the click that ends a drag — the drag already jumped. A keyboard
      // activation carries no pointer detail and always jumps.
      if (event.detail !== 0 && draggedRef.current) {
        return
      }
      emitJump(bucket, false)
    },
    [emitJump],
  )

  // Restore the anchor the URL carries (a reload, a shared link, or Back onto a
  // jumped-to view) exactly once per anchor, as soon as the buckets that resolve
  // it to a grid index have arrived. Jumping straight to a month costs one scroll
  // and one page fetch, so this is as cheap on the archive's oldest month as on
  // its newest.
  useEffect(() => {
    if (anchor === '' || buckets.length === 0 || appliedAnchorRef.current === anchor) {
      return
    }
    const bucket = buckets.find((candidate) => anchorOf(candidate) === anchor)
    appliedAnchorRef.current = anchor
    if (bucket === undefined) {
      // The month holds nothing under the current filters; leave the grid alone
      // rather than guessing at a neighbour.
      return
    }
    emitJump(bucket, true)
  }, [anchor, buckets, emitJump])

  // Nothing to scrub (loading, error, an empty library, or a result too short in
  // time to be worth a scale of months): render no rail so the grid layout never
  // shifts.
  if (status !== 'ready' || buckets.length === 0 || total <= 0) {
    return null
  }
  if (spanMonths(buckets) < minSpanMonths) {
    return null
  }

  return (
    <nav
      ref={setRail}
      className={`kukatko-timeline${awake ? ' is-active' : ''}`}
      style={
        gridTop === null ? undefined : ({ '--kukatko-timeline-top': `${gridTop}px` } as RailStyle)
      }
      aria-label={t('library.timeline.label')}
      onPointerDown={handlePointerDown}
      onPointerMove={handlePointerMove}
      onPointerUp={endDrag}
      onPointerCancel={endDrag}
    >
      {activeBucket && (
        <span
          className="kukatko-timeline-current"
          style={{ top: `${fractionForRank(activeRank, buckets.length) * 100}%` }}
          aria-hidden="true"
        >
          {formatMonth(activeBucket.year, activeBucket.month, i18n.language)}
        </span>
      )}
      {ticks.map((tick) => {
        const active = activeRank >= tick.firstRank && activeRank <= tick.lastRank
        const collapsed = tick.firstRank !== tick.lastRank
        return (
          <button
            key={tick.key}
            type="button"
            className={`kukatko-timeline-tick${tick.year === null ? '' : ' has-year'}${active ? ' active' : ''}`}
            style={{ top: `${tick.top}%` }}
            aria-current={active ? 'true' : undefined}
            aria-label={
              collapsed
                ? t('library.timeline.jumpToRange', {
                    from: formatMonth(tick.oldest.year, tick.oldest.month, i18n.language),
                    to: formatMonth(tick.newest.year, tick.newest.month, i18n.language),
                  })
                : t('library.timeline.jumpTo', {
                    month: formatMonth(tick.newest.year, tick.newest.month, i18n.language),
                  })
            }
            onClick={(event) => {
              handleTickClick(event, tick.target)
            }}
          >
            <span className="kukatko-timeline-mark" aria-hidden="true" />
            {tick.year !== null && (
              <span className="kukatko-timeline-year" aria-hidden="true">
                {tick.year}
              </span>
            )}
          </button>
        )
      })}
    </nav>
  )
}
