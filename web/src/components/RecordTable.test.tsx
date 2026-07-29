import { render, screen, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { RecordTable, type RecordColumn } from './RecordTable'

/** A throwaway record type, so the test exercises the generic, not a page model. */
interface Row {
  id: string
  name: string
  role: string
}

const ROWS: Row[] = [
  { id: 'r1', name: 'Ada', role: 'Editor' },
  { id: 'r2', name: 'Bob', role: 'Viewer' },
]

const COLUMNS: RecordColumn<Row>[] = [
  { key: 'name', header: 'Name', cellClassName: 'text-break', cell: (row) => row.name },
  { key: 'role', header: 'Role', cell: (row) => row.role },
  {
    key: 'actions',
    header: 'Actions',
    cardHidden: true,
    cell: (row) => <button type="button">{`Edit ${row.name}`}</button>,
  },
]

/** The shared setup stubs a non-matching (desktop) `matchMedia`; restore it after. */
const realMatchMedia = window.matchMedia

/** Points `window.matchMedia` at a fixed phone/desktop answer. */
function mockViewport(narrow: boolean): void {
  window.matchMedia = vi.fn().mockImplementation((query: string) => ({
    matches: narrow,
    media: query,
    onchange: null,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    addListener: vi.fn(),
    removeListener: vi.fn(),
    dispatchEvent: vi.fn(),
  }))
}

afterEach(() => {
  window.matchMedia = realMatchMedia
})

describe('RecordTable', () => {
  it('renders a table of rows on a wide viewport', () => {
    mockViewport(false)
    render(<RecordTable records={ROWS} columns={COLUMNS} rowKey={(row) => row.id} />)

    const table = screen.getByRole('table')
    expect(
      within(table)
        .getAllByRole('columnheader')
        .map((th) => th.textContent),
    ).toEqual(['Name', 'Role', 'Actions'])
    // The header plus one row per record.
    expect(within(table).getAllByRole('row')).toHaveLength(3)
    expect(within(table).getByRole('cell', { name: 'Ada' })).toHaveClass('text-break')
    // A `cardHidden` column is a normal column here — it only drops out of a card.
    expect(within(table).getByRole('button', { name: 'Edit Ada' })).toBeInTheDocument()
    expect(screen.queryByRole('list')).toBeNull()
  })

  it('renders one stacked card per record on a phone', () => {
    mockViewport(true)
    render(
      <RecordTable
        records={ROWS}
        columns={COLUMNS}
        rowKey={(row) => row.id}
        cardActions={(row) => <button type="button">{`Disable ${row.name}`}</button>}
      />,
    )

    // Nothing to scroll sideways: the wide table is gone entirely.
    expect(screen.queryByRole('table')).toBeNull()
    const cards = screen.getAllByRole('listitem')
    expect(cards).toHaveLength(2)

    // Every column that is not the action column becomes a "label: value" line.
    const first = cards[0]
    expect(within(first).getByText('Name')).toBeInTheDocument()
    expect(within(first).getByText('Ada')).toBeInTheDocument()
    expect(within(first).getByText('Role')).toBeInTheDocument()
    expect(within(first).getByText('Editor')).toBeInTheDocument()

    // The `cardHidden` column is replaced by the card's own full-width action row.
    expect(within(first).queryByText('Actions')).toBeNull()
    expect(within(first).queryByRole('button', { name: 'Edit Ada' })).toBeNull()
    const action = within(first).getByRole('button', { name: 'Disable Ada' })
    expect(action.parentElement).toHaveClass('kk-record-card__actions', 'd-grid')
  })

  it('spans a record’s detail across every column of the table', () => {
    mockViewport(false)
    render(
      <RecordTable
        records={ROWS}
        columns={COLUMNS}
        rowKey={(row) => row.id}
        detail={(row) => (row.id === 'r1' ? <p>{`Payload of ${row.name}`}</p> : null)}
      />,
    )

    const cell = screen.getByText('Payload of Ada').closest('td')
    expect(cell).toHaveAttribute('colspan', String(COLUMNS.length))
    // Only the record whose detail is open gets an extra row: 1 header + 2 + 1.
    expect(screen.getAllByRole('row')).toHaveLength(4)
    expect(screen.queryByText('Payload of Bob')).toBeNull()
  })

  it('puts a record’s detail inside its own card on a phone', () => {
    mockViewport(true)
    render(
      <RecordTable
        records={ROWS}
        columns={COLUMNS}
        rowKey={(row) => row.id}
        detail={(row) => (row.id === 'r1' ? <p>{`Payload of ${row.name}`}</p> : null)}
      />,
    )

    const cards = screen.getAllByRole('listitem')
    expect(within(cards[0]).getByText('Payload of Ada')).toBeInTheDocument()
    expect(within(cards[1]).queryByText(/Payload/)).toBeNull()
  })

  it('renders no action row when the caller offers no actions', () => {
    mockViewport(true)
    const { container } = render(
      <RecordTable records={ROWS} columns={COLUMNS} rowKey={(row) => row.id} />,
    )

    expect(container.querySelector('.kk-record-card__actions')).toBeNull()
  })
})
