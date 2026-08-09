import { useEffect, useRef } from 'react'
import { Link } from 'react-router-dom'

import './charts.css'

/** One column of a {@link ColumnChart}. */
export interface ColumnDatum {
  /** Stable identity of the bucket (the year, the `YYYY-MM` month). */
  key: string
  /** The measured value; zero draws a baseline tick rather than nothing. */
  value: number
  /** Hover text spelling the bucket and its value out ("1978: 42 photos"). */
  title: string
  /** Axis label. Only ticked columns carry one — a label per bar is unreadable. */
  tick?: string
  /** Where the column leads when it leads anywhere; omitted for empty buckets. */
  href?: string
  /** Accessible name of that link: the destination, not the number. */
  linkLabel?: string
}

/**
 * A column histogram of one measure over time — plain CSS bars, no charting
 * dependency, in the app's own accent.
 *
 * The whole column is the hit area rather than the drawn bar, because a thin
 * year would otherwise be a five-pixel target, and an empty bucket is never a
 * link: there would be nothing behind it. Buckets with no data still draw a
 * baseline tick so the axis reads as continuous time instead of as a chart with
 * holes in it, and the plot scrolls sideways on a narrow screen rather than
 * squeezing a century of years into hairlines — opening at its newest end, which
 * is the end a reader is looking for and the end that is not empty.
 *
 * Accessibility: the summary in `ariaLabel` carries the key numbers, so a screen
 * reader gets the chart's content without the bars. A chart whose columns link
 * somewhere is a `group` (its links stay reachable, each named by its
 * destination); one that does not is a single `img`, its bars hidden.
 */
export function ColumnChart({
  data,
  ariaLabel,
  testId,
}: {
  data: ColumnDatum[]
  ariaLabel: string
  testId?: string
}) {
  const max = data.reduce((highest, datum) => Math.max(highest, datum.value), 0)
  const interactive = data.some((datum) => datum.href !== undefined)
  const ticked = data.some((datum) => datum.tick !== undefined)
  const scroll = useRef<HTMLDivElement>(null)

  // An axis too wide for the screen opens at its newest end. A century-long
  // library is emptiest at its oldest end, so a phone landing on 1905 sees a row
  // of blank years and reads the chart as broken. Mount only: after that the
  // scroll position is the reader's.
  useEffect(() => {
    if (scroll.current !== null) {
      scroll.current.scrollLeft = scroll.current.scrollWidth
    }
  }, [])

  return (
    <div className="kk-chart-scroll" ref={scroll}>
      <div role={interactive ? 'group' : 'img'} aria-label={ariaLabel} data-testid={testId}>
        <div className="kk-chart-plot" aria-hidden={interactive ? undefined : true}>
          {data.map((datum) => (
            <Column key={datum.key} datum={datum} max={max} />
          ))}
        </div>
        {ticked && (
          <div className="kk-chart-axis" aria-hidden="true">
            {data.map((datum) => (
              <div className="kk-chart-tick" key={datum.key}>
                {datum.tick !== undefined && <span>{datum.tick}</span>}
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  )
}

/**
 * One column: the bar scaled against the chart's tallest, wrapped in a link when
 * the bucket has both photos and somewhere to send the reader.
 */
function Column({ datum, max }: { datum: ColumnDatum; max: number }) {
  const empty = datum.value <= 0
  const bar = (
    <div
      className={`kk-chart-bar${empty ? ' kk-chart-bar--empty' : ''}`}
      style={empty ? undefined : { height: `${barHeight(datum.value, max)}%` }}
      data-testid={`chart-bar-${datum.key}`}
    />
  )
  if (empty || datum.href === undefined) {
    return (
      <div className="kk-chart-col" title={datum.title}>
        {bar}
      </div>
    )
  }
  return (
    <Link
      to={datum.href}
      className="kk-chart-col"
      title={datum.title}
      aria-label={datum.linkLabel ?? datum.title}
    >
      {bar}
    </Link>
  )
}

/**
 * The bar's height as a percentage of the plot. A non-zero value never rounds
 * away to nothing: one photo in a year of thousands still has to be visible, or
 * the chart would claim the year is empty.
 */
function barHeight(value: number, max: number): number {
  if (max <= 0) {
    return 0
  }
  return Math.max(2, Math.round((value / max) * 1000) / 10)
}
