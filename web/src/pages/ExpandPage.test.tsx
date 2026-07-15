import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { type ReactNode } from 'react'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
import i18n from '../i18n'
import { type ExpandCandidate, type ExpandResult } from '../services/expand'
import { type AlbumSummary, type LabelCount } from '../services/organize'
import { type Photo } from '../services/photos'

import { ExpandPage } from './ExpandPage'

// Minimal stand-in for react-virtuoso's grid (jsdom has no layout).
interface MockGridProps {
  data: Photo[]
  itemContent: (index: number, item: Photo) => ReactNode
}
vi.mock('react-virtuoso', () => ({
  VirtuosoGrid: ({ data, itemContent }: MockGridProps) => (
    <div data-testid="grid">
      {data.map((item, index) => (
        <div key={item.uid}>{itemContent(index, item)}</div>
      ))}
    </div>
  ),
}))

vi.mock('../services/expand', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/expand')>()
  return { ...actual, searchSimilar: vi.fn() }
})

vi.mock('../services/feedback', () => ({
  rejectLabel: vi.fn(),
  unrejectLabel: vi.fn(),
  rejectFace: vi.fn(),
  unrejectFace: vi.fn(),
}))

vi.mock('../services/organize', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/organize')>()
  return { ...actual, fetchAlbums: vi.fn(), fetchLabels: vi.fn() }
})

vi.mock('../services/bulk', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/bulk')>()
  return { ...actual, bulkUpdatePhotos: vi.fn() }
})

const { searchSimilar } = await import('../services/expand')
const { rejectLabel } = await import('../services/feedback')
const { fetchAlbums, fetchLabels } = await import('../services/organize')
const { bulkUpdatePhotos } = await import('../services/bulk')
const searchMock = vi.mocked(searchSimilar)
const rejectMock = vi.mocked(rejectLabel)
const albumsMock = vi.mocked(fetchAlbums)
const labelsMock = vi.mocked(fetchLabels)
const bulkMock = vi.mocked(bulkUpdatePhotos)

/** Builds a photo whose tile is findable by its file name. */
function photo(uid: string): Photo {
  return {
    uid,
    file_name: `${uid}.jpg`,
    title: '',
    thumb_url: `/thumb/${uid}`,
  } as unknown as Photo
}

/** Builds one expansion candidate for the photo `uid`. */
function candidate(uid: string, similarity: number, matchCount: number): ExpandCandidate {
  return {
    photo: photo(uid),
    distance: 1 - similarity,
    similarity,
    match_count: matchCount,
  }
}

/** Wraps candidates in a full result for the given collection. */
function makeResult(
  kind: ExpandResult['kind'],
  collectionUid: string,
  candidates: ExpandCandidate[],
  overrides: Partial<ExpandResult> = {},
): ExpandResult {
  return {
    kind,
    collection_uid: collectionUid,
    source_photo_count: 5,
    source_photos_sampled: 5,
    source_photos_with_embedding: 5,
    source_capped: false,
    source_cap: 200,
    min_match_count: 2,
    threshold: 0.3,
    limit: 50,
    result_count: candidates.length,
    candidates,
    ...overrides,
  }
}

/** An album the picker and the bulk dialog can resolve `al1` against. */
const ALBUMS = [
  { uid: 'al1', title: 'Trips', photo_count: 9 },
  { uid: 'al0', title: 'Empty', photo_count: 0 },
] as unknown as AlbumSummary[]

const LABELS = [{ uid: 'lb1', name: 'Cats', photo_count: 4 }] as unknown as LabelCount[]

/** Editor auth context: selection and bulk editing are enabled. */
const EDITOR = {
  status: 'authenticated',
  user: { uid: 'u1', username: 'u', display_name: 'U', role: 'editor' },
  role: 'editor',
  canWrite: true,
  isAdmin: false,
} as unknown as AuthContextValue

function renderPage(entry = '/expand') {
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={EDITOR}>
        <MemoryRouter initialEntries={[entry]}>
          <ExpandPage />
        </MemoryRouter>
      </AuthContext.Provider>
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  searchMock.mockReset()
  rejectMock.mockReset().mockResolvedValue(undefined)
  albumsMock.mockReset().mockResolvedValue(ALBUMS)
  labelsMock.mockReset().mockResolvedValue(LABELS)
  bulkMock.mockReset()
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('ExpandPage', () => {
  it('runs the URL-restored search with the percent converted to a distance', async () => {
    searchMock.mockResolvedValue(makeResult('album', 'al1', [candidate('p1', 0.92, 3)]))
    renderPage('/expand?type=album&source=al1&threshold=70&limit=50')

    await screen.findByRole('link', { name: 'p1.jpg' })
    // 70 % similarity is a maximum cosine distance of 0.3 on the wire.
    expect(searchMock).toHaveBeenCalledWith(
      'album',
      'al1',
      { threshold: 0.3, limit: 50 },
      expect.any(AbortSignal),
    )
  })

  it('renders tiles with similarity and match-count badges and the vote-rule summary', async () => {
    searchMock.mockResolvedValue(
      makeResult('album', 'al1', [candidate('p1', 0.92, 3), candidate('p2', 0.81, 1)]),
    )
    renderPage('/expand?type=album&source=al1')

    await screen.findByRole('link', { name: 'p1.jpg' })
    expect(screen.getByText('92 %')).toBeInTheDocument()
    expect(screen.getByText('81 %')).toBeInTheDocument()
    // Only a candidate more than one source photo voted for gets the badge.
    expect(screen.getByText('3×')).toBeInTheDocument()
    expect(screen.queryByText('1×')).not.toBeInTheDocument()
    // The vote rule and the ordering are spelled out, so the ranking is legible.
    expect(screen.getByText('A photo must match at least 2 source photos.')).toBeInTheDocument()
    expect(screen.getByText('Sorted by match count, then by similarity.')).toBeInTheDocument()
    // Albums have no rejection model: no ✗ is offered.
    expect(screen.queryByRole('button', { name: 'Never offer for this label' })).toBeNull()
  })

  it('bulk-adds the selection to the expanded album and drops the added tiles', async () => {
    const user = userEvent.setup()
    searchMock.mockResolvedValue(
      makeResult('album', 'al1', [
        candidate('p1', 0.92, 3),
        candidate('p2', 0.81, 2),
        candidate('p3', 0.75, 2),
      ]),
    )
    bulkMock.mockResolvedValue({
      results: [
        { photo_uid: 'p1', status: 'updated' },
        { photo_uid: 'p2', status: 'updated' },
      ],
      counts: { total: 2, updated: 2, skipped: 0, errored: 0 },
    })
    renderPage('/expand?type=album&source=al1')

    await screen.findByRole('link', { name: 'p1.jpg' })
    await user.click(screen.getByRole('button', { name: 'Select' }))
    await user.click(screen.getByRole('button', { name: 'p1.jpg' }))
    await user.click(screen.getByRole('button', { name: 'p2.jpg' }))
    await user.click(screen.getByRole('button', { name: 'Bulk edit' }))

    // The dialog opens pre-filled with the album being expanded, so plain Apply
    // is already "add these photos to it".
    await user.click(await screen.findByRole('button', { name: 'Apply' }))
    await waitFor(() => {
      expect(bulkMock).toHaveBeenCalledWith(['p1', 'p2'], { add_to_albums: ['al1'] })
    })

    await user.click(await screen.findByRole('button', { name: 'Done' }))
    // The added photos are members now: they leave the grid without a refetch.
    await waitFor(() => {
      expect(screen.queryByRole('button', { name: 'p1.jpg' })).toBeNull()
    })
    expect(screen.queryByRole('button', { name: 'p2.jpg' })).toBeNull()
    expect(screen.getByRole('button', { name: 'p3.jpg' })).toBeInTheDocument()
    expect(searchMock).toHaveBeenCalledTimes(1)
  })

  it('persists a label rejection from the tile ✗ and removes the tile', async () => {
    const user = userEvent.setup()
    searchMock.mockResolvedValue(
      makeResult('label', 'lb1', [candidate('p1', 0.9, 2), candidate('p2', 0.8, 2)]),
    )
    renderPage('/expand?type=label&source=lb1')

    await screen.findByRole('link', { name: 'p1.jpg' })
    const rejects = screen.getAllByRole('button', { name: 'Never offer for this label' })
    expect(rejects).toHaveLength(2)

    await user.click(rejects[0])
    expect(rejectMock).toHaveBeenCalledWith({ photo_uid: 'p1', label_uid: 'lb1' })
    await waitFor(() => {
      expect(screen.queryByRole('link', { name: 'p1.jpg' })).toBeNull()
    })
    expect(screen.getByRole('link', { name: 'p2.jpg' })).toBeInTheDocument()
  })

  it('restores the tile and says so when persisting a rejection fails', async () => {
    const user = userEvent.setup()
    rejectMock.mockRejectedValue(new Error('boom'))
    searchMock.mockResolvedValue(makeResult('label', 'lb1', [candidate('p1', 0.9, 2)]))
    renderPage('/expand?type=label&source=lb1')

    await screen.findByRole('link', { name: 'p1.jpg' })
    await user.click(screen.getByRole('button', { name: 'Never offer for this label' }))
    await screen.findByText('The rejection could not be saved; the photo stays in the results.')
    expect(screen.getByRole('link', { name: 'p1.jpg' })).toBeInTheDocument()
  })

  it('explains a collection whose photos have no embeddings, apart from "no results"', async () => {
    searchMock.mockResolvedValue(
      makeResult('album', 'al1', [], {
        source_photo_count: 8,
        source_photos_with_embedding: 0,
        reason: 'no_source_embeddings',
      }),
    )
    renderPage('/expand?type=album&source=al1')

    await screen.findByText("The collection's photos have no embeddings yet")
    expect(screen.getByText(/while the compute box is online/)).toBeInTheDocument()
    expect(screen.queryByText('No similar photos')).toBeNull()
  })

  it('suggests lowering the threshold when the search finds nothing', async () => {
    searchMock.mockResolvedValue(makeResult('album', 'al1', []))
    renderPage('/expand?type=album&source=al1')

    await screen.findByText('No similar photos')
    expect(screen.getByText(/Try lowering it/)).toBeInTheDocument()
  })
})
