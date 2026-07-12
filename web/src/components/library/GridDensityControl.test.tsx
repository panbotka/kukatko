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
  it('starts on auto with only the "more" step available', () => {
    renderControl()
    expect(screen.getByRole('button', { name: 'Fewer tiles per row' })).toBeDisabled()
    // The centre chip announces the auto state and cannot reset what is already auto.
    expect(screen.getByRole('button', { name: 'Automatic' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'More tiles per row' })).toBeEnabled()
  })

  it('enters the pinned range from auto and persists the column count', async () => {
    const user = userEvent.setup()
    renderControl()

    await user.click(screen.getByRole('button', { name: 'More tiles per row' }))

    // 'auto' steps to the minimum pinned count, which really reaches localStorage.
    expect(window.localStorage.getItem(STORAGE_KEY)).toBe('2')
    expect(screen.getByText('2')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Fewer tiles per row' })).toBeEnabled()
  })

  it('steps down to one photo per row and disables the fewer button there', async () => {
    window.localStorage.setItem(STORAGE_KEY, '2')
    const user = userEvent.setup()
    renderControl()

    await user.click(screen.getByRole('button', { name: 'Fewer tiles per row' }))

    expect(window.localStorage.getItem(STORAGE_KEY)).toBe('1')
    expect(screen.getByText('1')).toBeInTheDocument()
    // One photo per row is the floor: there is nothing fewer to step to.
    expect(screen.getByRole('button', { name: 'Fewer tiles per row' })).toBeDisabled()
  })

  it('steps back up from one photo per row', async () => {
    window.localStorage.setItem(STORAGE_KEY, '1')
    const user = userEvent.setup()
    renderControl()

    expect(screen.getByRole('button', { name: 'Fewer tiles per row' })).toBeDisabled()
    await user.click(screen.getByRole('button', { name: 'More tiles per row' }))

    expect(window.localStorage.getItem(STORAGE_KEY)).toBe('2')
    expect(screen.getByRole('button', { name: 'Fewer tiles per row' })).toBeEnabled()
  })

  it('cannot step past the maximum column count', () => {
    window.localStorage.setItem(STORAGE_KEY, '8')
    renderControl()
    expect(screen.getByRole('button', { name: 'More tiles per row' })).toBeDisabled()
  })

  it('resets a pinned density to auto from the centre chip', async () => {
    window.localStorage.setItem(STORAGE_KEY, '5')
    const user = userEvent.setup()
    renderControl()

    // The centre chip doubles as the reset control; its name states the current count.
    await user.click(screen.getByRole('button', { name: 'Reset to automatic (now 5 per row)' }))

    expect(window.localStorage.getItem(STORAGE_KEY)).toBe('"auto"')
    expect(screen.getByRole('button', { name: 'Automatic' })).toBeDisabled()
  })
})
