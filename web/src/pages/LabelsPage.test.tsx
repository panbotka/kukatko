import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
import i18n from '../i18n'
import { type Label, type LabelCount } from '../services/organize'

import { LabelsPage } from './LabelsPage'

vi.mock('../services/organize', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/organize')>()
  return {
    ...actual,
    fetchLabels: vi.fn(),
    createLabel: vi.fn(),
    updateLabel: vi.fn(),
    deleteLabel: vi.fn(),
  }
})

const { fetchLabels, createLabel, updateLabel, deleteLabel } = await import('../services/organize')
const fetchMock = vi.mocked(fetchLabels)
const createMock = vi.mocked(createLabel)
const updateMock = vi.mocked(updateLabel)
const deleteMock = vi.mocked(deleteLabel)

function label(uid: string, name: string, priority = 0, reviewEnabled = true): LabelCount {
  return {
    uid,
    slug: name.toLowerCase(),
    name,
    priority,
    review_enabled: reviewEnabled,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    photo_count: 5,
  }
}

function auth(canWrite: boolean): AuthContextValue {
  return {
    status: 'authenticated',
    user: { uid: 'u1', username: 'u', display_name: 'U', role: canWrite ? 'editor' : 'viewer' },
    role: canWrite ? 'editor' : 'viewer',
    downloadToken: null,
    canWrite,
    isAdmin: false,
    login: vi.fn(),
    logout: vi.fn(),
    refresh: vi.fn(),
  } as unknown as AuthContextValue
}

function renderPage(canWrite = true) {
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={auth(canWrite)}>
        <MemoryRouter>
          <LabelsPage />
        </MemoryRouter>
      </AuthContext.Provider>
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  fetchMock.mockReset()
  createMock.mockReset()
  updateMock.mockReset()
  deleteMock.mockReset()
})

describe('LabelsPage', () => {
  it('lists labels with their counts', async () => {
    fetchMock.mockResolvedValue([label('lb_1', 'Sunset')])
    renderPage()
    expect(await screen.findByText('Sunset')).toBeInTheDocument()
    expect(screen.getByText('5')).toBeInTheDocument()
  })

  it('creates a label: calls the API and adds it to the list', async () => {
    fetchMock.mockResolvedValue([])
    const created: Label = {
      uid: 'lb_new',
      slug: 'beach',
      name: 'Beach',
      priority: 0,
      review_enabled: true,
      created_at: '2026-01-01T00:00:00Z',
      updated_at: '2026-01-01T00:00:00Z',
    }
    createMock.mockResolvedValue(created)
    const user = userEvent.setup()
    renderPage()

    await screen.findByText('No labels yet')
    await user.click(screen.getByRole('button', { name: 'New label' }))
    await user.type(screen.getByLabelText('Name'), 'Beach')
    await user.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => {
      expect(createMock).toHaveBeenCalledWith({ name: 'Beach', priority: 0 })
    })
    expect(await screen.findByText('Beach')).toBeInTheDocument()
  })

  it('renames a label: calls the API and updates the list', async () => {
    fetchMock.mockResolvedValue([label('lb_1', 'Sunset')])
    updateMock.mockResolvedValue({
      uid: 'lb_1',
      slug: 'sundown',
      name: 'Sundown',
      priority: 0,
      review_enabled: true,
      created_at: '2026-01-01T00:00:00Z',
      updated_at: '2026-01-02T00:00:00Z',
    })
    const user = userEvent.setup()
    renderPage()

    await screen.findByText('Sunset')
    await user.click(screen.getByRole('button', { name: 'Rename' }))
    const input = screen.getByLabelText('Name')
    await user.clear(input)
    await user.type(input, 'Sundown')
    await user.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => {
      expect(updateMock).toHaveBeenCalledWith('lb_1', { name: 'Sundown', priority: 0 })
    })
    expect(await screen.findByText('Sundown')).toBeInTheDocument()
  })

  it('deletes a label after confirming in the styled dialog and drops it from the list', async () => {
    fetchMock.mockResolvedValue([label('lb_1', 'Sunset')])
    deleteMock.mockResolvedValue(undefined)
    const user = userEvent.setup()
    renderPage()

    await screen.findByText('Sunset')
    // The row control opens the dialog; nothing is deleted until it is confirmed.
    await user.click(screen.getByRole('button', { name: 'Delete' }))
    const dialog = await screen.findByRole('dialog')
    expect(deleteMock).not.toHaveBeenCalled()
    await user.click(within(dialog).getByRole('button', { name: 'Delete label' }))

    await waitFor(() => {
      expect(deleteMock).toHaveBeenCalledWith('lb_1')
    })
    await waitFor(() => {
      expect(screen.queryByText('Sunset')).not.toBeInTheDocument()
    })
  })

  it('keeps a long unbroken name inside the row on a phone', async () => {
    // A 360px row cannot fit a name plus two Czech-worded buttons: the name has
    // to be allowed to shrink (min-width:0) and truncate, and the actions may
    // not be squeezed, or the whole page scrolls sideways.
    const long = 'Fotky'.repeat(20)
    fetchMock.mockResolvedValue([label('lb_1', long)])
    renderPage()

    const name = await screen.findByText(long)
    expect(name).toHaveClass('text-truncate')
    const link = name.closest('a')
    expect(link).toHaveClass('kk-min-w-0')
    // The count rides along beside the truncated name and keeps its own width.
    expect(within(link as HTMLElement).getByText('5')).toHaveClass('flex-shrink-0')
    expect(screen.getByRole('button', { name: 'Rename' }).parentElement).toHaveClass(
      'flex-shrink-0',
    )
  })

  it('collapses the row actions to their glyph below the sm breakpoint', async () => {
    // Icon + a word that hides itself on a narrow viewport; the `aria-label`
    // keeps the accessible name identical at every width.
    fetchMock.mockResolvedValue([label('lb_1', 'Sunset')])
    renderPage()

    await screen.findByText('Sunset')
    for (const [name, glyph] of [
      ['Rename', 'bi-pencil'],
      ['Delete', 'bi-trash'],
    ]) {
      const button = screen.getByRole('button', { name })
      expect(button.querySelector(`.${glyph}`)).toHaveAttribute('aria-hidden', 'true')
      expect(within(button).getByText(name)).toHaveClass('d-none', 'd-sm-inline')
      expect(button).toHaveClass('kukatko-tap-target-touch')
    }
  })

  it('hides mutation controls from viewers', async () => {
    fetchMock.mockResolvedValue([label('lb_1', 'Sunset')])
    renderPage(false)
    await screen.findByText('Sunset')
    expect(screen.queryByRole('button', { name: 'New label' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Rename' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Delete' })).not.toBeInTheDocument()
    expect(queryReviewSwitch('Sunset')).toBeNull()
  })

  describe('the review-game switch', () => {
    it('reflects each label’s current setting', async () => {
      fetchMock.mockResolvedValue([label('lb_1', 'Sunset'), label('lb_2', 'Cats', 0, false)])
      renderPage()

      await screen.findByText('Sunset')
      expect(reviewSwitch('Sunset')).toBeChecked()
      expect(reviewSwitch('Cats')).not.toBeChecked()
    })

    it('switches a label out of the game, carrying its other fields across', async () => {
      // The endpoint takes the whole editable record, so the toggle has to send
      // the name and priority back unchanged or it would rewrite them.
      fetchMock.mockResolvedValue([label('lb_1', 'Sunset', 3)])
      updateMock.mockResolvedValue({
        uid: 'lb_1',
        slug: 'sunset',
        name: 'Sunset',
        priority: 3,
        review_enabled: false,
        created_at: '2026-01-01T00:00:00Z',
        updated_at: '2026-01-02T00:00:00Z',
      })
      const user = userEvent.setup()
      renderPage()

      await screen.findByText('Sunset')
      await user.click(reviewSwitch('Sunset'))

      await waitFor(() => {
        expect(updateMock).toHaveBeenCalledWith('lb_1', {
          name: 'Sunset',
          priority: 3,
          review_enabled: false,
        })
      })
      expect(reviewSwitch('Sunset')).not.toBeChecked()
    })

    it('switches a label back into the game', async () => {
      fetchMock.mockResolvedValue([label('lb_1', 'Sunset', 0, false)])
      updateMock.mockResolvedValue({
        uid: 'lb_1',
        slug: 'sunset',
        name: 'Sunset',
        priority: 0,
        review_enabled: true,
        created_at: '2026-01-01T00:00:00Z',
        updated_at: '2026-01-02T00:00:00Z',
      })
      const user = userEvent.setup()
      renderPage()

      await screen.findByText('Sunset')
      await user.click(reviewSwitch('Sunset'))

      await waitFor(() => {
        expect(updateMock).toHaveBeenCalledWith('lb_1', {
          name: 'Sunset',
          priority: 0,
          review_enabled: true,
        })
      })
      expect(reviewSwitch('Sunset')).toBeChecked()
    })

    it('rolls the switch back and says so when the save fails', async () => {
      // A switch that stays flipped after a failed save is a lie: the operator
      // would walk away believing the label is out of the game when it is not.
      fetchMock.mockResolvedValue([label('lb_1', 'Sunset')])
      updateMock.mockRejectedValue(new Error('boom'))
      const user = userEvent.setup()
      renderPage()

      await screen.findByText('Sunset')
      await user.click(reviewSwitch('Sunset'))

      expect(await screen.findByText('The action failed. Please try again.')).toBeInTheDocument()
      expect(reviewSwitch('Sunset')).toBeChecked()
    })

    it('is wordless, carrying the game’s icon and an accessible name', async () => {
      // A per-row sentence would repeat itself down the whole page, so the glyph
      // carries the meaning — and the accessible name has to name both the label
      // and what the switch does, since the glyph says neither out loud.
      fetchMock.mockResolvedValue([label('lb_1', 'Sunset')])
      renderPage()

      await screen.findByText('Sunset')
      // The hover text says which way the switch currently sits, since the glyph
      // is the same either way, and it wraps the control it describes.
      const wrapper = screen.getByTitle('The review game asks about this label')
      expect(wrapper).toContainElement(reviewSwitch('Sunset'))
      expect(wrapper.querySelector('.bi-ui-checks')).toHaveAttribute('aria-hidden', 'true')
      expect(wrapper).not.toHaveTextContent(/\w/)
    })
  })
})

/** The accessible name the review-game switch carries for a given label. */
function reviewSwitchName(name: string): string {
  return `Ask about the label “${name}” in the review game`
}

/** Finds a label row's review-game switch by the label it is about. */
function reviewSwitch(name: string): HTMLElement {
  return screen.getByRole('checkbox', { name: reviewSwitchName(name) })
}

/** Like {@link reviewSwitch}, but yields null where no switch is rendered. */
function queryReviewSwitch(name: string): HTMLElement | null {
  return screen.queryByRole('checkbox', { name: reviewSwitchName(name) })
}
