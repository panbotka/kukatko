import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../i18n'
import { type DuplicateGroup, type DuplicatesResponse } from '../services/duplicates'

import { DuplicatesPage } from './DuplicatesPage'

// Mock the network layer only, keeping the real types.
vi.mock('../services/duplicates', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/duplicates')>()
  return { ...actual, fetchDuplicates: vi.fn() }
})
vi.mock('../services/bulk', () => ({ bulkUpdatePhotos: vi.fn() }))

const { fetchDuplicates } = await import('../services/duplicates')
const { bulkUpdatePhotos } = await import('../services/bulk')
const fetchMock = vi.mocked(fetchDuplicates)
const bulkMock = vi.mocked(bulkUpdatePhotos)

// group builds a two-member duplicate group with the first member as keeper.
function group(id: string, keeper: string, other: string): DuplicateGroup {
  return {
    id,
    reason: 'phash',
    keeper_uid: keeper,
    members: [member(keeper, 400, 400, true), member(other, 200, 200, false)],
  }
}

function member(uid: string, w: number, h: number, isKeeper: boolean) {
  return {
    uid,
    title: '',
    file_name: `${uid}.jpg`,
    file_width: w,
    file_height: h,
    file_size: 1000,
    media_type: 'image',
    is_keeper: isKeeper,
    phash_distance: isKeeper ? undefined : 3,
  }
}

function page(groups: DuplicateGroup[]): DuplicatesResponse {
  return { groups, total: groups.length, limit: 20, offset: 0, next_offset: null }
}

function renderPage() {
  return render(
    <I18nextProvider i18n={i18n}>
      <MemoryRouter initialEntries={['/duplicates']}>
        <DuplicatesPage />
      </MemoryRouter>
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  fetchMock.mockReset()
  bulkMock.mockReset()
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('DuplicatesPage', () => {
  it('renders the duplicate groups returned by the API', async () => {
    fetchMock.mockResolvedValue(page([group('g1', 'ph_keep', 'ph_dup')]))
    renderPage()

    expect(await screen.findByRole('img', { name: 'ph_keep.jpg' })).toBeInTheDocument()
    expect(screen.getByRole('img', { name: 'ph_dup.jpg' })).toBeInTheDocument()
    expect(screen.getByText('2 photos')).toBeInTheDocument()
  })

  it('archives the non-kept members via the bulk API when keeping a photo', async () => {
    const user = userEvent.setup()
    fetchMock.mockResolvedValue(page([group('g1', 'ph_keep', 'ph_dup')]))
    bulkMock.mockResolvedValue({
      results: [{ photo_uid: 'ph_dup', status: 'updated' }],
      counts: { total: 1, updated: 1, skipped: 0, errored: 0 },
    })
    renderPage()
    await screen.findByRole('img', { name: 'ph_keep.jpg' })

    await user.click(screen.getByRole('button', { name: /Keep & archive/ }))

    await waitFor(() => {
      expect(bulkMock).toHaveBeenCalledWith(['ph_dup'], { archive: true })
    })
    // The resolved group disappears from the view.
    await waitFor(() => {
      expect(screen.queryByRole('img', { name: 'ph_keep.jpg' })).not.toBeInTheDocument()
    })
  })

  it('archives the chosen keeper, not the suggested one, when the user changes it', async () => {
    const user = userEvent.setup()
    fetchMock.mockResolvedValue(page([group('g1', 'ph_keep', 'ph_dup')]))
    bulkMock.mockResolvedValue({
      results: [],
      counts: { total: 1, updated: 1, skipped: 0, errored: 0 },
    })
    renderPage()
    await screen.findByRole('img', { name: 'ph_keep.jpg' })

    // Pick the other photo as the keeper, then archive the rest.
    await user.click(screen.getByRole('radio', { name: 'Keep this', checked: false }))
    await user.click(screen.getByRole('button', { name: /Keep & archive/ }))

    await waitFor(() => {
      expect(bulkMock).toHaveBeenCalledWith(['ph_keep'], { archive: true })
    })
  })

  it('removes a dismissed group from the view without calling the bulk API', async () => {
    const user = userEvent.setup()
    fetchMock.mockResolvedValue(page([group('g1', 'ph_keep', 'ph_dup')]))
    renderPage()
    await screen.findByRole('img', { name: 'ph_keep.jpg' })

    await user.click(screen.getByRole('button', { name: 'Not a duplicate' }))

    await waitFor(() => {
      expect(screen.queryByRole('img', { name: 'ph_keep.jpg' })).not.toBeInTheDocument()
    })
    expect(bulkMock).not.toHaveBeenCalled()
  })

  it('shows the empty state when there are no duplicate groups', async () => {
    fetchMock.mockResolvedValue(page([]))
    renderPage()
    expect(await screen.findByText('No duplicates found')).toBeInTheDocument()
  })

  it('shows an unavailable notice when detection is disabled (503)', async () => {
    const { ApiError } = await import('../services/auth')
    fetchMock.mockRejectedValue(new ApiError(503, 'duplicate detection not available'))
    renderPage()
    expect(
      await screen.findByText('Duplicate detection is disabled in the server configuration.'),
    ).toBeInTheDocument()
  })
})
