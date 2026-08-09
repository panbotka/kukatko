import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'
import { type WhatsNew } from '../../services/whatsNew'

import { WhatsNewPanel } from './WhatsNewPanel'

// Mock the service so the digest is controlled; the real useWhatsNew hook still
// runs (fetch-on-mount), so the panel's own logic — the `since`-keyed dismissal
// and the link building — is exercised end to end.
vi.mock('../../services/whatsNew', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../services/whatsNew')>()
  return { ...actual, fetchWhatsNew: vi.fn() }
})

const { fetchWhatsNew } = await import('../../services/whatsNew')
const fetchMock = vi.mocked(fetchWhatsNew)

/** The localStorage key the panel persists its dismissal under. */
const DISMISS_KEY = 'kukatko.whatsNew.dismissedSince'

/** A digest with one of everything, for the render and link assertions. */
const FULL_DIGEST: WhatsNew = {
  has_news: true,
  since: '2026-08-08T20:30:00Z',
  photos: 5,
  comments: 2,
  albums: [{ uid: 'al-1', title: 'Summer 2026' }],
  album_count: 1,
  people: [{ uid: 'su-1', name: 'Anna' }],
  person_count: 1,
}

/** Renders the panel within the i18n provider and a router (it renders links). */
function renderPanel() {
  return render(
    <I18nextProvider i18n={i18n}>
      <MemoryRouter>
        <WhatsNewPanel />
      </MemoryRouter>
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  window.localStorage.clear()
  fetchMock.mockReset()
})

describe('WhatsNewPanel', () => {
  it('summarizes the visit and links every entity it names', async () => {
    fetchMock.mockResolvedValue(FULL_DIGEST)
    renderPanel()

    expect(await screen.findByText("What's new")).toBeInTheDocument()
    // The photo count links to the library in "recently added" order, so the
    // arrivals it counted are the first tiles there.
    expect(screen.getByRole('link', { name: '5 new photos' })).toHaveAttribute(
      'href',
      '/?sort=added',
    )
    expect(screen.getByRole('link', { name: 'Summer 2026' })).toHaveAttribute(
      'href',
      '/albums/al-1',
    )
    expect(screen.getByRole('link', { name: 'Anna' })).toHaveAttribute('href', '/people/su-1')
    // Comments have no per-library destination, so they are a count, not a link.
    expect(screen.getByText('2 new comments')).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: '2 new comments' })).not.toBeInTheDocument()
  })

  it('renders nothing when the visit produced no news', async () => {
    fetchMock.mockResolvedValue({ has_news: false })
    renderPanel()

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalled()
    })
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('renders nothing while the digest is still loading', () => {
    fetchMock.mockReturnValue(new Promise(() => undefined))
    renderPanel()

    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('omits the lines whose count is zero', async () => {
    fetchMock.mockResolvedValue({
      has_news: true,
      since: '2026-08-08T20:30:00Z',
      photos: 1,
      comments: 0,
      album_count: 0,
      person_count: 0,
    })
    renderPanel()

    expect(await screen.findByRole('link', { name: '1 new photo' })).toBeInTheDocument()
    expect(screen.queryByText('New albums:')).not.toBeInTheDocument()
    expect(screen.queryByText('Newly named:')).not.toBeInTheDocument()
    expect(screen.queryByText(/new comment/)).not.toBeInTheDocument()
  })

  it('names the first few people and counts the rest', async () => {
    fetchMock.mockResolvedValue({
      has_news: true,
      since: '2026-08-08T20:30:00Z',
      people: [
        { uid: 'su-1', name: 'Anna' },
        { uid: 'su-2', name: 'Bedřich' },
      ],
      person_count: 5,
    })
    renderPanel()

    expect(await screen.findByRole('link', { name: 'Anna' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Bedřich' })).toBeInTheDocument()
    expect(screen.getByText('and 3 more')).toBeInTheDocument()
  })

  it('dismiss persists keyed on since, and a new visit re-shows the panel', async () => {
    const user = userEvent.setup()

    fetchMock.mockResolvedValue(FULL_DIGEST)
    const first = renderPanel()
    expect(await screen.findByText("What's new")).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Dismiss the summary' }))
    expect(screen.queryByText("What's new")).not.toBeInTheDocument()
    expect(window.localStorage.getItem(DISMISS_KEY)).toBe('2026-08-08T20:30:00Z')
    first.unmount()

    // Navigating away and back within the same visit keeps it dismissed: the
    // reference point has not moved, so neither has the dismissal.
    const second = renderPanel()
    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledTimes(2)
    })
    expect(screen.queryByText("What's new")).not.toBeInTheDocument()
    second.unmount()

    // A new visit carries a fresh `since`, so the panel comes back.
    fetchMock.mockResolvedValue({ ...FULL_DIGEST, since: '2026-08-09T08:00:00Z', photos: 9 })
    renderPanel()
    expect(await screen.findByText("What's new")).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '9 new photos' })).toBeInTheDocument()
  })

  it('uses Czech plural forms for 1, 2 and 5 photos', async () => {
    await i18n.changeLanguage('cs')

    for (const [count, want] of [
      [1, '1 nová fotka'],
      [2, '2 nové fotky'],
      [5, '5 nových fotek'],
    ] as const) {
      fetchMock.mockResolvedValue({ has_news: true, since: `t-${count}`, photos: count })
      const view = renderPanel()
      expect(await screen.findByRole('link', { name: want })).toBeInTheDocument()
      view.unmount()
    }
  })
})
