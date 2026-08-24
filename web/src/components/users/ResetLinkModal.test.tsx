import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'
import { ApiError } from '../../services/auth'
import { type AdminUser } from '../../services/users'

import { ResetLinkModal } from './ResetLinkModal'

vi.mock('../../services/users', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../services/users')>()
  return { ...actual, issuePasswordReset: vi.fn() }
})

const { issuePasswordReset } = await import('../../services/users')
const issueMock = vi.mocked(issuePasswordReset)

/** The link the backend hands back, and the address it says it mailed it to. */
const RESET_URL = 'https://kukatko.example/password-reset/tok-123'

/** An account row; only the uid and the username reach this dialog. */
const ADA = { uid: 'u1', username: 'ada' } as AdminUser

function renderModal(onHide = vi.fn()) {
  render(
    <I18nextProvider i18n={i18n}>
      <ResetLinkModal user={ADA} onHide={onHide} />
    </I18nextProvider>,
  )
  return onHide
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  issueMock.mockReset()
  issueMock.mockResolvedValue({
    reset_url: RESET_URL,
    expires_at: '2026-09-01T10:00:00Z',
    email: 'ada@example.com',
  })
})

describe('ResetLinkModal', () => {
  it('asks before it issues anything', () => {
    renderModal()

    expect(screen.getByText(/one-time link by e-mail/)).toBeInTheDocument()
    expect(issueMock).not.toHaveBeenCalled()
    // Nothing to copy yet — the field only exists once a link does.
    expect(screen.queryByLabelText('Password reset link')).not.toBeInTheDocument()
  })

  it('issues the link on confirmation and offers it for copying', async () => {
    const actor = userEvent.setup()
    renderModal()

    await actor.click(screen.getByRole('button', { name: 'Issue the link' }))
    await waitFor(() => {
      expect(issueMock).toHaveBeenCalledWith('u1')
    })

    // The whole link is in a field, so it can be selected and dragged out even
    // where the clipboard is denied.
    const field = screen.getByLabelText('Password reset link')
    expect(field).toHaveValue(RESET_URL)
    expect(field).toHaveAttribute('readonly')

    // Where it went and how long it lasts, both said out loud.
    expect(screen.getByText(/e-mailed to ada@example.com/)).toBeInTheDocument()
    expect(screen.getByText(/valid for 7 days/)).toBeInTheDocument()

    await actor.click(screen.getByRole('button', { name: 'Copy' }))
    await expect(navigator.clipboard.readText()).resolves.toBe(RESET_URL)
    expect(screen.getByRole('button', { name: 'Copied' })).toBeInTheDocument()
  })

  it('says nothing was mailed when the address is a placeholder', async () => {
    issueMock.mockResolvedValue({
      reset_url: RESET_URL,
      expires_at: '2026-09-01T10:00:00Z',
      email: 'ada@kukatko.invalid',
    })
    const actor = userEvent.setup()
    renderModal()

    await actor.click(screen.getByRole('button', { name: 'Issue the link' }))

    // Promising a mail that the backend refuses to send would leave the
    // administrator waiting instead of passing the link on.
    expect(await screen.findByText(/pass the link on yourself/)).toBeInTheDocument()
    expect(screen.queryByText(/kukatko.invalid/)).not.toBeInTheDocument()
    expect(screen.getByLabelText('Password reset link')).toHaveValue(RESET_URL)
  })

  it('explains a refusal and leaves the question standing', async () => {
    issueMock.mockRejectedValue(new ApiError(409, 'auth: user is disabled'))
    const actor = userEvent.setup()
    renderModal()

    await actor.click(screen.getByRole('button', { name: 'Issue the link' }))

    // A 409 here is never a taken username: it is the blocked account, and the
    // message says what to do about it.
    expect(await screen.findByRole('alert')).toHaveTextContent('The account is blocked.')
    expect(screen.queryByLabelText('Password reset link')).not.toBeInTheDocument()
    // The same button is still there to retry with.
    expect(screen.getByRole('button', { name: 'Issue the link' })).toBeEnabled()
  })
})
