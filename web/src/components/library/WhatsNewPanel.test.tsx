import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../../auth/AuthContext'
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

/**
 * A signed-in session, optionally linked to a person of the library — which is
 * what decides whether the "new photos of you" line can appear at all.
 */
function auth(subjectUid: string | null = null): AuthContextValue {
  return {
    status: 'authenticated',
    user: { uid: 'u1', username: 'u', display_name: 'User One', subject_uid: subjectUid },
    role: 'viewer',
  } as unknown as AuthContextValue
}

/** Renders the panel within the i18n provider and a router (it renders links). */
function renderPanel(subjectUid: string | null = null) {
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={auth(subjectUid)}>
        <MemoryRouter>
          <WhatsNewPanel />
        </MemoryRouter>
      </AuthContext.Provider>
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

  it('condenses the visit to one line, with the detail one click away', async () => {
    // The digest sits above everything on the library page, so its height comes
    // straight out of the photographs. At rest it states the counts and nothing
    // else; the names, the links and the exact moment of the last visit are a
    // click below.
    fetchMock.mockResolvedValue({ ...FULL_DIGEST, mine_photos: 3 })
    renderPanel('sub123')

    expect(
      await screen.findByText(
        /5 new photos · 3 new photos of you · 1 new album · 1 newly named person · 2 new comments/,
      ),
    ).toBeInTheDocument()

    const toggle = screen.getByRole('button', { name: 'Details' })
    expect(toggle).toHaveAttribute('aria-expanded', 'false')
    const detail = document.getElementById(toggle.getAttribute('aria-controls') ?? '')
    expect(detail).not.toBeNull()
    // jsdom loads no Bootstrap, so the shut region is still in the document; what
    // the test can hold is that it is shut and that the toggle says so.
    expect(detail).not.toHaveClass('show')
    expect(detail).toContainElement(screen.getByRole('link', { name: 'Summer 2026' }))
    expect(detail).toContainElement(screen.getByText(/Since your last visit/))

    await userEvent.setup().click(toggle)
    await waitFor(() => {
      expect(detail).toHaveClass('show')
    })
    expect(screen.getByRole('button', { name: 'Hide details' })).toHaveAttribute(
      'aria-expanded',
      'true',
    )
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

describe('WhatsNewPanel — new photos of you', () => {
  it('names the reader’s own new photos and links to their scoped grid', async () => {
    fetchMock.mockResolvedValue({ ...FULL_DIGEST, mine_photos: 2 })
    renderPanel('sub123')

    const mine = await screen.findByRole('link', { name: '2 new photos of you' })
    // The person facet, in recently-added order like the total above it.
    expect(mine).toHaveAttribute('href', '/?person=sub123&sort=added')
  })

  it('says nothing when none of the new photos is of the reader', async () => {
    fetchMock.mockResolvedValue({ ...FULL_DIGEST, mine_photos: 0 })
    renderPanel('sub123')

    expect(await screen.findByText("What's new")).toBeInTheDocument()
    expect(screen.queryByText(/new photos of you/)).not.toBeInTheDocument()
  })

  it('says nothing for an account that has not named a person', async () => {
    // The backend cannot count "photos of you" without the link, so it reports
    // none; the panel must not invent a line — or a link — out of that.
    fetchMock.mockResolvedValue({ ...FULL_DIGEST, mine_photos: 0 })
    renderPanel(null)

    expect(await screen.findByText("What's new")).toBeInTheDocument()
    expect(screen.queryByText(/new photos of you/)).not.toBeInTheDocument()
  })
})
