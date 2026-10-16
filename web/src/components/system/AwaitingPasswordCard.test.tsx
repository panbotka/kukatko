import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'
import type { AwaitingMember } from '../../services/projects'

import { AwaitingPasswordCard } from './AwaitingPasswordCard'

vi.mock('../../services/projects', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../services/projects')>()
  return { ...actual, fetchAwaitingPassword: vi.fn(), resendPasswordLink: vi.fn() }
})

const { fetchAwaitingPassword, resendPasswordLink } = await import('../../services/projects')
const listMock = vi.mocked(fetchAwaitingPassword)
const resendMock = vi.mocked(resendPasswordLink)

const JANA: AwaitingMember = {
  user_uid: 'usj',
  username: 'jana',
  display_name: 'Jana',
  email: 'jana@example.com',
  role: 'admin',
  link_expires_at: '2026-10-23T10:00:00Z',
}

function renderCard() {
  return render(
    <I18nextProvider i18n={i18n}>
      <AwaitingPasswordCard projectUid="pjc" />
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('cs')
  listMock.mockReset()
  resendMock.mockReset()
})

describe('AwaitingPasswordCard', () => {
  it('renders nothing while nobody waits for a password', async () => {
    listMock.mockResolvedValue([])
    renderCard()
    await waitFor(() => {
      expect(listMock).toHaveBeenCalledWith('pjc', expect.anything())
    })
    expect(screen.queryByTestId('awaiting-password')).not.toBeInTheDocument()
  })

  it('lists the member with the link expiry and an expired one as such', async () => {
    listMock.mockResolvedValue([
      JANA,
      { ...JANA, user_uid: 'usp', username: 'petr', link_expires_at: null },
    ])
    renderCard()

    const card = await screen.findByTestId('awaiting-password')
    expect(within(card).getByText('Čeká na nastavení hesla')).toBeInTheDocument()
    expect(card).toHaveTextContent(/odkaz platí do/)
    expect(card).toHaveTextContent('odkaz už neplatí')
  })

  it('re-sends the link and shows it when no e-mail went out', async () => {
    const user = userEvent.setup()
    listMock.mockResolvedValue([JANA])
    resendMock.mockResolvedValue({
      user_uid: 'usj',
      username: 'jana',
      display_name: 'Jana',
      email: 'jana@example.com',
      account: 'existing',
      mail_sent: false,
      set_password_url: 'https://k.example/password-reset/new?project=chata',
      expires_at: '2026-10-23T12:00:00Z',
    })
    renderCard()

    await user.click(await screen.findByRole('button', { name: 'Poslat odkaz znovu' }))
    expect(resendMock).toHaveBeenCalledWith('pjc', 'usj')
    const group = await screen.findByRole('group', { name: 'Nový odkaz pro jana' })
    expect(within(group).getByLabelText('Odkaz na nastavení hesla')).toHaveValue(
      'https://k.example/password-reset/new?project=chata',
    )
    // The list is re-read, so the new expiry shows.
    expect(listMock).toHaveBeenCalledTimes(2)
  })

  it('says so when the re-send fails', async () => {
    const user = userEvent.setup()
    listMock.mockResolvedValue([JANA])
    resendMock.mockRejectedValue(new Error('boom'))
    renderCard()

    await user.click(await screen.findByRole('button', { name: 'Poslat odkaz znovu' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('Odkaz se nepodařilo poslat.')
  })
})
