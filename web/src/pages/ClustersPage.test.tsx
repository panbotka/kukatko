import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../i18n'
import { type ClusterView } from '../services/people'

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
  fetchMock.mockReset()
  assignMock.mockReset()
  assignMock.mockResolvedValue(undefined)
})

afterEach(() => {
  vi.restoreAllMocks()
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

  it('shows the empty state when no clusters await review', async () => {
    fetchMock.mockResolvedValue([])
    renderPage()
    expect(await screen.findByText('No face groups to review')).toBeInTheDocument()
  })
})
