import { useTranslation } from 'react-i18next'

import { useGridDensity } from '../../hooks/useGridDensity'
import { GRID_GAP_PX, gridTemplateColumns } from '../../lib/gridDensity'
import { Skeleton } from '../Skeleton'

/**
 * Placeholder grid shown during the first-page load. It mirrors the real grid's
 * columns — the user's chosen density included — and square tiles so there is no
 * layout shift when the photos arrive. Announced once to assistive tech via the
 * container's `role="status"`; the `label` localizes that announcement (a
 * subject gallery says "loading person's photos", the library "loading photos").
 */
export function GridSkeleton({ count = 24, label }: { count?: number; label?: string }) {
  const { t } = useTranslation()
  const { density } = useGridDensity()
  const busyLabel = label ?? t('library.loading')
  return (
    <div
      role="status"
      aria-busy="true"
      aria-label={busyLabel}
      style={{
        display: 'grid',
        gridTemplateColumns: gridTemplateColumns(density),
        gap: `${GRID_GAP_PX}px`,
      }}
    >
      {Array.from({ length: count }, (_, i) => (
        <Skeleton key={i} radius="var(--kk-radius-tile)" style={{ aspectRatio: '1 / 1' }} />
      ))}
      <span className="visually-hidden">{busyLabel}</span>
    </div>
  )
}
