import { Link } from 'react-router-dom'

import './charts.css'

/** One row of a {@link BarList}. */
export interface BarDatum {
  /** Stable identity of the row (the camera model, the media bucket). */
  key: string
  /** What the row is — always visible text, so identity is never colour alone. */
  label: string
  /** The measured value, compared against the list's largest. */
  value: number
  /** The value as the reader should see it (a grouped count, a size). */
  valueLabel: string
  /** Hover text spelling the row out in full. */
  title: string
  /** Where the row leads when it leads anywhere. */
  href?: string
  /** Accessible name of that link: the destination, not the number. */
  linkLabel?: string
}

/**
 * A horizontal bar list: one measure across a handful of named things, with the
 * name and the value written out beside every bar. It is the right shape when
 * the labels are words rather than a time axis (cameras, media types) — they get
 * room to be read — and it doubles as the track behind the coverage meters.
 *
 * Accessibility: the summary in `ariaLabel` carries the key numbers; because the
 * rows are text, a reader is never asked to decode a bar. A list whose rows link
 * somewhere is a `group`, one that does not is a single `img`.
 */
export function BarList({
  data,
  ariaLabel,
  testId,
}: {
  data: BarDatum[]
  ariaLabel: string
  testId?: string
}) {
  const max = data.reduce((highest, datum) => Math.max(highest, datum.value), 0)
  const interactive = data.some((datum) => datum.href !== undefined)

  return (
    <div
      className="kk-barlist"
      role={interactive ? 'group' : 'img'}
      aria-label={ariaLabel}
      data-testid={testId}
    >
      {data.map((datum) => (
        <div className="kk-barlist-row" key={datum.key} title={datum.title}>
          <div className="kk-barlist-label">
            {datum.href === undefined ? (
              datum.label
            ) : (
              <Link to={datum.href} className="text-reset" aria-label={datum.linkLabel}>
                {datum.label}
              </Link>
            )}
          </div>
          <div className="kk-barlist-value">{datum.valueLabel}</div>
          <div className="kk-barlist-track" aria-hidden="true">
            <div
              className="kk-barlist-fill"
              style={{ width: `${share(datum.value, max)}%` }}
              data-testid={`bar-fill-${datum.key}`}
            />
          </div>
        </div>
      ))}
    </div>
  )
}

/**
 * The fill's width as a percentage of the row's track — the value against the
 * list's largest, so the longest bar always fills the track and the rest are read
 * against it. A zero value fills nothing.
 */
function share(value: number, max: number): number {
  if (max <= 0 || value <= 0) {
    return 0
  }
  return Math.round((value / max) * 1000) / 10
}
