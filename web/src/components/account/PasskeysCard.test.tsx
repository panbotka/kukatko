import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { CapabilitiesContext, type CapabilitiesState } from '../../capabilities/CapabilitiesContext'
import i18n from '../../i18n'
import { PasskeyError, type Passkey } from '../../services/passkeys'

import { PasskeysCard } from './PasskeysCard'

vi.mock('../../services/passkeys', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../services/passkeys')>()
  return {
    ...actual,
    isPasskeySupported: vi.fn(),
    fetchPasskeys: vi.fn(),
    registerPasskey: vi.fn(),
    deletePasskey: vi.fn(),
  }
})

const { deletePasskey, fetchPasskeys, isPasskeySupported, registerPasskey } =
  await import('../../services/passkeys')
const listMock = vi.mocked(fetchPasskeys)
const addMock = vi.mocked(registerPasskey)
const removeMock = vi.mocked(deletePasskey)
const supportedMock = vi.mocked(isPasskeySupported)

/** A passkey that has already signed somebody in. */
const USED_PASSKEY: Passkey = {
  id: 'pk1',
  name: 'Tomášův telefon',
  transports: ['internal', 'hybrid'],
  created_at: '2026-02-01T10:00:00Z',
  last_used_at: '2026-03-04T08:30:00Z',
}

/** A passkey nobody has signed in with yet. */
const FRESH_PASSKEY: Passkey = {
  id: 'pk2',
  name: 'notebook',
  transports: ['internal'],
  created_at: '2026-02-02T10:00:00Z',
}

/**
 * The same date-and-time rendering the card uses, so the assertions do not
 * hard-code one machine's locale data or timezone.
 */
function stamp(iso: string): string {
  return new Date(iso).toLocaleString('en', {
    year: 'numeric',
    month: 'numeric',
    day: 'numeric',
    hour: 'numeric',
    minute: '2-digit',
  })
}

/** Renders the card as an instance with passkeys configured, unless told otherwise. */
function renderCard(capabilities: Partial<CapabilitiesState> = {}) {
  const value: CapabilitiesState = {
    semantic_search: false,
    passkeys: true,
    known: true,
    ...capabilities,
  }
  return render(
    <I18nextProvider i18n={i18n}>
      <CapabilitiesContext.Provider value={value}>
        <PasskeysCard />
      </CapabilitiesContext.Provider>
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  listMock.mockResolvedValue([])
  addMock.mockResolvedValue(FRESH_PASSKEY)
  removeMock.mockResolvedValue(undefined)
  supportedMock.mockReturnValue(true)
})

describe('PasskeysCard', () => {
  it('renders nothing at all on an instance without passkeys', () => {
    renderCard({ passkeys: false })

    expect(screen.queryByText('Passkeys')).not.toBeInTheDocument()
    expect(listMock).not.toHaveBeenCalled()
  })

  it('renders nothing while the capabilities are still unknown', () => {
    // The flags start all-off, so a card drawn before the first answer would
    // advertise a feature this instance may not have.
    renderCard({ known: false })

    expect(screen.queryByText('Passkeys')).not.toBeInTheDocument()
  })

  it('explains what a passkey is when the account has none', async () => {
    renderCard()

    expect(await screen.findByText('No passkeys yet')).toBeInTheDocument()
    expect(screen.getByText(/key kept on your phone or computer/i)).toBeInTheDocument()
  })

  it('lists each passkey with when it was added and last used', async () => {
    listMock.mockResolvedValue([USED_PASSKEY, FRESH_PASSKEY])
    renderCard()

    expect(await screen.findByText('Tomášův telefon')).toBeInTheDocument()
    expect(
      screen.getByText(new RegExp(`Added ${stamp(USED_PASSKEY.created_at)}`)),
    ).toBeInTheDocument()
    expect(
      screen.getByText(new RegExp(`Last used ${stamp('2026-03-04T08:30:00Z')}`)),
    ).toBeInTheDocument()
    expect(screen.getByText(/Never used/)).toBeInTheDocument()
  })

  it('adds a passkey under the name that was typed and says so', async () => {
    const user = userEvent.setup()
    addMock.mockResolvedValue(USED_PASSKEY)
    renderCard()

    await user.type(await screen.findByLabelText('Name of the new passkey'), '  Tomášův telefon  ')
    await user.click(screen.getByRole('button', { name: 'Add a passkey' }))

    // The name is trimmed, exactly as the token form trims its own.
    expect(addMock).toHaveBeenCalledWith('Tomášův telefon')
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'The passkey “Tomášův telefon” has been added.',
    )
    // The list is re-read, so the new key shows up like any other client's would.
    expect(listMock).toHaveBeenCalledTimes(2)
    expect(screen.getByLabelText('Name of the new passkey')).toHaveValue('')
  })

  it('adds an unnamed passkey rather than refusing an empty name', async () => {
    const user = userEvent.setup()
    addMock.mockResolvedValue({ ...FRESH_PASSKEY, name: '' })
    renderCard()

    await user.click(await screen.findByRole('button', { name: 'Add a passkey' }))

    expect(addMock).toHaveBeenCalledWith('')
    expect(await screen.findByRole('alert')).toHaveTextContent('Unnamed passkey')
  })

  it('says a cancelled prompt in plain words, never the exception', async () => {
    const user = userEvent.setup()
    addMock.mockRejectedValue(
      new PasskeyError('cancelled', 'The operation either timed out or was not allowed.'),
    )
    renderCard()

    await user.click(await screen.findByRole('button', { name: 'Add a passkey' }))

    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent(/was not completed/i)
    expect(alert).not.toHaveTextContent(/timed out or was not allowed/i)
  })

  it('tells an authenticator that already has a key from a real failure', async () => {
    const user = userEvent.setup()
    addMock.mockRejectedValue(new PasskeyError('duplicate'))
    renderCard()

    await user.click(await screen.findByRole('button', { name: 'Add a passkey' }))

    expect(await screen.findByRole('alert')).toHaveTextContent(/already holds a passkey/i)
  })

  it('removes a passkey only after the confirmation', async () => {
    const user = userEvent.setup()
    listMock.mockResolvedValue([USED_PASSKEY])
    renderCard()

    await user.click(
      await screen.findByRole('button', { name: 'Remove the passkey Tomášův telefon' }),
    )

    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByText(/Tomášův telefon/)).toBeInTheDocument()
    expect(removeMock).not.toHaveBeenCalled()

    await user.click(within(dialog).getByRole('button', { name: 'Remove the passkey' }))

    expect(removeMock).toHaveBeenCalledWith('pk1')
    expect(screen.queryByText('Tomášův telefon')).not.toBeInTheDocument()
  })

  it('keeps the passkey listed when the removal is called off', async () => {
    const user = userEvent.setup()
    listMock.mockResolvedValue([USED_PASSKEY])
    renderCard()

    await user.click(
      await screen.findByRole('button', { name: 'Remove the passkey Tomášův telefon' }),
    )
    const dialog = await screen.findByRole('dialog')
    await user.click(within(dialog).getByRole('button', { name: 'Cancel' }))

    expect(removeMock).not.toHaveBeenCalled()
    expect(screen.getByText('Tomášův telefon')).toBeInTheDocument()
  })

  it('puts a failed removal back in the list and says so', async () => {
    const user = userEvent.setup()
    listMock.mockResolvedValue([USED_PASSKEY])
    removeMock.mockRejectedValue(new PasskeyError('generic'))
    renderCard()

    await user.click(
      await screen.findByRole('button', { name: 'Remove the passkey Tomášův telefon' }),
    )
    const dialog = await screen.findByRole('dialog')
    await user.click(within(dialog).getByRole('button', { name: 'Remove the passkey' }))

    expect(await screen.findByRole('alert')).toHaveTextContent(/could not be removed/i)
    expect(screen.getByText('Tomášův telefon')).toBeInTheDocument()
  })

  it('keeps the list but drops the form in a browser without WebAuthn', async () => {
    // The keys are still this account's, and still worth removing; only minting
    // a new one is impossible here.
    supportedMock.mockReturnValue(false)
    listMock.mockResolvedValue([USED_PASSKEY])
    renderCard()

    expect(await screen.findByText('Tomášův telefon')).toBeInTheDocument()
    expect(screen.getByText(/cannot use passkeys/i)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Add a passkey' })).not.toBeInTheDocument()
  })

  it('explains an instance that stopped offering passkeys instead of failing', async () => {
    listMock.mockRejectedValue(new PasskeyError('unavailable'))
    renderCard()

    expect(await screen.findByRole('alert')).toHaveTextContent(/does not offer passkeys yet/i)
    expect(screen.queryByRole('button', { name: 'Add a passkey' })).not.toBeInTheDocument()
  })

  it('offers a retry when the listing simply failed', async () => {
    listMock.mockRejectedValueOnce(new PasskeyError('generic'))
    renderCard()

    expect(await screen.findByText('The passkeys could not be loaded.')).toBeInTheDocument()
  })
})
