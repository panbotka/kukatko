import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../i18n'

import { ConfirmModal, type ConfirmModalProps } from './ConfirmModal'

function renderModal(props: Partial<ConfirmModalProps> = {}) {
  const onConfirm = vi.fn()
  const onCancel = vi.fn()
  render(
    <I18nextProvider i18n={i18n}>
      <ConfirmModal
        show
        title="Delete album?"
        confirmLabel="Delete album"
        onConfirm={onConfirm}
        onCancel={onCancel}
        {...props}
      >
        The album "Holidays" will be removed. The photos themselves are kept.
      </ConfirmModal>
    </I18nextProvider>,
  )
  return { onConfirm, onCancel }
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
})

describe('ConfirmModal', () => {
  it('states the title, the consequence and an action-named confirm button', () => {
    renderModal()

    const dialog = screen.getByRole('dialog')
    expect(within(dialog).getByText('Delete album?')).toBeInTheDocument()
    expect(within(dialog).getByText(/The photos themselves are kept/)).toBeInTheDocument()
    // The confirm button carries the action itself, never a bare "OK".
    expect(within(dialog).getByRole('button', { name: 'Delete album' })).toBeInTheDocument()
    expect(within(dialog).queryByRole('button', { name: 'OK' })).not.toBeInTheDocument()
  })

  it('defaults the cancel button to the shared localized label', () => {
    renderModal()

    expect(screen.getByRole('button', { name: 'Cancel' })).toBeInTheDocument()
  })

  it('uses a custom cancel label when one is given', () => {
    renderModal({ cancelLabel: 'Keep album' })

    expect(screen.getByRole('button', { name: 'Keep album' })).toBeInTheDocument()
  })

  it('paints the confirm button as destructive by default', () => {
    renderModal()

    expect(screen.getByRole('button', { name: 'Delete album' })).toHaveClass('btn-danger')
  })

  it('paints the confirm button in the accent when the action is not destructive', () => {
    renderModal({ variant: 'primary', confirmLabel: 'Start import' })

    expect(screen.getByRole('button', { name: 'Start import' })).toHaveClass('btn-primary')
  })

  it('runs onConfirm from the confirm button and onCancel from cancel', async () => {
    const user = userEvent.setup()
    const { onConfirm, onCancel } = renderModal()

    await user.click(screen.getByRole('button', { name: 'Delete album' }))
    expect(onConfirm).toHaveBeenCalledTimes(1)

    await user.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(onCancel).toHaveBeenCalledTimes(1)
  })

  it('cancels on Escape', async () => {
    const user = userEvent.setup()
    const { onCancel } = renderModal()

    await user.keyboard('{Escape}')
    expect(onCancel).toHaveBeenCalled()
  })

  it('disables both buttons while the confirmed action is in flight', () => {
    renderModal({ busy: true })

    expect(screen.getByRole('button', { name: 'Delete album' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Cancel' })).toBeDisabled()
  })
})
