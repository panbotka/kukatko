import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'

import { UnknownFiltersAlert } from './UnknownFiltersAlert'

function renderAlert(tokens: string[], query?: string, onFix?: (q: string) => void) {
  return render(
    <I18nextProvider i18n={i18n}>
      <UnknownFiltersAlert tokens={tokens} query={query} onFix={onFix} />
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
})

describe('UnknownFiltersAlert', () => {
  it('names every token the query language did not understand', () => {
    renderAlert(['osoba:Jarmila', 'color:red'])

    const alert = screen.getByRole('alert')
    expect(alert).toHaveTextContent("I don't understand these filters")
    expect(alert).toHaveTextContent('osoba:Jarmila, color:red')
  })

  it('renders nothing when every token parsed', () => {
    const { container } = renderAlert([])
    expect(container).toBeEmptyDOMElement()
  })

  it('keeps a repeated token, so the list matches what was typed', () => {
    renderAlert(['color:red', 'color:red'])
    expect(screen.getAllByText('color:red')).toHaveLength(2)
  })
})

describe('UnknownFiltersAlert typo hints', () => {
  it('names the key the mistyped one was reaching for', () => {
    renderAlert(['osoba:Jarmila'])

    expect(screen.getByRole('alert')).toHaveTextContent('Did you mean person:Jarmila?')
  })

  it('types the fix back into the query when the caller can take it', async () => {
    const user = userEvent.setup()
    const onFix = vi.fn()
    renderAlert(['osoba:Jarmila'], 'svatba osoba:Jarmila', onFix)

    await user.click(screen.getByRole('button', { name: 'Did you mean person:Jarmila?' }))

    expect(onFix).toHaveBeenCalledWith('svatba person:Jarmila')
  })

  it('states the suggestion without a button for a caller that cannot rewrite its query', () => {
    renderAlert(['osoba:Jarmila'])

    expect(
      screen.queryByRole('button', { name: 'Did you mean person:Jarmila?' }),
    ).not.toBeInTheDocument()
  })

  it('hints once for a token typed twice, since one mistake is one mistake', () => {
    renderAlert(['osoba:Jarmila', 'osoba:Jarmila'], 'osoba:Jarmila osoba:Jarmila', vi.fn())

    expect(screen.getAllByText('Did you mean person:Jarmila?')).toHaveLength(1)
  })

  it('stays a bare list when nothing valid is close enough to propose', () => {
    renderAlert(['color:red'], 'color:red', vi.fn())

    expect(screen.getByRole('alert')).toHaveTextContent('color:red')
    expect(screen.queryByText(/did you mean/i)).not.toBeInTheDocument()
  })
})
