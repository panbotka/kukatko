import { render, screen, within } from '@testing-library/react'
import type { ReactNode } from 'react'
import { I18nextProvider } from 'react-i18next'
import { beforeEach, describe, expect, it } from 'vitest'

import i18n from '../i18n'

import Modal from './Modal'

/** Renders a dialog shaped like the app's, with the ✕ its header offers. */
function renderDialog(headerProps: { closeLabel?: string } = {}, body: ReactNode = 'Body') {
  render(
    <I18nextProvider i18n={i18n}>
      <Modal show onHide={() => undefined}>
        <Modal.Header closeButton {...headerProps}>
          <Modal.Title>New album</Modal.Title>
        </Modal.Header>
        <Modal.Body>{body}</Modal.Body>
        <Modal.Footer>Footer</Modal.Footer>
      </Modal>
    </I18nextProvider>,
  )
  return screen.getByRole('dialog')
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
})

/**
 * react-bootstrap names the header's ✕ with the English literal `Close`, so on a
 * Czech instance a screen reader read it out in the wrong language on every
 * dialog. These tests pin the name to the catalogue instead — the Czech case is
 * the one that tells the two apart, since the English catalogue value happens to
 * match the library's default.
 */
describe('Modal', () => {
  it('names the close button from the Czech catalogue, not the library default', async () => {
    await i18n.changeLanguage('cs')

    const dialog = renderDialog()

    expect(within(dialog).getByRole('button', { name: 'Zavřít' })).toBeInTheDocument()
    expect(within(dialog).queryByRole('button', { name: /close/i })).toBeNull()
  })

  it('names it from the English catalogue when the interface is English', () => {
    const dialog = renderDialog()

    expect(within(dialog).getByRole('button', { name: 'Close' })).toBeInTheDocument()
  })

  it('still lets a dialog name its own ✕ something more specific', async () => {
    await i18n.changeLanguage('cs')

    const dialog = renderDialog({ closeLabel: 'Zavřít náhled' })

    expect(within(dialog).getByRole('button', { name: 'Zavřít náhled' })).toBeInTheDocument()
  })

  it('renders the title, body and footer the library would', () => {
    const dialog = renderDialog({}, 'The album is empty for now.')

    expect(within(dialog).getByText('New album')).toBeInTheDocument()
    expect(within(dialog).getByText('The album is empty for now.')).toBeInTheDocument()
    expect(within(dialog).getByText('Footer')).toBeInTheDocument()
  })
})
