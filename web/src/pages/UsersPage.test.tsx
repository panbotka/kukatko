import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
import i18n from '../i18n'
import { ApiError } from '../services/auth'
import { type AdminUser } from '../services/users'

import { UsersPage } from './UsersPage'

vi.mock('../services/users', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/users')>()
  return {
    ...actual,
    fetchUsers: vi.fn(),
    createUser: vi.fn(),
    updateUser: vi.fn(),
    setUserDisabled: vi.fn(),
    resetUserPassword: vi.fn(),
  }
})

const { fetchUsers, createUser, setUserDisabled, updateUser } = await import('../services/users')
const fetchUsersMock = vi.mocked(fetchUsers)
const createUserMock = vi.mocked(createUser)
const setUserDisabledMock = vi.mocked(setUserDisabled)
const updateUserMock = vi.mocked(updateUser)

/**
 * What the backend answers when a change would take away the instance's last
 * enabled maintainer (`auth.ErrLastMaintainer` → 409); the page tells it apart
 * from the other 409, a duplicate username, by the message.
 */
const LAST_MAINTAINER_ERROR = new ApiError(409, 'auth: cannot remove the last maintainer')

/** The opening words of the last-maintainer explanation, for a partial match. */
const LAST_MAINTAINER_TEXT = /This is the last enabled maintainer/

/** The signed-in administrator; their own row must not offer self-disabling. */
const ME = 'u-admin'

/** Builds an admin user row, defaulting to an enabled viewer. */
function user(overrides: Partial<AdminUser> = {}): AdminUser {
  return {
    uid: 'u1',
    username: 'ada',
    display_name: 'Ada Lovelace',
    email: '',
    role: 'viewer',
    disabled: false,
    note: '',
    created_at: '2026-01-02T10:00:00Z',
    updated_at: '2026-01-02T10:00:00Z',
    ...overrides,
  }
}

function auth(opts: { isAdmin?: boolean; isMaintainer?: boolean } = {}): AuthContextValue {
  const { isMaintainer = false } = opts
  // A maintainer is admin-or-higher, so it satisfies isAdmin too.
  const isAdmin = opts.isAdmin ?? isMaintainer
  const role = isMaintainer ? 'maintainer' : isAdmin ? 'admin' : 'viewer'
  return {
    status: 'authenticated',
    user: { uid: ME, username: 'root', display_name: 'Root', role },
    role,
    downloadToken: null,
    canWrite: isAdmin,
    isAdmin,
    isMaintainer,
    login: vi.fn(),
    logout: vi.fn(),
    refresh: vi.fn(),
  } as unknown as AuthContextValue
}

/** The shared setup stubs a non-matching (desktop) `matchMedia`; restore it after. */
const realMatchMedia = window.matchMedia

/**
 * Points `window.matchMedia` at a fixed phone/desktop answer, so
 * `useIsNarrowViewport` — and through it the roster's table/card choice — takes
 * the branch under test.
 */
function mockViewport(narrow: boolean): void {
  window.matchMedia = vi.fn().mockImplementation((query: string) => ({
    matches: narrow,
    media: query,
    onchange: null,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    addListener: vi.fn(),
    removeListener: vi.fn(),
    dispatchEvent: vi.fn(),
  }))
}

function renderPage(value: AuthContextValue = auth({ isAdmin: true })) {
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={value}>
        <MemoryRouter>
          <UsersPage />
        </MemoryRouter>
      </AuthContext.Provider>
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  fetchUsersMock.mockReset()
  fetchUsersMock.mockResolvedValue([])
  createUserMock.mockReset()
  setUserDisabledMock.mockReset()
  updateUserMock.mockReset()
})

afterEach(() => {
  window.matchMedia = realMatchMedia
})

describe('UsersPage', () => {
  it('denies access to non-admins and never fetches the roster', () => {
    renderPage(auth())

    expect(screen.getByText('This page is available to administrators only.')).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: 'Users' })).not.toBeInTheDocument()
    expect(fetchUsersMock).not.toHaveBeenCalled()
  })

  it('offers the maintainer role only to a maintainer', async () => {
    fetchUsersMock.mockResolvedValue([])
    const actor = userEvent.setup()

    // A plain admin cannot grant maintainer, so the role select omits it.
    const adminView = renderPage(auth({ isAdmin: true }))
    await actor.click(screen.getByRole('button', { name: 'New user' }))
    const adminDialog = await screen.findByRole('dialog')
    const adminSelect = within(adminDialog).getByLabelText('Role')
    expect(within(adminSelect).getByRole('option', { name: 'Administrator' })).toBeInTheDocument()
    expect(within(adminSelect).queryByRole('option', { name: 'Maintainer' })).toBeNull()
    adminView.unmount()

    // A maintainer sees the maintainer option and can assign it.
    renderPage(auth({ isMaintainer: true }))
    await actor.click(screen.getByRole('button', { name: 'New user' }))
    const maintainerDialog = await screen.findByRole('dialog')
    const maintainerSelect = within(maintainerDialog).getByLabelText('Role')
    expect(within(maintainerSelect).getByRole('option', { name: 'Maintainer' })).toBeInTheDocument()
  })

  it('locks a maintainer account against a non-maintainer admin', async () => {
    fetchUsersMock.mockResolvedValue([user({ uid: 'u9', username: 'ops', role: 'maintainer' })])
    renderPage(auth({ isAdmin: true }))

    expect(await screen.findByText('ops')).toBeInTheDocument()
    const row = screen.getByText('ops').closest('tr') as HTMLElement
    // A non-maintainer cannot edit, reset the password of, or disable a maintainer.
    expect(within(row).getByRole('button', { name: 'Edit' })).toBeDisabled()
    expect(within(row).getByRole('button', { name: 'Change password' })).toBeDisabled()
    expect(within(row).getByRole('button', { name: 'Disable' })).toBeDisabled()
  })

  it('lets a maintainer manage another maintainer account', async () => {
    fetchUsersMock.mockResolvedValue([user({ uid: 'u9', username: 'ops', role: 'maintainer' })])
    renderPage(auth({ isMaintainer: true }))

    expect(await screen.findByText('ops')).toBeInTheDocument()
    const row = screen.getByText('ops').closest('tr') as HTMLElement
    expect(within(row).getByRole('button', { name: 'Edit' })).toBeEnabled()
  })

  it('renders the table from the fetched users', async () => {
    fetchUsersMock.mockResolvedValue([
      user({
        uid: 'u1',
        username: 'ada',
        display_name: 'Ada Lovelace',
        role: 'editor',
        note: 'On loan from the analytical engine',
        last_login_at: '2026-06-30T08:15:00Z',
      }),
      user({ uid: 'u2', username: 'bob', display_name: '', role: 'viewer', disabled: true }),
    ])
    renderPage()

    expect(await screen.findByText('ada')).toBeInTheDocument()
    expect(screen.getByText('Ada Lovelace')).toBeInTheDocument()
    expect(screen.getByText('Editor')).toBeInTheDocument()
    expect(screen.getByText('On loan from the analytical engine')).toBeInTheDocument()

    // The disabled account is flagged as such, and a user who never signed in
    // renders "Never" rather than an empty cell.
    expect(screen.getByText('bob')).toBeInTheDocument()
    expect(screen.getByText('Disabled')).toBeInTheDocument()
    expect(screen.getByText('Never')).toBeInTheDocument()
  })

  it('keeps the full eight-column table on a wide viewport', async () => {
    fetchUsersMock.mockResolvedValue([user()])
    renderPage()

    expect(await screen.findByText('ada')).toBeInTheDocument()
    const table = screen.getByRole('table')
    expect(within(table).getAllByRole('columnheader')).toHaveLength(8)
    // No card stack alongside it: only one of the two layouts is ever in the DOM.
    expect(screen.queryByRole('listitem')).toBeNull()
  })

  it('reflows the roster into stacked cards with a full-width action row on a phone', async () => {
    mockViewport(true)
    fetchUsersMock.mockResolvedValue([
      user({
        uid: 'u1',
        username: 'ada',
        display_name: 'Ada Lovelace',
        role: 'editor',
        note: 'On loan from the analytical engine',
        last_login_at: '2026-06-30T08:15:00Z',
      }),
    ])
    renderPage()

    expect(await screen.findByText('ada')).toBeInTheDocument()
    // The wide table is gone entirely — nothing left to drag sideways.
    expect(screen.queryByRole('table')).toBeNull()

    // One record, one card, every profile column kept as a "label: value" line.
    const card = screen.getByRole('listitem')
    for (const label of [
      'Username',
      'Real name',
      'Role',
      'State',
      'Note',
      'Last login',
      'Created',
    ]) {
      expect(within(card).getByText(label)).toBeInTheDocument()
    }
    expect(within(card).getByText('Ada Lovelace')).toBeInTheDocument()
    expect(within(card).getByText('Editor')).toBeInTheDocument()
    expect(within(card).getByText('Enabled')).toBeInTheDocument()
    expect(within(card).getByText('On loan from the analytical engine')).toBeInTheDocument()

    // The three row actions sit on the card itself, in the full-width action row,
    // instead of trailing off the right edge of eight columns.
    const edit = within(card).getByRole('button', { name: 'Edit' })
    expect(edit.parentElement).toHaveClass('kk-record-card__actions', 'd-grid')
    expect(within(card).getByRole('button', { name: 'Change password' })).toBeEnabled()
    expect(within(card).getByRole('button', { name: 'Disable' })).toBeEnabled()
    // The actions column header is not repeated as a field label.
    expect(within(card).queryByText('Actions')).toBeNull()
  })

  it('keeps the maintainer boundary and the self-disable guard on a phone card', async () => {
    mockViewport(true)
    fetchUsersMock.mockResolvedValue([
      user({ uid: ME, username: 'root', display_name: 'Root', role: 'admin' }),
      user({ uid: 'u9', username: 'ops', role: 'maintainer' }),
    ])
    renderPage(auth({ isAdmin: true }))

    expect(await screen.findByText('ops')).toBeInTheDocument()
    const [own, maintainer] = screen.getAllByRole('listitem')

    // Own account: disabling is refused, with the reason spelled out on the card.
    expect(within(own).getByRole('button', { name: 'Disable' })).toBeDisabled()
    expect(within(own).getByText('You cannot disable your own account.')).toBeInTheDocument()
    // A maintainer's account is untouchable for a plain admin, same as on the table.
    expect(within(maintainer).getByRole('button', { name: 'Edit' })).toBeDisabled()
    expect(within(maintainer).getByRole('button', { name: 'Change password' })).toBeDisabled()
    expect(within(maintainer).getByRole('button', { name: 'Disable' })).toBeDisabled()
  })

  it('opens the confirmation dialog from a phone card’s action row', async () => {
    mockViewport(true)
    const ada = user({ uid: 'u1', username: 'ada' })
    fetchUsersMock.mockResolvedValue([ada])
    setUserDisabledMock.mockResolvedValue({ ...ada, disabled: true })
    const actor = userEvent.setup()
    renderPage()

    expect(await screen.findByText('ada')).toBeInTheDocument()
    const card = screen.getByRole('listitem')
    await actor.click(within(card).getByRole('button', { name: 'Disable' }))

    const dialog = await screen.findByRole('dialog')
    await actor.click(within(dialog).getByRole('button', { name: 'Disable' }))
    await waitFor(() => {
      expect(setUserDisabledMock).toHaveBeenCalledWith(ada, true)
    })
    expect(await screen.findByText('Disabled')).toBeInTheDocument()
  })

  it('shows a retry button when the fetch fails, and reloads on click', async () => {
    fetchUsersMock.mockRejectedValueOnce(new ApiError(500, 'boom'))
    fetchUsersMock.mockResolvedValueOnce([user()])
    const actor = userEvent.setup()
    renderPage()

    expect(await screen.findByText('Failed to load the users.')).toBeInTheDocument()

    await actor.click(screen.getByRole('button', { name: 'Try again' }))
    expect(await screen.findByText('ada')).toBeInTheDocument()
  })

  it('renders an empty state rather than crashing on an empty roster', async () => {
    fetchUsersMock.mockResolvedValue([])
    renderPage()

    expect(await screen.findByText('No users')).toBeInTheDocument()
  })

  it('shows an API validation error inline next to the offending field', async () => {
    createUserMock.mockRejectedValue(new ApiError(409, 'username already taken'))
    const actor = userEvent.setup()
    renderPage()

    await actor.click(screen.getByRole('button', { name: 'New user' }))
    const dialog = await screen.findByRole('dialog')

    await actor.type(within(dialog).getByLabelText('Username'), 'ada')
    await actor.type(within(dialog).getByLabelText('Password'), 'correct-horse')
    await actor.click(within(dialog).getByRole('button', { name: 'Create' }))

    await waitFor(() => {
      expect(createUserMock).toHaveBeenCalled()
    })

    // The message sits on the username input, not in a form-level alert.
    const username = within(dialog).getByLabelText('Username')
    expect(username).toHaveClass('is-invalid')
    expect(within(dialog).getByText('That username is already taken.')).toBeInTheDocument()
    expect(within(dialog).queryByRole('alert')).not.toBeInTheDocument()
  })

  it('disables the disable control on the signed-in admin’s own row', async () => {
    fetchUsersMock.mockResolvedValue([
      user({ uid: ME, username: 'root', display_name: 'Root', role: 'admin' }),
      user({ uid: 'u1', username: 'ada' }),
    ])
    renderPage()

    // Wait for the real table: the loading skeleton is made of rows too.
    expect(await screen.findByText('root')).toBeInTheDocument()
    const rows = screen.getAllByRole('row')
    // rows[0] is the header; the roster is ordered as stubbed.
    const own = within(rows[1]).getByRole('button', { name: 'Disable' })
    const other = within(rows[2]).getByRole('button', { name: 'Disable' })

    expect(own).toBeDisabled()
    expect(within(rows[1]).getByText('You cannot disable your own account.')).toBeInTheDocument()
    expect(other).toBeEnabled()
  })

  it('disables another user only after the confirmation step', async () => {
    const ada = user({ uid: 'u1', username: 'ada' })
    fetchUsersMock.mockResolvedValue([ada])
    setUserDisabledMock.mockResolvedValue({ ...ada, disabled: true })
    const actor = userEvent.setup()
    renderPage()

    expect(await screen.findByText('ada')).toBeInTheDocument()
    await actor.click(screen.getByRole('button', { name: 'Disable' }))

    // The click alone changes nothing: the dialog asks first.
    expect(setUserDisabledMock).not.toHaveBeenCalled()
    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByText(/signed out of every device/)).toBeInTheDocument()

    await actor.click(within(dialog).getByRole('button', { name: 'Disable' }))
    await waitFor(() => {
      expect(setUserDisabledMock).toHaveBeenCalledWith(ada, true)
    })
    expect(await screen.findByText('Disabled')).toBeInTheDocument()
  })

  it('explains why the last maintainer cannot be disabled', async () => {
    const solo = user({ uid: 'u1', username: 'solo', role: 'maintainer' })
    fetchUsersMock.mockResolvedValue([solo])
    setUserDisabledMock.mockRejectedValue(LAST_MAINTAINER_ERROR)
    const actor = userEvent.setup()
    renderPage(auth({ isMaintainer: true }))

    expect(await screen.findByText('solo')).toBeInTheDocument()
    await actor.click(screen.getByRole('button', { name: 'Disable' }))
    const dialog = await screen.findByRole('dialog')
    await actor.click(within(dialog).getByRole('button', { name: 'Disable' }))

    // The refusal names the rule and what to do about it, not "action failed".
    expect(await screen.findByRole('alert')).toHaveTextContent(LAST_MAINTAINER_TEXT)
    expect(screen.queryByText('The action could not be completed.')).not.toBeInTheDocument()
    // The row survives: nothing was disabled.
    expect(screen.getByText('Enabled')).toBeInTheDocument()
  })

  it('shows the last-maintainer refusal as a form-level alert, not a username error', async () => {
    const solo = user({ uid: 'u1', username: 'solo', role: 'maintainer' })
    fetchUsersMock.mockResolvedValue([solo])
    updateUserMock.mockRejectedValue(LAST_MAINTAINER_ERROR)
    const actor = userEvent.setup()
    renderPage(auth({ isMaintainer: true }))

    expect(await screen.findByText('solo')).toBeInTheDocument()
    await actor.click(screen.getByRole('button', { name: 'Edit' }))
    const dialog = await screen.findByRole('dialog')
    await actor.selectOptions(within(dialog).getByLabelText('Role'), 'admin')
    await actor.click(within(dialog).getByRole('button', { name: 'Save' }))

    await waitFor(() => {
      expect(updateUserMock).toHaveBeenCalled()
    })

    // The other 409 (a duplicate username) flags the username input; this one
    // belongs to no field, so it must not be mistaken for it.
    expect(within(dialog).getByRole('alert')).toHaveTextContent(LAST_MAINTAINER_TEXT)
    expect(within(dialog).getByLabelText('Username')).not.toHaveClass('is-invalid')
  })

  it('renders the username read-only when editing an existing user', async () => {
    fetchUsersMock.mockResolvedValue([user({ uid: 'u1', username: 'ada' })])
    const actor = userEvent.setup()
    renderPage()

    expect(await screen.findByText('ada')).toBeInTheDocument()
    await actor.click(screen.getByRole('button', { name: 'Edit' }))

    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByLabelText('Username')).toHaveAttribute('readonly')
    // Editing offers no password field; that is a separate dialog.
    expect(within(dialog).queryByLabelText('Password')).not.toBeInTheDocument()
  })

  it('gives the create/edit dialog the whole phone screen with its actions pinned', async () => {
    const actor = userEvent.setup()
    renderPage()

    await actor.click(screen.getByRole('button', { name: 'New user' }))
    const dialog = await screen.findByRole('dialog')

    // react-bootstrap maps `fullscreen="sm-down"` + `scrollable` onto these two
    // dialog classes: below `sm` the form gets the whole screen instead of a
    // cramped centred card, and the body is the only part that scrolls — so the
    // footer stays pinned above the on-screen keyboard rather than under it.
    expect(dialog.querySelector('.modal-dialog')).toHaveClass(
      'modal-fullscreen-sm-down',
      'modal-dialog-scrollable',
    )

    // The <form> wraps header, body and footer, so it has to hand Bootstrap's
    // height cap through to the body instead of sizing to its own content.
    expect(dialog.querySelector('form')).toHaveClass('d-flex', 'flex-column', 'overflow-hidden')

    // Both footer actions render — neither is dropped by the reflow.
    expect(within(dialog).getByRole('button', { name: 'Cancel' })).toBeInTheDocument()
    expect(within(dialog).getByRole('button', { name: 'Create' })).toBeInTheDocument()
  })

  it('gives the password dialog the same full-screen sheet and pinned actions', async () => {
    fetchUsersMock.mockResolvedValue([user()])
    const actor = userEvent.setup()
    renderPage()

    expect(await screen.findByText('ada')).toBeInTheDocument()
    await actor.click(screen.getByRole('button', { name: 'Change password' }))
    const dialog = await screen.findByRole('dialog')

    expect(dialog.querySelector('.modal-dialog')).toHaveClass(
      'modal-fullscreen-sm-down',
      'modal-dialog-scrollable',
    )
    expect(dialog.querySelector('form')).toHaveClass('d-flex', 'flex-column', 'overflow-hidden')
    expect(within(dialog).getByRole('button', { name: 'Cancel' })).toBeInTheDocument()
    expect(within(dialog).getByRole('button', { name: 'Change password' })).toBeInTheDocument()
  })

  it('keeps the enable/disable question a centred card, only scrollable', async () => {
    fetchUsersMock.mockResolvedValue([user()])
    const actor = userEvent.setup()
    renderPage()

    expect(await screen.findByText('ada')).toBeInTheDocument()
    await actor.click(screen.getByRole('button', { name: 'Disable' }))
    const dialog = await screen.findByRole('dialog')

    // A question with no inputs summons no keyboard, so it keeps its centred
    // card on every screen; `scrollable` still pins the two buttons.
    const dialogEl = dialog.querySelector('.modal-dialog')
    expect(dialogEl).toHaveClass('modal-dialog-scrollable', 'modal-dialog-centered')
    expect(dialogEl).not.toHaveClass('modal-fullscreen-sm-down')
    expect(within(dialog).getByRole('button', { name: 'Cancel' })).toBeInTheDocument()
    expect(within(dialog).getByRole('button', { name: 'Disable' })).toBeInTheDocument()
  })

  it('does not offer deleting a user', async () => {
    fetchUsersMock.mockResolvedValue([user()])
    renderPage()

    expect(await screen.findByText('ada')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /delete/i })).not.toBeInTheDocument()
  })
})
