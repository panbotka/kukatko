import { type ComponentProps } from 'react'

import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'
import { type SavedSearch } from '../../services/savedSearches'
import { expectLive, expectOff } from '../../test/reasoned'

import { SaveSearchModal } from './SaveSearchModal'

vi.mock('../../services/savedSearches', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../services/savedSearches')>()
  return { ...actual, createSavedSearch: vi.fn(), updateSavedSearch: vi.fn() }
})

const { createSavedSearch, updateSavedSearch } = await import('../../services/savedSearches')
const createMock = vi.mocked(createSavedSearch)
const updateMock = vi.mocked(updateSavedSearch)

const saved: SavedSearch = {
  uid: 'ss1',
  name: 'Beach',
  params: { q: 'sea' },
  created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-01-01T00:00:00Z',
}

const onSaved = vi.fn()
const onHide = vi.fn()

function renderModal(props: Partial<ComponentProps<typeof SaveSearchModal>> = {}) {
  return render(
    <I18nextProvider i18n={i18n}>
      <SaveSearchModal show params={{ q: 'sea' }} onHide={onHide} onSaved={onSaved} {...props} />
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  createMock.mockReset()
  updateMock.mockReset()
  onSaved.mockReset()
  onHide.mockReset()
})

describe('SaveSearchModal', () => {
  it('stores the current view under the typed name', async () => {
    createMock.mockResolvedValue(saved)
    const user = userEvent.setup()
    renderModal()

    await user.type(screen.getByLabelText('Name'), 'Beach')
    await user.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => {
      expect(createMock).toHaveBeenCalledWith('Beach', { q: 'sea' })
    })
    expect(onSaved).toHaveBeenCalledWith(saved)
  })

  it('rejects an empty name without calling the API', async () => {
    const user = userEvent.setup()
    renderModal()

    await user.click(screen.getByRole('button', { name: 'Save' }))

    expect(createMock).not.toHaveBeenCalled()
    expect(screen.getByText('Could not save. Check the name and try again.')).toBeInTheDocument()
  })

  it('says the save is running rather than greying both buttons in silence', async () => {
    let settle: (search: SavedSearch) => void = () => undefined
    createMock.mockReturnValue(
      new Promise<SavedSearch>((resolve) => {
        settle = resolve
      }),
    )
    const user = userEvent.setup()
    renderModal()

    const busy = 'The view is being saved — one moment.'
    await user.type(screen.getByLabelText('Name'), 'Beach')
    expectLive(screen.getByRole('button', { name: 'Save' }))

    await user.click(screen.getByRole('button', { name: 'Save' }))
    expectOff(screen.getByRole('button', { name: 'Save' }), busy)
    expectOff(screen.getByRole('button', { name: 'Cancel' }), busy)

    settle(saved)
    await waitFor(() => {
      expect(onSaved).toHaveBeenCalled()
    })
  })

  it('does not dismiss the dialog from an off Cancel button', async () => {
    createMock.mockReturnValue(new Promise<SavedSearch>(() => undefined))
    const user = userEvent.setup()
    renderModal()

    await user.type(screen.getByLabelText('Name'), 'Beach')
    await user.click(screen.getByRole('button', { name: 'Save' }))
    await user.click(screen.getByRole('button', { name: 'Cancel' }))

    expect(onHide).not.toHaveBeenCalled()
  })

  it('renames an existing saved search, leaving its stored params alone', async () => {
    updateMock.mockResolvedValue({ ...saved, name: 'Sea' })
    const user = userEvent.setup()
    renderModal({ search: saved })

    const field = screen.getByLabelText('Name')
    expect(field).toHaveValue('Beach')
    await user.clear(field)
    await user.type(field, 'Sea')
    await user.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => {
      expect(updateMock).toHaveBeenCalledWith('ss1', { name: 'Sea' })
    })
    expect(createMock).not.toHaveBeenCalled()
  })
})
