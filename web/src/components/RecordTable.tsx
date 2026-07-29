import { Fragment, type CSSProperties, type ReactNode } from 'react'
import Card from 'react-bootstrap/Card'
import Table from 'react-bootstrap/Table'

import { useIsNarrowViewport } from '../hooks/useIsNarrowViewport'

/**
 * One column of a {@link RecordTable}: its header and how to render the value
 * for a single record. The same definition drives both layouts — the `<th>`/`<td>`
 * pair of the desktop table and the label/value line of the phone card — so the
 * two can never drift apart.
 */
export interface RecordColumn<T> {
  /** Stable identity of the column; also the React key of its header and cells. */
  key: string
  /** Already-translated column heading (`<th>` on the table, `<dt>` on a card). */
  header: string
  /** Renders this column's value for one record. */
  cell: (record: T) => ReactNode
  /** Extra classes for the desktop `<td>` only (e.g. `text-nowrap`). */
  cellClassName?: string
  /** Inline style for the desktop `<td>` only (e.g. a column width cap). */
  cellStyle?: CSSProperties
  /**
   * Keeps the column out of the card's label/value list. Used for a column of row
   * actions, which a card renders as the full-width `cardActions` row instead of
   * squeezing a button cluster into a two-column grid.
   */
  cardHidden?: boolean
}

/** Props of {@link RecordTable}. */
export interface RecordTableProps<T> {
  /** The records to render, in display order. */
  records: readonly T[]
  /** The columns, in display order. */
  columns: readonly RecordColumn<T>[]
  /** Stable React key for one record. */
  rowKey: (record: T) => string
  /**
   * The record's actions, rendered as a full-width button row at the foot of its
   * card. Only the card layout calls it — on the table the actions stay a normal
   * column (typically the one marked `cardHidden`).
   */
  cardActions?: (record: T) => ReactNode
  /**
   * Extra detail for one record — an expanded row spanning every column on the
   * table, a block under the fields on the card. Return `null` for a record with
   * nothing to expand (a collapsed row, an entry with no payload).
   */
  detail?: (record: T) => ReactNode | null
  /** Extra classes for the desktop `<Table>` (spacing, alignment). */
  className?: string
}

/**
 * A record listing that is a table on tablet/desktop and a stack of cards on a
 * phone.
 *
 * A wide admin table (the user roster, the audit log) only ever survived a phone
 * by scrolling sideways inside `.table-responsive`: the later columns and — worse
 * — the per-row actions sat off-screen behind a horizontal drag. Below `md` the
 * same columns are re-laid as one card per record, each column becoming a
 * "label: value" line, and the row actions becoming a full-width button row that
 * needs no scrolling at all.
 *
 * The breakpoint is decided in JS ({@link useIsNarrowViewport}) rather than by a
 * `d-md-none` display rule, so only one of the two layouts is ever in the DOM —
 * assistive tech (and a test) never sees a duplicate copy of every record.
 *
 * It is deliberately generic: any admin table with a `columns` shape can adopt it
 * without teaching this component anything about its rows.
 */
export function RecordTable<T>({
  records,
  columns,
  rowKey,
  cardActions,
  detail,
  className,
}: RecordTableProps<T>) {
  const narrow = useIsNarrowViewport()

  if (narrow) {
    const fields = columns.filter((column) => column.cardHidden !== true)
    return (
      <ul className="kk-record-cards list-unstyled d-grid gap-3 mb-0">
        {records.map((record) => {
          const detailNode = detail?.(record) ?? null
          return (
            <li key={rowKey(record)}>
              <Card className="kk-record-card">
                <Card.Body>
                  <dl className="row g-0 mb-0">
                    {fields.map((column) => (
                      <Fragment key={column.key}>
                        <dt className="col-5 small fw-normal text-secondary text-break">
                          {column.header}
                        </dt>
                        <dd className="col-7 mb-1 text-break">{column.cell(record)}</dd>
                      </Fragment>
                    ))}
                  </dl>
                  {cardActions !== undefined && (
                    <div className="kk-record-card__actions d-grid gap-2 mt-3">
                      {cardActions(record)}
                    </div>
                  )}
                  {detailNode !== null && <div className="mt-3 border-top pt-3">{detailNode}</div>}
                </Card.Body>
              </Card>
            </li>
          )
        })}
      </ul>
    )
  }

  return (
    <Table striped hover responsive className={className}>
      <thead>
        <tr>
          {columns.map((column) => (
            <th key={column.key}>{column.header}</th>
          ))}
        </tr>
      </thead>
      <tbody>
        {records.map((record) => {
          const detailNode = detail?.(record) ?? null
          return (
            <Fragment key={rowKey(record)}>
              <tr>
                {columns.map((column) => (
                  <td key={column.key} className={column.cellClassName} style={column.cellStyle}>
                    {column.cell(record)}
                  </td>
                ))}
              </tr>
              {detailNode !== null && (
                <tr>
                  <td colSpan={columns.length} className="bg-body-tertiary">
                    {detailNode}
                  </td>
                </tr>
              )}
            </Fragment>
          )
        })}
      </tbody>
    </Table>
  )
}
