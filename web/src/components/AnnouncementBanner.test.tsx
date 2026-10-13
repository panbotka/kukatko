import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
import i18n from '../i18n'
import { ProjectContext, type ProjectContextValue } from '../project/ProjectContext'

import { AnnouncementBanner } from './AnnouncementBanner'

// Mock the announcement service so the polled message is controlled; the real
// useAnnouncement hook still runs (fetch-on-mount) so the banner's own logic —
// variant selection and updated_at-keyed dismissal — is exercised end to end.
vi.mock('../services/announcement', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/announcement')>()
  return { ...actual, fetchAnnouncement: vi.fn() }
})

const { fetchAnnouncement } = await import('../services/announcement')
const fetchMock = vi.mocked(fetchAnnouncement)

/** The localStorage key the banner persists u1's dismissal in project `v` under. */
const DISMISS_KEY = 'kukatko.announcement.dismissedAt.u1.v'

/** The project context of the project `slug` the app works in. */
function projectContext(slug: string | null): ProjectContextValue {
  return {
    project: slug === null ? null : { slug },
  } as unknown as ProjectContextValue
}

/**
 * The banner for the signed-in account `uid` in the project `slug` (it needs
 * no router).
 */
function bannerTree(uid: string, slug: string | null) {
  const auth = { user: { uid } } as unknown as AuthContextValue
  return (
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={auth}>
        <ProjectContext.Provider value={projectContext(slug)}>
          <AnnouncementBanner />
        </ProjectContext.Provider>
      </AuthContext.Provider>
    </I18nextProvider>
  )
}

/** Renders the banner for the signed-in account `uid` in the project `slug`. */
function renderBanner(uid = 'u1', slug: string | null = 'v') {
  return render(bannerTree(uid, slug))
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  window.localStorage.clear()
  fetchMock.mockReset()
})

describe('AnnouncementBanner', () => {
  it('renders the fetched message for a normal user', async () => {
    fetchMock.mockResolvedValue({ message: 'Downtime tonight', level: 'warning', updated_at: 't1' })
    renderBanner()
    expect(await screen.findByText('Downtime tonight')).toBeInTheDocument()
    expect(screen.getByRole('alert')).toHaveClass('alert-warning')
  })

  it('renders nothing when no announcement is published', async () => {
    fetchMock.mockResolvedValue({ message: '' })
    renderBanner()
    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalled()
    })
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('dismiss persists keyed on updated_at, and a new updated_at re-shows', async () => {
    const user = userEvent.setup()

    // First message shows and can be dismissed; the dismissal records its timestamp.
    fetchMock.mockResolvedValue({ message: 'Old', level: 'info', updated_at: 't1' })
    const first = renderBanner()
    expect(await screen.findByText('Old')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Dismiss announcement' }))
    expect(screen.queryByText('Old')).not.toBeInTheDocument()
    expect(window.localStorage.getItem(DISMISS_KEY)).toBe('t1')
    first.unmount()

    // Remounting on the same message keeps it hidden (dismissal persisted).
    const second = renderBanner()
    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledTimes(2)
    })
    expect(screen.queryByText('Old')).not.toBeInTheDocument()
    second.unmount()

    // A newly published message (fresh updated_at) reappears despite the dismissal.
    fetchMock.mockResolvedValue({ message: 'New', level: 'warning', updated_at: 't2' })
    renderBanner()
    expect(await screen.findByText('New')).toBeInTheDocument()
  })

  it('keeps showing a message one account dismissed to the next account', async () => {
    const user = userEvent.setup()
    fetchMock.mockResolvedValue({ message: 'Storage is full', level: 'warning', updated_at: 't1' })

    const first = renderBanner('u1')
    expect(await screen.findByText('Storage is full')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Dismiss announcement' }))
    first.unmount()

    renderBanner('u2')
    expect(await screen.findByText('Storage is full')).toBeInTheDocument()
  })

  it('ignores the global dismissal an older build wrote for the whole browser', async () => {
    window.localStorage.setItem('kukatko.announcement.dismissedAt', 't1')
    fetchMock.mockResolvedValue({ message: 'Downtime tonight', level: 'warning', updated_at: 't1' })

    renderBanner()

    expect(await screen.findByText('Downtime tonight')).toBeInTheDocument()
  })

  it('shows the banner when storage cannot be read, and still closes it', async () => {
    const user = userEvent.setup()
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('denied')
    })
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('denied')
    })
    fetchMock.mockResolvedValue({ message: 'Downtime tonight', level: 'warning', updated_at: 't1' })

    renderBanner()
    expect(await screen.findByText('Downtime tonight')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Dismiss announcement' }))

    expect(screen.queryByText('Downtime tonight')).not.toBeInTheDocument()
  })

  it('lets a message without updated_at be dismissed for good, until another is published', async () => {
    const user = userEvent.setup()
    fetchMock.mockResolvedValue({ message: 'No timestamp', level: 'info' })

    const first = renderBanner()
    expect(await screen.findByText('No timestamp')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Dismiss announcement' }))
    expect(screen.queryByText('No timestamp')).not.toBeInTheDocument()
    first.unmount()

    // It does not come back on the next mount…
    const second = renderBanner()
    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledTimes(2)
    })
    expect(screen.queryByText('No timestamp')).not.toBeInTheDocument()
    second.unmount()

    // …but a different message, also without a timestamp, does show.
    fetchMock.mockResolvedValue({ message: 'Another one', level: 'info' })
    renderBanner()
    expect(await screen.findByText('Another one')).toBeInTheDocument()
  })

  it("shows each project's own banner and drops the previous one on a switch", async () => {
    // The real client reads the project from the API layer's current slug; the
    // fake answers per the slug the banner is keyed on, which is what changes.
    let current = 'v'
    fetchMock.mockImplementation(() =>
      Promise.resolve(
        current === 'v'
          ? { message: 'Jen pro V', level: 'info' as const, updated_at: 't1' }
          : { message: '' },
      ),
    )
    const view = renderBanner('u1', 'v')
    expect(await screen.findByText('Jen pro V')).toBeInTheDocument()

    current = 'b'
    view.rerender(bannerTree('u1', 'b'))
    // Gone at once, before B's answer arrives — never shown in another project.
    expect(screen.queryByText('Jen pro V')).not.toBeInTheDocument()
    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledTimes(2)
    })
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it("keeps one project's dismissal out of another project's banner", async () => {
    const user = userEvent.setup()
    fetchMock.mockResolvedValue({ message: 'Odstávka', level: 'warning', updated_at: 't1' })

    const first = renderBanner('u1', 'v')
    expect(await screen.findByText('Odstávka')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Dismiss announcement' }))
    first.unmount()

    renderBanner('u1', 'b')
    expect(await screen.findByText('Odstávka')).toBeInTheDocument()
    expect(window.localStorage.getItem('kukatko.announcement.dismissedAt.u1.b')).toBeNull()
  })

  it('asks nothing without a project', async () => {
    renderBanner('u1', null)
    await Promise.resolve()
    expect(fetchMock).not.toHaveBeenCalled()
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })
})
