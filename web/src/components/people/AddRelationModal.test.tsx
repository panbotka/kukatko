import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'
import { type SubjectCount } from '../../services/people'

import { AddRelationModal } from './AddRelationModal'

vi.mock('../../services/people', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../services/people')>()
  return { ...actual, fetchSubjects: vi.fn() }
})

const { fetchSubjects } = await import('../../services/people')
const subjectsMock = vi.mocked(fetchSubjects)

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

/** Renders the dialog open on the parent row, over three other people. */
function renderModal() {
  render(
    <I18nextProvider i18n={i18n}>
      <AddRelationModal
        subjectUid="su0"
        subjectName="Jan Nečas"
        parents={[]}
        kind="parent"
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
