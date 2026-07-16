import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../i18n'
import { type OutlierFace, type OutlierResult, type SubjectCount } from '../services/people'

import { OutliersPage } from './OutliersPage'

vi.mock('../services/people', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/people')>()
  return { ...actual, fetchOutliers: vi.fn(), assignFace: vi.fn(), fetchSubjects: vi.fn() }
})

vi.mock('../services/feedback', () => ({
  confirmFace: vi.fn(),
  unconfirmFace: vi.fn(),
  rejectFace: vi.fn(),
  unrejectFace: vi.fn(),
}))

const { fetchOutliers, assignFace, fetchSubjects } = await import('../services/people')
const { confirmFace } = await import('../services/feedback')
const outliersMock = vi.mocked(fetchOutliers)
const assignMock = vi.mocked(assignFace)
const subjectsMock = vi.mocked(fetchSubjects)
const confirmMock = vi.mocked(confirmFace)

const SUBJECTS = [
  { uid: 's1', name: 'Alice', marker_count: 42 },
  { uid: 's2', name: 'Bob', marker_count: 7 },
] as unknown as SubjectCount[]

/** An outlier face; the bbox is off-centre so the padding has something to move. */
function face(overrides: Partial<OutlierFace> = {}): OutlierFace {
  return {
    photo_uid: 'ph1',
    face_index: 0,
    bbox: [0.4, 0.3, 0.2, 0.2],
    det_score: 0.9,
    distance: 0.42,
    marker_uid: 'mk1',
    width: 1200,
    height: 800,
    orientation: 1,
    ...overrides,
  }
}

/** Wraps faces in a full outlier response for subject `s1`. */
function makeResult(faces: OutlierFace[], overrides: Partial<OutlierResult> = {}): OutlierResult {
  return {
    subject_uid: 's1',
    count: faces.length,
    meaningful: true,
    avg_distance: 0.2,
    no_embedding: 0,
    faces,
    ...overrides,
  }
}

function renderPage(entry = '/outliers?subject=s1') {
  return render(
    <I18nextProvider i18n={i18n}>
      <MemoryRouter initialEntries={[entry]}>
        <OutliersPage />
      </MemoryRouter>
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  outliersMock.mockReset()
  assignMock.mockReset().mockResolvedValue(undefined)
  subjectsMock.mockReset().mockResolvedValue(SUBJECTS)
  confirmMock.mockReset().mockResolvedValue(undefined)
})

describe('OutliersPage', () => {
  it('opens with the person from the URL and asks the endpoint for them', async () => {
    outliersMock.mockResolvedValue(makeResult([face()]))
    renderPage()

    await waitFor(() => {
      expect(outliersMock).toHaveBeenCalledWith(
        's1',
        expect.objectContaining({ threshold: 0 }),
        expect.anything(),
      )
    })
    expect(await screen.findByTestId('outlier-card')).toBeInTheDocument()
  })

  it('draws the face box inside a padded context crop, not a tight one', async () => {
    outliersMock.mockResolvedValue(makeResult([face()]))
    renderPage()

    // The crop is the bbox grown 30 % per side, so within it the face covers
    // 0.2 / 0.32 = 62.5 % — the rest is the context you need to recognise anyone.
    const box = await screen.findByTestId('outlier-bbox')
    expect(parseFloat(box.style.width)).toBeCloseTo(62.5)
    expect(parseFloat(box.style.height)).toBeCloseTo(62.5)
    expect(parseFloat(box.style.left)).toBeCloseTo(18.75)
    expect(parseFloat(box.style.top)).toBeCloseTo(18.75)
  })

  it('✓ unassigns the person through the assign endpoint', async () => {
    const user = userEvent.setup()
    outliersMock.mockResolvedValue(makeResult([face()]))
    renderPage()
    await screen.findByTestId('outlier-card')

    await user.click(screen.getByRole('button', { name: /Yes, remove/ }))

    await waitFor(() => {
      expect(assignMock).toHaveBeenCalledWith('ph1', {
        action: 'unassign_person',
        marker_uid: 'mk1',
      })
    })
    // The card flips where it stands rather than vanishing.
    expect(screen.getByTestId('outlier-card')).toHaveAttribute('data-status', 'removed')
  })

  it('✗ records a confirmation so the face is not offered again', async () => {
    const user = userEvent.setup()
    outliersMock.mockResolvedValue(makeResult([face({ face_index: 2 })]))
    renderPage()
    await screen.findByTestId('outlier-card')

    await user.click(screen.getByRole('button', { name: /No, that is Alice/ }))

    await waitFor(() => {
      expect(confirmMock).toHaveBeenCalledWith({
        photo_uid: 'ph1',
        face_index: 2,
        subject_uid: 's1',
      })
    })
    expect(assignMock).not.toHaveBeenCalled()
    expect(screen.getByTestId('outlier-card')).toHaveAttribute('data-status', 'confirmed')
  })

  it('moves focus with the arrows and unassigns the focused card with y', async () => {
    const user = userEvent.setup()
    outliersMock.mockResolvedValue(
      makeResult([
        face({ photo_uid: 'ph1', marker_uid: 'mk1' }),
        face({ photo_uid: 'ph2', marker_uid: 'mk2' }),
      ]),
    )
    renderPage()
    await screen.findAllByTestId('outlier-card')

    // First move lands on the first card, second steps to the next.
    await user.keyboard('{ArrowRight}')
    await waitFor(() => {
      expect(screen.getAllByTestId('outlier-card')[0]).toHaveAttribute('data-focused', 'true')
    })
    await user.keyboard('{ArrowRight}')
    await waitFor(() => {
      expect(screen.getAllByTestId('outlier-card')[1]).toHaveAttribute('data-focused', 'true')
    })

    await user.keyboard('y')
    await waitFor(() => {
      expect(assignMock).toHaveBeenCalledWith('ph2', {
        action: 'unassign_person',
        marker_uid: 'mk2',
      })
    })
    // Focus advanced to the remaining undecided card: no reaching for the mouse.
    await waitFor(() => {
      expect(screen.getAllByTestId('outlier-card')[0]).toHaveAttribute('data-focused', 'true')
    })
  })

  it('confirms the focused card with n', async () => {
    const user = userEvent.setup()
    outliersMock.mockResolvedValue(makeResult([face({ face_index: 5 })]))
    renderPage()
    await screen.findByTestId('outlier-card')

    await user.keyboard('{ArrowRight}')
    await user.keyboard('n')

    await waitFor(() => {
      expect(confirmMock).toHaveBeenCalledWith({
        photo_uid: 'ph1',
        face_index: 5,
        subject_uid: 's1',
      })
    })
  })

  it('bulk-unassigns the selection and reports a partial failure honestly', async () => {
    const user = userEvent.setup()
    outliersMock.mockResolvedValue(
      makeResult([
        face({ photo_uid: 'ph1', marker_uid: 'mk1' }),
        face({ photo_uid: 'ph2', marker_uid: 'mk2' }),
      ]),
    )
    // The second face refuses; the first must stay unassigned regardless.
    assignMock.mockImplementation((photoUid: string) =>
      photoUid === 'ph2' ? Promise.reject(new Error('nope')) : Promise.resolve(undefined),
    )
    renderPage()
    await screen.findAllByTestId('outlier-card')

    // x enters selection mode and picks the focused card.
    await user.keyboard('{ArrowRight}')
    await user.keyboard('x')
    await user.keyboard('{ArrowRight}')
    await user.keyboard('x')

    await user.click(await screen.findByRole('button', { name: 'Remove 2 faces' }))

    await waitFor(() => {
      expect(assignMock).toHaveBeenCalledTimes(2)
    })
    // One failed: say so, and say how many — never swallow it.
    expect(await screen.findByText('1 face could not be removed.')).toBeInTheDocument()

    const cards = screen.getAllByTestId('outlier-card')
    expect(cards[0]).toHaveAttribute('data-status', 'removed')
    expect(cards[1]).toHaveAttribute('data-status', 'error')
  })

  it('selects everything with Ctrl+A', async () => {
    const user = userEvent.setup()
    outliersMock.mockResolvedValue(
      makeResult([face({ photo_uid: 'ph1' }), face({ photo_uid: 'ph2' })]),
    )
    renderPage()
    await screen.findAllByTestId('outlier-card')

    await user.keyboard('{Control>}a{/Control}')

    expect(await screen.findByRole('button', { name: 'Remove 2 faces' })).toBeInTheDocument()
  })

  it('says how many faces have no embedding instead of quietly omitting them', async () => {
    outliersMock.mockResolvedValue(makeResult([face()], { no_embedding: 3, count: 10 }))
    renderPage()

    const note = await screen.findByTestId('outlier-no-embedding')
    expect(note).toHaveTextContent('3 faces have no embedding')
  })

  it('flags a ranking too small to mean anything', async () => {
    outliersMock.mockResolvedValue(makeResult([face()], { meaningful: false, count: 2 }))
    renderPage()

    expect(await screen.findByTestId('outlier-not-meaningful')).toBeInTheDocument()
  })

  it('waits for a person before querying anything', async () => {
    renderPage('/outliers')

    expect(
      await screen.findByText('Pick someone to see their faces ranked, most suspicious first.'),
    ).toBeInTheDocument()
    expect(outliersMock).not.toHaveBeenCalled()
  })

  it('reports a failed query', async () => {
    outliersMock.mockRejectedValue(new Error('offline'))
    renderPage()

    expect(await screen.findByText('Could not load the faces.')).toBeInTheDocument()
  })

  it('offers no unassign for a face with no marker, and says why', async () => {
    outliersMock.mockResolvedValue(makeResult([face({ marker_uid: '' })]))
    renderPage()

    const card = await screen.findByTestId('outlier-card')
    expect(within(card).getByRole('button', { name: /Yes, remove/ })).toBeDisabled()
  })
})
