import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
import i18n from '../i18n'
import { ApiError } from '../services/auth'
import type { ProjectSettings } from '../services/settings'

import { SettingsPage } from './SettingsPage'

vi.mock('../services/settings', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/settings')>()
  return {
    ...actual,
    fetchProjectSettings: vi.fn(),
    fetchProjectUsage: vi.fn(),
    updateProjectSettings: vi.fn(),
  }
})

// The composer is the announcement banner's own component, tested on its own;
// here only whether the page offers it matters, so its reads never resolve.
vi.mock('../services/announcement', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/announcement')>()
  return { ...actual, fetchAnnouncement: vi.fn(() => new Promise(() => undefined)) }
})

const { fetchProjectSettings, fetchProjectUsage, updateProjectSettings } =
  await import('../services/settings')
const fetchMock = vi.mocked(fetchProjectSettings)
const usageMock = vi.mocked(fetchProjectUsage)
const updateMock = vi.mocked(updateProjectSettings)

/** A settings record, with the fields a test cares about overridden. */
function settings(overrides: Partial<ProjectSettings> = {}): ProjectSettings {
  return {
    registration_enabled: false,
    registration_secret: 'veselice',
    welcome_markdown: '',
    updated_at: '2026-08-20T09:00:00Z',
    updated_by_uid: 'u1',
    ...overrides,
  }
}

/** The signed-in account: a project admin, or the system administrator. */
function auth(isMaintainer: boolean): AuthContextValue {
  const role = isMaintainer ? 'maintainer' : 'admin'
  return {
    status: 'authenticated',
    user: { uid: 'u1', username: 'u', display_name: 'U', role },
    role,
    instanceRole: isMaintainer ? 'maintainer' : 'user',
    downloadToken: null,
    canCurate: true,
    canWrite: true,
    isAdmin: true,
    isMaintainer,
    login: vi.fn(),
    logout: vi.fn(),
    refresh: vi.fn(),
  } as unknown as AuthContextValue
}

function renderPage(isMaintainer = false) {
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={auth(isMaintainer)}>
        <MemoryRouter initialEntries={['/settings']}>
          <SettingsPage />
        </MemoryRouter>
      </AuthContext.Provider>
    </I18nextProvider>,
  )
}

/** The shared-secret text box, found by its label. */
function secretField(): HTMLInputElement {
  return screen.getByLabelText<HTMLInputElement>('Shared registration secret')
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  fetchMock.mockReset()
  updateMock.mockReset()
  fetchMock.mockResolvedValue(settings())
  usageMock.mockReset().mockResolvedValue({
    photos: 1234,
    originals_bytes: 5 * 1024 * 1024,
    max_file_bytes: null,
    max_total_bytes: 10 * 1024 * 1024 * 1024,
  })
})

describe('SettingsPage', () => {
  it('sits in the shared narrow reading column, loaded or not', async () => {
    fetchMock.mockRejectedValueOnce(new ApiError(500, 'boom'))
    renderPage()

    // The failed load keeps the same column, so retrying does not reflow the page.
    const failed = await screen.findByText('The settings could not be loaded.')
    expect(screen.getByTestId('narrow-page')).toContainElement(failed)

    fetchMock.mockResolvedValue(settings())
    await userEvent.click(screen.getByRole('button', { name: /try again/i }))

    const heading = await screen.findByRole('heading', { level: 1 })
    expect(screen.getByTestId('narrow-page')).toContainElement(heading)
  })

  it('loads the current settings into the form', async () => {
    fetchMock.mockResolvedValue(
      settings({
        registration_enabled: true,
        registration_secret: 'letni-tabor',
        welcome_markdown: '# Ahoj',
      }),
    )
    renderPage()

    expect(await screen.findByLabelText('Allow self-service registration')).toBeChecked()
    expect(secretField()).toHaveValue('letni-tabor')
    expect(screen.getByLabelText('Welcome text (Markdown)')).toHaveValue('# Ahoj')
    // Nothing is dirty yet, so there is nothing to save.
    expect(screen.getByRole('button', { name: 'Save settings' })).toBeDisabled()
  })

  it('reports a failed load and retries', async () => {
    fetchMock.mockRejectedValueOnce(new ApiError(500, 'boom'))
    renderPage()

    expect(await screen.findByText('The settings could not be loaded.')).toBeInTheDocument()

    fetchMock.mockResolvedValue(settings({ registration_secret: 'druhy-pokus' }))
    await userEvent.click(screen.getByRole('button', { name: /try again/i }))

    await waitFor(() => {
      expect(secretField()).toHaveValue('druhy-pokus')
    })
  })

  it('renders the welcome Markdown in the live preview', async () => {
    const user = userEvent.setup()
    renderPage()

    const editor = await screen.findByLabelText('Welcome text (Markdown)')
    await user.clear(editor)
    // Pasted rather than typed: userEvent reads `[` in typed text as a key
    // descriptor, which would swallow the link's label.
    await user.click(editor)
    await user.paste('# Vitej\n\n[archiv](https://example.com)')

    const preview = screen.getByRole('region', { name: 'Preview' })
    expect(within(preview).getByRole('heading', { level: 1, name: 'Vitej' })).toBeInTheDocument()
    // A link in the welcome points away from the app: new tab, and the usual
    // protections on the opened page.
    const link = within(preview).getByRole('link', { name: 'archiv' })
    expect(link).toHaveAttribute('href', 'https://example.com')
    expect(link).toHaveAttribute('target', '_blank')
    expect(link).toHaveAttribute('rel', 'noopener noreferrer')
  })

  it('refuses to enable registration while the secret is empty', async () => {
    const user = userEvent.setup()
    fetchMock.mockResolvedValue(settings({ registration_secret: '' }))
    renderPage()

    const toggle = await screen.findByLabelText('Allow self-service registration')
    await user.click(toggle)

    expect(toggle).not.toBeChecked()
    expect(
      screen.getByText('Registration cannot be turned on while the shared secret is empty.'),
    ).toBeInTheDocument()
    expect(updateMock).not.toHaveBeenCalled()

    // With a secret typed, the same click goes through.
    await user.type(secretField(), 'veselice')
    await user.click(toggle)
    expect(toggle).toBeChecked()
    expect(
      screen.queryByText('Registration cannot be turned on while the shared secret is empty.'),
    ).not.toBeInTheDocument()
  })

  it('shows the secret as readable text and hides it on request', async () => {
    const user = userEvent.setup()
    renderPage()

    // Readable by default: an administrator's job with the secret is to read it
    // back and pass it on.
    expect(await screen.findByLabelText('Shared registration secret')).toHaveAttribute(
      'type',
      'text',
    )

    await user.click(screen.getByRole('button', { name: 'Hide' }))
    expect(secretField()).toHaveAttribute('type', 'password')

    await user.click(screen.getByRole('button', { name: 'Show' }))
    expect(secretField()).toHaveAttribute('type', 'text')
    expect(secretField()).toHaveValue('veselice')
  })

  it('saves the three values together and reports success', async () => {
    const user = userEvent.setup()
    updateMock.mockResolvedValue(
      settings({
        registration_enabled: true,
        registration_secret: 'veselice',
        welcome_markdown: 'Ahoj',
        updated_at: '2026-08-24T12:00:00Z',
      }),
    )
    renderPage()

    const editor = await screen.findByLabelText('Welcome text (Markdown)')
    await user.type(editor, 'Ahoj')
    await user.click(screen.getByLabelText('Allow self-service registration'))
    await user.click(screen.getByRole('button', { name: 'Save settings' }))

    expect(await screen.findByText('The settings were saved.')).toBeInTheDocument()
    expect(updateMock).toHaveBeenCalledWith({
      registration_enabled: true,
      registration_secret: 'veselice',
      welcome_markdown: 'Ahoj',
    })
    // The form now matches what is stored, so there is nothing left to save.
    expect(screen.getByRole('button', { name: 'Save settings' })).toBeDisabled()
  })

  it('repeats the server’s own rejection when the save fails', async () => {
    const user = userEvent.setup()
    updateMock.mockRejectedValue(
      new ApiError(400, 'registration secret must not be empty when registration is enabled'),
    )
    renderPage()

    const editor = await screen.findByLabelText('Welcome text (Markdown)')
    await user.type(editor, 'Ahoj')
    await user.click(screen.getByRole('button', { name: 'Save settings' }))

    expect(
      await screen.findByText('registration secret must not be empty when registration is enabled'),
    ).toBeInTheDocument()
  })

  it('warns before leaving with unsaved changes', async () => {
    const user = userEvent.setup()
    render(
      <I18nextProvider i18n={i18n}>
        <AuthContext.Provider value={auth(false)}>
          <MemoryRouter initialEntries={['/settings']}>
            <SettingsPage />
            <a href="/users">Users</a>
          </MemoryRouter>
        </AuthContext.Provider>
      </I18nextProvider>,
    )

    await user.type(await screen.findByLabelText('Welcome text (Markdown)'), 'Ahoj')
    await user.click(screen.getByRole('link', { name: 'Users' }))

    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByText('Leave without saving?')).toBeInTheDocument()

    await user.click(within(dialog).getByRole('button', { name: 'Stay here' }))
    await waitFor(() => {
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    })
  })

  it('discards the draft and goes back to what is stored', async () => {
    const user = userEvent.setup()
    renderPage()

    const editor = await screen.findByLabelText('Welcome text (Markdown)')
    await user.type(editor, 'Ahoj')
    expect(screen.getByRole('button', { name: 'Discard changes' })).toBeEnabled()

    await user.click(screen.getByRole('button', { name: 'Discard changes' }))
    expect(editor).toHaveValue('')
    expect(screen.getByRole('button', { name: 'Save settings' })).toBeDisabled()
  })

  it('drafts a fresh random secret that waits for Save', async () => {
    const user = userEvent.setup()
    renderPage()

    await screen.findByDisplayValue('veselice')
    await user.click(screen.getByRole('button', { name: 'Generate new' }))

    expect(secretField().value).toMatch(/^[a-z2-9]{4}-[a-z2-9]{4}$/)
    expect(secretField()).toHaveAttribute('type', 'text')
    // Nothing is written until Save: the fresh word is a draft like any other.
    expect(updateMock).not.toHaveBeenCalled()
    expect(screen.getByRole('button', { name: 'Save settings' })).toBeEnabled()
  })

  it("links the project's registration page to pass on", async () => {
    renderPage()

    const link = await screen.findByTestId('settings-register-url')
    expect(link).toHaveAttribute('href', `${window.location.origin}/p/v/register`)
  })

  it('shows the usage beside the limits, read-only, "unlimited" where none is set', async () => {
    renderPage()

    const usage = await screen.findByTestId('settings-usage')
    await waitFor(() => {
      expect(usage).toHaveTextContent('1,234')
    })
    expect(usage).toHaveTextContent('5.0 MB')
    expect(usage).toHaveTextContent('10.0 GB')
    expect(usage).toHaveTextContent('unlimited')
    expect(usage).toHaveTextContent('Limits are set by the system administrator.')
    expect(within(usage).queryByRole('textbox')).not.toBeInTheDocument()
  })

  it('keeps the form working when the usage cannot be read', async () => {
    usageMock.mockRejectedValue(new ApiError(500, 'boom'))
    renderPage()

    expect(await screen.findByText('Usage could not be loaded.')).toBeInTheDocument()
    expect(screen.getByLabelText('Shared registration secret')).toBeEnabled()
  })

  it("offers the project's announcement only to the system administrator", async () => {
    const { unmount } = renderPage(false)
    await screen.findByTestId('settings-usage')
    expect(screen.queryByRole('heading', { name: 'Announcement' })).not.toBeInTheDocument()
    unmount()

    renderPage(true)
    expect(await screen.findByRole('heading', { name: 'Announcement' })).toBeInTheDocument()
  })
})
