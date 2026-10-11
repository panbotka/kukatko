import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'

import { SEARCH_EXAMPLES, START_RECENT_LIMIT } from '../../lib/searchStart'

import { SearchStartState } from './SearchStartState'

vi.mock('../../services/searchHistory', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../services/searchHistory')>()
  return { ...actual, fetchSearchHistory: vi.fn(), clearSearchHistory: vi.fn() }
})

const { fetchSearchHistory, clearSearchHistory } = await import('../../services/searchHistory')
const historyMock = vi.mocked(fetchSearchHistory)
const clearMock = vi.mocked(clearSearchHistory)

function entries(count: number) {
  return Array.from({ length: count }, (_, i) => ({
    query: `query ${String(i)}`,
    searched_at: '2026-10-01T12:00:00Z',
  }))
}

function renderStart() {
  const onRun = vi.fn()
  const onInsert = vi.fn()
  render(
    <I18nextProvider i18n={i18n}>
      <SearchStartState onRun={onRun} onInsert={onInsert} />
    </I18nextProvider>,
  )
  return { onRun, onInsert }
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  historyMock.mockReset()
  historyMock.mockResolvedValue([])
  clearMock.mockReset()
  clearMock.mockResolvedValue()
})

describe('SearchStartState', () => {
  it('keeps the one-line prompt and lists the example queries', () => {
    renderStart()

    expect(screen.getByText('Enter a search term.')).toBeInTheDocument()
    const examples = screen.getByRole('region', { name: 'Example queries' })
    for (const example of SEARCH_EXAMPLES) {
      expect(within(examples).getByRole('button', { name: example })).toBeInTheDocument()
    }
  })

  it('lists the recent searches from the history service, capped', async () => {
    historyMock.mockResolvedValue(entries(START_RECENT_LIMIT + 5))
    renderStart()

    const recent = await screen.findByRole('region', { name: 'Recent searches' })
    // The searches, plus the action that forgets them.
    expect(within(recent).getAllByRole('button')).toHaveLength(START_RECENT_LIMIT + 1)
    expect(within(recent).getByRole('button', { name: 'query 0' })).toBeInTheDocument()
    expect(within(recent).queryByRole('button', { name: 'query 10' })).not.toBeInTheDocument()
  })

  it('leaves the recent row out when there is no history', async () => {
    renderStart()

    await screen.findByRole('region', { name: 'Example queries' })
    expect(historyMock).toHaveBeenCalled()
    expect(screen.queryByRole('region', { name: 'Recent searches' })).not.toBeInTheDocument()
  })

  it('runs a recent search and inserts an example', async () => {
    historyMock.mockResolvedValue(entries(1))
    const user = userEvent.setup()
    const { onRun, onInsert } = renderStart()

    await user.click(await screen.findByRole('button', { name: 'query 0' }))
    expect(onRun).toHaveBeenCalledWith('query 0')

    await user.click(screen.getByRole('button', { name: 'year:1965' }))
    expect(onInsert).toHaveBeenCalledWith('year:1965')
    expect(onRun).toHaveBeenCalledTimes(1)
  })

  it('forgets the history at once', async () => {
    historyMock.mockResolvedValue(entries(2))
    const user = userEvent.setup()
    renderStart()

    const recent = await screen.findByRole('region', { name: 'Recent searches' })
    await user.click(within(recent).getByRole('button', { name: 'Clear history' }))

    expect(clearMock).toHaveBeenCalledTimes(1)
    expect(screen.queryByRole('region', { name: 'Recent searches' })).not.toBeInTheDocument()
  })
})
