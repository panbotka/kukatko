import { render, screen } from '@testing-library/react'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'
import { type GlobalSearchResult } from '../../services/search'

import { GlobalSearchSections } from './GlobalSearchSections'

vi.mock('../../services/search', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../services/search')>()
  return { ...actual, globalSearch: vi.fn() }
})

const { globalSearch } = await import('../../services/search')
const searchMock = vi.mocked(globalSearch)

/** A well-formed album id the direct-hit cases paste into the search box. */
const ALBUM_UID = 'al7lpul2io09bcg2rvp2rljsr6'

const RESULT: GlobalSearchResult = {
  query: 'beach',
  albums: [{ uid: 'al1', title: 'Beach trip', cover: 'ph9', photo_count: 12 }],
  labels: [{ uid: 'lb1', name: 'beachy', photo_count: 40 }],
  people: [{ uid: 'su1', name: 'Beatrice', cover: 'ph3' }],
  photos: [],
}

function renderSections(query: string) {
  return render(
    <I18nextProvider i18n={i18n}>
      <MemoryRouter>
        <GlobalSearchSections query={query} />
      </MemoryRouter>
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  searchMock.mockReset()
})

describe('GlobalSearchSections', () => {
  it('renders album, people and label sections linking to each entity', async () => {
    searchMock.mockResolvedValue(RESULT)
    renderSections('beach')

    const albumLink = await screen.findByRole('link', { name: /Beach trip/ })
    expect(albumLink).toHaveAttribute('href', '/albums/al1')
    expect(screen.getByRole('link', { name: 'Beatrice' })).toHaveAttribute('href', '/people/su1')
    expect(screen.getByRole('link', { name: /beachy/ })).toHaveAttribute('href', '/labels/lb1')
  })

  it('renders nothing while idle (empty query) — no request', () => {
    const { container } = renderSections('')
    expect(container).toBeEmptyDOMElement()
    expect(searchMock).not.toHaveBeenCalled()
  })

  it('renders nothing when only photos match (no entity chrome)', async () => {
    searchMock.mockResolvedValue({ query: 'x', albums: [], labels: [], people: [], photos: [] })
    const { container } = renderSections('x')

    // Give the debounced fetch time to resolve, then assert no sections rendered.
    await vi.waitFor(() => {
      expect(searchMock).toHaveBeenCalled()
    })
    expect(container.querySelector('section')).toBeNull()
  })

  it('renders a pasted UID as a "go to" banner instead of the fuzzy chips', async () => {
    searchMock.mockResolvedValue({
      query: ALBUM_UID,
      direct: {
        uid: ALBUM_UID,
        kind: 'album',
        found: true,
        target_kind: 'album',
        target_uid: ALBUM_UID,
        title: 'Beach trip',
        cover: 'ph9',
      },
      albums: [],
      labels: [],
      people: [],
      photos: [],
    })
    renderSections(ALBUM_UID)

    const link = await screen.findByRole('link', { name: /Beach trip/ })
    expect(link).toHaveAttribute('href', `/albums/${ALBUM_UID}`)
    expect(screen.getByRole('region', { name: 'Go to' })).toBeInTheDocument()
    expect(link).toHaveTextContent('album ID')
  })

  it('states plainly that a well-formed UID names nothing', async () => {
    searchMock.mockResolvedValue({
      query: ALBUM_UID,
      direct: { uid: ALBUM_UID, kind: 'album', found: false },
      albums: [],
      labels: [],
      people: [],
      photos: [],
    })
    renderSections(ALBUM_UID)

    expect(
      await screen.findByText(`Nothing matches this album ID: ${ALBUM_UID}`),
    ).toBeInTheDocument()
    expect(screen.queryByRole('link')).not.toBeInTheDocument()
  })
})
