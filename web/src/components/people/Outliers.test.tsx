import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'
import { type OutlierResult } from '../../services/people'

import { Outliers } from './Outliers'

vi.mock('../../services/people', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../services/people')>()
  return { ...actual, fetchOutliers: vi.fn(), assignFace: vi.fn() }
})

const { fetchOutliers, assignFace } = await import('../../services/people')
const fetchMock = vi.mocked(fetchOutliers)
const assignMock = vi.mocked(assignFace)

/** An outlier result with two ranked, marker-backed faces. */
function outliers(): OutlierResult {
  return {
    subject_uid: 'su_a',
    count: 2,
    meaningful: true,
    avg_distance: 0.3,
    no_embedding: 0,
    faces: [
      {
        photo_uid: 'p1',
        face_index: 0,
        bbox: [0.1, 0.1, 0.2, 0.2],
        det_score: 0.9,
        distance: 0.7,
        marker_uid: 'mk1',
        width: 1000,
        height: 800,
        orientation: 1,
      },
      {
        photo_uid: 'p2',
        face_index: 1,
        bbox: [0.3, 0.3, 0.2, 0.2],
        det_score: 0.8,
        distance: 0.5,
        marker_uid: 'mk2',
        width: 1000,
        height: 800,
        orientation: 1,
      },
    ],
  }
}

function renderOutliers() {
  return render(
    <I18nextProvider i18n={i18n}>
      <MemoryRouter>
        <Outliers subjectUid="su_a" />
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

describe('Outliers', () => {
  it('unassigns a suspected face and removes it from the list', async () => {
    fetchMock.mockResolvedValue(outliers())
    const user = userEvent.setup()
    renderOutliers()

    const buttons = await screen.findAllByRole('button', { name: 'Not this person' })
    expect(buttons).toHaveLength(2)

    await user.click(buttons[0])

    await waitFor(() => {
      expect(assignMock).toHaveBeenCalledWith('p1', {
        action: 'unassign_person',
        marker_uid: 'mk1',
      })
    })
    // The unassigned face is dropped; one remains.
    await waitFor(() => {
      expect(screen.getAllByRole('button', { name: 'Not this person' })).toHaveLength(1)
    })
  })

  it('shows the empty state once every face has been reviewed', async () => {
    fetchMock.mockResolvedValue({
      subject_uid: 'su_a',
      count: 0,
      meaningful: false,
      avg_distance: 0,
      no_embedding: 0,
      faces: [],
    })
    renderOutliers()
    expect(await screen.findByText('No assigned faces to review.')).toBeInTheDocument()
  })
})
