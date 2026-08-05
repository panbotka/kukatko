import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { type ReactNode } from 'react'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, expect, it, vi } from 'vitest'

import i18n from '../i18n'
import { ApiError } from '../services/auth'
import { type DuplicateMarkerGroup, type DuplicateMarkersResponse } from '../services/dupmarkers'

import { DuplicateMarkersPage } from './DuplicateMarkersPage'

// jsdom has no layout, so react-virtuoso mounts nothing: stand it in with a
// plain list that renders every row, the way the other virtualized pages' tests
// do (see UploadPage.test.tsx).
vi.mock('react-virtuoso', () => ({
  Virtuoso: ({
    data,
    itemContent,
    computeItemKey,
  }: {
    data: DuplicateMarkerGroup[]
    itemContent: (index: number, item: DuplicateMarkerGroup) => ReactNode
    computeItemKey: (index: number, item: DuplicateMarkerGroup) => string
  }) => (
    <div data-testid="dup-marker-list">
      {data.map((item, index) => (
        <div key={computeItemKey(index, item)}>{itemContent(index, item)}</div>
      ))}
    </div>
  ),
}))

// Mock the network layer only, keeping the real types.
vi.mock('../services/dupmarkers', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/dupmarkers')>()
  return {
    ...actual,
    fetchDuplicateMarkers: vi.fn(),
    keepMarker: vi.fn(),
    invalidateMarker: vi.fn(),
  }
})

vi.mock('../services/feedback', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/feedback')>()
  return { ...actual, dismissDuplicateMarkers: vi.fn() }
})

const { fetchDuplicateMarkers, keepMarker, invalidateMarker } =
  await import('../services/dupmarkers')
const { dismissDuplicateMarkers } = await import('../services/feedback')

const fetchMock = vi.mocked(fetchDuplicateMarkers)
const keepMock = vi.mocked(keepMarker)
const invalidMock = vi.mocked(invalidateMarker)
const dismissMock = vi.mocked(dismissDuplicateMarkers)

/** group builds a finding with `count` markers of `name` on `photo`. */
function group(photo: string, name: string, count: number): DuplicateMarkerGroup {
  return {
    photo_uid: photo,
    photo_title: `${photo}.jpg`,
    width: 4000,
    height: 3000,
    orientation: 1,
    subject_uid: `s-${name}`,
    subject_name: name,
    markers: Array.from({ length: count }, (_, i) => ({
      uid: `${photo}-m${String(i + 1)}`,
      bbox: [0.1 * (i + 1), 0.2, 0.1, 0.1] as [number, number, number, number],
      score: 0,
      reviewed: false,
    })),
  }
}

/** page wraps findings in a listing response; nextOffset drives "load more". */
function page(
  groups: DuplicateMarkerGroup[],
  nextOffset: number | null = null,
): DuplicateMarkersResponse {
  return { groups, total: groups.length, limit: 20, offset: 0, next_offset: nextOffset }
}

function renderPage() {
  return render(
    <I18nextProvider i18n={i18n}>
      <MemoryRouter initialEntries={['/duplicate-markers']}>
        <DuplicateMarkersPage />
      </MemoryRouter>
    </I18nextProvider>,
  )
}

/** cards returns the rendered finding cards, in order. */
function cards() {
  return screen.getAllByTestId('dup-marker-group')
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  fetchMock.mockReset()
  keepMock.mockReset()
  invalidMock.mockReset()
  dismissMock.mockReset()
})

it('lists every finding with one numbered box per marker', async () => {
  fetchMock.mockResolvedValue(page([group('p1', 'Marie', 3), group('p2', 'Jan', 2)]))

  renderPage()

  await waitFor(() => {
    expect(cards()).toHaveLength(2)
  })
  expect(screen.getByText('Marie')).toBeInTheDocument()
  expect(screen.getByText('Jan')).toBeInTheDocument()
  // Three boxes drawn over the first photo, three close-ups under it.
  const first = cards()[0]
  expect(within(first).getAllByTestId('dup-marker-box')).toHaveLength(3)
  expect(within(first).getAllByTestId('dup-marker-crop')).toHaveLength(3)
})

it('crops the close-ups from a fit_* size, never a centre-cropped tile', async () => {
  fetchMock.mockResolvedValue(page([group('p1', 'Marie', 2)]))

  renderPage()

  await waitFor(() => {
    expect(screen.getAllByTestId('dup-marker-crop')).toHaveLength(2)
  })
  for (const crop of screen.getAllByTestId('dup-marker-crop')) {
    expect(crop.getAttribute('data-thumb-size')).toMatch(/^fit_/)
  }
})

it('keeps one marker and drops the settled finding from the list', async () => {
  const user = userEvent.setup()
  fetchMock.mockResolvedValue(page([group('p1', 'Marie', 3), group('p2', 'Jan', 2)]))
  keepMock.mockResolvedValue({
    photo_uid: 'p1',
    subject_uid: 's-Marie',
    keep_marker_uid: 'p1-m2',
    detached: ['p1-m1', 'p1-m3'],
  })

  renderPage()
  await waitFor(() => {
    expect(cards()).toHaveLength(2)
  })

  await user.click(within(cards()[0]).getByRole('button', { name: /Keep #2/ }))

  await waitFor(() => {
    expect(cards()).toHaveLength(1)
  })
  expect(keepMock).toHaveBeenCalledWith({
    photo_uid: 'p1',
    subject_uid: 's-Marie',
    keep_marker_uid: 'p1-m2',
  })
  expect(screen.getByText('Jan')).toBeInTheDocument()
  expect(screen.queryByText('Marie')).not.toBeInTheDocument()
})

it('flags a box invalid, shrinking a three-marker finding instead of settling it', async () => {
  const user = userEvent.setup()
  fetchMock.mockResolvedValue(page([group('p1', 'Marie', 3)]))
  invalidMock.mockResolvedValue(undefined)

  renderPage()
  await waitFor(() => {
    expect(screen.getAllByTestId('dup-marker-crop')).toHaveLength(3)
  })

  await user.click(screen.getAllByRole('button', { name: /No face here/ })[0])

  await waitFor(() => {
    expect(screen.getAllByTestId('dup-marker-crop')).toHaveLength(2)
  })
  expect(invalidMock).toHaveBeenCalledWith('p1-m1')
  // Two markers of one person is still a mistake, so the card stays.
  expect(cards()).toHaveLength(1)
})

it('drops the finding once flagging takes it below two markers', async () => {
  const user = userEvent.setup()
  fetchMock.mockResolvedValue(page([group('p1', 'Marie', 2)]))
  invalidMock.mockResolvedValue(undefined)

  renderPage()
  await waitFor(() => {
    expect(cards()).toHaveLength(1)
  })

  await user.click(screen.getAllByRole('button', { name: /No face here/ })[0])

  await waitFor(() => {
    expect(screen.queryAllByTestId('dup-marker-group')).toHaveLength(0)
  })
  expect(screen.getByText('No repeated markers')).toBeInTheDocument()
})

it('records "leave it be" as durable feedback and hides the finding', async () => {
  const user = userEvent.setup()
  fetchMock.mockResolvedValue(page([group('p1', 'Marie', 2)]))
  dismissMock.mockResolvedValue(undefined)

  renderPage()
  await waitFor(() => {
    expect(cards()).toHaveLength(1)
  })

  await user.click(screen.getByRole('button', { name: /Leave it be/ }))

  await waitFor(() => {
    expect(screen.queryAllByTestId('dup-marker-group')).toHaveLength(0)
  })
  expect(dismissMock).toHaveBeenCalledWith({ photo_uid: 'p1', subject_uid: 's-Marie' })
})

it('reports a failed decision and leaves the finding in place', async () => {
  const user = userEvent.setup()
  fetchMock.mockResolvedValue(page([group('p1', 'Marie', 2)]))
  keepMock.mockRejectedValue(new ApiError(500, 'boom'))

  renderPage()
  await waitFor(() => {
    expect(cards()).toHaveLength(1)
  })

  await user.click(screen.getAllByRole('button', { name: /Keep #1/ })[0])

  await waitFor(() => {
    expect(screen.getByText('The action failed.')).toBeInTheDocument()
  })
  expect(cards()).toHaveLength(1)
})

it('explains a stale finding rather than blaming the user', async () => {
  const user = userEvent.setup()
  fetchMock.mockResolvedValue(page([group('p1', 'Marie', 2)]))
  keepMock.mockRejectedValue(new ApiError(404, 'gone'))

  renderPage()
  await waitFor(() => {
    expect(cards()).toHaveLength(1)
  })

  await user.click(screen.getAllByRole('button', { name: /Keep #1/ })[0])

  await waitFor(() => {
    expect(screen.getByText(/changed in the meantime/)).toBeInTheDocument()
  })
})

it('shows the empty state when nothing is repeated', async () => {
  fetchMock.mockResolvedValue(page([]))

  renderPage()

  expect(await screen.findByText('No repeated markers')).toBeInTheDocument()
})

it('offers a retry when the listing fails', async () => {
  const user = userEvent.setup()
  fetchMock.mockRejectedValueOnce(new ApiError(500, 'boom'))
  fetchMock.mockResolvedValueOnce(page([group('p1', 'Marie', 2)]))

  renderPage()
  expect(await screen.findByText('Failed to load repeated markers.')).toBeInTheDocument()

  await user.click(screen.getByRole('button', { name: /Try again|Retry/i }))

  await waitFor(() => {
    expect(cards()).toHaveLength(1)
  })
})

it('says so when the review is not available on the server', async () => {
  fetchMock.mockRejectedValue(new ApiError(503, 'off'))

  renderPage()

  expect(
    await screen.findByText('Repeated-marker review is not available on this server.'),
  ).toBeInTheDocument()
})

it('appends the next page without losing what is already reviewed', async () => {
  const user = userEvent.setup()
  fetchMock.mockResolvedValueOnce(page([group('p1', 'Marie', 2)], 20))
  fetchMock.mockResolvedValueOnce(page([group('p2', 'Jan', 2)]))

  renderPage()
  await waitFor(() => {
    expect(cards()).toHaveLength(1)
  })

  await user.click(screen.getByRole('button', { name: 'Load more' }))

  await waitFor(() => {
    expect(cards()).toHaveLength(2)
  })
  expect(fetchMock.mock.lastCall?.[0]).toEqual({ limit: 20, offset: 20 })
})
