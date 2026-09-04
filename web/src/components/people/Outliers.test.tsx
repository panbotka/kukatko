import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'
import { OUTLIER_SECTION_BATCH } from '../../lib/outlierSection'
import { type OutlierFace, type OutlierResult } from '../../services/people'
import { readCss, ruleBody } from '../../test/css'

import { Outliers } from './Outliers'

vi.mock('../../services/people', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../services/people')>()
  return { ...actual, fetchOutliers: vi.fn(), assignFace: vi.fn() }
})

const { fetchOutliers, assignFace } = await import('../../services/people')
const fetchMock = vi.mocked(fetchOutliers)
const assignMock = vi.mocked(assignFace)

/** One ranked, marker-backed face; `index` orders it and names its photo. */
function face(index: number): OutlierFace {
  return {
    photo_uid: `p${String(index)}`,
    face_index: index,
    bbox: [0.1, 0.1, 0.2, 0.2],
    det_score: 0.9,
    distance: 0.7,
    marker_uid: `mk${String(index)}`,
    width: 1000,
    height: 800,
    orientation: 1,
  }
}

/** An outlier result holding `count` ranked faces. */
function outliers(count: number): OutlierResult {
  return {
    subject_uid: 'su_a',
    count,
    meaningful: true,
    avg_distance: 0.3,
    no_embedding: 0,
    faces: Array.from({ length: count }, (_, i) => face(i)),
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

/** The "not this person" controls currently on screen. */
function answers() {
  return screen.getAllByRole('button', { name: 'Not this person' })
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  fetchMock.mockReset()
  assignMock.mockReset()
  assignMock.mockResolvedValue(undefined)
})

describe('Outliers', () => {
  it('asks about one batch at a time, with an explicit way to see more', async () => {
    fetchMock.mockResolvedValue(outliers(OUTLIER_SECTION_BATCH + 3))
    const user = userEvent.setup()
    renderOutliers()

    expect(await screen.findAllByRole('link', { name: 'Open photo' })).toHaveLength(
      OUTLIER_SECTION_BATCH,
    )

    await user.click(screen.getByRole('button', { name: 'Show more (3)' }))

    expect(screen.getAllByRole('link', { name: 'Open photo' })).toHaveLength(
      OUTLIER_SECTION_BATCH + 3,
    )
    // Everything is on screen, so the control that revealed it is gone.
    expect(screen.queryByRole('button', { name: /Show more/ })).toBeNull()
  })

  it('shows no "see more" control for a person with less than one batch', async () => {
    fetchMock.mockResolvedValue(outliers(2))
    renderOutliers()

    expect(await screen.findAllByRole('link', { name: 'Open photo' })).toHaveLength(2)
    expect(screen.queryByRole('button', { name: /Show more/ })).toBeNull()
  })

  it('captions a face in plain language and keeps the distance in a tooltip', async () => {
    fetchMock.mockResolvedValue(outliers(1))
    renderOutliers()

    const link = (await screen.findAllByRole('link', { name: 'Open photo' }))[0]
    expect(link).toHaveAttribute('title', 'Difference from this person: 70 %')
    // The raw cosine distance the tiles used to be captioned with is nowhere in
    // the text.
    expect(screen.queryByText('0.700')).toBeNull()
    expect(screen.getByRole('heading', { name: 'Is this still the same person?' })).toBeVisible()
  })

  it('detaches a face and offers the way back in the same place', async () => {
    fetchMock.mockResolvedValue(outliers(2))
    const user = userEvent.setup()
    renderOutliers()

    const buttons = await screen.findAllByRole('button', { name: 'Not this person' })
    expect(buttons).toHaveLength(2)

    await user.click(buttons[0])

    await waitFor(() => {
      expect(assignMock).toHaveBeenCalledWith('p0', {
        action: 'unassign_person',
        marker_uid: 'mk0',
      })
    })
    // The tile keeps its place — with the undo on it — so the answer can be
    // taken back a second later.
    await screen.findByRole('button', { name: 'Undo' })
    expect(screen.getAllByRole('link', { name: 'Open photo' })).toHaveLength(2)
    expect(answers()).toHaveLength(1)

    await user.click(screen.getByRole('button', { name: 'Undo' }))

    await waitFor(() => {
      expect(assignMock).toHaveBeenLastCalledWith('p0', {
        action: 'assign_person',
        marker_uid: 'mk0',
        subject_uid: 'su_a',
      })
    })
    await waitFor(() => {
      expect(answers()).toHaveLength(2)
    })
  })

  it('writes nothing until the reader answers', async () => {
    fetchMock.mockResolvedValue(outliers(3))
    renderOutliers()

    await screen.findAllByRole('button', { name: 'Not this person' })
    expect(assignMock).not.toHaveBeenCalled()
  })

  it('paints each face from its own small rendition, not from the photograph', async () => {
    // The measured regression: this section drew its tiles by fetching one whole
    // `fit_1280` preview per face and cropping it in CSS — 290 of them on one
    // person's page.
    fetchMock.mockResolvedValue(outliers(2))
    renderOutliers()

    await screen.findAllByRole('button', { name: 'Not this person' })
    const images = screen
      .getAllByRole('link', { name: 'Open photo' })
      .map((link) => link.querySelector('img'))

    expect(images.map((img) => img?.getAttribute('src'))).toEqual([
      '/api/v1/photos/p0/face?box=0.1000%2C0.1000%2C0.2000%2C0.2000',
      '/api/v1/photos/p1/face?box=0.1000%2C0.1000%2C0.2000%2C0.2000',
    ])
    for (const img of images) {
      // Lazily, so a long ranking fetches only what the reader reaches.
      expect(img).toHaveAttribute('loading', 'lazy')
    }
  })

  it('never asks about a face whose picture cannot be produced', async () => {
    fetchMock.mockResolvedValue(outliers(OUTLIER_SECTION_BATCH + 1))
    renderOutliers()

    const links = await screen.findAllByRole('link', { name: 'Open photo' })
    expect(links).toHaveLength(OUTLIER_SECTION_BATCH)
    const broken = links[0].querySelector('img')
    if (broken === null) {
      throw new Error('the first face rendered no image')
    }
    fireEvent.error(broken)

    // The grey square leaves the section entirely and the next ranked face takes
    // the place it held — the batch stays full.
    await waitFor(() => {
      expect(screen.getAllByRole('link', { name: 'Open photo' })).toHaveLength(
        OUTLIER_SECTION_BATCH,
      )
    })
    expect(screen.getByRole('heading', { name: 'Is this still the same person?' })).toBeVisible()
  })

  it('keeps the answer quiet at rest and names the consequence only under the pointer', async () => {
    // jsdom has no cascade, so assert the stylesheet: the control is a link-shaped
    // button in the muted body colour, and the danger red is a hover state — a row
    // of filled red buttons is what made this section read as an error report.
    fetchMock.mockResolvedValue(outliers(1))
    renderOutliers()
    expect(await screen.findByRole('button', { name: 'Not this person' })).toHaveClass(
      'kk-outlier-answer',
      'btn-link',
    )

    const css = readCss('src/components/people/outliers.css')
    const rest = ruleBody(css, /\.kk-outlier-answer\.btn\s*(?=\{)/)
    expect(rest).toMatch(/--bs-btn-color:\s*var\(--bs-secondary-color\)/)
    expect(rest).not.toMatch(/--bs-btn-bg/)
    expect(css).toMatch(/--bs-btn-hover-color:\s*var\(--bs-danger\)/)
  })

  it('renders nothing at all for a person with no outliers', async () => {
    fetchMock.mockResolvedValue({
      subject_uid: 'su_a',
      count: 0,
      meaningful: false,
      avg_distance: 0,
      no_embedding: 0,
      faces: [],
    })
    const { container } = renderOutliers()

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalled()
    })
    await waitFor(() => {
      expect(container).toBeEmptyDOMElement()
    })
  })
})
