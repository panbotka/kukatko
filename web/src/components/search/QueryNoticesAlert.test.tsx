import { render, screen } from '@testing-library/react'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it } from 'vitest'

import i18n from '../../i18n'

import { QueryNoticesAlert } from './QueryNoticesAlert'

function renderAlert(notices: string[]) {
  return render(
    <I18nextProvider i18n={i18n}>
      <MemoryRouter>
        <QueryNoticesAlert notices={notices} />
      </MemoryRouter>
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
})

describe('QueryNoticesAlert', () => {
  it('explains an empty grid caused by an unlinked person:me, and offers the fix', () => {
    renderAlert(['person_me_unlinked'])

    expect(screen.getByText(/person:me/)).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /My account/ })).toHaveAttribute('href', '/account')
  })

  it('renders nothing when there is nothing to say', () => {
    const { container } = renderAlert([])
    expect(container).toBeEmptyDOMElement()
  })

  it('ignores a code it does not know rather than printing an identifier', () => {
    // A client older than the server must not show the reader a raw code.
    const { container } = renderAlert(['some_future_reason'])
    expect(container).toBeEmptyDOMElement()
  })
})
