import { act, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { StrictMode, type ReactNode } from 'react'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
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
const { fetchAlbums, fetchLabels } = await import('../services/organize')
const { collectSharedFiles } = await import('../pwa/shareTarget')
const uploadMock = vi.mocked(uploadFile)
const bulkMock = vi.mocked(bulkUpdatePhotos)
const albumsMock = vi.mocked(fetchAlbums)
const labelsMock = vi.mocked(fetchLabels)
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

function renderPage(url = '/upload') {
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={auth()}>
        <MemoryRouter initialEntries={[url]}>
          <UploadPage />
        </MemoryRouter>
      </AuthContext.Provider>
    </I18nextProvider>,
  )
}

/** Picks files through the hidden gallery input (labelled for a11y). */
async function pickFiles(user: ReturnType<typeof userEvent.setup>, files: File[]): Promise<void> {
  const input = screen.getByLabelText('Choose photos or videos to upload')
  await user.upload(input, files)
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

beforeEach(async () => {
  await i18n.changeLanguage('en')
  uploadMock.mockReset()
  bulkMock.mockReset()
  albumsMock.mockReset().mockResolvedValue([albumSummary('al1', 'Trip')])
  labelsMock.mockReset().mockResolvedValue([labelCount('lb1', 'Sunset')])
  collectMock.mockReset().mockResolvedValue({ accepted: [], rejected: [] })
})

describe('UploadPage', () => {
  it('lays the flow out as three numbered steps, in order', async () => {
    renderPage()
    // The catalogs land a tick later; step 2 shows a spinner until they do.
    await screen.findByRole('combobox', { name: 'Albums' })

    const steps = screen.getAllByTestId('upload-step')
    expect(steps).toHaveLength(3)

    // The order is the flow: pick files → organise the batch → start. Each step
    // says its number to a screen reader too, not only in the drawn marker.
    expect(within(steps[0]).getByRole('heading', { level: 2 })).toHaveAccessibleName(
      'Step 1: Pick the files',
    )
    expect(within(steps[1]).getByRole('heading', { level: 2 })).toHaveAccessibleName(
      'Step 2: Add to albums and labels',
    )
    expect(within(steps[2]).getByRole('heading', { level: 2 })).toHaveAccessibleName(
      'Step 3: Upload them',
    )

    // …and each step actually holds its own controls, so the numbering is not
    // decoration over an unchanged pile.
    expect(within(steps[0]).getByLabelText('Choose photos or videos to upload')).toBeInTheDocument()
    expect(within(steps[1]).getByRole('combobox', { name: 'Albums' })).toBeInTheDocument()
    expect(
      within(steps[2]).getByText('Pick some files above and they show up here, ready to upload.'),
    ).toBeInTheDocument()
  })

  it('says the album and label choice covers the whole batch, counted', async () => {
    const user = userEvent.setup()
    renderPage()

    expect(
      screen.getByText('Chosen albums and labels are applied to every photo in this upload.'),
    ).toBeInTheDocument()

    await pickFiles(user, [file('a.jpg'), file('b.jpg')])

    expect(
      screen.getByText('Optional. What you choose here is added to all 2 files in this batch.'),
    ).toBeInTheDocument()
    expect(screen.getByText('2 files picked')).toBeInTheDocument()
  })

  it('previews every picked file locally, without uploading anything', async () => {
    const user = userEvent.setup()
    renderPage()

    await pickFiles(user, [file('a.jpg'), file('b.jpg')])

    const thumbs = screen.getAllByTestId('upload-thumb')
    expect(thumbs).toHaveLength(2)
    for (const thumb of thumbs) {
      expect(within(thumb).getByRole('presentation', { hidden: true })).toHaveAttribute(
        'src',
        expect.stringMatching(/^blob:/),
      )
    }
    expect(uploadMock).not.toHaveBeenCalled()
  })

  it('keeps the batch-wide choice in the sticky header, one tap from the picker', async () => {
    const user = userEvent.setup()
    renderPage()

    await pickFiles(user, [file('a.jpg')])
    // The recap only appears once there is a picker to go back to.
    await screen.findByRole('combobox', { name: 'Albums' })
    const header = screen.getByTestId('upload-progress-header')
    expect(within(header).getByText('no album or label')).toBeInTheDocument()

    // With a long queue the picker is scrolled far above the rows in view, so
    // the sticky header is the only place that can still show the choice.
    await selectOption(user, 'Albums', 'Trip')
    expect(within(header).getByText('Trip')).toBeInTheDocument()

    // And the way back to it is a tap, which lands in the field itself.
    await user.click(within(header).getByRole('button', { name: 'Change' }))
    expect(screen.getByRole('combobox', { name: 'Albums' })).toHaveFocus()
  })

  it('queues selected files and shows them with a queued status', async () => {
    const user = userEvent.setup()
    renderPage()

    await pickFiles(user, [file('a.jpg'), file('b.jpg')])

    expect(screen.getByText('a.jpg')).toBeInTheDocument()
    expect(screen.getByText('b.jpg')).toBeInTheDocument()
    expect(screen.getAllByText('Queued')).toHaveLength(2)
    expect(uploadMock).not.toHaveBeenCalled()
  })

  it('removes a queued file before upload', async () => {
    const user = userEvent.setup()
    renderPage()

    await pickFiles(user, [file('a.jpg')])
    expect(screen.getByText('a.jpg')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Remove' }))
    expect(screen.queryByText('a.jpg')).not.toBeInTheDocument()
  })

  it('uploads on start and shows the created status and a library link', async () => {
    uploadMock.mockResolvedValue(created('ph1'))
    const user = userEvent.setup()
    renderPage()

    await pickFiles(user, [file('a.jpg')])
    await user.click(screen.getByRole('button', { name: 'Upload (1)' }))

    expect(await screen.findByText('Uploaded')).toBeInTheDocument()
    const link = await screen.findByRole('link', { name: 'View in library' })
    expect(link).toHaveAttribute('href', '/?sort=added')
  })

  it('renders duplicate and error outcomes from the responses', async () => {
    uploadMock
      .mockResolvedValueOnce({ filename: 'a', status: 409, outcome: 'duplicate', photo_uid: 'ph2' })
      .mockResolvedValueOnce({ filename: 'b', status: 500, outcome: 'error', error: 'boom' })
    const user = userEvent.setup()
    renderPage()

    await pickFiles(user, [file('a.jpg'), file('b.jpg')])
    await user.click(screen.getByRole('button', { name: 'Upload (2)' }))

    expect(await screen.findByText('Already in library')).toBeInTheDocument()
    expect(await screen.findByText('Failed')).toBeInTheDocument()
    expect(screen.getByText('boom')).toBeInTheDocument()
  })

  it('shows a near-duplicate warning without blocking', async () => {
    uploadMock.mockResolvedValue({
      filename: 'a.jpg',
      status: 201,
      outcome: 'created',
      photo_uid: 'ph1',
      warnings: [{ code: 'near_duplicate', message: 'similar' }],
    })
    const user = userEvent.setup()
    renderPage()

    await pickFiles(user, [file('a.jpg')])
    await user.click(screen.getByRole('button', { name: 'Upload (1)' }))

    expect(await screen.findByText('Uploaded')).toBeInTheDocument()
    expect(screen.getByText('Looks similar to a photo already in the library.')).toBeInTheDocument()
  })

  it('retries a failed upload', async () => {
    uploadMock.mockRejectedValueOnce(new ApiError(0, 'network error'))
    const user = userEvent.setup()
    renderPage()

    await pickFiles(user, [file('a.jpg')])
    await user.click(screen.getByRole('button', { name: 'Upload (1)' }))

    expect(await screen.findByText('Failed')).toBeInTheDocument()

    uploadMock.mockResolvedValueOnce(created('ph1'))
    await user.click(screen.getByRole('button', { name: 'Retry' }))

    expect(await screen.findByText('Uploaded')).toBeInTheDocument()
  })

  it('renders the album and label selectors and leaves them optional', async () => {
    const user = userEvent.setup()
    renderPage()

    expect(await screen.findByRole('combobox', { name: 'Albums' })).toBeInTheDocument()
    expect(await screen.findByRole('combobox', { name: 'Labels' })).toBeInTheDocument()

    // Uploading with nothing selected must not trigger any assignment.
    uploadMock.mockResolvedValue(created('ph1'))
    await pickFiles(user, [file('a.jpg')])
    await user.click(screen.getByRole('button', { name: 'Upload (1)' }))

    expect(await screen.findByText('Upload complete.')).toBeInTheDocument()
    expect(bulkMock).not.toHaveBeenCalled()
  })

  it('assigns the whole batch — new and duplicate — to the selected album', async () => {
    bulkMock.mockResolvedValue(bulkResult())
    uploadMock.mockResolvedValueOnce(created('ph1')).mockResolvedValueOnce(duplicate('ph2'))
    const user = userEvent.setup()
    renderPage()

    await selectOption(user, 'Albums', 'Trip')

    await pickFiles(user, [file('a.jpg'), file('b.jpg')])
    await user.click(screen.getByRole('button', { name: 'Upload (2)' }))

    await waitFor(() => {
      expect(bulkMock).toHaveBeenCalledWith(['ph1', 'ph2'], { add_to_albums: ['al1'] })
    })
    expect(await screen.findByText('Added to your albums and labels.')).toBeInTheDocument()
  })

  it('applies album and label picks made after the batch already finished', async () => {
    bulkMock.mockResolvedValue(bulkResult())
    uploadMock.mockResolvedValue(created('ph1'))
    const user = userEvent.setup()
    renderPage()

    await pickFiles(user, [file('a.jpg')])
    await user.click(screen.getByRole('button', { name: 'Upload (1)' }))

    expect(await screen.findByText('Upload complete.')).toBeInTheDocument()
    expect(bulkMock).not.toHaveBeenCalled()

    // Picking an album only now still assigns the finished batch.
    await selectOption(user, 'Albums', 'Trip')
    await waitFor(() => {
      expect(bulkMock).toHaveBeenCalledWith(['ph1'], { add_to_albums: ['al1'] })
    })
    expect(await screen.findByText('Added to your albums and labels.')).toBeInTheDocument()

    // And so does a second pick — it used to be dropped while the success
    // alert kept claiming everything had been assigned.
    await selectOption(user, 'Labels', 'Sunset')
    await waitFor(() => {
      expect(bulkMock).toHaveBeenLastCalledWith(['ph1'], {
        add_to_albums: ['al1'],
        add_labels: ['lb1'],
      })
    })
    expect(await screen.findByText('Added to your albums and labels.')).toBeInTheDocument()
  })

  it('offers a retry when assignment fails after a successful upload', async () => {
    bulkMock
      .mockRejectedValueOnce(new ApiError(500, 'assign failed'))
      .mockResolvedValueOnce(bulkResult())
    uploadMock.mockResolvedValue(created('ph1'))
    const user = userEvent.setup()
    renderPage()

    await selectOption(user, 'Labels', 'Sunset')

    await pickFiles(user, [file('a.jpg')])
    await user.click(screen.getByRole('button', { name: 'Upload (1)' }))

    const retry = await screen.findByRole('button', { name: 'Retry' })
    await user.click(retry)

    await waitFor(() => {
      expect(bulkMock).toHaveBeenCalledTimes(2)
    })
    expect(bulkMock).toHaveBeenLastCalledWith(['ph1'], { add_labels: ['lb1'] })
    expect(await screen.findByText('Added to your albums and labels.')).toBeInTheDocument()
  })

  it('shows a live overall header with done/total, a partial bar and counts', async () => {
    // Capture in-flight uploads so we can observe the running (not just final)
    // header state and drive partial progress.
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
    await user.click(screen.getByRole('button', { name: 'Upload (2)' }))

    // Both files are in flight (cap is 3): nothing done yet, both remaining.
    expect(await screen.findByText('0 / 2')).toBeInTheDocument()
    expect(screen.getByText('2 remaining')).toBeInTheDocument()
    const header = screen.getByTestId('upload-progress-header')
    expect(within(header).getByRole('progressbar')).toHaveAttribute('aria-valuenow', '0')

    // Half of one file sent: the aggregate bar reflects the partial fraction,
    // (0.5 + 0) / 2 = 25%, rather than jumping only in whole-file steps.
    await act(async () => {
      pending[0].options.onProgress?.(0.5)
      await Promise.resolve()
    })
    expect(within(header).getByRole('progressbar')).toHaveAttribute('aria-valuenow', '25')

    // Finishing that file advances done/total and the live counts.
    await act(async () => {
      pending[0].resolve(created('ph1'))
      await Promise.resolve()
    })
    expect(await screen.findByText('1 / 2')).toBeInTheDocument()
    expect(screen.getByText('1 uploaded')).toBeInTheDocument()
    expect(screen.getByText('1 remaining')).toBeInTheDocument()
  })

  it('lets the user filter the list down to just the failed files', async () => {
    uploadMock
      .mockResolvedValueOnce(created('ph1'))
      .mockResolvedValueOnce({ filename: 'b', status: 500, outcome: 'error', error: 'boom' })
    const user = userEvent.setup()
    renderPage()

    await pickFiles(user, [file('a.jpg'), file('b.jpg')])
    await user.click(screen.getByRole('button', { name: 'Upload (2)' }))

    // Both files present; b.jpg is the failed one.
    expect(await screen.findByText('Failed')).toBeInTheDocument()
    expect(screen.getByText('a.jpg')).toBeInTheDocument()
    expect(screen.getByText('b.jpg')).toBeInTheDocument()

    // Filtering to errors drops the succeeded file out of the list.
    await user.click(screen.getByRole('button', { name: 'Show failed only (1)' }))
    expect(screen.queryByText('a.jpg')).not.toBeInTheDocument()
    expect(screen.getByText('b.jpg')).toBeInTheDocument()

    // And back to all.
    await user.click(screen.getByRole('button', { name: 'Show all' }))
    expect(screen.getByText('a.jpg')).toBeInTheDocument()
  })

  it('takes files pasted anywhere on the page', async () => {
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

    expect(screen.getByText('pasted.jpg')).toBeInTheDocument()
    expect(screen.getByText('Queued')).toBeInTheDocument()
  })

  it('stages files shared from the phone into the ordinary queue', async () => {
    collectMock.mockResolvedValue({
      accepted: [file('shared-a.jpg'), file('shared-b.jpg')],
      rejected: [],
    })
    const user = userEvent.setup()
    renderPage('/upload?share=1700000000000-1')

    expect(await screen.findByText('shared-a.jpg')).toBeInTheDocument()
    expect(screen.getByText('shared-b.jpg')).toBeInTheDocument()
    expect(collectMock).toHaveBeenCalledWith('1700000000000-1')
    expect(screen.getByText('2 shared files are ready. Check them and upload.')).toBeInTheDocument()

    // Nothing is uploaded behind the user's back — the share is staged, and the
    // ordinary start button (with the ordinary limits) sends it.
    expect(uploadMock).not.toHaveBeenCalled()
    uploadMock.mockResolvedValue(created('ph1'))
    await user.click(screen.getByRole('button', { name: 'Upload (2)' }))

    await waitFor(() => {
      expect(uploadMock).toHaveBeenCalledTimes(2)
    })
  })

  it('names the shared files it cannot take, and queues the rest', async () => {
    collectMock.mockResolvedValue({ accepted: [file('a.jpg')], rejected: ['smlouva.pdf'] })
    renderPage('/upload?share=abc')

    expect(await screen.findByText('a.jpg')).toBeInTheDocument()
    expect(
      screen.getByText('These files are not photos or videos, so they were left out: smlouva.pdf'),
    ).toBeInTheDocument()
  })

  it('says so when a share turned out to hold no photos at all', async () => {
    collectMock.mockResolvedValue({ accepted: [], rejected: [] })
    renderPage('/upload?share=abc')

    expect(await screen.findByText('That share contained no photos or videos.')).toBeInTheDocument()
    expect(screen.queryByTestId('upload-list')).not.toBeInTheDocument()
  })

  it('collects a share exactly once, even under a double-invoked effect', async () => {
    // Collecting consumes the cache entries, so a second pass would find nothing
    // and report an empty share. StrictMode — which the app really runs under —
    // mounts every effect twice, so this is the scenario, not a hypothetical.
    collectMock.mockResolvedValue({ accepted: [file('a.jpg')], rejected: [] })
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

    expect(await screen.findByText('a.jpg')).toBeInTheDocument()
    expect(collectMock).toHaveBeenCalledTimes(1)
    expect(screen.getByText('1 shared file is ready. Check it and upload.')).toBeInTheDocument()
  })

  it('does not go looking for a share when the page was opened without one', () => {
    renderPage()

    expect(collectMock).not.toHaveBeenCalled()
  })

  it('shows a completed summary and retries failed files from it', async () => {
    uploadMock
      .mockResolvedValueOnce(created('ph1'))
      .mockResolvedValueOnce({ filename: 'b', status: 500, outcome: 'error', error: 'boom' })
    const user = userEvent.setup()
    renderPage()

    await pickFiles(user, [file('a.jpg'), file('b.jpg')])
    await user.click(screen.getByRole('button', { name: 'Upload (2)' }))

    // Terminal state: a clear completed summary that surfaces the failure.
    expect(await screen.findByText('Upload complete.')).toBeInTheDocument()
    expect(screen.getByText('1 uploaded, 0 duplicates, 1 failed')).toBeInTheDocument()

    // One-tap whole-batch retry from the header recovers the failure.
    uploadMock.mockResolvedValueOnce(created('ph2'))
    await user.click(screen.getByRole('button', { name: 'Retry failed' }))

    await waitFor(() => {
      expect(screen.getByText('2 uploaded, 0 duplicates, 0 failed')).toBeInTheDocument()
    })
  })
})
