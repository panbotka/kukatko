import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'

import { SearchEmptyState, type SearchEmptyStateProps } from './SearchEmptyState'

function renderEmpty(props: Partial<SearchEmptyStateProps> = {}) {
  const onClearFilters = vi.fn()
  const onDescribe = vi.fn()
  render(
    <I18nextProvider i18n={i18n}>
      <SearchEmptyState
        query="svatba"
        hasFilters={false}
        onClearFilters={onClearFilters}
        canDescribe={false}
        onDescribe={onDescribe}
        {...props}
      />
    </I18nextProvider>,
  )
  return { onClearFilters, onDescribe }
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
})

describe('SearchEmptyState', () => {
  it('repeats the query it failed on and always offers the spelling', () => {
    renderEmpty()

    expect(screen.getByText(/nothing matches “svatba”/i)).toBeInTheDocument()
    expect(screen.getByText(/check the spelling/i)).toBeInTheDocument()
  })

  it('drops the filters on request, since they are the commonest reason', async () => {
    const user = userEvent.setup()
    const { onClearFilters } = renderEmpty({ hasFilters: true })

    expect(screen.getByText(/filters may be hiding it/i)).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Clear filters' }))

    expect(onClearFilters).toHaveBeenCalled()
  })

  it('offers describing the photo, which nobody discovers on their own', async () => {
    const user = userEvent.setup()
    const { onDescribe } = renderEmpty({ canDescribe: true })

    expect(screen.getByText(/try describing what should be in the photo/i)).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Search by what is in the photo' }))

    expect(onDescribe).toHaveBeenCalled()
  })

  it('proposes no step that would change nothing', () => {
    renderEmpty()

    expect(screen.queryByRole('button', { name: 'Clear filters' })).not.toBeInTheDocument()
    expect(
      screen.queryByRole('button', { name: 'Search by what is in the photo' }),
    ).not.toBeInTheDocument()
  })

  it('drops the quoted query when only the filters narrowed the search', () => {
    renderEmpty({ query: '', hasFilters: true })

    expect(screen.getByText(/try a different term or adjust the filters/i)).toBeInTheDocument()
  })
})
