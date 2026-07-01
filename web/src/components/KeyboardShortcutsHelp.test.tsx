import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { beforeEach, describe, expect, it } from 'vitest'

import i18n from '../i18n'

import { KeyboardShortcutsHelp } from './KeyboardShortcutsHelp'

function renderHelp() {
  return render(
    <I18nextProvider i18n={i18n}>
      <KeyboardShortcutsHelp />
      <input aria-label="field" />
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
})

describe('KeyboardShortcutsHelp', () => {
  it('opens the help overlay when ? is pressed', async () => {
    renderHelp()
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()

    fireEvent.keyDown(document, { key: '?' })

    expect(await screen.findByRole('dialog')).toBeInTheDocument()
    expect(screen.getByText('Photo grid')).toBeInTheDocument()
    expect(screen.getByText('Photo detail')).toBeInTheDocument()
    expect(screen.getByText('Open the focused photo')).toBeInTheDocument()
  })

  it('opens the overlay from the keyboard-icon button', async () => {
    const user = userEvent.setup()
    renderHelp()
    await user.click(screen.getByRole('button', { name: 'Keyboard shortcuts' }))
    expect(await screen.findByRole('dialog')).toBeInTheDocument()
  })

  it('closes via the close button', async () => {
    const user = userEvent.setup()
    renderHelp()
    fireEvent.keyDown(document, { key: '?' })
    await screen.findByRole('dialog')

    await user.click(screen.getByRole('button', { name: 'Close' }))
    await waitFor(() => {
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    })
  })

  it('does not open while typing in an input', () => {
    renderHelp()
    const input = screen.getByLabelText('field')
    input.focus()
    fireEvent.keyDown(input, { key: '?' })
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })
})
