import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'

import i18n from '../i18n'

import { TechnicalDetail } from './TechnicalDetail'

function renderDetail(label?: string) {
  return render(
    <I18nextProvider i18n={i18n}>
      <TechnicalDetail id="detail-region" label={label}>
        <span>pq: connection refused</span>
      </TechnicalDetail>
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
})

afterEach(async () => {
  await i18n.changeLanguage('en')
})

describe('TechnicalDetail', () => {
  it('keeps the machine-readable text out of the page until it is asked for', async () => {
    const user = userEvent.setup()
    renderDetail()

    // Closed on first render: the error is not in the DOM at all, so a page
    // search does not find it and a screen reader does not read it out.
    const toggle = screen.getByRole('button', { name: 'Technical details' })
    expect(toggle).toHaveAttribute('aria-expanded', 'false')
    expect(toggle).toHaveAttribute('aria-controls', 'detail-region')
    expect(screen.queryByText('pq: connection refused')).toBeNull()

    await user.click(toggle)
    expect(toggle).toHaveAttribute('aria-expanded', 'true')
    expect(screen.getByText('pq: connection refused')).toBeInTheDocument()

    await user.click(toggle)
    expect(screen.queryByText('pq: connection refused')).toBeNull()
  })

  it('takes a caller-supplied summary label', () => {
    renderDetail('What the server reported')

    expect(screen.getByRole('button', { name: 'What the server reported' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Technical details' })).toBeNull()
  })

  it('speaks Czech when the app does', async () => {
    await i18n.changeLanguage('cs')
    renderDetail()

    expect(screen.getByRole('button', { name: 'Technické podrobnosti' })).toBeInTheDocument()
  })
})
