import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { I18nextProvider } from 'react-i18next'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'
import { NEUTRAL_EDIT } from '../../lib/photoEdit'
import { type PhotoEdit } from '../../services/photos'

import { EditPanel } from './EditPanel'

vi.mock('../../services/photos', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../services/photos')>()
  return { ...actual, saveEdit: vi.fn() }
})

const { saveEdit } = await import('../../services/photos')
const saveEditMock = vi.mocked(saveEdit)

/**
 * The panel is a controlled component — the page owns the edit and previews it on
 * the photo — so the test owns it too. Holding the state here is what lets a
 * rotation be read back as the angle the panel displays, rather than as an
 * updater function nobody applied.
 */
function Harness({ initial = NEUTRAL_EDIT }: { initial?: PhotoEdit }) {
  const [edit, setEdit] = useState<PhotoEdit>(initial)
  return (
    <EditPanel
      uid="p1"
      edit={edit}
      onChange={(update) => {
        setEdit(update)
      }}
      onSaved={vi.fn()}
      onClose={vi.fn()}
    />
  )
}

function renderPanel(initial?: PhotoEdit) {
  render(
    <I18nextProvider i18n={i18n}>
      <Harness initial={initial} />
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  vi.clearAllMocks()
})

describe('EditPanel rotation', () => {
  it('offers both directions as labelled buttons', () => {
    renderPanel()

    // The icons are decorative (aria-hidden), so the direction has to be on the
    // button itself or the control is unreachable by name.
    expect(screen.getByRole('button', { name: 'Rotate left' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Rotate right' })).toBeInTheDocument()
    expect(screen.getByText('0°')).toBeInTheDocument()
    // And the header's X, the panel's third icon-only control, answers a hover
    // with the same sentence rather than with nothing.
    expect(screen.getByRole('button', { name: 'Close the edits panel' })).toHaveAttribute(
      'title',
      'Close the edits panel',
    )
  })

  it('gives both icon buttons a finger-sized hit area on touch', () => {
    renderPanel()

    // An icon-only `btn-sm` is ~32px wide; the app-wide coarse-pointer floor lifts
    // a `.btn` to 44px tall but not wide (`app.css`, guarded by
    // `styles/tapTargets.test.ts`), and this panel is a bottom sheet on a phone.
    for (const name of ['Rotate left', 'Rotate right']) {
      expect(screen.getByRole('button', { name })).toHaveClass('kukatko-tap-target-touch')
    }
  })

  it('turns clockwise a quarter at a time and wraps at a full turn', async () => {
    const user = userEvent.setup()
    renderPanel()

    const right = screen.getByRole('button', { name: 'Rotate right' })
    await user.click(right)
    expect(screen.getByText('90°')).toBeInTheDocument()
    await user.click(right)
    await user.click(right)
    await user.click(right)
    expect(screen.getByText('0°')).toBeInTheDocument()
  })

  it('turns counter-clockwise into 270°, never a negative angle', async () => {
    const user = userEvent.setup()
    renderPanel()

    await user.click(screen.getByRole('button', { name: 'Rotate left' }))
    // -90 is not one of the four rotations the API accepts, so the wrap matters.
    expect(screen.getByText('270°')).toBeInTheDocument()
    expect(screen.queryByText('-90°')).not.toBeInTheDocument()
  })

  it('undoes a wrong turn with one press of the other direction', async () => {
    const user = userEvent.setup()
    renderPanel()

    await user.click(screen.getByRole('button', { name: 'Rotate right' }))
    await user.click(screen.getByRole('button', { name: 'Rotate left' }))
    expect(screen.getByText('0°')).toBeInTheDocument()
  })

  it('saves the turned edit, not the one the panel opened with', async () => {
    saveEditMock.mockResolvedValue({ ...NEUTRAL_EDIT, rotation: 90 })
    const user = userEvent.setup()
    renderPanel()

    await user.click(screen.getByRole('button', { name: 'Rotate right' }))
    await user.click(screen.getByRole('button', { name: 'Save edits' }))

    await waitFor(() => {
      expect(saveEditMock).toHaveBeenCalledWith('p1', { ...NEUTRAL_EDIT, rotation: 90 })
    })
  })
})
