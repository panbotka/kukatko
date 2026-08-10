import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'
import { ApiError, type ApiToken } from '../../services/auth'

import { ApiTokensCard } from './ApiTokensCard'

vi.mock('../../services/auth', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../services/auth')>()
  return {
    ...actual,
    fetchApiTokens: vi.fn(),
    createApiToken: vi.fn(),
    revokeApiToken: vi.fn(),
  }
})

const { createApiToken, fetchApiTokens, revokeApiToken } = await import('../../services/auth')
const listMock = vi.mocked(fetchApiTokens)
const createMock = vi.mocked(createApiToken)
const revokeMock = vi.mocked(revokeApiToken)

/** A token that has been used, so the row shows both of its timestamps. */
const USED_TOKEN: ApiToken = {
  id: 'at1',
  user_uid: 'u1',
  name: 'backup script',
  created_at: '2026-02-01T10:00:00Z',
  last_used_at: '2026-03-04T08:30:00Z',
}

/** A token that has never authenticated anything. */
const FRESH_TOKEN: ApiToken = {
  id: 'at2',
  user_uid: 'u1',
  name: 'laptop cli',
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

function renderCard() {
  return render(
    <I18nextProvider i18n={i18n}>
      <MemoryRouter>
        <ApiTokensCard />
      </MemoryRouter>
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  listMock.mockResolvedValue([])
  createMock.mockResolvedValue({ token: FRESH_TOKEN, secret: 'kkt_at2_secret' })
  revokeMock.mockResolvedValue(undefined)
})

describe('ApiTokensCard list', () => {
  it('lists every token by name, with the prefix that identifies it', async () => {
    listMock.mockResolvedValue([USED_TOKEN, FRESH_TOKEN])
    renderCard()

    expect(await screen.findByText('backup script')).toBeInTheDocument()
    expect(screen.getByText('laptop cli')).toBeInTheDocument()
    // The public prefix identifies a token found in some script's config.
    expect(screen.getByText('kkt_at1_…')).toBeInTheDocument()
    expect(screen.getByText('kkt_at2_…')).toBeInTheDocument()
    // A token nothing has used yet says so rather than showing an empty slot.
    expect(screen.getByText(/Never used/)).toBeInTheDocument()
  })

  it('shows when a token was made and when it was last seen', async () => {
    listMock.mockResolvedValue([USED_TOKEN])
    renderCard()

    const meta = await screen.findByText(/^Created /)
    expect(meta).toHaveTextContent(`Created ${stamp(USED_TOKEN.created_at)}`)
    expect(meta).toHaveTextContent(`Last used ${stamp('2026-03-04T08:30:00Z')}`)
  })

  it('badges an expired token and keeps it in the list', async () => {
    listMock.mockResolvedValue([{ ...USED_TOKEN, expires_at: '2026-02-10T10:00:00Z' }])
    renderCard()

    expect(await screen.findByText('Expired')).toBeInTheDocument()
    expect(screen.getByText(/^Created /)).toHaveTextContent(
      `Valid until ${stamp('2026-02-10T10:00:00Z')}`,
    )
  })

  it('hides a revoked token, which can never authenticate again', async () => {
    listMock.mockResolvedValue([{ ...USED_TOKEN, revoked_at: '2026-03-05T09:00:00Z' }, FRESH_TOKEN])
    renderCard()

    expect(await screen.findByText('laptop cli')).toBeInTheDocument()
    expect(screen.queryByText('backup script')).not.toBeInTheDocument()
  })

  it('explains what a token is for, and links to help, when there is none', async () => {
    renderCard()

    expect(await screen.findByText('No tokens yet')).toBeInTheDocument()
    expect(screen.getByText(/a password for programs/)).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'How tokens are used' })).toHaveAttribute(
      'href',
      '/help#help-api-tokens',
    )
  })
})

describe('ApiTokensCard create', () => {
  it('shows the secret once, in a copyable field, and refreshes the list', async () => {
    const user = userEvent.setup()
    listMock.mockResolvedValueOnce([]).mockResolvedValueOnce([FRESH_TOKEN])
    renderCard()
    await screen.findByText('No tokens yet')

    await user.type(screen.getByLabelText('Name of the new token'), 'laptop cli')
    await user.click(screen.getByRole('button', { name: /Create token/ }))

    expect(createMock).toHaveBeenCalledWith('laptop cli')
    // The secret, once, with the warning that it will never be shown again.
    const secret = await screen.findByLabelText('Secret token')
    expect(secret).toHaveValue('kkt_at2_secret')
    expect(screen.getByText(/never be shown again/)).toBeInTheDocument()
    // …and the list now carries the token the server just minted.
    expect(await screen.findByText('laptop cli')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Copy' }))
    await expect(navigator.clipboard.readText()).resolves.toBe('kkt_at2_secret')
    expect(screen.getByRole('button', { name: 'Copied' })).toBeInTheDocument()

    // Dismissing takes the secret away for good: nothing can show it again.
    await user.click(screen.getByRole('button', { name: 'I have copied it' }))
    expect(screen.queryByLabelText('Secret token')).not.toBeInTheDocument()
    // The name field is empty again, ready for the next token.
    expect(screen.getByLabelText('Name of the new token')).toHaveValue('')
  })

  it('refuses an empty name without asking the server', async () => {
    const user = userEvent.setup()
    renderCard()
    await screen.findByText('No tokens yet')

    await user.click(screen.getByRole('button', { name: /Create token/ }))

    expect(await screen.findByText('Enter a name for the token.')).toBeInTheDocument()
    expect(createMock).not.toHaveBeenCalled()
  })

  it('names the rate limit when the server refuses a burst of tokens', async () => {
    const user = userEvent.setup()
    createMock.mockRejectedValue(new ApiError(429, 'too many token creations'))
    renderCard()
    await screen.findByText('No tokens yet')

    await user.type(screen.getByLabelText('Name of the new token'), 'cli')
    await user.click(screen.getByRole('button', { name: /Create token/ }))

    expect(await screen.findByText(/Too many tokens were created/)).toBeInTheDocument()
    expect(screen.queryByLabelText('Secret token')).not.toBeInTheDocument()
  })

  it('falls back to a generic message when creation fails outright', async () => {
    const user = userEvent.setup()
    createMock.mockRejectedValue(new Error('offline'))
    renderCard()
    await screen.findByText('No tokens yet')

    await user.type(screen.getByLabelText('Name of the new token'), 'cli')
    await user.click(screen.getByRole('button', { name: /Create token/ }))

    expect(
      await screen.findByText('The token could not be created. Please try again.'),
    ).toBeInTheDocument()
  })
})

describe('ApiTokensCard revoke', () => {
  it('names the token in the confirmation and removes the row on confirm', async () => {
    const user = userEvent.setup()
    listMock.mockResolvedValue([USED_TOKEN, FRESH_TOKEN])
    renderCard()
    await screen.findByText('backup script')

    await user.click(screen.getByRole('button', { name: 'Revoke token backup script' }))

    const dialog = screen.getByRole('dialog')
    expect(within(dialog).getByText('Revoke the token?')).toBeInTheDocument()
    expect(within(dialog).getByText(/backup script/)).toBeInTheDocument()

    await user.click(within(dialog).getByRole('button', { name: 'Revoke token' }))

    expect(revokeMock).toHaveBeenCalledWith('at1')
    // Gone at once — and only that one.
    expect(screen.queryByText('backup script')).not.toBeInTheDocument()
    expect(screen.getByText('laptop cli')).toBeInTheDocument()
  })

  it('keeps the token when the confirmation is cancelled', async () => {
    const user = userEvent.setup()
    listMock.mockResolvedValue([USED_TOKEN])
    renderCard()
    await screen.findByText('backup script')

    await user.click(screen.getByRole('button', { name: 'Revoke token backup script' }))
    await user.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Cancel' }))

    expect(revokeMock).not.toHaveBeenCalled()
    expect(screen.getByText('backup script')).toBeInTheDocument()
  })

  it('puts the row back and reports the failure when revoking fails', async () => {
    const user = userEvent.setup()
    listMock.mockResolvedValue([USED_TOKEN])
    revokeMock.mockRejectedValue(new Error('offline'))
    renderCard()
    await screen.findByText('backup script')

    await user.click(screen.getByRole('button', { name: 'Revoke token backup script' }))
    await user.click(
      within(screen.getByRole('dialog')).getByRole('button', { name: 'Revoke token' }),
    )

    expect(await screen.findByText(/could not be revoked/)).toBeInTheDocument()
    expect(screen.getByText('backup script')).toBeInTheDocument()
  })
})

describe('ApiTokensCard error states', () => {
  it('offers a retry when the list cannot be loaded', async () => {
    const user = userEvent.setup()
    listMock.mockRejectedValueOnce(new ApiError(500, 'boom')).mockResolvedValueOnce([USED_TOKEN])
    renderCard()

    expect(await screen.findByText('The tokens could not be loaded.')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: /Try again/ }))

    expect(await screen.findByText('backup script')).toBeInTheDocument()
  })

  it('reads as read-only, with no create form, when the role may not have tokens', async () => {
    listMock.mockRejectedValue(new ApiError(403, 'forbidden'))
    renderCard()

    expect(await screen.findByText(/Your role cannot create API tokens/)).toBeInTheDocument()
    expect(screen.queryByLabelText('Name of the new token')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /Create token/ })).not.toBeInTheDocument()
  })

  it('drops the create form when the server refuses the creation itself', async () => {
    const user = userEvent.setup()
    createMock.mockRejectedValue(new ApiError(403, 'forbidden'))
    renderCard()
    await screen.findByText('No tokens yet')

    await user.type(screen.getByLabelText('Name of the new token'), 'cli')
    await user.click(screen.getByRole('button', { name: /Create token/ }))

    expect(await screen.findByText(/Your role cannot create API tokens/)).toBeInTheDocument()
    expect(screen.queryByLabelText('Name of the new token')).not.toBeInTheDocument()
  })
})
