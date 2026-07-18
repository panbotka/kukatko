import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { afterEach, beforeAll, beforeEach, describe, expect, it } from 'vitest'

import i18n from '../../i18n'

import { GridDensityControl } from './GridDensityControl'

const STORAGE_KEY = 'kukatko.grid.density'

function renderControl() {
  return render(
    <I18nextProvider i18n={i18n}>
      <GridDensityControl />
    </I18nextProvider>,
  )
}

beforeAll(async () => {
  await i18n.changeLanguage('en')
})

beforeEach(() => {
  window.localStorage.clear()
})

afterEach(() => {
  window.localStorage.clear()
})

describe('GridDensityControl', () => {
  it('shows the current column count as a read-only readout', () => {
    window.localStorage.setItem(STORAGE_KEY, '4')
    renderControl()
    expect(screen.getByText('4')).toBeInTheDocument()
  })

  it('offers only fewer/more steps — no auto or reset control', () => {
    window.localStorage.setItem(STORAGE_KEY, '5')
    renderControl()
    // Exactly two buttons: the − and + steppers. The centre readout is not a button.
    const buttons = screen.getAllByRole('button')
    expect(buttons).toHaveLength(2)
    expect(screen.queryByRole('button', { name: /automatic/i })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /reset/i })).not.toBeInTheDocument()
  })

  it('steps up one column at a time and persists the count', async () => {
    window.localStorage.setItem(STORAGE_KEY, '4')
    const user = userEvent.setup()
    renderControl()

    await user.click(screen.getByRole('button', { name: 'More tiles per row' }))

    expect(window.localStorage.getItem(STORAGE_KEY)).toBe('5')
    expect(screen.getByText('5')).toBeInTheDocument()
  })

  it('steps down one column at a time and persists the count', async () => {
    window.localStorage.setItem(STORAGE_KEY, '4')
    const user = userEvent.setup()
    renderControl()

    await user.click(screen.getByRole('button', { name: 'Fewer tiles per row' }))

    expect(window.localStorage.getItem(STORAGE_KEY)).toBe('3')
    expect(screen.getByText('3')).toBeInTheDocument()
  })

  it('disables the fewer button at one photo per row', () => {
    window.localStorage.setItem(STORAGE_KEY, '1')
    renderControl()
    expect(screen.getByText('1')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Fewer tiles per row' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'More tiles per row' })).toBeEnabled()
  })

  it('disables the more button at the maximum column count', () => {
    window.localStorage.setItem(STORAGE_KEY, '10')
    renderControl()
    expect(screen.getByText('10')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'More tiles per row' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Fewer tiles per row' })).toBeEnabled()
  })
})
