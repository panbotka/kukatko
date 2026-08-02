import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { useTimeline } from '../../hooks/useTimeline'
import { formatMonth } from '../../lib/format'
import { type PhotoListParams } from '../../services/photos'

import {
  bucketKey,
  buildRail,
  fractionForRank,
  rankForFraction,
  rankForIndex,
} from './timelineRail'

/**
 * How far a pointer has to travel before a press turns into a drag. Below it the
 * press stays a click, so a click on a tick lands on that tick's exact month
 * instead of on whatever month sits under the pointer.
 */
const DRAG_THRESHOLD_PX = 3

/** Props for {@link TimelineScrubber}. */
export interface TimelineScrubberProps {
  /** The active library filters; the timeline is fetched with these and refetched on change. */
  params: PhotoListParams
  /** The first visible photo index in the grid, used to highlight the current month. */
  activeIndex: number
  /** Jumps the grid to a photo index (loads pages first when it lies ahead). */
  onJump: (index: number) => void
}

/**
 * A thin, fixed vertical date rail beside the library grid. It is a scale of the
 * months the library holds: every month bucket owns an equal slice of the rail
 * (`fractionForRank`), the rail is thinned to what its measured height can show
 * (`buildRail` — ticks no closer than a few pixels, year labels only where one
 * clears the previous), and a click or a drag jumps the grid to a month via
 * {@link TimelineScrubberProps.onJump} using that bucket's `cumulative` as the
 * scroll index.
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
 * never shifts the grid layout; on very small screens it is hidden via CSS to
 * avoid crowding the grid.
 */
export function TimelineScrubber({ params, activeIndex, onJump }: TimelineScrubberProps) {
  const { t, i18n } = useTranslation()
  const { buckets, total, status } = useTimeline(params)
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
        onJump(bucket.cumulative)
      }
    },
    [rail, buckets, onJump],
  )

  const handlePointerDown = useCallback(
    (event: React.PointerEvent<HTMLElement>) => {
      draggingRef.current = true
      draggedRef.current = false
      dragOriginRef.current = event.clientY
      lastJumpedRef.current = null
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
    [jumpToPointer],
  )

  const handlePointerMove = useCallback(
    (event: React.PointerEvent<HTMLElement>) => {
      if (!draggingRef.current) {
        return
      }
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
    [rail, jumpToPointer],
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
    (event: React.MouseEvent<HTMLButtonElement>, cumulative: number) => {
      // Ignore the click that ends a drag — the drag already jumped. A keyboard
      // activation carries no pointer detail and always jumps.
      if (event.detail !== 0 && draggedRef.current) {
        return
      }
      onJump(cumulative)
    },
    [onJump],
  )

  // Nothing to scrub yet (loading, error or an empty library): render no rail so
  // the grid layout never shifts.
  if (status !== 'ready' || buckets.length === 0 || total <= 0) {
    return null
  }

  return (
    <nav
      ref={setRail}
      className="kukatko-timeline"
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
              handleTickClick(event, tick.target.cumulative)
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
