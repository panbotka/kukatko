import { useTranslation } from 'react-i18next'

import { useGridDensity } from '../../hooks/useGridDensity'
import { GRID_GAP_PX, gridTemplateColumns } from '../../lib/gridDensity'

/**
 * Placeholder grid shown during the first-page load. It mirrors the real grid's
 * columns — the user's chosen density included — and square tiles so there is no
 * layout shift when the photos arrive. Purely decorative — announced once to
 * assistive tech.
 */
export function GridSkeleton({ count = 24 }: { count?: number }) {
  const { t } = useTranslation()
  const { density } = useGridDensity()
  return (
    <div
      role="status"
      aria-busy="true"
      aria-label={t('library.loading')}
      style={{
        display: 'grid',
        gridTemplateColumns: gridTemplateColumns(density),
        gap: `${GRID_GAP_PX}px`,
      }}
    >
      {Array.from({ length: count }, (_, i) => (
        <div
          key={i}
          className="bg-secondary-subtle placeholder-wave rounded"
          style={{ aspectRatio: '1 / 1' }}
          aria-hidden="true"
        >
          <span className="placeholder w-100 h-100 d-block rounded" />
        </div>
      ))}
      <span className="visually-hidden">{t('library.loading')}</span>
    </div>
  )
}
