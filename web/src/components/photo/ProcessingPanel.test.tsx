import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'
import { ApiError } from '../../services/auth'
import { type PhotoProcessing } from '../../services/photos'

import { ProcessingPanel } from './ProcessingPanel'

vi.mock('../../services/photos', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../services/photos')>()
  return { ...actual, runProcessingStep: vi.fn() }
})

const { runProcessingStep } = await import('../../services/photos')
const runProcessingStepMock = vi.mocked(runProcessingStep)

/** A full report, one entry per step, in the order the backend sends them. */
function report(overrides: Partial<Record<PhotoProcessing['step'], PhotoProcessing>> = {}) {
  const base: PhotoProcessing[] = [
    { step: 'metadata', state: 'done', at: '2026-08-17T10:00:00Z' },
    { step: 'thumbnail', state: 'done', at: '2026-08-17T10:01:00Z' },
    { step: 'image_embed', state: 'queued' },
    { step: 'face_detect', state: 'failed', error: 'the box refused the image' },
    { step: 'ocr', state: 'pending' },
    { step: 'places', state: 'skipped' },
    { step: 'sidecar', state: 'running' },
  ]
  return base.map((row) => overrides[row.step] ?? row)
}

function renderPanel(steps: PhotoProcessing[], canRun = false) {
  render(
    <I18nextProvider i18n={i18n}>
      <ProcessingPanel uid="p1" steps={steps} canRun={canRun} />
    </I18nextProvider>,
  )
}

/** Returns the list item whose step label matches, so a row can be asserted on. */
function row(label: string | RegExp): HTMLElement {
  const item = screen.getByText(label).closest('li')
  if (item === null) {
    throw new Error(`no row for ${String(label)}`)
  }
  return item
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  vi.clearAllMocks()
})

describe('ProcessingPanel', () => {
  it('lists every step with the state it is in', () => {
    renderPanel(report())

    expect(
      within(row('Metadata from the file')).getByText(/17\/08\/2026|8\/17\/2026|2026/),
    ).toBeInTheDocument()
    expect(
      within(row('Semantic fingerprint')).getByText('Waiting in the queue'),
    ).toBeInTheDocument()
    expect(within(row('Face detection')).getByText('Failed')).toBeInTheDocument()
    expect(within(row('Text in the photo')).getByText('Not run yet')).toBeInTheDocument()
    expect(within(row('Place lookup')).getByText('Not applicable')).toBeInTheDocument()
    expect(within(row('Metadata sidecar')).getByText('Running')).toBeInTheDocument()
  })

  it('shows why a failed step failed', () => {
    renderPanel(report())

    expect(within(row('Face detection')).getByText('the box refused the image')).toBeInTheDocument()
  })

  it('reads an empty result as a success, not as a gap', () => {
    renderPanel(
      report({
        face_detect: {
          step: 'face_detect',
          state: 'done',
          at: '2026-08-17T10:00:00Z',
          face_count: 0,
        },
        ocr: { step: 'ocr', state: 'done', at: '2026-08-17T10:00:00Z', text_found: false },
      }),
    )

    expect(within(row('Face detection')).getByText(/0 faces/)).toBeInTheDocument()
    expect(within(row('Text in the photo')).getByText(/no text/)).toBeInTheDocument()
  })

  it('offers no run button to a non-maintainer', () => {
    renderPanel(report())

    expect(screen.queryByRole('button', { name: /run now/i })).not.toBeInTheDocument()
  })

  it('offers a maintainer a run button on every step that is neither done nor skipped', () => {
    renderPanel(report(), true)

    // queued, failed, pending, running — but not the two done steps nor the
    // skipped one.
    expect(screen.getAllByRole('button', { name: /run now/i })).toHaveLength(4)
    expect(within(row('Metadata from the file')).queryByRole('button')).not.toBeInTheDocument()
    expect(within(row('Place lookup')).queryByRole('button')).not.toBeInTheDocument()
    expect(
      within(row('Text in the photo')).getByRole('button', { name: /run now/i }),
    ).toBeInTheDocument()
  })

  it('schedules the step and refreshes its row', async () => {
    runProcessingStepMock.mockResolvedValue({ step: 'ocr', state: 'queued' })
    const user = userEvent.setup()
    renderPanel(report(), true)

    await user.click(within(row('Text in the photo')).getByRole('button', { name: /run now/i }))

    await waitFor(() => {
      expect(runProcessingStepMock).toHaveBeenCalledWith('p1', 'ocr')
    })
    expect(within(row('Text in the photo')).getByText('Waiting in the queue')).toBeInTheDocument()
  })

  it('explains a refusal rather than silently doing nothing', async () => {
    runProcessingStepMock.mockRejectedValue(new ApiError(409, 'nope'))
    const user = userEvent.setup()
    renderPanel(report(), true)

    await user.click(within(row('Text in the photo')).getByRole('button', { name: /run now/i }))

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'This step does not apply to this photo.',
    )
  })

  it('reports a failed request', async () => {
    runProcessingStepMock.mockRejectedValue(new ApiError(500, 'boom'))
    const user = userEvent.setup()
    renderPanel(report(), true)

    await user.click(within(row('Text in the photo')).getByRole('button', { name: /run now/i }))

    expect(await screen.findByRole('alert')).toHaveTextContent('Could not schedule the step.')
  })
})
