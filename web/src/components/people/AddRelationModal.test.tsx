import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'
import { type AddRelationResult, type Relative } from '../../services/family'
import { type SubjectCount } from '../../services/people'

import { AddRelationModal, type RelationKind } from './AddRelationModal'

vi.mock('../../services/people', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../services/people')>()
  return { ...actual, fetchSubjects: vi.fn() }
})

vi.mock('../../services/family', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../services/family')>()
  return { ...actual, addRelation: vi.fn() }
})

const { fetchSubjects } = await import('../../services/people')
const subjectsMock = vi.mocked(fetchSubjects)

const { addRelation } = await import('../../services/family')
const addRelationMock = vi.mocked(addRelation)

/**
 * Points `window.matchMedia` at a fixed phone/desktop answer, driving the
 * `useAnchoredMenu` hook the dialog's field uses to place its suggestion list.
 */
function mockViewport(narrow: boolean): void {
  window.matchMedia = vi.fn().mockImplementation((query: string) => ({
    matches: narrow,
    media: query,
    onchange: null,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    addListener: vi.fn(),
    removeListener: vi.fn(),
    dispatchEvent: vi.fn(),
  }))
}

/** A listed person, as `GET /subjects` returns one. */
function counted(uid: string, name: string): SubjectCount {
  return {
    uid,
    slug: name.toLowerCase(),
    name,
    nickname: '',
    type: 'person',
    favorite: false,
    private: false,
    notes: '',
    birth_year: null,
    death_year: null,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    marker_count: 2,
    photo_count: 2,
  }
}

/** A recorded relative, in the shape the strip renders one. */
function related(uid: string, name: string): Relative {
  return {
    uid,
    slug: name.toLowerCase(),
    name,
    type: 'person',
    birth_year: null,
    death_year: null,
    photo_count: 0,
  }
}

/**
 * Renders the dialog open on the given row, over three other people. `parents`
 * is what makes the sibling row's copy differ: a person whose parents nobody
 * recorded is no longer a dead end, only a different sentence.
 */
function renderModal(kind: RelationKind = 'parent', parents: Relative[] = []) {
  render(
    <I18nextProvider i18n={i18n}>
      <AddRelationModal
        subjectUid="su0"
        subjectName="Jan Nečas"
        parents={parents}
        kind={kind}
        show
        onHide={vi.fn()}
        onAdded={vi.fn()}
      />
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  subjectsMock.mockResolvedValue([
    counted('su1', 'Bohumil Nečas'),
    counted('su2', 'Marie Nečasová'),
    counted('su3', 'Petr Doležal'),
  ])
  addRelationMock.mockResolvedValue({
    family: {
      uid: 'fm1',
      partner_a_uid: null,
      partner_b_uid: null,
      kind: 'unknown',
      from_year: null,
      to_year: null,
      note: '',
      created_at: '2026-01-01T00:00:00Z',
      updated_at: '2026-01-01T00:00:00Z',
    },
    relative: related('su1', 'Bohumil Nečas'),
    created: false,
  } satisfies AddRelationResult)
})

afterEach(() => {
  // Restore the shared desktop default so a phone test never leaks into the next.
  mockViewport(false)
})

describe('AddRelationModal', () => {
  it('gives a phone the whole screen, so the field, its list and the keyboard fit', async () => {
    mockViewport(true)
    renderModal()

    await waitFor(() => {
      expect(subjectsMock).toHaveBeenCalled()
    })
    const dialog = screen.getByRole('dialog')
    expect(dialog.querySelector('.modal-dialog')).toHaveClass('modal-fullscreen-sm-down')
  })

  it('records a sibling of somebody with no parents, in one request on the subject', async () => {
    const user = userEvent.setup()
    renderModal('sibling')

    await waitFor(() => {
      expect(subjectsMock).toHaveBeenCalled()
    })
    // The dead-end alert is gone: the field is there, and the copy says where the
    // pair will hang instead of what to record first.
    expect(screen.getByRole('combobox')).toBeInTheDocument()
    expect(screen.getByText(/family with no parents in it/)).toBeInTheDocument()

    await user.type(screen.getByRole('combobox'), 'nečas')
    await user.click(screen.getByRole('option', { name: /Bohumil Nečas/ }))

    await waitFor(() => {
      expect(addRelationMock).toHaveBeenCalledTimes(1)
    })
    // On the subject itself, with the sibling role — not a `child` posted once
    // per parent, which is what this dialog used to do and could not do at all
    // for somebody whose parents nobody wrote down.
    expect(addRelationMock).toHaveBeenCalledWith('su0', { role: 'sibling', subject_uid: 'su1' })
  })

  it('names the parents a sibling will hang off when the subject has some', async () => {
    renderModal('sibling', [related('su2', 'Marie Nečasová')])

    await waitFor(() => {
      expect(subjectsMock).toHaveBeenCalled()
    })
    expect(screen.getByText(/a child of the parents: Marie Nečasová/)).toBeInTheDocument()
  })

  it('shows the whole suggestion list, which the scrollable body used to clip', async () => {
    const user = userEvent.setup()
    renderModal()

    await waitFor(() => {
      expect(subjectsMock).toHaveBeenCalled()
    })
    await user.type(screen.getByRole('combobox'), 'nečas')

    // The body still scrolls (the dialog is `scrollable`), but the list is no
    // longer its child's absolute box: it is a fixed overlay measured off the
    // input, so no ancestor's `overflow: auto` applies to it.
    const dialog = screen.getByRole('dialog')
    expect(dialog.querySelector('.modal-dialog')).toHaveClass('modal-dialog-scrollable')
    const listbox = screen.getByRole('listbox', { name: 'Person’s name' })
    expect(listbox).toHaveStyle({ position: 'fixed' })
    // Both matches plus the "create this name" row, all of them reachable.
    expect(screen.getAllByRole('option').map((row) => row.textContent)).toEqual([
      'Bohumil Nečas2',
      'Marie Nečasová2',
      'Create “nečas”',
    ])
  })
})
