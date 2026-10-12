import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'

import { MapsetControl } from './MapsetControl'

beforeEach(async () => {
  await i18n.changeLanguage('en')
})

function renderControl(compact: boolean) {
  const onChange = vi.fn()
  render(
    <I18nextProvider i18n={i18n}>
      <MapsetControl mapset="basic" onChange={onChange} compact={compact} />
    </I18nextProvider>,
  )
  return onChange
}

describe('MapsetControl', () => {
  it('lays the three base maps out as pressed/unpressed buttons on a desktop', async () => {
    const user = userEvent.setup()
    const onChange = renderControl(false)
    expect(screen.getByRole('group', { name: 'Base map' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Basic' })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByRole('button', { name: 'Aerial' })).toHaveAttribute('aria-pressed', 'false')
    await user.click(screen.getByRole('button', { name: 'Aerial' }))
    expect(onChange).toHaveBeenCalledWith('aerial')
  })

  it('folds them behind one layers button on a phone', async () => {
    const user = userEvent.setup()
    const onChange = renderControl(true)
    expect(screen.queryByRole('button', { name: 'Aerial' })).not.toBeInTheDocument()
    const toggle = screen.getByRole('button', { name: 'Change the base map' })
    await user.click(toggle)
    expect(toggle).toHaveAttribute('aria-expanded', 'true')
    await user.click(screen.getByRole('button', { name: 'Outdoor' }))
    expect(onChange).toHaveBeenCalledWith('outdoor')
    expect(screen.queryByRole('button', { name: 'Outdoor' })).not.toBeInTheDocument()
  })
})
