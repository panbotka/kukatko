import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { type ComponentType, type ReactNode } from 'react'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../i18n'
import { REVIEW_GRID_SCOPE } from '../lib/gridDensity'
import { type ClusterPage, type ClusterView } from '../services/people'
import { readCss, ruleBody } from '../test/css'

import { ClustersPage } from './ClustersPage'

/** Context the review grid threads to its List and Footer components. */
interface GridContext {
  density: number
  footer?: ReactNode
}

/** The props the fake grid reads off `VirtuosoGrid`; the rest is ignored. */
interface MockGridProps {
  data: ClusterView[]
  context: GridContext
  components: {
    List: ComponentType<{ context: GridContext; children: ReactNode }>
    Footer: ComponentType<{ context: GridContext }>
  }
  itemContent: (index: number, item: ClusterView) => ReactNode
  computeItemKey: (index: number, item: ClusterView) => string
  endReached?: () => void
}

/**
 * jsdom lays nothing out, so the real virtualizer measures a viewport of zero
 * and mounts no cards at all. This stand-in renders them through the component's
 * own `List` (the element carrying the column template) and exposes
 * `endReached`, so the scroll-driven paging can be triggered by a test.
 */
const virtuoso = vi.hoisted(() => ({ props: null as MockGridProps | null }))

vi.mock('react-virtuoso', () => ({
  VirtuosoGrid: (props: MockGridProps) => {
    virtuoso.props = props
    const { components, context, data, itemContent, computeItemKey } = props
    return (
      <components.List context={context}>
        {data.map((item, index) => (
          <div key={computeItemKey(index, item)}>{itemContent(index, item)}</div>
        ))}
        <components.Footer context={context} />
      </components.List>
    )
  },
}))

vi.mock('../services/people', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/people')>()
  return { ...actual, fetchClusters: vi.fn(), assignCluster: vi.fn(), removeClusterFace: vi.fn() }
})

const { fetchClusters, assignCluster } = await import('../services/people')
const fetchMock = vi.mocked(fetchClusters)
const assignMock = vi.mocked(assignCluster)

/** A two-cluster queue; the first carries a suggestion. */
function clusters(): ClusterView[] {
  return [
    {
      uid: 'fc_1',
      size: 4,
      representative: {
        photo_uid: 'p1',
        face_index: 0,
        bbox: [0.1, 0.1, 0.2, 0.2],
        det_score: 0.9,
      },
      examples: [{ photo_uid: 'p1', face_index: 0, bbox: [0.1, 0.1, 0.2, 0.2], det_score: 0.9 }],
      suggestion: { subject_uid: 'su_a', subject_name: 'Alice', distance: 0.1, confidence: 0.9 },
      created_at: '2026-01-01T00:00:00Z',
    },
    {
      uid: 'fc_2',
      size: 2,
      representative: {
        photo_uid: 'p2',
        face_index: 1,
        bbox: [0.3, 0.3, 0.2, 0.2],
        det_score: 0.8,
      },
      examples: [{ photo_uid: 'p2', face_index: 1, bbox: [0.3, 0.3, 0.2, 0.2], det_score: 0.8 }],
      created_at: '2026-01-01T00:00:00Z',
    },
  ]
}

/** One more group, for the second page. */
function thirdCluster(): ClusterView {
  return {
    uid: 'fc_3',
    size: 7,
    representative: { photo_uid: 'p3', face_index: 0, bbox: [0.2, 0.2, 0.2, 0.2], det_score: 0.7 },
    examples: [{ photo_uid: 'p3', face_index: 0, bbox: [0.2, 0.2, 0.2, 0.2], det_score: 0.7 }],
    created_at: '2026-01-02T00:00:00Z',
  }
}

/** Wraps views in a listing page, filling in the paging fields. */
function page(views: ClusterView[], extra: Partial<ClusterPage> = {}): ClusterPage {
  return {
    clusters: views,
    total: views.length,
    pending: 0,
    grouping: false,
    limit: 24,
    offset: 0,
    next_offset: null,
    ...extra,
  }
}

function renderPage() {
  return render(
    <I18nextProvider i18n={i18n}>
      <MemoryRouter>
        <ClustersPage />
      </MemoryRouter>
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  window.localStorage.clear()
  fetchMock.mockReset()
  assignMock.mockReset()
  assignMock.mockResolvedValue(undefined)
  virtuoso.props = null
})

describe('ClustersPage', () => {
  it('names a cluster by free text, calls the API, and drops it from the list', async () => {
    fetchMock.mockResolvedValue(page(clusters()))
    const user = userEvent.setup()
    renderPage()

    // Two cluster size badges initially.
    expect(await screen.findByText('4 faces')).toBeInTheDocument()
    expect(screen.getByText('2 faces')).toBeInTheDocument()

    const inputs = screen.getAllByLabelText('Name this group')
    await user.type(inputs[0], 'Bob')
    const nameButtons = screen.getAllByRole('button', { name: 'Name group' })
    await user.click(nameButtons[0])

    await waitFor(() => {
      expect(assignMock).toHaveBeenCalledWith('fc_1', { subject_name: 'Bob' })
    })
    // The named cluster is removed from the list; only the second remains.
    await waitFor(() => {
      expect(screen.queryByText('4 faces')).not.toBeInTheDocument()
    })
    expect(screen.getByText('2 faces')).toBeInTheDocument()
  })

  it('accepts the subject suggestion with one tap', async () => {
    fetchMock.mockResolvedValue(page(clusters()))
    const user = userEvent.setup()
    renderPage()

    await user.click(await screen.findByRole('button', { name: /Name as Alice/ }))

    await waitFor(() => {
      expect(assignMock).toHaveBeenCalledWith('fc_1', { subject_uid: 'su_a' })
    })
  })

  it('asks for a bounded first page rather than the whole library', async () => {
    fetchMock.mockResolvedValue(page(clusters()))
    renderPage()
    await screen.findByText('4 faces')

    expect(fetchMock).toHaveBeenCalledWith({ limit: 24, offset: 0 }, expect.anything())
  })

  it('appends the next page as the reader reaches the end', async () => {
    fetchMock.mockResolvedValueOnce(page(clusters(), { total: 3, next_offset: 24 }))
    fetchMock.mockResolvedValueOnce(page([thirdCluster()], { total: 3, offset: 24 }))
    renderPage()
    await screen.findByText('4 faces')

    // The scroll-driven append: virtuoso reports the end of the loaded cards.
    virtuoso.props?.endReached?.()

    expect(await screen.findByText('7 faces')).toBeInTheDocument()
    expect(fetchMock).toHaveBeenLastCalledWith({ limit: 24, offset: 24 }, undefined)
    // The groups already loaded stay mounted; the page grew, it did not reload.
    expect(screen.getByText('4 faces')).toBeInTheDocument()
  })

  it('keeps the loaded groups and offers a retry when a later page fails', async () => {
    fetchMock.mockResolvedValueOnce(page(clusters(), { total: 3, next_offset: 24 }))
    fetchMock.mockRejectedValueOnce(new Error('network'))
    const user = userEvent.setup()
    renderPage()
    await screen.findByText('4 faces')

    virtuoso.props?.endReached?.()

    expect(await screen.findByText('Could not load more groups.')).toBeInTheDocument()
    expect(screen.getByText('4 faces')).toBeInTheDocument()

    fetchMock.mockResolvedValueOnce(page([thirdCluster()], { total: 3, offset: 24 }))
    await user.click(screen.getByRole('button', { name: 'Load more' }))
    expect(await screen.findByText('7 faces')).toBeInTheDocument()
  })

  it('says how many groups are still being prepared instead of spinning', async () => {
    fetchMock.mockResolvedValue(page(clusters(), { total: 2, pending: 431 }))
    renderPage()

    expect(
      await screen.findByText('Ready groups: 2 · still being prepared in the background: 431'),
    ).toBeInTheDocument()
    // The prepared groups are shown meanwhile, not withheld.
    expect(screen.getByText('4 faces')).toBeInTheDocument()
  })

  it('does not call an unprepared library empty', async () => {
    fetchMock.mockResolvedValue(page([], { total: 0, pending: 12 }))
    renderPage()

    expect(
      await screen.findByText('Ready groups: 0 · still being prepared in the background: 12'),
    ).toBeInTheDocument()
    expect(screen.queryByText('No face groups to review')).not.toBeInTheDocument()
  })

  it('reloads from the first page when the reader looks again', async () => {
    fetchMock.mockResolvedValueOnce(page([], { total: 0, pending: 3 }))
    fetchMock.mockResolvedValueOnce(page(clusters(), { total: 2, pending: 0 }))
    const user = userEvent.setup()
    renderPage()

    await user.click(await screen.findByRole('button', { name: 'Look again' }))

    expect(await screen.findByText('4 faces')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Look again' })).not.toBeInTheDocument()
  })

  it('columns the cluster grid from the review density stepper', async () => {
    window.localStorage.setItem(REVIEW_GRID_SCOPE.storageKey, '2')
    fetchMock.mockResolvedValue(page(clusters()))
    const user = userEvent.setup()
    renderPage()
    await screen.findByText('4 faces')

    const grid = document.querySelector<HTMLElement>('[data-density]')
    expect(grid?.style.gridTemplateColumns).toBe('repeat(2, 1fr)')

    await user.click(screen.getByRole('button', { name: 'More tiles per row' }))

    await waitFor(() => {
      expect(document.querySelector<HTMLElement>('[data-density]')?.style.gridTemplateColumns).toBe(
        'repeat(3, 1fr)',
      )
    })
    // The count is the one every review tool shares, not a private one.
    expect(window.localStorage.getItem(REVIEW_GRID_SCOPE.storageKey)).toBe('3')
  })

  it('keeps the pinned column count from being widened by a card', async () => {
    // `1fr` takes its automatic minimum from the item's min-content width, and a
    // cluster card carries a name field and a button — so on the three columns a
    // phone is capped to, the tracks would outgrow the row and scroll the whole
    // page sideways. The guard is the pair: the class on the grid, and the rule
    // that class stands for.
    fetchMock.mockResolvedValue(page(clusters()))
    renderPage()
    await screen.findByText('4 faces')

    expect(document.querySelector('[data-density]')).toHaveClass('kk-review-grid')
    expect(ruleBody(readCss('src/components/review/review.css'), /\.kk-review-grid > \*/)).toMatch(
      /min-width:\s*0/,
    )
  })

  it('shows the empty state when no clusters await review', async () => {
    fetchMock.mockResolvedValue(page([]))
    renderPage()
    expect(await screen.findByText('No face groups to review')).toBeInTheDocument()
    expect(screen.queryByText('Looking for face groups…')).not.toBeInTheDocument()
  })

  it('says the groups are being worked out while a pass is running', async () => {
    // A library whose faces have never been grouped: nothing to list yet and
    // nothing pending either, but a pass is under way — the one case the bare
    // empty state used to describe as "there is nothing here".
    fetchMock.mockResolvedValue(page([], { grouping: true }))
    renderPage()

    expect(await screen.findByText('Looking for face groups…')).toBeInTheDocument()
    expect(screen.queryByText('No face groups to review')).not.toBeInTheDocument()
  })

  it('looks again from the grouping state once the pass has had a moment', async () => {
    fetchMock.mockResolvedValueOnce(page([], { grouping: true }))
    fetchMock.mockResolvedValueOnce(page(clusters(), { total: 2 }))
    const user = userEvent.setup()
    renderPage()

    await user.click(await screen.findByRole('button', { name: 'Look again' }))

    expect(await screen.findByText('4 faces')).toBeInTheDocument()
    expect(screen.queryByText('Looking for face groups…')).not.toBeInTheDocument()
  })

  it('offers a retry when the first page fails', async () => {
    fetchMock.mockRejectedValueOnce(new Error('network'))
    fetchMock.mockResolvedValueOnce(page(clusters()))
    const user = userEvent.setup()
    renderPage()

    await user.click(await screen.findByRole('button', { name: /Try again/i }))
    expect(await screen.findByText('4 faces')).toBeInTheDocument()
  })
})
