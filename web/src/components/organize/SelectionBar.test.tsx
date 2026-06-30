import { render, screen } from '@testing-library/react'
import Button from 'react-bootstrap/Button'
import { I18nextProvider } from 'react-i18next'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'

import { SelectionBar } from './SelectionBar'

function renderBar(count = 3) {
  return render(
    <I18nextProvider i18n={i18n}>
      <SelectionBar count={count} onCancel={vi.fn()}>
        <Button>Action</Button>
      </SelectionBar>
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
})

describe('SelectionBar', () => {
  it('renders the count, the action and a cancel control', () => {
    renderBar(2)
    expect(screen.getByText('2 selected')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Action' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Cancel' })).toBeInTheDocument()
  })

  it('uses the shared sticky-toolbar offset so it rests below the navbar', () => {
    renderBar()
    const toolbar = screen.getByRole('toolbar')
    // `.kukatko-sticky-toolbar` offsets the sticky bar by the navbar height (plus
    // any top safe-area inset) instead of tucking under the sticky navbar.
    expect(toolbar).toHaveClass('kukatko-sticky-toolbar')
    expect(toolbar).toHaveClass('flex-wrap')
  })
})
