import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../i18n'
import {
  type DuplicateGroup,
  type DuplicatesResponse,
  type MergeResult,
} from '../services/duplicates'

import { DuplicatesPage } from './DuplicatesPage'

// Mock the network layer only, keeping the real types.
vi.mock('../services/duplicates', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/duplicates')>()
  return { ...actual, fetchDuplicates: vi.fn(), mergeDuplicates: vi.fn() }
})

const { fetchDuplicates, mergeDuplicates } = await import('../services/duplicates')
const fetchMock = vi.mocked(fetchDuplicates)
const mergeMock = vi.mocked(mergeDuplicates)

// group builds a two-member duplicate group with the first member as keeper.
function group(id: string, keeper: string, other: string, confirmed = false): DuplicateGroup {
  return {
    id,
    reason: 'phash',
    keeper_uid: keeper,
    confirmed,
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

// page wraps groups in a listing response; nextOffset drives the Load more control.
function page(groups: DuplicateGroup[], nextOffset: number | null = null): DuplicatesResponse {
  return { groups, total: groups.length, limit: 20, offset: 0, next_offset: nextOffset }
}

// preview builds a merge preview/result with a mix of moves and one archived copy.
function preview(keeper: string, dryRun: boolean): MergeResult {
  return {
    keeper_uid: keeper,
    albums_added: 1,
    labels_added: 0,
    people_added: 2,
    metadata_filled: ['title'],
    archived: 1,
    dry_run: dryRun,
  }
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
  mergeMock.mockReset()
})

describe('DuplicatesPage', () => {
  it('renders the duplicate groups returned by the API', async () => {
    fetchMock.mockResolvedValue(page([group('g1', 'ph_keep', 'ph_dup')]))
    renderPage()

    expect(await screen.findByRole('img', { name: 'ph_keep.jpg' })).toBeInTheDocument()
    expect(screen.getByRole('img', { name: 'ph_dup.jpg' })).toBeInTheDocument()
    expect(screen.getByText('2 photos')).toBeInTheDocument()
  })

  it('previews the merge then, on confirm, merges and drops the group', async () => {
    const user = userEvent.setup()
    fetchMock.mockResolvedValue(page([group('g1', 'ph_keep', 'ph_dup')]))
    mergeMock
      .mockResolvedValueOnce(preview('ph_keep', true))
      .mockResolvedValueOnce(preview('ph_keep', false))
    renderPage()
    await screen.findByRole('img', { name: 'ph_keep.jpg' })

    await user.click(screen.getByRole('button', { name: 'Keep best & merge' }))

    // A dry-run preview is fetched and shown in a confirmation dialog.
    await waitFor(() => {
      expect(mergeMock).toHaveBeenCalledWith({
        keeper_uid: 'ph_keep',
        member_uids: ['ph_keep', 'ph_dup'],
        dry_run: true,
      })
    })
    expect(await screen.findByText('Merge duplicates')).toBeInTheDocument()
    expect(screen.getByText(/1 copy will be archived/)).toBeInTheDocument()

    // Confirming performs the real merge (no dry_run) and removes the group.
    await user.click(screen.getByRole('button', { name: 'Confirm merge' }))
    await waitFor(() => {
      expect(mergeMock).toHaveBeenLastCalledWith({
        keeper_uid: 'ph_keep',
        member_uids: ['ph_keep', 'ph_dup'],
      })
    })
    await waitFor(() => {
      expect(screen.queryByRole('img', { name: 'ph_keep.jpg' })).not.toBeInTheDocument()
    })
  })

  it('previews the chosen keeper, not the suggested one, when the user changes it', async () => {
    const user = userEvent.setup()
    fetchMock.mockResolvedValue(page([group('g1', 'ph_keep', 'ph_dup')]))
    mergeMock.mockResolvedValue(preview('ph_dup', true))
    renderPage()
    await screen.findByRole('img', { name: 'ph_keep.jpg' })

    // Pick the other photo as the keeper, then start the merge.
    await user.click(screen.getByRole('radio', { name: 'Keep this', checked: false }))
    await user.click(screen.getByRole('button', { name: 'Keep best & merge' }))

    await waitFor(() => {
      expect(mergeMock).toHaveBeenCalledWith({
        keeper_uid: 'ph_dup',
        member_uids: ['ph_keep', 'ph_dup'],
        dry_run: true,
      })
    })
  })

  it('cancels the merge without calling the commit and keeps the group', async () => {
    const user = userEvent.setup()
    fetchMock.mockResolvedValue(page([group('g1', 'ph_keep', 'ph_dup')]))
    mergeMock.mockResolvedValue(preview('ph_keep', true))
    renderPage()
    await screen.findByRole('img', { name: 'ph_keep.jpg' })

    await user.click(screen.getByRole('button', { name: 'Keep best & merge' }))
    await screen.findByText('Merge duplicates')
    await user.click(screen.getByRole('button', { name: 'Cancel' }))

    // Only the dry-run preview was called; the group stays.
    expect(mergeMock).toHaveBeenCalledTimes(1)
    expect(screen.getByRole('img', { name: 'ph_keep.jpg' })).toBeInTheDocument()
  })

  it('removes a dismissed group from the view without calling the merge API', async () => {
    const user = userEvent.setup()
    fetchMock.mockResolvedValue(page([group('g1', 'ph_keep', 'ph_dup')]))
    renderPage()
    await screen.findByRole('img', { name: 'ph_keep.jpg' })

    await user.click(screen.getByRole('button', { name: 'Not a duplicate' }))

    await waitFor(() => {
      expect(screen.queryByRole('img', { name: 'ph_keep.jpg' })).not.toBeInTheDocument()
    })
    expect(mergeMock).not.toHaveBeenCalled()
  })

  it('shows the empty state when there are no duplicate groups', async () => {
    fetchMock.mockResolvedValue(page([]))
    renderPage()
    expect(await screen.findByText('No duplicates found')).toBeInTheDocument()
  })

  it('keeps the loaded groups and offers a retry when loading more fails', async () => {
    const user = userEvent.setup()
    fetchMock
      .mockResolvedValueOnce(page([group('g1', 'ph_keep', 'ph_dup')], 20))
      .mockRejectedValueOnce(new Error('network down'))
      .mockResolvedValueOnce(page([group('g2', 'ph_more', 'ph_copy')]))
    renderPage()
    await screen.findByRole('img', { name: 'ph_keep.jpg' })

    await user.click(screen.getByRole('button', { name: 'Load more' }))

    // The failed page is reported inline; the first page stays on screen instead
    // of being replaced by a full-page error.
    expect(await screen.findByText('Could not load more duplicates.')).toBeInTheDocument()
    expect(screen.getByRole('img', { name: 'ph_keep.jpg' })).toBeInTheDocument()
    expect(screen.queryByText('Failed to load duplicates.')).not.toBeInTheDocument()

    // Load more doubles as the retry: a successful retry appends and clears it.
    await user.click(screen.getByRole('button', { name: 'Load more' }))
    expect(await screen.findByRole('img', { name: 'ph_more.jpg' })).toBeInTheDocument()
    expect(screen.getByRole('img', { name: 'ph_keep.jpg' })).toBeInTheDocument()
    expect(screen.queryByText('Could not load more duplicates.')).not.toBeInTheDocument()
  })

  it('locks every group while a dry-run is in flight, before the modal appears', async () => {
    const user = userEvent.setup()
    fetchMock.mockResolvedValue(
      page([group('g1', 'ph_keep', 'ph_dup'), group('g2', 'ph_other', 'ph_copy')]),
    )
    // Hold the first group's dry-run open: this is the window in which no modal
    // (and no backdrop) is up yet, so the second group must be locked by itself.
    let finishDryRun!: (result: MergeResult) => void
    const dryRun = new Promise<MergeResult>((resolve) => {
      finishDryRun = resolve
    })
    mergeMock.mockImplementationOnce(() => dryRun)
    renderPage()
    await screen.findByRole('img', { name: 'ph_keep.jpg' })

    const mergeButtons = screen.getAllByRole('button', { name: 'Keep best & merge' })
    expect(mergeButtons).toHaveLength(2)
    await user.click(mergeButtons[0])

    await waitFor(() => {
      expect(mergeButtons[1]).toBeDisabled()
    })
    expect(screen.queryByText('Merge duplicates')).not.toBeInTheDocument()
    expect(screen.getAllByRole('button', { name: 'Not a duplicate' })[1]).toBeDisabled()

    // A click on the second group cannot start a competing merge…
    await user.click(mergeButtons[1])
    expect(mergeMock).toHaveBeenCalledTimes(1)

    // …and the first group's preview is the one that opens.
    finishDryRun(preview('ph_keep', true))
    expect(await screen.findByText('Merge duplicates')).toBeInTheDocument()
    expect(mergeMock).toHaveBeenCalledWith({
      keeper_uid: 'ph_keep',
      member_uids: ['ph_keep', 'ph_dup'],
      dry_run: true,
    })
  })

  it('marks a human-confirmed group, and only that one', async () => {
    fetchMock.mockResolvedValue({
      groups: [group('g1', 'ph_keep', 'ph_dup', true), group('g2', 'ph_a', 'ph_b')],
      total: 2,
      limit: 20,
      offset: 0,
      next_offset: null,
    })
    renderPage()

    // The badge is what explains why this group is at the top — the backend sorts
    // confirmed groups first, and without it one would look like an ordinary
    // machine guess that jumped the queue.
    expect(await screen.findAllByTestId('duplicate-confirmed')).toHaveLength(1)
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
