import { useCallback, useMemo, useRef } from 'react'
import { useTranslation } from 'react-i18next'

import { useTimeline } from '../../hooks/useTimeline'
import { formatMonth } from '../../lib/format'
import { type PhotoListParams, type TimelineBucket } from '../../services/photos'

/** A stable key for a month bucket (year+month uniquely identifies it). */
function bucketKey(bucket: TimelineBucket): string {
  return `${bucket.year}-${bucket.month}`
}

/**
 * Returns the bucket that owns a grid `index`: the last bucket whose cumulative
 * start is at or before the index. Buckets are newest-first with ascending
 * cumulatives, so this maps a scroll position back to its month. Returns
 * `undefined` only for an empty list.
 */
function bucketForIndex(buckets: TimelineBucket[], index: number): TimelineBucket | undefined {
  let found = buckets[0]
  for (const bucket of buckets) {
    if (bucket.cumulative <= index) {
      found = bucket
    } else {
      break
    }
  }
  return found
}

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
 * A thin, fixed vertical date rail beside the library grid. Each month bucket is
 * a clickable tick positioned proportionally to its `cumulative / total`, so the
 * rail is a scaled overview of the whole (default date-sorted) library. Clicking
 * a tick — or dragging along the rail — jumps the grid to that month via
 * {@link TimelineScrubberProps.onJump} using the bucket's `cumulative` as the
 * scroll index. As the grid scrolls, the bucket owning the visible range start is
 * highlighted. The rail overlays the viewport (position: fixed), so a loading or
 * empty timeline simply renders nothing and never shifts the grid layout; on very
 * small screens it is hidden via CSS to avoid crowding the grid.
 */
export function TimelineScrubber({ params, activeIndex, onJump }: TimelineScrubberProps) {
  const { t, i18n } = useTranslation()
  const { buckets, total, status } = useTimeline(params)
  const railRef = useRef<HTMLElement>(null)
  // The last bucket a drag jumped to, so a continuous drag only fires a new jump
  // when it crosses into a different month.
  const lastJumpedRef = useRef<string | null>(null)
  const draggingRef = useRef(false)

  const activeBucket = useMemo(
    () => (buckets.length > 0 ? bucketForIndex(buckets, activeIndex) : undefined),
    [buckets, activeIndex],
  )
  const activeKey = activeBucket ? bucketKey(activeBucket) : null

  // Maps a pointer Y position on the rail to the month it lands in and jumps to
  // it, de-duplicating repeated jumps to the same month during a drag.
  const jumpToPointer = useCallback(
    (clientY: number) => {
      const rail = railRef.current
      if (!rail || total <= 0 || buckets.length === 0) {
        return
      }
      const rect = rail.getBoundingClientRect()
      if (rect.height <= 0) {
        return
      }
      const fraction = Math.min(1, Math.max(0, (clientY - rect.top) / rect.height))
      const bucket = bucketForIndex(buckets, Math.floor(fraction * total))
      if (!bucket) {
        return
      }
      const key = bucketKey(bucket)
      if (key !== lastJumpedRef.current) {
        lastJumpedRef.current = key
        onJump(bucket.cumulative)
      }
    },
    [buckets, total, onJump],
  )

  const handlePointerDown = useCallback(
    (event: React.PointerEvent<HTMLElement>) => {
      // Let a tick button handle its own click; only the bare rail starts a drag.
      if ((event.target as HTMLElement).closest('button') !== null) {
        return
      }
      draggingRef.current = true
      lastJumpedRef.current = null
      // Capture the pointer so a drag keeps tracking outside the rail's bounds.
      railRef.current?.setPointerCapture(event.pointerId)
      jumpToPointer(event.clientY)
    },
    [jumpToPointer],
  )

  const handlePointerMove = useCallback(
    (event: React.PointerEvent<HTMLElement>) => {
      if (!draggingRef.current) {
        return
      }
      jumpToPointer(event.clientY)
    },
    [jumpToPointer],
  )

  const endDrag = useCallback((event: React.PointerEvent<HTMLElement>) => {
    draggingRef.current = false
    lastJumpedRef.current = null
    if (railRef.current?.hasPointerCapture(event.pointerId) === true) {
      railRef.current.releasePointerCapture(event.pointerId)
    }
  }, [])

  // Nothing to scrub yet (loading, error or an empty library): render no rail so
  // the grid layout never shifts.
  if (status !== 'ready' || buckets.length === 0 || total <= 0) {
    return null
  }

  return (
    <nav
      ref={railRef}
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
          style={{ top: `${(activeBucket.cumulative / total) * 100}%` }}
          aria-hidden="true"
        >
          {formatMonth(activeBucket.year, activeBucket.month, i18n.language)}
        </span>
      )}
      {buckets.map((bucket, index) => {
        const key = bucketKey(bucket)
        const yearBoundary = index === 0 || bucket.year !== buckets[index - 1].year
        const active = key === activeKey
        return (
          <button
            key={key}
            type="button"
            className={`kukatko-timeline-tick${active ? ' active' : ''}`}
            style={{ top: `${(bucket.cumulative / total) * 100}%` }}
            aria-current={active ? 'true' : undefined}
            aria-label={t('library.timeline.jumpTo', {
              month: formatMonth(bucket.year, bucket.month, i18n.language),
            })}
            onClick={() => {
              onJump(bucket.cumulative)
            }}
          >
            <span className="kukatko-timeline-mark" aria-hidden="true" />
            {yearBoundary && (
              <span className="kukatko-timeline-year" aria-hidden="true">
                {bucket.year}
              </span>
            )}
          </button>
        )
      })}
    </nav>
  )
}
