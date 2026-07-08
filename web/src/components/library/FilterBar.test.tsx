import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'
import { LIBRARY_DEFAULTS, type LibraryView } from '../../lib/libraryView'
import { type SetUrlState } from '../../lib/urlState'

import { FilterBar } from './FilterBar'

function renderBar(
  view: LibraryView,
  onChange: SetUrlState<LibraryView>,
  props: { showSearch?: boolean; showSort?: boolean } = {},
) {
  return render(
    <I18nextProvider i18n={i18n}>
      <FilterBar view={view} onChange={onChange} total={0} {...props} />
    </I18nextProvider>,
  )
}

/** Opens the advanced-filter panel so its controls become reachable. */
async function openPanel(user: ReturnType<typeof userEvent.setup>) {
  await user.click(screen.getByRole('button', { name: /Filters/ }))
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
})

describe('FilterBar header', () => {
  it('keeps the sort selector in the header without expanding', async () => {
    const onChange = vi.fn()
    const user = userEvent.setup()
    renderBar(LIBRARY_DEFAULTS, onChange)

    await user.selectOptions(screen.getByLabelText('Sort'), 'rating')
    expect(onChange).toHaveBeenCalledWith({ sort: 'rating' })
  })

  it('hides the search and sort controls when asked', () => {
    renderBar(LIBRARY_DEFAULTS, vi.fn(), { showSearch: false, showSort: false })
    expect(screen.queryByLabelText('Search')).not.toBeInTheDocument()
    expect(screen.queryByLabelText('Sort')).not.toBeInTheDocument()
    // The filters toggle is still available.
    expect(screen.getByRole('button', { name: /Filters/ })).toBeInTheDocument()
  })

  it('toggles the advanced panel open and closed', async () => {
    const user = userEvent.setup()
    renderBar(LIBRARY_DEFAULTS, vi.fn())

    const toggle = screen.getByRole('button', { name: /Filters/ })
    expect(toggle).toHaveAttribute('aria-expanded', 'false')
    await user.click(toggle)
    expect(toggle).toHaveAttribute('aria-expanded', 'true')
    await user.click(toggle)
    expect(toggle).toHaveAttribute('aria-expanded', 'false')
  })
})

describe('FilterBar advanced controls', () => {
  it('pushes the minimum-rating filter when selected', async () => {
    const onChange = vi.fn()
    const user = userEvent.setup()
    renderBar(LIBRARY_DEFAULTS, onChange)

    await openPanel(user)
    await user.selectOptions(screen.getByLabelText('Rating'), '3')
    expect(onChange).toHaveBeenCalledWith({ min_rating: '3' })
  })

  it('pushes the flag filter when selected', async () => {
    const onChange = vi.fn()
    const user = userEvent.setup()
    renderBar(LIBRARY_DEFAULTS, onChange)

    await openPanel(user)
    await user.selectOptions(screen.getByLabelText('Flag'), 'pick')
    expect(onChange).toHaveBeenCalledWith({ flag: 'pick' })
  })

  it('replaces history for live-typed free-text input', async () => {
    const onChange = vi.fn()
    const user = userEvent.setup()
    renderBar(LIBRARY_DEFAULTS, onChange)

    await openPanel(user)
    await user.type(screen.getByLabelText('Camera'), 'C')
    expect(onChange).toHaveBeenCalledWith({ camera: 'C' }, { replace: true })
  })
})

describe('FilterBar active-filter chips', () => {
  it('renders a chip per active filter and badges the toggle count', () => {
    renderBar({ ...LIBRARY_DEFAULTS, min_rating: '4', flag: 'pick' }, vi.fn())

    expect(screen.getByText('Rating: ≥ 4')).toBeInTheDocument()
    expect(screen.getByText('Flag: Picks')).toBeInTheDocument()
    const toggle = screen.getByRole('button', { name: /Filters/ })
    expect(within(toggle).getByText('2')).toBeInTheDocument()
  })

  it('clears a single filter when its chip is dismissed', async () => {
    const onChange = vi.fn()
    const user = userEvent.setup()
    renderBar({ ...LIBRARY_DEFAULTS, min_rating: '4' }, onChange)

    await user.click(screen.getByRole('button', { name: /Remove filter/ }))
    expect(onChange).toHaveBeenCalledWith({ min_rating: '' })
  })

  it('treats an active rating filter as a clearable filter', () => {
    renderBar({ ...LIBRARY_DEFAULTS, min_rating: '4' }, vi.fn())
    expect(screen.getByRole('button', { name: 'Clear filters' })).toBeInTheDocument()
  })

  it('does not show chips or clear-all when no filters are active', () => {
    renderBar(LIBRARY_DEFAULTS, vi.fn())
    expect(screen.queryByRole('button', { name: 'Clear filters' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /Remove filter/ })).not.toBeInTheDocument()
  })

  it('resets every filter but keeps the sort on clear-all', async () => {
    const onChange = vi.fn()
    const user = userEvent.setup()
    renderBar({ ...LIBRARY_DEFAULTS, sort: 'title', min_rating: '4', camera: 'Canon' }, onChange)

    await user.click(screen.getByRole('button', { name: 'Clear filters' }))
    expect(onChange).toHaveBeenCalledWith({ ...LIBRARY_DEFAULTS, sort: 'title' })
  })
})
