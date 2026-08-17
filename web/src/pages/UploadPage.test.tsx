import { act, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { StrictMode, type ReactNode } from 'react'
import { I18nextProvider } from 'react-i18next'
import { Link, MemoryRouter, Route, Routes } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
import { type UploadQueueItem } from '../hooks/useUploadQueue'
import i18n from '../i18n'
import { ApiError } from '../services/auth'
import { type BulkResult } from '../services/bulk'
import { type AlbumSummary, type LabelCount } from '../services/organize'
import { type UploadFileOptions, type UploadFileResult } from '../services/upload'

import { UploadPage } from './UploadPage'

vi.mock('../services/upload', () => ({
  uploadFile: vi.fn(),
  isAbortError: (error: unknown): boolean =>
    error instanceof DOMException && error.name === 'AbortError',
}))

// jsdom has no layout, so the real virtualized list renders nothing. This
// stand-in renders every item, which is all the assertions here need.
vi.mock('react-virtuoso', () => ({
  Virtuoso: ({
    data,
    itemContent,
  }: {
    data: UploadQueueItem[]
    itemContent: (index: number, item: UploadQueueItem) => ReactNode
  }) => (
    <div data-testid="upload-list">
      {data.map((item, index) => (
        <div key={item.id}>{itemContent(index, item)}</div>
      ))}
    </div>
  ),
}))
vi.mock('../services/bulk', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/bulk')>()
  return { ...actual, bulkUpdatePhotos: vi.fn() }
})
vi.mock('../services/organize', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/organize')>()
  return {
    ...actual,
    fetchAlbums: vi.fn(),
    fetchLabels: vi.fn(),
    createAlbum: vi.fn(),
    createLabel: vi.fn(),
  }
})
// The share arrives through the browser's Cache Storage, which jsdom has none
// of; the collection itself is covered in src/pwa/shareTarget.test.ts.
vi.mock('../pwa/shareTarget', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../pwa/shareTarget')>()
  return { ...actual, collectSharedFiles: vi.fn() }
})

const { uploadFile } = await import('../services/upload')
const { bulkUpdatePhotos } = await import('../services/bulk')
const { fetchAlbums, fetchLabels, createAlbum } = await import('../services/organize')
const { collectSharedFiles } = await import('../pwa/shareTarget')
const uploadMock = vi.mocked(uploadFile)
const bulkMock = vi.mocked(bulkUpdatePhotos)
const albumsMock = vi.mocked(fetchAlbums)
const labelsMock = vi.mocked(fetchLabels)
const createAlbumMock = vi.mocked(createAlbum)
const collectMock = vi.mocked(collectSharedFiles)

function file(name: string): File {
  return new File(['data'], name, { type: 'image/jpeg' })
}

function created(uid: string): UploadFileResult {
  return { filename: 'x', status: 201, outcome: 'created', photo_uid: uid }
}

function duplicate(uid: string): UploadFileResult {
  return { filename: 'x', status: 409, outcome: 'duplicate', photo_uid: uid }
}

function failure(name: string): UploadFileResult {
  return { filename: name, status: 500, outcome: 'error', error: 'boom' }
}

function albumSummary(uid: string, title: string): AlbumSummary {
  return {
    uid,
    slug: title.toLowerCase(),
    title,
    description: '',
    type: 'album',
    private: false,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    photo_count: 0,
  }
}

function labelCount(uid: string, name: string): LabelCount {
  return {
    uid,
    slug: name.toLowerCase(),
    name,
    priority: 0,
    review_enabled: true,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    photo_count: 0,
  }
}

function bulkResult(): BulkResult {
  return { results: [], counts: { total: 0, updated: 0, skipped: 0, errored: 0 } }
}

/** A signed-in editor, so inline album/label creation is offered. */
function auth(): AuthContextValue {
  return {
    status: 'authenticated',
    user: { uid: 'u1', username: 'u', display_name: 'U', role: 'editor' },
    role: 'editor',
    downloadToken: null,
    canWrite: true,
    isAdmin: false,
    login: vi.fn(),
    logout: vi.fn(),
    refresh: vi.fn(),
  } as unknown as AuthContextValue
}

/**
 * Renders the page inside a two-route memory router, so an in-app link out of
 * `/upload` is a real navigation the leave guard can hold back.
 */
function renderPage(url = '/upload') {
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={auth()}>
        <MemoryRouter initialEntries={[url]}>
          <Link to="/albums">Albums page</Link>
          <Routes>
            <Route path="/upload" element={<UploadPage />} />
            <Route path="/albums" element={<h1>Albums</h1>} />
          </Routes>
        </MemoryRouter>
      </AuthContext.Provider>
    </I18nextProvider>,
  )
}

/** Picks files through the hidden gallery input (labelled for a11y). */
async function pickFiles(
  user: ReturnType<typeof userEvent.setup>,
  files: File[],
  label = 'Choose photos or videos to upload',
): Promise<void> {
  const inputs = screen.getAllByLabelText(label)
  await user.upload(inputs[0], files)
}

/** Types into a batch selector and clicks the option whose label matches. */
async function selectOption(
  user: ReturnType<typeof userEvent.setup>,
  field: string,
  query: string,
): Promise<void> {
  const input = await screen.findByRole('combobox', { name: field })
  await user.type(input, query)
  const listbox = screen.getByRole('listbox', { name: field })
  await user.click(within(listbox).getByRole('option', { name: new RegExp(`^${query}`, 'i') }))
}

/** Opens the demoted per-file list. */
async function openQueue(user: ReturnType<typeof userEvent.setup>): Promise<void> {
  await user.click(screen.getByRole('button', { name: /^Files in this batch/ }))
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  uploadMock.mockReset()
  bulkMock.mockReset()
  createAlbumMock.mockReset()
  albumsMock.mockReset().mockResolvedValue([albumSummary('al1', 'Trip')])
  labelsMock.mockReset().mockResolvedValue([labelCount('lb1', 'Sunset')])
  collectMock.mockReset().mockResolvedValue({ accepted: [], rejected: [] })
})

describe('UploadPage — stage 1, pick', () => {
  it('shows only the pick stage: no album fields, no queue, no start button', async () => {
    renderPage()
    // Give the album catalogs a tick; they must still not put a picker on an
    // empty page — there is no batch for it to apply to yet.
    await act(async () => {
      await Promise.resolve()
    })

    expect(screen.getByRole('heading', { name: 'Add photos to your library' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Choose photos' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Take a photo' })).toBeInTheDocument()

    expect(screen.queryByRole('combobox', { name: 'Albums' })).not.toBeInTheDocument()
    expect(screen.queryByTestId('upload-list')).not.toBeInTheDocument()
    expect(screen.queryByRole('progressbar')).not.toBeInTheDocument()
  })

  it('starts uploading the moment files are picked, with no button in between', async () => {
    uploadMock.mockReturnValue(new Promise<UploadFileResult>(() => undefined))
    const user = userEvent.setup()
    renderPage()

    await pickFiles(user, [file('a.jpg'), file('b.jpg')])

    // Bytes are moving without anything else being pressed…
    expect(uploadMock).toHaveBeenCalledTimes(2)
    // …and the page is the progress now.
    expect(screen.getByRole('heading', { name: 'Uploading…' })).toBeInTheDocument()
    expect(screen.getByRole('progressbar')).toHaveAttribute('aria-valuenow', '0')
    expect(screen.getByText('0 / 2')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /^Upload \(/ })).not.toBeInTheDocument()
  })

  it('takes files pasted anywhere on the page and uploads them too', async () => {
    uploadMock.mockReturnValue(new Promise<UploadFileResult>(() => undefined))
    renderPage()

    // The paste route is what carries an iPhone's camera roll here, since iOS
    // has no share target at all. jsdom has no clipboard that can hold a file,
    // so the event is built by hand.
    const event = new Event('paste')
    Object.defineProperty(event, 'clipboardData', { value: { files: [file('pasted.jpg')] } })
    await act(async () => {
      window.dispatchEvent(event)
      await Promise.resolve()
    })

    expect(uploadMock).toHaveBeenCalledTimes(1)
    expect(screen.getByText('0 / 1')).toBeInTheDocument()
  })
})

describe('UploadPage — stage 2, uploading', () => {
  it('puts the progress and the album picker on the same screen, list demoted', async () => {
    uploadMock.mockReturnValue(new Promise<UploadFileResult>(() => undefined))
    const user = userEvent.setup()
    renderPage()

    await pickFiles(user, [file('a.jpg'), file('b.jpg')])

    // The progress lives in the action bar, together with the stage's control.
    const bar = screen.getByTestId('upload-action-bar')
    expect(within(bar).getByText('0 / 2')).toBeInTheDocument()
    expect(within(bar).getByText('2 left')).toBeInTheDocument()
    expect(within(bar).getByRole('button', { name: 'Add more files' })).toBeInTheDocument()

    // The picker is right there, in the wait.
    expect(await screen.findByRole('combobox', { name: 'Albums' })).toBeInTheDocument()
    expect(screen.getByRole('combobox', { name: 'Labels' })).toBeInTheDocument()

    // The per-file rows are behind a disclosure, not in the way.
    expect(screen.queryByTestId('upload-list')).not.toBeInTheDocument()
    await openQueue(user)
    expect(screen.getByText('a.jpg')).toBeInTheDocument()
    expect(screen.getByText('b.jpg')).toBeInTheDocument()
  })

  it('appends files added mid-flight to the running batch', async () => {
    uploadMock.mockReturnValue(new Promise<UploadFileResult>(() => undefined))
    const user = userEvent.setup()
    renderPage()

    await pickFiles(user, [file('a.jpg')])
    expect(screen.getByText('0 / 1')).toBeInTheDocument()

    await pickFiles(user, [file('b.jpg')])

    expect(screen.getByText('0 / 2')).toBeInTheDocument()
    await openQueue(user)
    expect(screen.getByText('a.jpg')).toBeInTheDocument()
    expect(screen.getByText('b.jpg')).toBeInTheDocument()
  })

  it('advances the one progress bar with the batch, partial files included', async () => {
    const pending: { options: UploadFileOptions; resolve: (r: UploadFileResult) => void }[] = []
    uploadMock.mockImplementation(
      (_file: File, options: UploadFileOptions = {}) =>
        new Promise<UploadFileResult>((resolve) => {
          pending.push({ options, resolve })
        }),
    )
    const user = userEvent.setup()
    renderPage()

    await pickFiles(user, [file('a.jpg'), file('b.jpg')])

    // Half of one file sent: the aggregate bar reflects the partial fraction,
    // (0.5 + 0) / 2 = 25%, rather than jumping only in whole-file steps.
    await act(async () => {
      pending[0].options.onProgress?.(0.5)
      await Promise.resolve()
    })
    expect(screen.getByRole('progressbar')).toHaveAttribute('aria-valuenow', '25')

    await act(async () => {
      pending[0].resolve(created('ph1'))
      await Promise.resolve()
    })
    expect(await screen.findByText('1 / 2')).toBeInTheDocument()
    expect(screen.getByText('1 left')).toBeInTheDocument()
  })

  it('keeps per-file remove in the demoted list, for a file still waiting its turn', async () => {
    uploadMock.mockReturnValue(new Promise<UploadFileResult>(() => undefined))
    const user = userEvent.setup()
    renderPage()

    // More files than the concurrency cap, so the tail is still queued and can
    // be dropped — "I picked one too many" is what this control is for.
    await pickFiles(user, [file('a.jpg'), file('b.jpg'), file('c.jpg'), file('d.jpg')])
    await openQueue(user)
    expect(screen.getByText('0 / 4')).toBeInTheDocument()

    const remove = screen.getAllByRole('button', { name: 'Remove' })
    expect(remove).toHaveLength(1)
    await user.click(remove[0])

    expect(screen.queryByText('d.jpg')).not.toBeInTheDocument()
    expect(screen.getByText('0 / 3')).toBeInTheDocument()
  })
})

describe('UploadPage — stage 3, done', () => {
  it('states the outcome in one sentence and leads with the library', async () => {
    uploadMock.mockResolvedValue(created('ph1'))
    const user = userEvent.setup()
    renderPage()

    await pickFiles(user, [file('a.jpg'), file('b.jpg')])

    expect(await screen.findByText('2 photos uploaded.')).toBeInTheDocument()
    const bar = screen.getByTestId('upload-action-bar')
    expect(within(bar).getByRole('link', { name: 'Show them in the library' })).toHaveAttribute(
      'href',
      '/?sort=added',
    )
    expect(within(bar).getByRole('button', { name: 'Upload more' })).toBeInTheDocument()
  })

  it('names the albums in the outcome once the batch is actually in them', async () => {
    bulkMock.mockResolvedValue(bulkResult())
    uploadMock.mockResolvedValueOnce(created('ph1')).mockResolvedValueOnce(duplicate('ph2'))
    const user = userEvent.setup()
    renderPage()

    await pickFiles(user, [file('a.jpg'), file('b.jpg')])
    // Chosen *during* the upload — the whole point of starting first.
    await selectOption(user, 'Albums', 'Trip')

    await waitFor(() => {
      // Created and duplicate alike: a re-upload belongs in the album too.
      expect(bulkMock).toHaveBeenCalledWith(['ph1', 'ph2'], { add_to_albums: ['al1'] })
    })
    expect(await screen.findByText('1 photo uploaded, added to Trip.')).toBeInTheDocument()
    expect(screen.getByText('1 file was already in your library.')).toBeInTheDocument()
  })

  it('says so when the batch ended up in no album, and offers the picker', async () => {
    bulkMock.mockResolvedValue(bulkResult())
    uploadMock.mockResolvedValue(created('ph1'))
    const user = userEvent.setup()
    renderPage()

    await pickFiles(user, [file('a.jpg')])

    expect(await screen.findByText('1 photo uploaded.')).toBeInTheDocument()
    expect(
      screen.getByText(
        'These photos are not in any album or label yet. Choose one and we will add them.',
      ),
    ).toBeInTheDocument()
    expect(bulkMock).not.toHaveBeenCalled()

    // Picking one only now still assigns the finished batch…
    await selectOption(user, 'Albums', 'Trip')
    await waitFor(() => {
      expect(bulkMock).toHaveBeenCalledWith(['ph1'], { add_to_albums: ['al1'] })
    })
    expect(await screen.findByText('1 photo uploaded, added to Trip.')).toBeInTheDocument()

    // …and so does a second pick, which used to be dropped while the page kept
    // claiming everything had been assigned.
    await selectOption(user, 'Labels', 'Sunset')
    await waitFor(() => {
      expect(bulkMock).toHaveBeenLastCalledWith(['ph1'], {
        add_to_albums: ['al1'],
        add_labels: ['lb1'],
      })
    })
    expect(await screen.findByText('1 photo uploaded, added to Trip, Sunset.')).toBeInTheDocument()
  })

  it('brings back the per-file list — with its filter and retry — when files failed', async () => {
    uploadMock.mockResolvedValueOnce(created('ph1')).mockResolvedValueOnce(failure('b'))
    const user = userEvent.setup()
    renderPage()

    await pickFiles(user, [file('a.jpg'), file('b.jpg')])

    // The rows appear unasked, because a failure is the one time they matter.
    expect(await screen.findByText('Failed')).toBeInTheDocument()
    expect(screen.getByText('a.jpg')).toBeInTheDocument()
    expect(screen.getByText('b.jpg')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Show failed only (1)' }))
    expect(screen.queryByText('a.jpg')).not.toBeInTheDocument()
    expect(screen.getByText('b.jpg')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Show all' }))
    expect(screen.getByText('a.jpg')).toBeInTheDocument()

    // And the row's own retry still works, one file at a time.
    uploadMock.mockResolvedValueOnce(created('ph2'))
    await user.click(screen.getByRole('button', { name: 'Retry' }))
    expect(await screen.findByText('2 photos uploaded.')).toBeInTheDocument()
  })

  it('offers no picker and claims no survivors when the whole batch failed', async () => {
    uploadMock.mockResolvedValue(failure('a'))
    const user = userEvent.setup()
    renderPage()

    await pickFiles(user, [file('a.jpg')])
    expect(await screen.findByText('1 file did not upload.')).toBeInTheDocument()

    // Nothing landed, so there is nothing to file and nothing "else" to point at.
    expect(screen.queryByRole('combobox', { name: 'Albums' })).not.toBeInTheDocument()
    expect(
      screen.queryByText('Everything else is in your library. Retry the ones that failed.'),
    ).not.toBeInTheDocument()
    expect(screen.queryByRole('link', { name: 'Show them in the library' })).not.toBeInTheDocument()

    // And removing the last file walks the page back to the start.
    await user.click(screen.getByRole('button', { name: 'Remove' }))
    expect(screen.getByRole('heading', { name: 'Add photos to your library' })).toBeInTheDocument()
  })

  it('leads with the failures and makes retry the primary action', async () => {
    uploadMock.mockResolvedValueOnce(created('ph1')).mockResolvedValueOnce(failure('b'))
    const user = userEvent.setup()
    renderPage()

    await pickFiles(user, [file('a.jpg'), file('b.jpg')])

    expect(await screen.findByText('1 file did not upload.')).toBeInTheDocument()
    expect(
      screen.getByText('Everything else is in your library. Retry the ones that failed.'),
    ).toBeInTheDocument()

    const bar = screen.getByTestId('upload-action-bar')
    // Retry is the primary; the library is demoted beside it, and "upload more"
    // is not offered while there is something to fix.
    expect(within(bar).getByRole('button', { name: 'Retry the failed files' })).toHaveClass(
      'btn-primary',
    )
    expect(within(bar).queryByRole('button', { name: 'Upload more' })).not.toBeInTheDocument()

    uploadMock.mockResolvedValueOnce(created('ph2'))
    await user.click(within(bar).getByRole('button', { name: 'Retry the failed files' }))

    expect(await screen.findByText('2 photos uploaded.')).toBeInTheDocument()
  })

  it('offers a retry when the assignment alone fails', async () => {
    bulkMock
      .mockRejectedValueOnce(new ApiError(500, 'assign failed'))
      .mockResolvedValueOnce(bulkResult())
    uploadMock.mockResolvedValue(created('ph1'))
    const user = userEvent.setup()
    renderPage()

    await pickFiles(user, [file('a.jpg')])
    await selectOption(user, 'Labels', 'Sunset')

    const retry = await screen.findByRole('button', { name: 'Retry' })
    await user.click(retry)

    await waitFor(() => {
      expect(bulkMock).toHaveBeenCalledTimes(2)
    })
    expect(bulkMock).toHaveBeenLastCalledWith(['ph1'], { add_labels: ['lb1'] })
    expect(await screen.findByText('1 photo uploaded, added to Sunset.')).toBeInTheDocument()
  })

  it('goes back to stage one on "upload more" but keeps the albums and labels', async () => {
    bulkMock.mockResolvedValue(bulkResult())
    uploadMock.mockResolvedValue(created('ph1'))
    const user = userEvent.setup()
    renderPage()

    await pickFiles(user, [file('a.jpg')])
    await selectOption(user, 'Albums', 'Trip')
    expect(await screen.findByText('1 photo uploaded, added to Trip.')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Upload more' }))

    // Stage one again…
    expect(screen.getByRole('heading', { name: 'Add photos to your library' })).toBeInTheDocument()
    expect(screen.queryByTestId('upload-list')).not.toBeInTheDocument()

    // …and the next batch of the same event lands in the same album, unasked.
    uploadMock.mockResolvedValue(created('ph2'))
    await pickFiles(user, [file('b.jpg')])
    await waitFor(() => {
      expect(bulkMock).toHaveBeenLastCalledWith(['ph2'], { add_to_albums: ['al1'] })
    })
  })
})

describe('UploadPage — leaving mid-upload', () => {
  it('asks before an in-app navigation drops a running upload', async () => {
    uploadMock.mockReturnValue(new Promise<UploadFileResult>(() => undefined))
    const user = userEvent.setup()
    renderPage()

    await pickFiles(user, [file('a.jpg')])
    await user.click(screen.getByRole('link', { name: 'Albums page' }))

    // Held back: the question is up and the page has not moved.
    const dialog = await screen.findByRole('dialog')
    expect(
      within(dialog).getByText(
        '1 file is still uploading. Leaving this page stops it, and it will not be in your library.',
      ),
    ).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'Uploading…' })).toBeInTheDocument()

    // Staying keeps the batch exactly where it was.
    await user.click(within(dialog).getByRole('button', { name: 'Stay here' }))
    expect(screen.getByRole('heading', { name: 'Uploading…' })).toBeInTheDocument()

    // Confirming lets the navigation through.
    await user.click(screen.getByRole('link', { name: 'Albums page' }))
    const again = await screen.findByRole('dialog')
    await user.click(within(again).getByRole('button', { name: 'Leave and stop uploading' }))
    expect(await screen.findByRole('heading', { name: 'Albums' })).toBeInTheDocument()
  })

  it('lets a navigation through once nothing is in flight', async () => {
    uploadMock.mockResolvedValue(created('ph1'))
    const user = userEvent.setup()
    renderPage()

    await pickFiles(user, [file('a.jpg')])
    expect(await screen.findByText('1 photo uploaded.')).toBeInTheDocument()

    await user.click(screen.getByRole('link', { name: 'Albums page' }))
    expect(await screen.findByRole('heading', { name: 'Albums' })).toBeInTheDocument()
  })
})

describe('UploadPage — shared files', () => {
  it('lands a share directly in the uploading stage, already uploading', async () => {
    collectMock.mockResolvedValue({
      accepted: [file('shared-a.jpg'), file('shared-b.jpg')],
      rejected: [],
    })
    uploadMock.mockReturnValue(new Promise<UploadFileResult>(() => undefined))
    renderPage('/upload?share=1700000000000-1')

    expect(await screen.findByRole('heading', { name: 'Uploading…' })).toBeInTheDocument()
    expect(collectMock).toHaveBeenCalledWith('1700000000000-1')
    expect(uploadMock).toHaveBeenCalledTimes(2)
    expect(screen.getByText('2 shared files are ready. Check them and upload.')).toBeInTheDocument()
  })

  it('names the shared files it cannot take, and uploads the rest', async () => {
    collectMock.mockResolvedValue({ accepted: [file('a.jpg')], rejected: ['smlouva.pdf'] })
    uploadMock.mockReturnValue(new Promise<UploadFileResult>(() => undefined))
    renderPage('/upload?share=abc')

    expect(
      await screen.findByText(
        'These files are not photos or videos, so they were left out: smlouva.pdf',
      ),
    ).toBeInTheDocument()
    expect(uploadMock).toHaveBeenCalledTimes(1)
  })

  it('says so when a share turned out to hold no photos at all', async () => {
    collectMock.mockResolvedValue({ accepted: [], rejected: [] })
    renderPage('/upload?share=abc')

    expect(await screen.findByText('That share contained no photos or videos.')).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'Add photos to your library' })).toBeInTheDocument()
  })

  it('collects a share exactly once, even under a double-invoked effect', async () => {
    // Collecting consumes the cache entries, so a second pass would find nothing
    // and report an empty share. StrictMode — which the app really runs under —
    // mounts every effect twice, so this is the scenario, not a hypothetical.
    collectMock.mockResolvedValue({ accepted: [file('a.jpg')], rejected: [] })
    uploadMock.mockReturnValue(new Promise<UploadFileResult>(() => undefined))
    render(
      <StrictMode>
        <I18nextProvider i18n={i18n}>
          <AuthContext.Provider value={auth()}>
            <MemoryRouter initialEntries={['/upload?share=abc']}>
              <UploadPage />
            </MemoryRouter>
          </AuthContext.Provider>
        </I18nextProvider>
      </StrictMode>,
    )

    expect(await screen.findByText('0 / 1')).toBeInTheDocument()
    expect(collectMock).toHaveBeenCalledTimes(1)
    expect(screen.getByText('1 shared file is ready. Check it and upload.')).toBeInTheDocument()
  })

  it('does not go looking for a share when the page was opened without one', () => {
    renderPage()

    expect(collectMock).not.toHaveBeenCalled()
  })
})

describe('UploadPage — creating an album inline', () => {
  it('closes the suggestions and leaves the new name as a chip', async () => {
    uploadMock.mockReturnValue(new Promise<UploadFileResult>(() => undefined))
    const user = userEvent.setup()
    renderPage()

    await pickFiles(user, [file('a.jpg')])
    const field = await screen.findByRole('combobox', { name: 'Albums' })
    await user.type(field, 'Pouť 2026')
    await user.click(screen.getByRole('option', { name: 'Create “Pouť 2026”' }))

    // On a phone the open list plus the on-screen keyboard was the whole bug:
    // the query cleared, nothing else moved, and it read as "nothing happened".
    expect(screen.queryByRole('listbox', { name: 'Albums' })).not.toBeInTheDocument()
    expect(field).not.toHaveFocus()
    expect(screen.getByRole('button', { name: 'Remove Pouť 2026' })).toBeInTheDocument()
    // Nothing is created on the server until the batch is assigned.
    expect(createAlbumMock).not.toHaveBeenCalled()
  })
})
