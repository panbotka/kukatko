import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext } from '../auth/AuthContext'
import i18n from '../i18n'
import type { DuplicateGroup, DuplicatesResponse, MergeResult } from '../services/duplicates'
import type { PhotoDetail } from '../services/photos'

import { DupComparePage } from './DupComparePage'

// Mock only the network layer, keeping the real types and pure logic.
vi.mock('../services/duplicates', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/duplicates')>()
  return { ...actual, fetchDuplicates: vi.fn(), mergeDuplicates: vi.fn() }
})
vi.mock('../services/feedback', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/feedback')>()
  return { ...actual, dismissDuplicate: vi.fn() }
})
vi.mock('../services/photos', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/photos')>()
  return { ...actual, fetchPhoto: vi.fn() }
})
vi.mock('../services/people', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/people')>()
  return { ...actual, fetchFaces: vi.fn() }
})

const { fetchDuplicates, mergeDuplicates } = await import('../services/duplicates')
const { dismissDuplicate } = await import('../services/feedback')
const { fetchPhoto } = await import('../services/photos')
const { fetchFaces } = await import('../services/people')
const fetchMock = vi.mocked(fetchDuplicates)
const mergeMock = vi.mocked(mergeDuplicates)
const dismissMock = vi.mocked(dismissDuplicate)
const photoMock = vi.mocked(fetchPhoto)
const facesMock = vi.mocked(fetchFaces)

function member(uid: string, w: number, h: number) {
  return {
    uid,
    title: '',
    file_name: `${uid}.jpg`,
    file_width: w,
    file_height: h,
    file_size: 1000,
    media_type: 'image',
    is_keeper: false,
  }
}

/** A group with `keeper` first, then the other members. */
function group(id: string, keeper: string, ...others: string[]): DuplicateGroup {
  return {
    id,
    reason: 'phash',
    keeper_uid: keeper,
    members: [member(keeper, 4000, 3000), ...others.map((u) => member(u, 1000, 750))],
  }
}

function page(groups: DuplicateGroup[]): DuplicatesResponse {
  return { groups, total: groups.length, limit: 20, offset: 0, next_offset: null }
}

function detail(uid: string, over: Partial<PhotoDetail> = {}): PhotoDetail {
  return {
    uid,
    file_hash: `hash-${uid}`,
    file_name: `${uid}.jpg`,
    file_size: 1000,
    file_mime: 'image/jpeg',
    file_width: 4000,
    file_height: 3000,
    taken_at_source: 'exif',
    // Present in the base so the spread of `over` keeps them `string` rather than
    // `string | undefined`, matching the fixture convention in PhotoLocation.test.
    thumb_url: '/api/v1/photos/x/thumb/fit_1920',
    download_url: '/api/v1/photos/x/download?original=true',
    title: '',
    description: '',
    camera_make: '',
    camera_model: '',
    lens_model: '',
    created_at: '2024-01-01T00:00:00Z',
    updated_at: '2024-01-01T00:00:00Z',
    files: [],
    albums: [],
    labels: [],
    ...over,
  }
}

function preview(keeper: string, dryRun: boolean): MergeResult {
  return {
    keeper_uid: keeper,
    albums_added: 1,
    labels_added: 0,
    people_added: 0,
    metadata_filled: [],
    archived: 1,
    dry_run: dryRun,
  }
}

/** Auth context stub: the page only reads the download token for media URLs. */
const authValue = {
  status: 'authenticated' as const,
  user: null,
  downloadToken: null,
  login: vi.fn(),
  logout: vi.fn(),
  refresh: vi.fn(),
}

function renderPage(entry = '/duplicates/compare') {
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={authValue as never}>
        <MemoryRouter initialEntries={[entry]}>
          <DupComparePage />
        </MemoryRouter>
      </AuthContext.Provider>
    </I18nextProvider>,
  )
}

/** Waits for the stage to paint, i.e. both photos of the pair have loaded. */
async function waitForPair() {
  await waitFor(() => {
    expect(screen.getByTestId('diff-table')).toBeInTheDocument()
  })
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  fetchMock.mockReset()
  mergeMock.mockReset()
  dismissMock.mockReset()
  photoMock.mockReset()
  facesMock.mockReset()
  // Both sides differ in dimensions by default, so the diff table has something to
  // mark; individual tests override.
  photoMock.mockImplementation((uid) =>
    Promise.resolve(
      uid === 'ph_keep' ? detail(uid) : detail(uid, { file_width: 1000, file_height: 750 }),
    ),
  )
  facesMock.mockImplementation((uid) =>
    Promise.resolve({ photo_uid: uid, width: 100, height: 100, orientation: 1, faces: [] }),
  )
  dismissMock.mockResolvedValue(undefined)
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('the difference table', () => {
  it('marks exactly the differing rows and leaves the identical ones unmarked', async () => {
    fetchMock.mockResolvedValue(page([group('g1', 'ph_keep', 'ph_dup')]))
    renderPage()
    await waitForPair()

    // Dimensions differ (4000×3000 vs 1000×750); the format is image/jpeg on both.
    expect(screen.getByTestId('diff-row-dimensions')).toHaveAttribute('data-differs', 'true')
    expect(screen.getByTestId('diff-row-format')).toHaveAttribute('data-differs', 'false')
    expect(screen.getByTestId('diff-row-camera')).toHaveAttribute('data-differs', 'false')
  })

  it('reports how many fields differ', async () => {
    fetchMock.mockResolvedValue(page([group('g1', 'ph_keep', 'ph_dup')]))
    renderPage()
    await waitForPair()

    // Only the dimensions and the file name differ between the two stubs.
    expect(screen.getByTestId('compare-diff-summary')).toHaveTextContent('2 fields differ')
  })

  it('can hide the identical rows, leaving only the differences', async () => {
    fetchMock.mockResolvedValue(page([group('g1', 'ph_keep', 'ph_dup')]))
    const user = userEvent.setup()
    renderPage()
    await waitForPair()

    expect(screen.getByTestId('diff-row-format')).toBeInTheDocument()
    await user.click(screen.getByRole('checkbox', { name: 'Differences only' }))
    expect(screen.queryByTestId('diff-row-format')).not.toBeInTheDocument()
    expect(screen.getByTestId('diff-row-dimensions')).toBeInTheDocument()
  })
})

describe('the synchronised zoom', () => {
  it('moves both images together, because they render one shared view', async () => {
    fetchMock.mockResolvedValue(page([group('g1', 'ph_keep', 'ph_dup')]))
    const user = userEvent.setup()
    renderPage()
    await waitForPair()

    const leftImage = within(screen.getByTestId('compare-pane-ph_keep')).getByRole('img')
    const rightImage = within(screen.getByTestId('compare-pane-ph_dup')).getByRole('img')
    expect(leftImage).toHaveStyle({ transform: 'translate(0px, 0px) scale(1)' })

    // Zoom by acting on ONE pane only; the other must follow.
    await user.click(screen.getByRole('button', { name: 'Zoom in' }))

    await waitFor(() => {
      expect(leftImage.style.transform).not.toBe('translate(0px, 0px) scale(1)')
    })
    expect(rightImage.style.transform).toBe(leftImage.style.transform)
  })

  it('returns both images to fit-to-view together', async () => {
    fetchMock.mockResolvedValue(page([group('g1', 'ph_keep', 'ph_dup')]))
    const user = userEvent.setup()
    renderPage()
    await waitForPair()

    await user.click(screen.getByRole('button', { name: 'Zoom in' }))
    await user.click(screen.getByRole('button', { name: 'Fit to view' }))

    const leftImage = within(screen.getByTestId('compare-pane-ph_keep')).getByRole('img')
    const rightImage = within(screen.getByTestId('compare-pane-ph_dup')).getByRole('img')
    expect(leftImage).toHaveStyle({ transform: 'translate(0px, 0px) scale(1)' })
    expect(rightImage).toHaveStyle({ transform: 'translate(0px, 0px) scale(1)' })
  })
})

describe('keep-left / keep-right', () => {
  it('previews then merges with the left photo as the keeper', async () => {
    fetchMock.mockResolvedValue(page([group('g1', 'ph_keep', 'ph_dup')]))
    mergeMock.mockImplementation((input) =>
      Promise.resolve(preview(input.keeper_uid, input.dry_run === true)),
    )
    const user = userEvent.setup()
    renderPage()
    await waitForPair()

    await user.click(screen.getByRole('button', { name: /Keep left/ }))

    // The destructive action is previewed first — the loser gets archived.
    await waitFor(() => {
      expect(mergeMock).toHaveBeenCalledWith({
        keeper_uid: 'ph_keep',
        member_uids: ['ph_keep', 'ph_dup'],
        dry_run: true,
      })
    })
    expect(await screen.findByText('Merge duplicates')).toBeInTheDocument()
    // The confirmation states that the loser is archived, never deleted.
    expect(screen.getByText(/archived to the trash, never deleted/)).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Confirm merge' }))
    await waitFor(() => {
      expect(mergeMock).toHaveBeenLastCalledWith({
        keeper_uid: 'ph_keep',
        member_uids: ['ph_keep', 'ph_dup'],
      })
    })
  })

  it('merges with the right photo as the keeper when the user keeps right', async () => {
    fetchMock.mockResolvedValue(page([group('g1', 'ph_keep', 'ph_dup')]))
    mergeMock.mockImplementation((input) =>
      Promise.resolve(preview(input.keeper_uid, input.dry_run === true)),
    )
    const user = userEvent.setup()
    renderPage()
    await waitForPair()

    await user.click(screen.getByRole('button', { name: /Keep right/ }))
    await waitFor(() => {
      expect(mergeMock).toHaveBeenCalledWith({
        keeper_uid: 'ph_dup',
        member_uids: ['ph_dup', 'ph_keep'],
        dry_run: true,
      })
    })
  })

  it('merges only the compared pair, never a third member of the group', async () => {
    fetchMock.mockResolvedValue(page([group('g1', 'ph_keep', 'ph_dup', 'ph_third')]))
    mergeMock.mockImplementation((input) =>
      Promise.resolve(preview(input.keeper_uid, input.dry_run === true)),
    )
    const user = userEvent.setup()
    renderPage()
    await waitForPair()

    await user.click(screen.getByRole('button', { name: /Keep left/ }))
    await waitFor(() => {
      expect(mergeMock).toHaveBeenCalled()
    })
    // ph_third was never on screen and must not be archived by this answer.
    expect(mergeMock.mock.calls[0][0].member_uids).not.toContain('ph_third')
  })
})

describe('keep-both', () => {
  it('persists a dismissal so the pair is not offered again', async () => {
    fetchMock.mockResolvedValue(page([group('g1', 'ph_keep', 'ph_dup')]))
    const user = userEvent.setup()
    renderPage()
    await waitForPair()

    await user.click(screen.getByRole('button', { name: /Keep both/ }))

    await waitFor(() => {
      expect(dismissMock).toHaveBeenCalledWith({ photo_uid: 'ph_keep', other_uid: 'ph_dup' })
    })
    // Nothing is merged or archived: "keep both" is an opinion, not an edit.
    expect(mergeMock).not.toHaveBeenCalled()
  })

  it('leaves the dismissed pair out of the queue built from the next load', async () => {
    // The server no longer reports the pair once it is dismissed, which is exactly
    // what the persisted dismissal buys — so a fresh mount has nothing to review.
    fetchMock.mockResolvedValue(page([]))
    renderPage()

    expect(await screen.findByText('All done')).toBeInTheDocument()
  })
})

describe('the queue', () => {
  it('advances to the next pair after a decision, without going back to the list', async () => {
    fetchMock.mockResolvedValue(
      page([group('g1', 'ph_keep', 'ph_dup'), group('g2', 'ph_x', 'ph_y')]),
    )
    const user = userEvent.setup()
    renderPage()
    await waitForPair()

    expect(screen.getByTestId('compare-progress')).toHaveTextContent('Pair 1 of 2')
    await user.click(screen.getByRole('button', { name: /Keep both/ }))

    await waitFor(() => {
      expect(screen.getByTestId('compare-progress')).toHaveTextContent('Pair 2 of 2')
    })
    // The next pair's photos are the ones now being fetched.
    await waitFor(() => {
      expect(photoMock).toHaveBeenCalledWith('ph_x', expect.anything())
    })
  })

  it('says which pair of a larger group is on screen, rather than hiding the third member', async () => {
    fetchMock.mockResolvedValue(page([group('g1', 'ph_keep', 'ph_dup', 'ph_third')]))
    renderPage()
    await waitForPair()

    expect(screen.getByTestId('compare-group-note')).toHaveTextContent('Pair 1 of 2 in this group')
    expect(screen.getByTestId('compare-progress')).toHaveTextContent('Pair 1 of 2')
  })

  it('shows the finished state once every pair is answered', async () => {
    fetchMock.mockResolvedValue(page([group('g1', 'ph_keep', 'ph_dup')]))
    const user = userEvent.setup()
    renderPage()
    await waitForPair()

    await user.click(screen.getByRole('button', { name: /Keep both/ }))
    expect(await screen.findByText('All done')).toBeInTheDocument()
  })

  it('starts on the pair named in the URL, so a reload lands where the user was', async () => {
    fetchMock.mockResolvedValue(page([group('g1', 'ph_a', 'ph_b'), group('g2', 'ph_x', 'ph_y')]))
    renderPage('/duplicates/compare?pair=ph_x%7Cph_y')
    await waitForPair()

    expect(screen.getByTestId('compare-progress')).toHaveTextContent('Pair 2 of 2')
  })
})

describe('the keyboard shortcuts', () => {
  it('keeps left on ArrowLeft', async () => {
    fetchMock.mockResolvedValue(page([group('g1', 'ph_keep', 'ph_dup')]))
    mergeMock.mockImplementation((input) =>
      Promise.resolve(preview(input.keeper_uid, input.dry_run === true)),
    )
    const user = userEvent.setup()
    renderPage()
    await waitForPair()

    await user.keyboard('{ArrowLeft}')
    await waitFor(() => {
      expect(mergeMock).toHaveBeenCalledWith(
        expect.objectContaining({ keeper_uid: 'ph_keep', dry_run: true }),
      )
    })
  })

  it('keeps right on ArrowRight', async () => {
    fetchMock.mockResolvedValue(page([group('g1', 'ph_keep', 'ph_dup')]))
    mergeMock.mockImplementation((input) =>
      Promise.resolve(preview(input.keeper_uid, input.dry_run === true)),
    )
    const user = userEvent.setup()
    renderPage()
    await waitForPair()

    await user.keyboard('{ArrowRight}')
    await waitFor(() => {
      expect(mergeMock).toHaveBeenCalledWith(
        expect.objectContaining({ keeper_uid: 'ph_dup', dry_run: true }),
      )
    })
  })

  it('keeps both on b', async () => {
    fetchMock.mockResolvedValue(page([group('g1', 'ph_keep', 'ph_dup')]))
    const user = userEvent.setup()
    renderPage()
    await waitForPair()

    await user.keyboard('b')
    await waitFor(() => {
      expect(dismissMock).toHaveBeenCalledWith({ photo_uid: 'ph_keep', other_uid: 'ph_dup' })
    })
  })
})

describe('failures', () => {
  it('reports the 503 when duplicate detection is disabled server-side', async () => {
    const { ApiError } = await import('../services/auth')
    fetchMock.mockRejectedValue(new ApiError(503, 'off'))
    renderPage()

    expect(
      await screen.findByText('Duplicate detection is disabled in the server configuration.'),
    ).toBeInTheDocument()
  })

  it('reports a pair whose photos will not load, rather than half a table', async () => {
    fetchMock.mockResolvedValue(page([group('g1', 'ph_keep', 'ph_dup')]))
    photoMock.mockRejectedValue(new Error('boom'))
    renderPage()

    expect(await screen.findByText('Failed to load this pair.')).toBeInTheDocument()
    expect(screen.queryByTestId('diff-table')).not.toBeInTheDocument()
  })
})

describe('regressions', () => {
  it('does not skip a pair when archiving also drops an earlier pair of the group', async () => {
    // The keeper is in BOTH of g1's pairs, so keeping a non-suggested member
    // archives it and drops the pair behind the cursor as well as the current one.
    // Counting the cursor forward would land past the last survivor and declare the
    // queue finished with g2 never reviewed.
    fetchMock.mockResolvedValue(
      page([group('g1', 'ph_keep', 'ph_a', 'ph_b'), group('g2', 'ph_x', 'ph_y')]),
    )
    mergeMock.mockImplementation((input) =>
      Promise.resolve(preview(input.keeper_uid, input.dry_run === true)),
    )
    const user = userEvent.setup()
    renderPage()
    await waitForPair()

    // Pair 1 of g1: keep both, so it stays in the queue and the cursor moves on.
    expect(screen.getByTestId('compare-progress')).toHaveTextContent('Pair 1 of 3')
    await user.click(screen.getByRole('button', { name: /Keep both/ }))
    await waitFor(() => {
      expect(screen.getByTestId('compare-progress')).toHaveTextContent('Pair 2 of 3')
    })
    await waitForPair()

    // Pair 2 of g1: keep the right (non-suggested) copy, archiving ph_keep. Both of
    // g1's pairs name ph_keep, so both drop and only g2's pair survives.
    await user.click(screen.getByRole('button', { name: /Keep right/ }))
    await screen.findByText('Merge duplicates')
    await user.click(screen.getByRole('button', { name: 'Confirm merge' }))

    // g2 must still be offered — it was never answered.
    await waitFor(() => {
      expect(screen.getByTestId('compare-progress')).toHaveTextContent('Pair 1 of 1')
    })
    expect(screen.queryByText('All done')).not.toBeInTheDocument()
    await waitFor(() => {
      expect(photoMock).toHaveBeenCalledWith('ph_x', expect.anything())
    })
  })

  it("never shows the previous pair's metadata against the next pair's photos", async () => {
    fetchMock.mockResolvedValue(
      page([group('g1', 'ph_keep', 'ph_dup'), group('g2', 'ph_x', 'ph_y')]),
    )
    // The second pair's photos load slowly, which is when a stale table would show.
    const slow = new Promise<never>(() => undefined)
    photoMock.mockImplementation((uid) => {
      if (uid === 'ph_x' || uid === 'ph_y') {
        return slow
      }
      return Promise.resolve(
        uid === 'ph_keep' ? detail(uid) : detail(uid, { file_width: 1000, file_height: 750 }),
      )
    })
    const user = userEvent.setup()
    renderPage()
    await waitForPair()
    expect(screen.getByText('4000 × 3000 (12.0 Mpx)')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: /Keep both/ }))
    await waitFor(() => {
      expect(screen.getByTestId('compare-progress')).toHaveTextContent('Pair 2 of 2')
    })

    // Pair 1's numbers must be gone, not lingering under pair 2's images...
    expect(screen.queryByText('4000 × 3000 (12.0 Mpx)')).not.toBeInTheDocument()
    expect(screen.queryByTestId('diff-table')).not.toBeInTheDocument()
    // ...and the destructive actions must not be armed on data that is not there.
    expect(screen.getByRole('button', { name: /Keep left/ })).toBeDisabled()
    expect(screen.getByRole('button', { name: /Keep right/ })).toBeDisabled()
  })
})
