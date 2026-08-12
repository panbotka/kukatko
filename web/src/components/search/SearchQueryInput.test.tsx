import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { I18nextProvider } from 'react-i18next'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'
import { type AlbumSummary, type LabelCount } from '../../services/organize'
import { type SubjectCount } from '../../services/people'
import { type SearchHistoryEntry } from '../../services/searchHistory'

import { SearchQueryInput } from './SearchQueryInput'

vi.mock('../../services/searchHistory', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../services/searchHistory')>()
  return {
    ...actual,
    fetchSearchHistory: vi.fn(),
    clearSearchHistory: vi.fn(),
  }
})

vi.mock('../../services/organize', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../services/organize')>()
  return { ...actual, fetchAlbums: vi.fn(), fetchLabels: vi.fn() }
})

vi.mock('../../services/people', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../services/people')>()
  return { ...actual, fetchSubjects: vi.fn() }
})

const { fetchSearchHistory, clearSearchHistory } = await import('../../services/searchHistory')
const { fetchAlbums, fetchLabels } = await import('../../services/organize')
const { fetchSubjects } = await import('../../services/people')

const historyMock = vi.mocked(fetchSearchHistory)
const clearMock = vi.mocked(clearSearchHistory)
const albumsMock = vi.mocked(fetchAlbums)
const labelsMock = vi.mocked(fetchLabels)
const subjectsMock = vi.mocked(fetchSubjects)

/** Builds a history entry; only the query text matters to the dropdown. */
function entry(query: string): SearchHistoryEntry {
  return { query, searched_at: '2026-08-09T12:00:00Z' }
}

/** Builds a subject with just the fields the value autocomplete reads. */
function subject(name: string, photoCount: number): SubjectCount {
  return {
    uid: `su-${name}`,
    name,
    slug: name.toLowerCase(),
    type: 'person',
    favorite: false,
    private: false,
    notes: '',
    birth_year: null,
    death_year: null,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    marker_count: photoCount,
    photo_count: photoCount,
  }
}

/** Builds an album with just the fields the value autocomplete reads. */
function album(title: string, photoCount: number): AlbumSummary {
  return {
    uid: `al-${title}`,
    slug: title.toLowerCase(),
    title,
    description: '',
    type: 'album',
    private: false,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    photo_count: photoCount,
  }
}

/**
 * Renders the input as the search page does — controlled, with a form around it —
 * and exposes what was typed and what was run so both can be asserted.
 */
function Harness({ onRun }: { onRun?: (value: string) => void }) {
  const [text, setText] = useState('')
  const [ran, setRan] = useState<string | null>(null)
  return (
    <form
      onSubmit={(event) => {
        event.preventDefault()
        setRan(text)
      }}
    >
      <SearchQueryInput
        id="q"
        value={text}
        onChange={setText}
        onRun={(value) => {
          setRan(value)
          onRun?.(value)
        }}
      />
      <output data-testid="ran">{ran}</output>
    </form>
  )
}

function renderInput(onRun?: (value: string) => void) {
  return render(
    <I18nextProvider i18n={i18n}>
      <Harness onRun={onRun} />
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  historyMock.mockResolvedValue([])
  clearMock.mockResolvedValue()
  albumsMock.mockResolvedValue([])
  labelsMock.mockResolvedValue([] as LabelCount[])
  subjectsMock.mockResolvedValue([])
})

describe('SearchQueryInput — recent searches', () => {
  it('offers the history when the empty box is focused, and runs the one clicked', async () => {
    const user = userEvent.setup()
    historyMock.mockResolvedValue([entry('svatba 1974'), entry('person:Anna')])
    renderInput()

    // Nothing is fetched, and nothing offered, until the box is focused.
    expect(historyMock).not.toHaveBeenCalled()
    await user.click(screen.getByRole('combobox'))

    const list = await screen.findByRole('listbox', { name: 'Recent searches' })
    expect(list).toBeInTheDocument()
    // Most recent first, exactly as the server ordered them.
    const options = screen.getAllByRole('option')
    expect(options.map((option) => option.textContent)).toEqual(['svatba 1974', 'person:Anna'])

    await user.click(options[0])
    // Clicking one both fills the box and runs it.
    expect(screen.getByRole('combobox')).toHaveValue('svatba 1974')
    expect(screen.getByTestId('ran')).toHaveTextContent('svatba 1974')
  })

  it('shows no dropdown at all when there is no history yet', async () => {
    const user = userEvent.setup()
    renderInput()
    await user.click(screen.getByRole('combobox'))

    await waitFor(() => {
      expect(historyMock).toHaveBeenCalled()
    })
    expect(screen.queryByRole('listbox')).not.toBeInTheDocument()
    expect(screen.queryByRole('option')).not.toBeInTheDocument()
  })

  it('does not hijack Enter on a freshly focused box', async () => {
    const user = userEvent.setup()
    historyMock.mockResolvedValue([entry('svatba 1974')])
    renderInput()
    await user.click(screen.getByRole('combobox'))
    await screen.findByRole('option', { name: 'svatba 1974' })

    // Nothing is highlighted, so Enter submits the (empty) query the reader owns
    // rather than running whatever sits at the top of their history.
    await user.keyboard('{Enter}')
    expect(screen.getByRole('combobox')).toHaveValue('')
    expect(screen.getByTestId('ran')).toBeEmptyDOMElement()
  })

  it('walks the history with the arrow keys and runs the highlighted one on Enter', async () => {
    const user = userEvent.setup()
    historyMock.mockResolvedValue([entry('svatba 1974'), entry('person:Anna')])
    renderInput()
    await user.click(screen.getByRole('combobox'))
    await screen.findByRole('option', { name: 'svatba 1974' })

    await user.keyboard('{ArrowDown}{ArrowDown}')
    const options = screen.getAllByRole('option')
    expect(options[1]).toHaveAttribute('aria-selected', 'true')
    expect(screen.getByRole('combobox')).toHaveAttribute('aria-activedescendant', options[1].id)

    await user.keyboard('{Enter}')
    expect(screen.getByTestId('ran')).toHaveTextContent('person:Anna')
  })

  it('wraps the highlight and closes on Escape', async () => {
    const user = userEvent.setup()
    historyMock.mockResolvedValue([entry('a'), entry('b')])
    renderInput()
    await user.click(screen.getByRole('combobox'))
    await screen.findByRole('option', { name: 'a' })

    // ArrowUp from nothing-highlighted wraps to the last row.
    await user.keyboard('{ArrowUp}')
    expect(screen.getAllByRole('option')[1]).toHaveAttribute('aria-selected', 'true')

    await user.keyboard('{Escape}')
    expect(screen.queryByRole('option')).not.toBeInTheDocument()
  })

  it('forgets the history from the dropdown, at once', async () => {
    const user = userEvent.setup()
    historyMock.mockResolvedValue([entry('svatba 1974')])
    renderInput()
    await user.click(screen.getByRole('combobox'))
    await screen.findByRole('option', { name: 'svatba 1974' })

    await user.click(screen.getByRole('button', { name: /Clear history/ }))
    expect(clearMock).toHaveBeenCalledTimes(1)
    // The list is gone before the round trip resolves — the whole point of the action.
    expect(screen.queryByRole('option')).not.toBeInTheDocument()
  })
})

describe('SearchQueryInput — value suggestions', () => {
  it('offers people for person: and inserts the picked name', async () => {
    const user = userEvent.setup()
    subjectsMock.mockResolvedValue([
      subject('Anna Marie', 40),
      subject('Anna', 12),
      subject('Bob', 3),
    ])
    renderInput()

    await user.type(screen.getByRole('combobox'), 'person:an')
    const list = await screen.findByRole('listbox', { name: 'Value suggestions' })
    expect(list).toBeInTheDocument()
    // Prefix match only, ranked by how many photos each is on.
    expect(screen.getAllByRole('option').map((option) => option.textContent)).toEqual([
      'Anna Marie40',
      'Anna12',
    ])

    await user.click(screen.getByRole('option', { name: /Anna Marie/ }))
    // A name with a space arrives quoted, with the caret on a fresh token.
    expect(screen.getByRole('combobox')).toHaveValue('person:"Anna Marie" ')
  })

  it('matches values without diacritics', async () => {
    const user = userEvent.setup()
    subjectsMock.mockResolvedValue([subject('Jarmila', 5), subject('Božena', 8)])
    renderInput()

    await user.type(screen.getByRole('combobox'), 'person:boz')
    expect(await screen.findByRole('option', { name: /Božena/ })).toBeInTheDocument()
    expect(screen.getAllByRole('option')).toHaveLength(1)
  })

  it('completes an album title with Tab, without the highlight having moved', async () => {
    const user = userEvent.setup()
    albumsMock.mockResolvedValue([album('Léto 2024', 30)])
    renderInput()

    await user.type(screen.getByRole('combobox'), 'album:let')
    await screen.findByRole('option', { name: /Léto 2024/ })

    await user.keyboard('{Tab}')
    expect(screen.getByRole('combobox')).toHaveValue('album:"Léto 2024" ')
  })

  it('leaves Enter to the form while no value is highlighted', async () => {
    const user = userEvent.setup()
    subjectsMock.mockResolvedValue([subject('Anna Marie', 40), subject('Anna', 12)])
    renderInput()

    await user.type(screen.getByRole('combobox'), 'person:Anna')
    await screen.findByRole('option', { name: /Anna Marie/ })

    // The reader typed a whole name; Enter must search for it, not swap in the
    // busier suggestion that happens to sit on top.
    await user.keyboard('{Enter}')
    expect(screen.getByRole('combobox')).toHaveValue('person:Anna')
    expect(screen.getByTestId('ran')).toHaveTextContent('person:Anna')
  })

  it('says nothing matches rather than dropping the dropdown mid-word', async () => {
    const user = userEvent.setup()
    subjectsMock.mockResolvedValue([subject('Anna', 12)])
    renderInput()

    await user.type(screen.getByRole('combobox'), 'person:zz')
    expect(await screen.findByText('Nothing matches')).toBeInTheDocument()
    expect(screen.queryByRole('option')).not.toBeInTheDocument()
  })

  it('offers no values for a key nothing can propose for', async () => {
    const user = userEvent.setup()
    renderInput()

    await user.type(screen.getByRole('combobox'), 'iso:10')
    await waitFor(() => {
      expect(screen.queryByRole('option')).not.toBeInTheDocument()
    })
    // Nothing was fetched for it either.
    expect(subjectsMock).not.toHaveBeenCalled()
    expect(albumsMock).not.toHaveBeenCalled()
    expect(labelsMock).not.toHaveBeenCalled()
  })

  it('fetches a facet list once, not once per keystroke', async () => {
    const user = userEvent.setup()
    subjectsMock.mockResolvedValue([subject('Anna', 12), subject('Aneta', 3)])
    renderInput()

    await user.type(screen.getByRole('combobox'), 'person:ann')
    await screen.findByRole('option', { name: /Anna/ })
    await user.type(screen.getByRole('combobox'), 'a')
    await user.keyboard('{Backspace}{Backspace}')
    await screen.findByRole('option', { name: /Aneta/ })

    expect(subjectsMock).toHaveBeenCalledTimes(1)
  })

  it('still completes filter keys, which values never displace', async () => {
    const user = userEvent.setup()
    renderInput()

    await user.type(screen.getByRole('combobox'), 'ca')
    const list = await screen.findByRole('listbox', { name: 'Filter suggestions' })
    expect(list).toBeInTheDocument()
    // Tab is the completion key, so it accepts the first row untouched.
    await user.keyboard('{Tab}')
    expect(screen.getByRole('combobox')).toHaveValue('camera:')
  })

  it('completes a filter key arrowed into, on Enter', async () => {
    const user = userEvent.setup()
    renderInput()

    await user.type(screen.getByRole('combobox'), 'ca')
    await screen.findByRole('listbox', { name: 'Filter suggestions' })

    await user.keyboard('{ArrowDown}')
    expect(screen.getAllByRole('option')[0]).toHaveAttribute('aria-selected', 'true')
    await user.keyboard('{Enter}')
    expect(screen.getByRole('combobox')).toHaveValue('camera:')
  })

  it('submits a phrase verbatim when its last token merely looks like a filter key', async () => {
    const user = userEvent.setup()
    renderInput()

    // Czech is full of words and endings that prefix an English filter key —
    // here `u` prefixes `uid`. Nothing was highlighted, so Enter must run the
    // typed query, not rewrite it into `svatba uid:`.
    await user.type(screen.getByRole('combobox'), 'svatba u')
    await screen.findByRole('listbox', { name: 'Filter suggestions' })

    await user.keyboard('{Enter}')
    expect(screen.getByRole('combobox')).toHaveValue('svatba u')
    expect(screen.getByTestId('ran').textContent).toBe('svatba u')
  })
})
