import type { ReactNode } from 'react'
import Card from 'react-bootstrap/Card'

import { Icon, type IconName } from '../Icon'

/**
 * The shell every chart on the statistics page sits in: an eyebrow title with
 * its icon, a one-line hint saying what the chart answers, and the chart itself.
 * It matches the counts cards above it ({@link import('../LibraryStatsCards')}),
 * so the page reads as one dashboard rather than as a grid of counts with
 * pictures bolted underneath.
 */
export function ChartCard({
  title,
  icon,
  hint,
  children,
}: {
  title: string
  icon: IconName
  hint?: string
  children: ReactNode
}) {
  return (
    <Card className="h-100">
      <Card.Body>
        <h3 className="kk-text-eyebrow text-secondary d-flex align-items-center gap-2 mb-1">
          <Icon name={icon} />
          {title}
        </h3>
        {hint !== undefined && <p className="text-secondary kk-text-caption mb-3">{hint}</p>}
        {children}
      </Card.Body>
    </Card>
  )
}
