import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../i18n'
import { initialColumns, LIBRARY_GRID_SCOPE, REVIEW_GRID_SCOPE } from '../lib/gridDensity'
import { type OutlierFace, type OutlierResult, type SubjectCount } from '../services/people'
import { readCss } from '../test/css'

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

/** A frame big enough that the thumbnail ladder is not capped by the original. */
const BIG_FRAME = { width: 6000, height: 4000 }

/** The grid element the cards sit in. */
function gridElement(container: HTMLElement): HTMLElement {
  const grid = container.querySelector<HTMLElement>('[data-density]')
  if (grid === null) {
    throw new Error('outlier grid not rendered')
  }
  return grid
}

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
  window.localStorage.clear()
  outliersMock.mockReset()
  assignMock.mockReset().mockResolvedValue(undefined)
  subjectsMock.mockReset().mockResolvedValue(SUBJECTS)
  confirmMock.mockReset().mockResolvedValue(undefined)
})

afterEach(() => {
  window.localStorage.clear()
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

  it('renders the column count stored for the review grid', async () => {
    window.localStorage.setItem(REVIEW_GRID_SCOPE.storageKey, '4')
    outliersMock.mockResolvedValue(makeResult([face()]))
    const { container } = renderPage()
    await screen.findByTestId('outlier-card')

    const grid = gridElement(container)
    expect(grid).toHaveAttribute('data-density', '4')
    expect(grid.style.gridTemplateColumns).toBe('repeat(4, 1fr)')
  })

  it('keeps its own density rather than the library’s', async () => {
    // Someone browses the library at ten tiny tiles per row. That is a fine
    // density for photographs and a terrible one for judging faces, so this grid
    // seeds its own count from its own (much wider) tile instead of inheriting it.
    window.localStorage.setItem(LIBRARY_GRID_SCOPE.storageKey, '10')
    outliersMock.mockResolvedValue(makeResult([face()]))
    const { container } = renderPage()
    await screen.findByTestId('outlier-card')

    const seeded = initialColumns(REVIEW_GRID_SCOPE)
    expect(gridElement(container)).toHaveAttribute('data-density', String(seeded))
    expect(window.localStorage.getItem(REVIEW_GRID_SCOPE.storageKey)).toBe(String(seeded))
    // …and the library's own preference is left exactly where it was.
    expect(window.localStorage.getItem(LIBRARY_GRID_SCOPE.storageKey)).toBe('10')
  })

  it('re-columns the grid from the density stepper and persists the choice', async () => {
    window.localStorage.setItem(REVIEW_GRID_SCOPE.storageKey, '2')
    const user = userEvent.setup()
    outliersMock.mockResolvedValue(makeResult([face()]))
    const { container } = renderPage()
    await screen.findByTestId('outlier-card')

    await user.click(screen.getByRole('button', { name: 'More tiles per row' }))

    await waitFor(() => {
      expect(gridElement(container)).toHaveAttribute('data-density', '3')
    })
    expect(gridElement(container).style.gridTemplateColumns).toBe('repeat(3, 1fr)')
    expect(window.localStorage.getItem(REVIEW_GRID_SCOPE.storageKey)).toBe('3')
    // The library's density is a separate preference and must not have moved.
    expect(window.localStorage.getItem(LIBRARY_GRID_SCOPE.storageKey)).toBeNull()
  })

  it('draws the face marker inside a padded context crop, not a tight one', async () => {
    outliersMock.mockResolvedValue(makeResult([face()]))
    renderPage()

    // The crop is the bbox grown 30 % per side, so within it the face covers
    // 0.2 / 0.32 = 62.5 % — the rest is the context you need to recognise anyone.
    // The marker is anchored on the face's centre, which is the crop's centre.
    const box = await screen.findByTestId('outlier-bbox')
    expect(box.style.getPropertyValue('--kk-face-w')).toBe('62.5%')
    expect(box.style.getPropertyValue('--kk-face-h')).toBe('62.5%')
    expect(box.style.getPropertyValue('--kk-face-x')).toBe('50%')
    expect(box.style.getPropertyValue('--kk-face-y')).toBe('50%')
  })

  it('keeps the marker on screen at every density', async () => {
    // The crop is defined *from* the box, so the marker is 62.5 % of a tile
    // whatever the tile's size — one column or ten, it neither vanishes nor
    // wanders out of the frame.
    for (const density of ['1', '5', '10']) {
      window.localStorage.setItem(REVIEW_GRID_SCOPE.storageKey, density)
      outliersMock.mockResolvedValue(makeResult([face()]))
      const view = renderPage()
      const box = await screen.findByTestId('outlier-bbox')

      expect(gridElement(view.container)).toHaveAttribute('data-density', density)
      expect(box.style.getPropertyValue('--kk-face-w')).toBe('62.5%')
      expect(box.style.getPropertyValue('--kk-face-x')).toBe('50%')
      view.unmount()
    }
  })

  it('guarantees the marker a minimum size and an unclippable stroke', () => {
    // The two promises that cannot be asserted in jsdom (it has no layout) live
    // in CSS, so assert the CSS: a floor under the marker's size, and a ring made
    // only of the element's own border and `inset` shadows, which `overflow:
    // hidden` on the card cannot clip.
    const css = readCss('src/components/people/outliers.css')
    const rule = /\.kk-face-marker\s*\{([\s\S]*?)\n\}/.exec(css)?.[1] ?? ''

    expect(rule).toMatch(/--kk-face-min:\s*\d+px/)
    expect(rule).toMatch(/width:\s*max\(var\(--kk-face-w[^)]*\),\s*var\(--kk-face-min\)\)/)
    expect(rule).toMatch(/height:\s*max\(var\(--kk-face-h[^)]*\),\s*var\(--kk-face-min\)\)/)
    // Centre-anchored and clamped, so the minimum grows the box around the face
    // and never pushes it past the crop's edge.
    expect(rule).toMatch(/transform:\s*translate\(-50%,\s*-50%\)/)
    expect(rule).toMatch(/left:\s*clamp\(/)
    expect(rule).toMatch(/top:\s*clamp\(/)
    expect(rule).toMatch(/box-shadow:[\s\S]*inset/)
    expect(rule).not.toMatch(/box-shadow:\s*0 0 0/)
  })

  it('cuts a small face from a larger thumbnail than a big one', async () => {
    // The complaint this exists for: a hard-coded fit_720 leaves a 2 %-wide face
    // ~35 px across before a 7× upscale into the card. The source is chosen per
    // face, so the small one asks for more pixels and the big one does not.
    outliersMock.mockResolvedValue(
      makeResult([
        face({ photo_uid: 'small', bbox: [0.4, 0.3, 0.02, 0.03], ...BIG_FRAME }),
        face({ photo_uid: 'big', bbox: [0.3, 0.2, 0.25, 0.35], ...BIG_FRAME }),
      ]),
    )
    renderPage()
    const images = await screen.findAllByTestId('outlier-photo')

    expect(images[0]).toHaveAttribute('data-thumb-size', 'fit_3840')
    expect(images[1]).toHaveAttribute('data-thumb-size', 'fit_720')
    expect(images[0].getAttribute('src')).toContain('/thumb/fit_3840')
  })

  it('degrades to a smaller thumbnail rather than showing a broken image', async () => {
    // On a publishing storage backend the thumb route redirects instead of
    // generating, so a size that never reached the bucket answers 404. Step down
    // the ladder — the fit_720 every card used before is always there.
    outliersMock.mockResolvedValue(
      makeResult([face({ bbox: [0.4, 0.3, 0.05, 0.07], ...BIG_FRAME })]),
    )
    renderPage()
    const img = await screen.findByTestId('outlier-photo')
    expect(img).toHaveAttribute('data-thumb-size', 'fit_2560')

    fireEvent.error(img)

    await waitFor(() => {
      expect(screen.getByTestId('outlier-photo')).toHaveAttribute('data-thumb-size', 'fit_1920')
    })
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
