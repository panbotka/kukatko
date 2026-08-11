import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../i18n'
import { REVIEW_GRID_SCOPE } from '../lib/gridDensity'
import { type ClusterView } from '../services/people'
import { readCss, ruleBody } from '../test/css'

import { ClustersPage } from './ClustersPage'

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
})

describe('ClustersPage', () => {
  it('names a cluster by free text, calls the API, and drops it from the list', async () => {
    fetchMock.mockResolvedValue(clusters())
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
    fetchMock.mockResolvedValue(clusters())
    const user = userEvent.setup()
    renderPage()

    await user.click(await screen.findByRole('button', { name: /Name as Alice/ }))

    await waitFor(() => {
      expect(assignMock).toHaveBeenCalledWith('fc_1', { subject_uid: 'su_a' })
    })
  })

  it('columns the cluster grid from the review density stepper', async () => {
    window.localStorage.setItem(REVIEW_GRID_SCOPE.storageKey, '2')
    fetchMock.mockResolvedValue(clusters())
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
    fetchMock.mockResolvedValue(clusters())
    renderPage()
    await screen.findByText('4 faces')

    expect(document.querySelector('[data-density]')).toHaveClass('kk-review-grid')
    expect(ruleBody(readCss('src/components/review/review.css'), /\.kk-review-grid > \*/)).toMatch(
      /min-width:\s*0/,
    )
  })

  it('shows the empty state when no clusters await review', async () => {
    fetchMock.mockResolvedValue([])
    renderPage()
    expect(await screen.findByText('No face groups to review')).toBeInTheDocument()
  })
})
