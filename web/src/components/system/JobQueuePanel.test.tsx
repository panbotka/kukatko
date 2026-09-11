import { render, screen } from '@testing-library/react'
import { I18nextProvider } from 'react-i18next'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'
import type { JobsStatus } from '../../services/system'

import { JobQueuePanel } from './JobQueuePanel'

/**
 * A queue with one type in every state at once — the whole colour scale visible
 * in a single row — beside a type that has nothing but history.
 */
function jobs(overrides: Partial<JobsStatus> = {}): JobsStatus {
  return {
    by_state: {},
    by_type: {},
    by_type_state: {
      face_detect: { queued: 12, running: 1, failed: 2, dead: 3, done: 41594 },
      thumbnail: { queued: 0, running: 0, failed: 0, dead: 0, done: 20930 },
    },
    total: 62542,
    dead_letter: 3,
    pending_embeddings: 0,
    ...overrides,
  }
}

function renderPanel(status: JobsStatus) {
  return render(
    <I18nextProvider i18n={i18n}>
      <JobQueuePanel jobs={status} onRequeue={vi.fn()} requeuing={null} />
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
})

describe('JobQueuePanel colour scale', () => {
  it('paints each state in the meaning it carries', () => {
    renderPanel(jobs())

    expect(screen.getByTestId('job-face_detect-queued')).toHaveClass('kk-count--queued')
    const running = screen.getByTestId('job-face_detect-running')
    expect(running).toHaveClass('kk-count--running')
    expect(running).toHaveClass('kk-count--running')
    // History must not compete with the states somebody can act on.
    expect(screen.getByTestId('job-face_detect-done')).toHaveClass('text-secondary')
    expect(screen.getByTestId('job-face_detect-done')).not.toHaveClass('kk-count--queued')
  })

  it('treats a dead letter and a retryable failure as the same bad news', () => {
    renderPanel(jobs())

    for (const state of ['failed', 'dead']) {
      const cell = screen.getByTestId(`job-face_detect-${state}`)
      expect(cell).toHaveClass('kk-count--failed')
      // The old rule painted `dead` amber and `failed` nothing at all; both are
      // danger now, and neither leans on colour alone to say so.
      expect(cell).not.toHaveClass('text-warning')
      expect(cell.querySelector('.bi-exclamation-triangle')).not.toBeNull()
    }
  })

  it('leaves every zero muted, uncoloured and unflagged', () => {
    renderPanel(jobs())

    for (const state of ['queued', 'running', 'failed', 'dead']) {
      const cell = screen.getByTestId(`job-thumbnail-${state}`)
      expect(cell).toHaveTextContent('0')
      expect(cell).toHaveClass('text-secondary')
      expect(cell.className).not.toMatch(/kk-count--(queued|running|failed)/)
      expect(cell).not.toHaveClass('kk-count--running')
      expect(cell.querySelector('.bi-exclamation-triangle')).toBeNull()
    }
  })

  it('keeps the row order, the per-row requeue button and the legend', () => {
    renderPanel(jobs())

    // Dead letter first, exactly as before the colours arrived.
    const rows = screen.getAllByTestId(/^job-row-/)
    expect(rows.map((row) => row.dataset.testid)).toEqual([
      'job-row-face_detect',
      'job-row-thumbnail',
    ])
    // One per-row button (only the row with a dead letter has one) plus the
    // whole-dead-letter button below the table.
    expect(screen.getByRole('button', { name: 'Retry' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Retry the permanently failed' })).toBeInTheDocument()
    // Every state the colours stand for is named twice in words: once as the
    // column header over the number, once in the legend below the table. Colour
    // is the third signal, never the first.
    expect(screen.getAllByText('Permanently failed')).toHaveLength(2)
    expect(screen.getAllByText('In progress')).toHaveLength(2)
  })
})
