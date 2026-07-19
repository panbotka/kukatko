import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../i18n'

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

/** The localStorage key the banner persists its dismissal under. */
const DISMISS_KEY = 'kukatko.announcement.dismissedAt'

/** Renders the banner within the i18n provider (it needs no router or auth). */
function renderBanner() {
  return render(
    <I18nextProvider i18n={i18n}>
      <AnnouncementBanner />
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  window.localStorage.clear()
  fetchMock.mockReset()
})

afterEach(() => {
  vi.restoreAllMocks()
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
})
