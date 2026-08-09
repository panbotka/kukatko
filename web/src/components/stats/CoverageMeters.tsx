import { useTranslation } from 'react-i18next'

import { formatCount, formatPercent } from '../../lib/format'
import type { LibraryStats } from '../../services/system'

import { ChartCard } from './ChartCard'
import './charts.css'

/** One coverage row: how much of a whole has been done. */
interface Coverage {
  key: string
  label: string
  done: number
  total: number
}

/**
 * How complete the library is, as three progress rows: how many photos know
 * where they were taken, how many can be searched by what is in them, and how
 * many of the detected faces have a name.
 *
 * They are shares, not counts — the cards above already carry the counts — so
 * they answer the one question the numbers alone do not: how far along is it?
 * Each row is a real ARIA `meter` carrying both the share and the two numbers
 * behind it, so the answer does not depend on seeing the bar. A whole with
 * nothing in it (no faces detected yet) reads as 0 %, never as a division by
 * zero or as full coverage.
 */
export function CoverageMeters({ stats }: { stats: LibraryStats }) {
  const { t, i18n } = useTranslation()
  const rows: Coverage[] = [
    {
      key: 'gps',
      label: t('stats.charts.coverage.gps'),
      done: stats.photos_with_gps,
      total: stats.photos,
    },
    {
      key: 'content',
      label: t('stats.charts.coverage.content'),
      done: stats.photos_with_embedding,
      total: stats.photos,
    },
    {
      key: 'faces',
      label: t('stats.charts.coverage.faces'),
      done: stats.faces_assigned,
      total: stats.faces,
    },
  ]

  return (
    <ChartCard
      title={t('stats.charts.coverage.title')}
      icon="ui-checks"
      hint={t('stats.charts.coverage.hint')}
    >
      <div className="kk-barlist" data-testid="coverage-meters">
        {rows.map((row) => {
          const ratio = row.total > 0 ? row.done / row.total : 0
          const percent = formatPercent(ratio, i18n.language)
          const label = t('stats.charts.coverage.meter', {
            label: row.label,
            done: formatCount(row.done, i18n.language),
            total: formatCount(row.total, i18n.language),
            percent,
          })
          return (
            <div className="kk-barlist-row" key={row.key}>
              <div className="kk-barlist-label">{row.label}</div>
              <div className="kk-barlist-value" data-testid={`coverage-${row.key}`}>
                {percent}
              </div>
              <div
                className="kk-barlist-track"
                role="meter"
                aria-label={label}
                aria-valuenow={Math.round(ratio * 100)}
                aria-valuemin={0}
                aria-valuemax={100}
                aria-valuetext={label}
              >
                <div
                  className="kk-barlist-fill kk-meter-fill"
                  style={{ width: `${ratio * 100}%` }}
                />
              </div>
            </div>
          )
        })}
      </div>
    </ChartCard>
  )
}
