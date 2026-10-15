import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'
import type { Invite } from '../../services/invites'
import { ToastContext, type ToastApi } from '../toast/ToastContext'

import { InvitesSection } from './InvitesSection'

vi.mock('../../services/invites', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../services/invites')>()
  return { ...actual, fetchInvites: vi.fn(), createInvite: vi.fn(), revokeInvite: vi.fn() }
})

const { fetchInvites, createInvite, revokeInvite, InviteApiError } =
  await import('../../services/invites')
const listMock = vi.mocked(fetchInvites)
const createMock = vi.mocked(createInvite)
const revokeMock = vi.mocked(revokeInvite)

const toastShow = vi.fn<ToastApi['show']>()

/** An invite as the list shows it, overridable. */
function invite(overrides: Partial<Invite> = {}): Invite {
  return {
    uid: 'iv1',
    role: 'curator',
    expires_at: '2026-10-20T10:00:00Z',
    max_uses: 5,
    uses: 2,
    revoked_at: null,
    created_by: 'u1',
    created_by_name: 'Správce',
    created_at: '2026-10-13T10:00:00Z',
    code: 'AB3DEF7H',
    url: '/invite/AB3DEF7H',
    state: 'active',
    ...overrides,
  }
}

/** Puts a clipboard stub on `navigator` (userEvent.setup() installs its own). */
function stubClipboard() {
  const writeText = vi.fn().mockResolvedValue(undefined)
  Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true })
  return writeText
}

function renderSection() {
  return render(
    <I18nextProvider i18n={i18n}>
      <ToastContext.Provider value={{ show: toastShow }}>
        <MemoryRouter>
          <InvitesSection />
        </MemoryRouter>
      </ToastContext.Provider>
    </I18nextProvider>,
  )
}

/** The table row of the invite whose role cell reads `role`. */
function rowOf(role: string): HTMLElement {
  const row = screen.getByRole('cell', { name: role }).closest('tr')
  if (row === null) {
    throw new Error(`no row for ${role}`)
  }
  return row
}

beforeEach(async () => {
  await i18n.changeLanguage('cs')
  listMock.mockReset()
  createMock.mockReset()
  revokeMock.mockReset()
  toastShow.mockReset()
  listMock.mockResolvedValue({ invites: [invite()], default_days: 7, max_days: 90 })
})

describe('InvitesSection', () => {
  it('lists the invites: role, state, expiry, uses of the cap and the creator', async () => {
    listMock.mockResolvedValue({
      invites: [
        invite(),
        invite({
          uid: 'iv2',
          role: 'editor',
          max_uses: null,
          uses: 0,
          state: 'revoked',
          created_by_name: '',
        }),
        invite({ uid: 'iv3', role: 'viewer', created_by_name: '', created_by_former: true }),
      ],
      default_days: 7,
      max_days: 90,
    })
    renderSection()

    const curator = within(await screen.findByRole('row', { name: /Kurátor/ }))
    expect(curator.getByText('Platná')).toBeInTheDocument()
    expect(curator.getByText('2 / 5')).toBeInTheDocument()
    expect(curator.getByText('Správce')).toBeInTheDocument()
    expect(curator.getByRole('button', { name: 'Kopírovat' })).toBeInTheDocument()
    expect(curator.getByRole('button', { name: 'Zrušit pozvánku' })).toBeInTheDocument()

    // A revoked invite offers neither copy nor revoke.
    const editor = within(rowOf('Editor'))
    expect(editor.getByText('Zrušená')).toBeInTheDocument()
    expect(editor.getByText('0 / bez omezení')).toBeInTheDocument()
    expect(editor.getByText('smazaný účet')).toBeInTheDocument()
    expect(editor.queryByRole('button')).not.toBeInTheDocument()

    // A creator who has left the project is a former member, not a deleted account.
    const viewer = within(rowOf('Prohlížeč'))
    expect(viewer.getByText('Bývalý člen projektu')).toBeInTheDocument()
    expect(viewer.queryByText('smazaný účet')).not.toBeInTheDocument()
  })

  it('creates an invite and shows its address with a copy button', async () => {
    listMock.mockResolvedValue({ invites: [], default_days: 7, max_days: 90 })
    createMock.mockResolvedValue(
      invite({ uid: 'iv9', role: 'editor', code: 'ZX9KLM2P', url: '/invite/ZX9KLM2P' }),
    )
    const user = userEvent.setup()
    renderSection()

    expect(await screen.findByLabelText('Platnost (dní)')).toHaveValue(7)
    await user.selectOptions(screen.getByLabelText('Role'), 'editor')
    await user.clear(screen.getByLabelText('Platnost (dní)'))
    await user.type(screen.getByLabelText('Platnost (dní)'), '14')
    await user.type(screen.getByLabelText('Nejvýš použití'), '3')
    await user.click(screen.getByRole('button', { name: 'Vytvořit pozvánku' }))

    expect(createMock).toHaveBeenCalledWith({ role: 'editor', days: 14, max_uses: 3 })
    const created = await screen.findByTestId('invite-created')
    const url = `${window.location.origin}/invite/ZX9KLM2P`
    const field = within(created).getByRole('textbox', { name: 'Odkaz pozvánky' })
    expect(field).toHaveValue(url)
    expect(field).toHaveAttribute('readonly')

    const writeText = stubClipboard()
    await user.click(within(created).getByRole('button', { name: 'Kopírovat' }))
    expect(writeText).toHaveBeenCalledWith(url)
    expect(await within(created).findByRole('button', { name: 'Zkopírováno' })).toBeVisible()
    // …and it is in the list too, copyable later.
    expect(within(rowOf('Editor')).getByRole('button', { name: 'Kopírovat' })).toBeInTheDocument()
  })

  it('sends no cap when the uses field is empty, and refuses days over the maximum', async () => {
    createMock.mockResolvedValue(invite({ uid: 'iv9' }))
    const user = userEvent.setup()
    renderSection()

    const days = await screen.findByLabelText('Platnost (dní)')
    await user.clear(days)
    await user.type(days, '91')
    expect(screen.getByRole('button', { name: 'Vytvořit pozvánku' })).toBeDisabled()
    expect(screen.getByText('Zadejte celé číslo od 1 do 90.')).toBeInTheDocument()

    await user.clear(days)
    await user.type(days, '30')
    await user.click(screen.getByRole('button', { name: 'Vytvořit pozvánku' }))
    expect(createMock).toHaveBeenCalledWith({ role: 'viewer', days: 30 })
  })

  it('notes that an admin invite registers an editor at most', async () => {
    const user = userEvent.setup()
    renderSection()

    await user.selectOptions(await screen.findByLabelText('Role'), 'admin')
    expect(screen.getByTestId('invite-admin-note')).toHaveTextContent('nejvýš roli editora')
  })

  it('words a refusal of the backend', async () => {
    createMock.mockRejectedValue(new InviteApiError(422, 'invalid_max_uses'))
    const user = userEvent.setup()
    renderSection()

    await user.click(await screen.findByRole('button', { name: 'Vytvořit pozvánku' }))
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Zadejte celé číslo alespoň 1, nebo pole nechte prázdné.',
    )
  })

  it('copies a listed invite’s absolute address later', async () => {
    const user = userEvent.setup()
    renderSection()

    const row = within(await screen.findByRole('row', { name: /Kurátor/ }))
    const writeText = stubClipboard()
    await user.click(row.getByRole('button', { name: 'Kopírovat' }))

    expect(writeText).toHaveBeenCalledWith(`${window.location.origin}/invite/AB3DEF7H`)
    expect(await row.findByRole('button', { name: 'Zkopírováno' })).toBeInTheDocument()
  })

  it('revokes an invite after a confirmation', async () => {
    revokeMock.mockResolvedValue(undefined)
    const user = userEvent.setup()
    renderSection()

    const row = within(await screen.findByRole('row', { name: /Kurátor/ }))
    await user.click(row.getByRole('button', { name: 'Zrušit pozvánku' }))
    expect(revokeMock).not.toHaveBeenCalled()
    const dialog = await screen.findByRole('dialog')
    await user.click(within(dialog).getByRole('button', { name: 'Zrušit pozvánku' }))

    expect(revokeMock).toHaveBeenCalledWith('iv1')
    expect(await row.findByText('Zrušená')).toBeInTheDocument()
    expect(row.queryByRole('button', { name: 'Kopírovat' })).not.toBeInTheDocument()
    expect(toastShow).toHaveBeenCalledWith({ message: 'Pozvánka je zrušená.', variant: 'success' })
  })

  it('refetches the list when the invite vanished meanwhile', async () => {
    revokeMock.mockRejectedValue(new InviteApiError(404, 'not_found'))
    const user = userEvent.setup()
    renderSection()

    const row = within(await screen.findByRole('row', { name: /Kurátor/ }))
    await user.click(row.getByRole('button', { name: 'Zrušit pozvánku' }))
    const dialog = await screen.findByRole('dialog')
    await user.click(within(dialog).getByRole('button', { name: 'Zrušit pozvánku' }))

    await waitFor(() => {
      expect(listMock).toHaveBeenCalledTimes(2)
    })
    expect(toastShow).toHaveBeenCalledWith({
      message: 'Tahle pozvánka už neexistuje — seznam jsem obnovil.',
      variant: 'danger',
    })
  })
})
