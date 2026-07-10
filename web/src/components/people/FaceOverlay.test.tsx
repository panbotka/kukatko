import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'
import { type FaceView } from '../../services/people'

import { FaceOverlay } from './FaceOverlay'

/** An unnamed detection and a named one, to cover both box styles. */
function faces(): FaceView[] {
  return [
    {
      face_index: 0,
      bbox: [0.1, 0.2, 0.3, 0.4],
      det_score: 0.9,
      action: 'create_marker',
      suggestions: [],
    },
    {
      face_index: 1,
      bbox: [0.5, 0.5, 0.2, 0.2],
      det_score: 0.8,
      action: 'assign_person',
      marker_uid: 'mk_1',
      subject_name: 'Alice',
      suggestions: [],
    },
  ]
}

function renderOverlay(readOnly = false, selected: number | null = null) {
  const onSelect = vi.fn()
  const result = render(
    <I18nextProvider i18n={i18n}>
      <FaceOverlay faces={faces()} selected={selected} onSelect={onSelect} readOnly={readOnly} />
    </I18nextProvider>,
  )
  return { ...result, onSelect }
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
})

describe('FaceOverlay', () => {
  it('draws no image of its own — only the boxes over the photo below', () => {
    const { container } = renderOverlay()

    expect(container.querySelector('img')).toBeNull()
    expect(screen.getAllByRole('button')).toHaveLength(2)
  })

  it('positions each face box from the normalized bbox', () => {
    renderOverlay()

    expect(screen.getByRole('button', { name: 'Unnamed face 1' })).toHaveStyle({
      left: '10%',
      top: '20%',
      width: '30%',
      height: '40%',
    })
  })

  it('names a matched face by its subject and leaves unmatched ones numbered', () => {
    renderOverlay()

    expect(screen.getByRole('button', { name: 'Alice' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Unnamed face 1' })).toBeInTheDocument()
  })

  it('selects a face on click and marks the selected box pressed', async () => {
    const user = userEvent.setup()
    const { onSelect } = renderOverlay()

    await user.click(screen.getByRole('button', { name: 'Unnamed face 1' }))
    expect(onSelect).toHaveBeenCalledWith(0)
  })

  it('marks the selected box pressed', () => {
    renderOverlay(false, 0)

    expect(screen.getByRole('button', { name: 'Unnamed face 1' })).toHaveAttribute(
      'aria-pressed',
      'true',
    )
    expect(screen.getByRole('button', { name: 'Alice' })).toHaveAttribute('aria-pressed', 'false')
  })

  it('is read-only and click-through for viewers', () => {
    const { onSelect } = renderOverlay(true)

    const box = screen.getByRole('button', { name: 'Unnamed face 1' })
    expect(box).toBeDisabled()
    // The box does not swallow clicks meant for the image underneath it.
    expect(box).toHaveStyle({ pointerEvents: 'none' })
    expect(onSelect).not.toHaveBeenCalled()
  })
})
