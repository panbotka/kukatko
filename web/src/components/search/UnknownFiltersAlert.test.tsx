import { render, screen } from '@testing-library/react'
import { I18nextProvider } from 'react-i18next'
import { beforeEach, describe, expect, it } from 'vitest'

import i18n from '../../i18n'

import { UnknownFiltersAlert } from './UnknownFiltersAlert'

function renderAlert(tokens: string[]) {
  return render(
    <I18nextProvider i18n={i18n}>
      <UnknownFiltersAlert tokens={tokens} />
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
