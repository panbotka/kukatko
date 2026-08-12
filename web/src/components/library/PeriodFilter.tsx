import { useEffect, useMemo, useRef, useState } from 'react'
import Form from 'react-bootstrap/Form'
import { useTranslation } from 'react-i18next'

import {
  ANY_PERIOD,
  DECADE_YEARS,
  type DecadeGroup,
  decadeOf,
  formatPeriod,
  groupYearsIntoDecades,
  isAnyPeriod,
  type Period,
  periodForYears,
  yearSpanOf,
} from '../../lib/period'
import { type YearBucket } from '../../services/photos'
import { Icon } from '../Icon'

/** Props for {@link PeriodFilter}. */
export interface PeriodFilterProps {
  /** Unique id tying the label, the trigger and the panel together. */
  id: string
  /** The period currently in force, {@link ANY_PERIOD} for "everything". */
  value: Period
  /**
   * The years that hold photos, newest first, each with its count — grouped into
   * decades here. Empty on pages that supply no facets; the panel then offers its
   * exact-date fields alone rather than a list of periods the page cannot count.
   */
  years: YearBucket[]
  /**
   * The period the search query itself sets, or `null` when it sets none that can
   * be shown as one. It is what the trigger displays while the control's own
   * value is empty, so the control can never read "any period" over a grid the
   * query has narrowed to the sixties.
   */
  queryPeriod: Period | null
  /** DOM id of the note quoting the query tokens, for `aria-describedby`. */
  describedBy?: string
  /** Called with the chosen period; {@link ANY_PERIOD} clears the filter. */
  onChange: (period: Period) => void
}

/**
 * The library's one control on the time axis.
 *
 * It replaced two filters that both meant "when was this taken": a **Rok**
 * dropdown of 109 single years, out of which no decade could be assembled, and a
 * **Pořízeno od / do** pair hidden one click deeper in the advanced panel, which
 * could express a range but nobody found. This control is that range, in the
 * primary row, at both grains: a list of the **decades** the library holds, each
 * expandable to its own years (with counts, so the reader sees how much a period
 * holds before committing), and the exact-date pair underneath for "summer 2019".
 * Only decades and years that actually hold photos are offered, exactly as the
 * year dropdown promised.
 *
 * Everything it writes is the one {@link Period} — one pair of URL keys — so
 * there is no second state to drift out of step, and its trigger states the
 * period in words (`1960–1969`, `od 1960`, `1. 6. 2019 – 31. 8. 2019`) rather
 * than making the reader open it to find out.
 *
 * When the search box already scopes the period (`year:1960-1969`), the trigger
 * shows **that** period — `queryPeriod`, derived from the query itself — so the
 * control and the query cannot contradict each other; the note the caller renders
 * below quotes the responsible tokens. Picking a period on top of one still
 * narrows further, as ANDed filters do everywhere else in the bar.
 *
 * Built from the same primitives as {@link import('./SearchableSelect').SearchableSelect}
 * — a trigger styled as a select plus an inline `.dropdown-menu` — so it closes
 * on Escape and on focus leaving the widget, and needs no popper.
 */
export function PeriodFilter({
  id,
  value,
  years,
  queryPeriod,
  describedBy,
  onChange,
}: PeriodFilterProps) {
  const { t, i18n } = useTranslation()
  const [open, setOpen] = useState(false)
  const [expanded, setExpanded] = useState<number | null>(null)
  const containerRef = useRef<HTMLDivElement>(null)
  const triggerRef = useRef<HTMLButtonElement>(null)

  const panelId = `${id}-panel`
  const decades = useMemo(() => groupYearsIntoDecades(years), [years])
  const span = yearSpanOf(value)

  // What the trigger says: the control's own period, or — while it has none —
  // the one the query sets, so the resting "any period" is only ever shown over
  // a grid that really is unfiltered in time.
  const shown = isAnyPeriod(value) && queryPeriod !== null ? queryPeriod : value
  const label = formatPeriod(shown, t, i18n.language)

  // Open on the decade the current period sits in, so refining "the sixties"
  // down to 1965 is one click rather than a hunt through thirteen rows.
  useEffect(() => {
    if (!open) {
      return
    }
    const start = span?.from ?? null
    setExpanded(start === null ? null : decadeOf(start))
    // The current span is read once, when the panel opens; re-reading it on every
    // change would fight the reader's own expand/collapse.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open])

  function choose(period: Period) {
    setOpen(false)
    onChange(period)
    triggerRef.current?.focus()
  }

  return (
    <div
      ref={containerRef}
      className="position-relative"
      onBlur={(event) => {
        // Close only when focus leaves the whole widget, not on inner moves.
        if (!containerRef.current?.contains(event.relatedTarget)) {
          setOpen(false)
        }
      }}
      onKeyDown={(event) => {
        if (event.key === 'Escape' && open) {
          event.stopPropagation()
          setOpen(false)
          triggerRef.current?.focus()
        }
      }}
    >
      <Form.Label htmlFor={id} className="small mb-1">
        {t('library.filters.period')}
      </Form.Label>
      <button
        ref={triggerRef}
        id={id}
        type="button"
        // Styled as the selects beside it, so the primary row reads as one row of
        // pickers; it is a disclosure button because its panel is not a list of
        // options but two grains of one.
        className="form-select text-start kukatko-tap-target d-flex align-items-center gap-2"
        aria-expanded={open}
        aria-controls={panelId}
        aria-label={t('library.filters.periodValue', { value: label })}
        aria-describedby={describedBy}
        onClick={() => {
          setOpen((prev) => !prev)
        }}
      >
        <Icon name="calendar-range" />
        <span className="text-truncate">{label}</span>
      </button>

      {open && (
        <div
          id={panelId}
          className="dropdown-menu show w-100 mt-1 shadow overflow-auto kukatko-period-panel"
          style={{ top: '100%' }}
        >
          <button
            type="button"
            className={`dropdown-item kukatko-tap-target ${isAnyPeriod(value) ? 'active' : ''}`}
            onClick={() => {
              choose(ANY_PERIOD)
            }}
          >
            {t('library.filters.anyPeriod')}
          </button>

          {decades.length > 0 && (
            <>
              <h6 className="dropdown-header">{t('library.filters.decades')}</h6>
              <ul className="list-unstyled mb-0">
                {decades.map((group) => (
                  <DecadeRow
                    key={group.decade}
                    group={group}
                    span={span}
                    expanded={expanded === group.decade}
                    onToggle={() => {
                      setExpanded((prev) => (prev === group.decade ? null : group.decade))
                    }}
                    onChoose={choose}
                  />
                ))}
              </ul>
            </>
          )}

          {/* Pinned to the bottom of the scroll area, not left to scroll away
              below thirteen decades: a date pair nobody finds is exactly the
              filter this control replaced. */}
          <fieldset className="px-3 pb-2 kukatko-period-exact">
            <legend className="dropdown-header px-0 fs-6">{t('library.filters.exactDates')}</legend>
            {/* Side by side, which is what the panel's own `min-width` buys: a
                pinned footer has to stay short, and stacking these two costs
                another 76 px of the decade list behind it. */}
            <div className="row g-2">
              <div className="col-6">
                <Form.Group controlId="library-taken-after">
                  <Form.Label className="small mb-1">{t('library.filters.takenAfter')}</Form.Label>
                  <Form.Control
                    type="date"
                    value={value.from}
                    onChange={(event) => {
                      onChange({ ...value, from: event.target.value })
                    }}
                  />
                </Form.Group>
              </div>
              <div className="col-6">
                <Form.Group controlId="library-taken-before">
                  <Form.Label className="small mb-1">{t('library.filters.takenBefore')}</Form.Label>
                  <Form.Control
                    type="date"
                    value={value.to}
                    onChange={(event) => {
                      onChange({ ...value, to: event.target.value })
                    }}
                  />
                </Form.Group>
              </div>
            </div>
          </fieldset>
        </div>
      )}
    </div>
  )
}

/**
 * One decade of the panel: a row selecting the whole decade, and a chevron
 * opening the years inside it. Two targets rather than one because both are real
 * answers — "the sixties" and "1965" — and burying either behind the other costs
 * a click on the axis this library is searched by.
 */
function DecadeRow({
  group,
  span,
  expanded,
  onToggle,
  onChoose,
}: {
  group: DecadeGroup
  span: { from: number | null; to: number | null } | null
  expanded: boolean
  onToggle: () => void
  onChoose: (period: Period) => void
}) {
  const { t } = useTranslation()
  const range = `${group.decade}–${group.decade + DECADE_YEARS - 1}`
  const selected = span?.from === group.decade && span.to === group.decade + DECADE_YEARS - 1
  // A bare chevron beside a decade could unfold anything; the sentence is
  // interpolated once and worn as both the accessible name and the hover hint.
  const yearsLabel = t('library.filters.decadeYears', { range })
  return (
    <li>
      <div className="d-flex align-items-center">
        <button
          type="button"
          className={`dropdown-item kukatko-tap-target d-flex align-items-center justify-content-between gap-2 ${
            selected ? 'active' : ''
          }`}
          onClick={() => {
            onChoose(periodForYears(group.decade, group.decade + DECADE_YEARS - 1))
          }}
        >
          <span>{range}</span>
          <span className="text-secondary small flex-shrink-0">{group.count}</span>
        </button>
        <button
          type="button"
          className="btn btn-link text-body-secondary px-2 flex-shrink-0"
          aria-expanded={expanded}
          aria-label={yearsLabel}
          title={yearsLabel}
          onClick={onToggle}
        >
          <Icon name={expanded ? 'chevron-up' : 'chevron-down'} />
        </button>
      </div>
      {expanded && (
        <ul className="list-unstyled mb-0 ms-3">
          {group.years.map((bucket) => (
            <li key={bucket.year}>
              <button
                type="button"
                className={`dropdown-item kukatko-tap-target d-flex align-items-center justify-content-between gap-2 ${
                  span?.from === bucket.year && span.to === bucket.year ? 'active' : ''
                }`}
                onClick={() => {
                  onChoose(periodForYears(bucket.year, bucket.year))
                }}
              >
                <span>{bucket.year}</span>
                <span className="text-secondary small flex-shrink-0">{bucket.count}</span>
              </button>
            </li>
          ))}
        </ul>
      )}
    </li>
  )
}
