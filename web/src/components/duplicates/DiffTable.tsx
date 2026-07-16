import Table from 'react-bootstrap/Table'
import { useTranslation } from 'react-i18next'

import { type DiffRow, countDiffering } from '../../lib/duplicateCompare'
import { Icon } from '../Icon'

import './compare.css'

/** Props for {@link DiffTable}. */
export interface DiffTableProps {
  rows: DiffRow[]
  /** Whether to hide the rows that match, leaving only the differences. */
  onlyDifferences: boolean
}

/**
 * The metadata difference between the two photos, with the differing rows marked.
 *
 * Marking is the entire point: eleven identical rows and one that differs is a
 * table where only one row is information, and the eye has to find it. A differing
 * row is marked three ways — a coloured left border, bolded values and an explicit
 * "differs" label for screen readers — so the signal does not rest on colour alone.
 */
export function DiffTable({ rows, onlyDifferences }: DiffTableProps) {
  const { t } = useTranslation()
  const shown = onlyDifferences ? rows.filter((row) => row.differs) : rows
  const differing = countDiffering(rows)

  if (shown.length === 0) {
    return (
      <p className="text-secondary small mb-0" data-testid="diff-identical">
        {t('duplicates.compare.diff.identical')}
      </p>
    )
  }

  return (
    <Table
      size="sm"
      borderless
      className="kk-diff-table mb-0 align-middle"
      data-testid="diff-table"
    >
      <caption className="visually-hidden">
        {t('duplicates.compare.diff.summary', { count: differing })}
      </caption>
      <thead>
        <tr>
          <th scope="col" className="kk-diff-table__field">
            {t('duplicates.compare.diff.field')}
          </th>
          <th scope="col">{t('duplicates.compare.left')}</th>
          <th scope="col">{t('duplicates.compare.right')}</th>
        </tr>
      </thead>
      <tbody>
        {shown.map((row) => (
          <DiffTableRow key={row.key} row={row} />
        ))}
      </tbody>
    </Table>
  )
}

/** One row of the table, marked when the two sides differ. */
function DiffTableRow({ row }: { row: DiffRow }) {
  const { t } = useTranslation()
  return (
    <tr
      className={row.differs ? 'kk-diff-table__row--differs' : undefined}
      data-testid={`diff-row-${row.key}`}
      data-differs={row.differs ? 'true' : 'false'}
    >
      <th scope="row" className="kk-diff-table__field fw-normal text-secondary">
        {row.differs && <Icon name="exclamation-triangle" className="me-1 text-warning" />}
        {t(`duplicates.compare.diff.${row.key}` as const)}
        {row.differs && (
          <span className="visually-hidden"> — {t('duplicates.compare.diff.differs')}</span>
        )}
      </th>
      <DiffValue value={row.left} differs={row.differs} />
      <DiffValue value={row.right} differs={row.differs} />
    </tr>
  )
}

/** One side's value; an absent value reads as an em dash rather than a blank cell. */
function DiffValue({ value, differs }: { value: string; differs: boolean }) {
  const { t } = useTranslation()
  return (
    <td className={differs ? 'fw-semibold' : 'text-secondary'}>
      {value === '' ? (
        <span className="text-secondary" title={t('duplicates.compare.diff.empty')}>
          —
        </span>
      ) : (
        value
      )}
    </td>
  )
}
