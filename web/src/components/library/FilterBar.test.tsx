import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'
import { LIBRARY_DEFAULTS, type LibraryView } from '../../lib/libraryView'
import { type SetUrlState } from '../../lib/urlState'

import { FilterBar } from './FilterBar'

function renderBar(view: LibraryView, onChange: SetUrlState<LibraryView>) {
  return render(
    <I18nextProvider i18n={i18n}>
      <FilterBar view={view} onChange={onChange} total={0} />
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
})

describe('FilterBar rating controls', () => {
  it('pushes the minimum-rating filter when selected', async () => {
    const onChange = vi.fn()
    const user = userEvent.setup()
    renderBar(LIBRARY_DEFAULTS, onChange)

    await user.selectOptions(screen.getByLabelText('Rating'), '3')
    expect(onChange).toHaveBeenCalledWith({ min_rating: '3' })
  })

  it('pushes the flag filter when selected', async () => {
    const onChange = vi.fn()
    const user = userEvent.setup()
    renderBar(LIBRARY_DEFAULTS, onChange)

    await user.selectOptions(screen.getByLabelText('Flag'), 'pick')
    expect(onChange).toHaveBeenCalledWith({ flag: 'pick' })
  })

  it('offers a rating sort option', async () => {
    const onChange = vi.fn()
    const user = userEvent.setup()
    renderBar(LIBRARY_DEFAULTS, onChange)

    await user.selectOptions(screen.getByLabelText('Sort'), 'rating')
    expect(onChange).toHaveBeenCalledWith({ sort: 'rating' })
  })

  it('treats an active rating filter as a clearable filter', () => {
    renderBar({ ...LIBRARY_DEFAULTS, min_rating: '4' }, vi.fn())
    expect(screen.getByRole('button', { name: 'Clear filters' })).toBeInTheDocument()
  })
})
