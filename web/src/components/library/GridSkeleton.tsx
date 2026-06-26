import { useTranslation } from 'react-i18next'

/**
 * Placeholder grid shown during the first-page load. It mirrors the real grid's
 * responsive columns and square tiles so there is no layout shift when the
 * photos arrive. Purely decorative — announced once to assistive tech.
 */
export function GridSkeleton({ count = 24 }: { count?: number }) {
  const { t } = useTranslation()
  return (
    <div
      role="status"
      aria-busy="true"
      aria-label={t('library.loading')}
      style={{
        display: 'grid',
        gridTemplateColumns: 'repeat(auto-fill, minmax(140px, 1fr))',
        gap: '6px',
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
