import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'
import { type MergeResult, type Subject, type SubjectCount } from '../../services/people'

import { MergeSubjectModal } from './MergeSubjectModal'

vi.mock('../../services/people', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../services/people')>()
  return { ...actual, fetchSubjects: vi.fn(), mergeSubject: vi.fn() }
})

const { fetchSubjects, mergeSubject } = await import('../../services/people')
const subjectsMock = vi.mocked(fetchSubjects)
const mergeMock = vi.mocked(mergeSubject)

/** The person whose page the dialog was opened from — the one merged away. */
function source(): Subject {
  return {
    uid: 'su_a',
    slug: 'anna-n',
    name: 'Anna N.',
    type: 'person',
    favorite: false,
    private: false,
    notes: '',
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
  }
}

/** A listed person with counts, as `GET /subjects` returns one. */
function counted(uid: string, name: string, photos: number): SubjectCount {
  return {
    uid,
    slug: name.toLowerCase(),
    name,
    type: 'person',
    favorite: false,
    private: false,
    notes: '',
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    marker_count: photos + 1,
    photo_count: photos,
  }
}

function result(): MergeResult {
  return {
    keeper_uid: 'su_b',
    source_uid: 'su_a',
    markers_moved: 4,
    faces_moved: 4,
    confirmations_moved: 1,
    rejections_moved: 0,
    rejections_dropped: 1,
    dismissals_moved: 0,
    shared_photos: 1,
  }
}

function renderModal(onMerged = vi.fn(), onHide = vi.fn()) {
  render(
    <I18nextProvider i18n={i18n}>
      <MergeSubjectModal subject={source()} show onHide={onHide} onMerged={onMerged} />
    </I18nextProvider>,
  )
  return { onMerged, onHide }
}

/** Picks `name` in the typeahead, which advances the dialog to the confirmation. */
async function pick(user: ReturnType<typeof userEvent.setup>, name: string) {
  const field = await screen.findByRole('combobox', { name: 'Merge into…' })
  await user.type(field, name.slice(0, 3))
  await user.click(await screen.findByRole('option', { name: new RegExp(name) }))
}

describe('MergeSubjectModal', () => {
  beforeEach(async () => {
    await i18n.changeLanguage('en')
    subjectsMock.mockResolvedValue([
      counted('su_a', 'Anna N.', 3),
      counted('su_b', 'Anna Nováková', 42),
    ])
    mergeMock.mockResolvedValue(result())
  })

  it('never offers the person being merged as their own keeper', async () => {
    const user = userEvent.setup()
    renderModal()

    const field = await screen.findByRole('combobox', { name: 'Merge into…' })
    await user.type(field, 'Anna')

    expect(await screen.findByRole('option', { name: /Anna Nováková/ })).toBeInTheDocument()
    expect(screen.queryByRole('option', { name: /^Anna N\./ })).not.toBeInTheDocument()
  })

  it('confirms with both records and a warning before anything is merged', async () => {
    const user = userEvent.setup()
    renderModal()

    await pick(user, 'Anna Nováková')

    // Both people are shown with their counts, which is what makes the choice of
    // keeper an informed one.
    expect(screen.getByText('Will be deleted')).toBeInTheDocument()
    expect(screen.getByText('Will be kept')).toBeInTheDocument()
    expect(screen.getByText(/3 photos/)).toBeInTheDocument()
    expect(screen.getByText(/42 photos/)).toBeInTheDocument()
    expect(screen.getByText(/cannot be undone/)).toBeInTheDocument()
    expect(mergeMock).not.toHaveBeenCalled()
  })

  it('merges the page subject into the picked keeper and reports it back', async () => {
    const user = userEvent.setup()
    const { onMerged } = renderModal()

    await pick(user, 'Anna Nováková')
    await user.click(screen.getByRole('button', { name: 'Merge' }))

    await waitFor(() => {
      expect(mergeMock).toHaveBeenCalledWith('su_a', 'su_b')
    })
    expect(onMerged).toHaveBeenCalledWith(
      expect.objectContaining({ uid: 'su_b' }),
      expect.objectContaining({ markers_moved: 4 }),
    )
  })

  it('keeps the dialog open with an error when the merge fails', async () => {
    mergeMock.mockRejectedValue(new Error('boom'))
    const user = userEvent.setup()
    const { onMerged } = renderModal()

    await pick(user, 'Anna Nováková')
    await user.click(screen.getByRole('button', { name: 'Merge' }))

    expect(await screen.findByText(/merge failed/i)).toBeInTheDocument()
    expect(onMerged).not.toHaveBeenCalled()
  })

  it('goes back to the pick, so a wrong keeper is one click from being changed', async () => {
    const user = userEvent.setup()
    renderModal()

    await pick(user, 'Anna Nováková')
    await user.click(screen.getByRole('button', { name: 'Back to the pick' }))

    expect(screen.getByRole('combobox', { name: 'Merge into…' })).toBeInTheDocument()
    expect(screen.queryByText(/cannot be undone/)).not.toBeInTheDocument()
  })
})
