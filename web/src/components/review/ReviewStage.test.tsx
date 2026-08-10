import { render, screen } from '@testing-library/react'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it } from 'vitest'

import i18n from '../../i18n'
import { frameRatio, loadImageAs } from '../../test/imageFrame'

import { ReviewStage, type ReviewStageProps } from './ReviewStage'

/** A landscape photo whose catalogue row agrees with the file. */
const BASE: ReviewStageProps = {
  photoUid: 'ph1',
  fileWidth: 4000,
  fileHeight: 3000,
  orientation: 1,
  size: 'fit_1280',
  alt: 'a photo',
}

function renderStage(props: Partial<ReviewStageProps> = {}) {
  return render(
    <I18nextProvider i18n={i18n}>
      <MemoryRouter>
        <ReviewStage {...BASE} {...props} />
      </MemoryRouter>
    </I18nextProvider>,
  )
}

/** The stage's `<img>`, which every test either measures or inspects. */
function stageImage(): HTMLImageElement {
  return screen.getByRole('img', { name: 'a photo' })
}

describe('ReviewStage', () => {
  it('draws no face box until the photo has actually been measured', () => {
    // A box placed against the catalogue row's estimate lands off the face on a
    // row with a transposed dimension pair, and then visibly jumps once the real
    // frame arrives. Nothing is drawn until the pixels are known.
    renderStage({ bbox: [0.4, 0.3, 0.2, 0.2] })
    expect(screen.queryByTestId('review-bbox')).toBeNull()

    loadImageAs(stageImage(), 4000, 3000)

    expect(screen.getByTestId('review-bbox')).toBeInTheDocument()
  })

  it('takes the frame from the loaded photo, not from the catalogue row', () => {
    // The row here claims portrait; the file is landscape. The loaded image wins.
    const { container } = renderStage({ fileWidth: 3000, fileHeight: 4000 })

    loadImageAs(stageImage(), 4000, 3000)

    expect(frameRatio(container.querySelector<HTMLElement>('.review-photo'))).toBeCloseTo(
      4000 / 3000,
      5,
    )
  })

  it('pads the drawn rectangle out around the face', () => {
    // A tight face crop is unrecognisable — you judge a person from the hair, the
    // shoulders and who is standing next to them.
    renderStage({ bbox: [0.4, 0.4, 0.1, 0.1] })
    loadImageAs(stageImage(), 4000, 3000)

    const box = screen.getByTestId('review-bbox')

    expect(Number.parseFloat(box.style.width)).toBeGreaterThan(10)
    expect(Number.parseFloat(box.style.left)).toBeLessThan(40)
  })

  it('leads out to the photo through a corner anchor, never through the photo', () => {
    // The preview carries the face rectangle, so a click into it must never be
    // ambiguous: enlarging owns the photo, leaving owns the corner control.
    renderStage({ href: '/photos/ph1' })

    const open = screen.getByTestId('review-open-photo')

    expect(open).toHaveAttribute('target', '_blank')
    expect(open).toHaveAttribute('rel', expect.stringContaining('noopener'))
    expect(open).toHaveAttribute('href', '/photos/ph1')
    expect(stageImage().closest('a')).toBeNull()
  })

  it('offers no way out when there is nowhere to go', () => {
    renderStage()

    expect(screen.queryByTestId('review-open-photo')).toBeNull()
  })
})
