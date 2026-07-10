import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { I18nextProvider } from 'react-i18next'
import { beforeEach, describe, expect, it } from 'vitest'

import i18n from '../i18n'

import { MultiSelect, type MultiSelectOption } from './MultiSelect'

const OPTIONS: MultiSelectOption[] = [
  { value: 'al1', label: 'Trips', count: 12 },
  { value: 'al2', label: 'Weddings' },
  { value: 'al3', label: 'Léto' },
]

/** Renders the control with real state, so a pick is reflected back into it. */
function Harness({ destructive }: { destructive?: boolean }) {
  const [selected, setSelected] = useState<string[]>([])
  return (
    <I18nextProvider i18n={i18n}>
      <MultiSelect
        id="albums"
        label="Albums"
        placeholder="Type to filter albums…"
        options={OPTIONS}
        selected={selected}
        onChange={setSelected}
        destructive={destructive}
      />
    </I18nextProvider>
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
})

describe('MultiSelect', () => {
  it('keeps picking: a chosen option becomes a chip and leaves the list', async () => {
    const user = userEvent.setup()
    render(<Harness />)

    const input = screen.getByLabelText('Albums')
    await user.click(input)
    await user.click(screen.getByRole('option', { name: 'Trips 12' }))
    await user.click(screen.getByRole('option', { name: 'Weddings' }))

    expect(screen.getByRole('button', { name: 'Remove Trips' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Remove Weddings' })).toBeInTheDocument()
    // Only the still-unchosen option is offered.
    expect(screen.getAllByRole('option')).toHaveLength(1)
    expect(screen.getByRole('option', { name: 'Léto' })).toBeInTheDocument()
    // The query is cleared after each pick, ready for the next one.
    expect(input).toHaveValue('')
  })

  it('takes the highlighted option on Enter and drops the last chip on Backspace', async () => {
    const user = userEvent.setup()
    render(<Harness />)

    const input = screen.getByLabelText('Albums')
    await user.click(input)
    await user.keyboard('{ArrowDown}{ArrowDown}{Enter}')
    expect(screen.getByRole('button', { name: 'Remove Weddings' })).toBeInTheDocument()

    // Backspace only bites once the query itself is empty.
    await user.type(input, 'x')
    await user.keyboard('{Backspace}')
    expect(screen.getByRole('button', { name: 'Remove Weddings' })).toBeInTheDocument()
    await user.keyboard('{Backspace}')
    expect(screen.queryByRole('button', { name: 'Remove Weddings' })).not.toBeInTheDocument()
  })

  it('takes the best match on Enter when nothing is highlighted', async () => {
    const user = userEvent.setup()
    render(<Harness />)

    // Accent-insensitive, mirroring the backend's unaccented search.
    await user.type(screen.getByLabelText('Albums'), 'leto{Enter}')
    expect(screen.getByRole('button', { name: 'Remove Léto' })).toBeInTheDocument()
  })

  it('closes the list on Escape without choosing anything', async () => {
    const user = userEvent.setup()
    render(<Harness />)

    await user.click(screen.getByLabelText('Albums'))
    expect(screen.getByRole('listbox')).toBeInTheDocument()
    await user.keyboard('{Escape}')
    expect(screen.queryByRole('listbox')).not.toBeInTheDocument()
  })

  it('paints a destructive field in the danger key', async () => {
    const user = userEvent.setup()
    render(<Harness destructive />)

    await user.click(screen.getByLabelText('Albums'))
    await user.click(screen.getByRole('option', { name: 'Trips 12' }))

    expect(screen.getByText('Trips').closest('.kk-chip')?.className).toContain('text-bg-danger')
  })
})
