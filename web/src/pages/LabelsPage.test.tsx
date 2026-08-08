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

/** The four numbered labels a folded family needs (`LABEL_FAMILY_MIN`). */
function houseNumbers(): LabelCount[] {
  return [
    label('lb_d11', 'Dum11'),
    label('lb_d12', 'Dum12'),
    label('lb_d20', 'Dum20'),
    label('lb_d4', 'Dum4'),
  ]
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

function renderPage(canWrite = true, url = '/labels') {
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={auth(canWrite)}>
        <MemoryRouter initialEntries={[url]}>
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
  it('draws the labels as a cloud of chips with their counts', async () => {
    fetchMock.mockResolvedValue([label('lb_1', 'Sunset')])
    renderPage()

    const cloud = await screen.findByRole('list', { name: 'Labels' })
    const chip = within(cloud).getByRole('link', { name: 'Sunset, 5 photos' })
    expect(chip).toHaveAttribute('href', '/labels/lb_1')
    expect(within(chip).getByText('5')).toBeInTheDocument()
  })

  it('opens on the most-used labels rather than on the alphabet', async () => {
    // The whole point: an alphabetical index puts the numbered house-number
    // labels first and pushes the meaningful ones off the screen.
    fetchMock.mockResolvedValue([
      { ...label('lb_1', 'Zima'), photo_count: 2 },
      { ...label('lb_2', 'Ales'), photo_count: 40 },
      { ...label('lb_3', 'Beach'), photo_count: 9 },
    ])
    renderPage()

    await screen.findByRole('list', { name: 'Labels' })
    expect(chipNames()).toEqual(['Ales', 'Beach', 'Zima'])
  })

  it('reorders alphabetically on demand, and back', async () => {
    fetchMock.mockResolvedValue([
      { ...label('lb_1', 'Zima'), photo_count: 40 },
      { ...label('lb_2', 'Ales'), photo_count: 2 },
      { ...label('lb_3', 'Beach'), photo_count: 9 },
    ])
    const user = userEvent.setup()
    renderPage()

    await screen.findByRole('list', { name: 'Labels' })
    expect(chipNames()).toEqual(['Zima', 'Beach', 'Ales'])

    await user.selectOptions(screen.getByLabelText('Sort'), 'name')
    expect(chipNames()).toEqual(['Ales', 'Beach', 'Zima'])

    await user.selectOptions(screen.getByLabelText('Sort'), 'count')
    expect(chipNames()).toEqual(['Zima', 'Beach', 'Ales'])
  })

  it('searches label names, folded, and says so when nothing matches', async () => {
    fetchMock.mockResolvedValue([label('lb_1', 'Dovolená'), label('lb_2', 'Hory')])
    const user = userEvent.setup()
    renderPage()

    await screen.findByRole('list', { name: 'Labels' })
    await user.type(screen.getByLabelText('Search labels'), 'dovolena')
    expect(chipNames()).toEqual(['Dovolená'])

    await user.clear(screen.getByLabelText('Search labels'))
    await user.type(screen.getByLabelText('Search labels'), 'nic')
    expect(screen.getByText('No label matches')).toBeInTheDocument()
    expect(screen.queryByRole('list', { name: 'Labels' })).not.toBeInTheDocument()
  })

  it('restores the view the URL carries', async () => {
    fetchMock.mockResolvedValue([label('lb_1', 'Dovolená'), label('lb_2', 'Hory')])
    renderPage(true, '/labels?q=hory')

    await screen.findByRole('list', { name: 'Labels' })
    expect(chipNames()).toEqual(['Hory'])
    expect(screen.getByLabelText('Search labels')).toHaveValue('hory')
  })

  describe('numbered families', () => {
    it('folds them into one chip instead of flooding the cloud', async () => {
      fetchMock.mockResolvedValue([...houseNumbers(), label('lb_1', 'Sunset')])
      renderPage()

      await screen.findByRole('list', { name: 'Labels' })
      expect(screen.getByRole('button', { name: 'Group Dum — 4 labels' })).toHaveAttribute(
        'aria-expanded',
        'false',
      )
      expect(chipNames()).toEqual(['Sunset'])
    })

    it('expands to its members and folds back', async () => {
      fetchMock.mockResolvedValue(houseNumbers())
      const user = userEvent.setup()
      renderPage()

      await screen.findByRole('list', { name: 'Labels' })
      const toggle = screen.getByRole('button', { name: 'Group Dum — 4 labels' })
      await user.click(toggle)

      expect(toggle).toHaveAttribute('aria-expanded', 'true')
      const members = screen.getByRole('list', { name: 'Labels in the group Dum' })
      // The fixture's counts are equal, so the name breaks the tie — numerically,
      // which is what puts 4 ahead of 11 rather than after 20.
      expect(within(members).getAllByRole('link').map(chipName)).toEqual([
        'Dum4',
        'Dum11',
        'Dum12',
        'Dum20',
      ])

      await user.click(toggle)
      expect(
        screen.queryByRole('list', { name: 'Labels in the group Dum' }),
      ).not.toBeInTheDocument()
    })

    it('dissolves them while a search is running', async () => {
      fetchMock.mockResolvedValue(houseNumbers())
      const user = userEvent.setup()
      renderPage()

      await screen.findByRole('list', { name: 'Labels' })
      await user.type(screen.getByLabelText('Search labels'), 'dum1')

      expect(screen.queryByRole('button', { name: /^Group Dum/ })).not.toBeInTheDocument()
      expect(chipNames()).toEqual(['Dum11', 'Dum12'])
    })

    it('opens the ones the URL asks for', async () => {
      fetchMock.mockResolvedValue(houseNumbers())
      renderPage(true, '/labels?open=dum')

      await screen.findByRole('list', { name: 'Labels in the group Dum' })
      expect(screen.getByRole('button', { name: 'Group Dum — 4 labels' })).toHaveAttribute(
        'aria-expanded',
        'true',
      )
    })
  })

  describe('the editor actions', () => {
    it('creates a label: calls the API and adds it to the cloud', async () => {
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

    it('expands the family a new numbered label lands in, so it is not swallowed', async () => {
      // A brand-new `Dum99` joining a folded family would otherwise disappear the
      // moment it is saved, which reads as a failed save.
      fetchMock.mockResolvedValue(houseNumbers())
      createMock.mockResolvedValue({
        uid: 'lb_d99',
        slug: 'dum99',
        name: 'Dum99',
        priority: 0,
        review_enabled: true,
        created_at: '2026-01-01T00:00:00Z',
        updated_at: '2026-01-01T00:00:00Z',
      })
      const user = userEvent.setup()
      renderPage()

      await screen.findByRole('list', { name: 'Labels' })
      await user.click(screen.getByRole('button', { name: 'New label' }))
      await user.type(screen.getByLabelText('Name'), 'Dum99')
      await user.click(screen.getByRole('button', { name: 'Save' }))

      const members = await screen.findByRole('list', { name: 'Labels in the group Dum' })
      expect(within(members).getAllByRole('link').map(chipName)).toContain('Dum99')
    })

    it('renames a label from its chip menu', async () => {
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
      await user.click(await openMenu(user, 'Sunset', 'Rename'))
      const input = screen.getByLabelText('Name')
      await user.clear(input)
      await user.type(input, 'Sundown')
      await user.click(screen.getByRole('button', { name: 'Save' }))

      await waitFor(() => {
        expect(updateMock).toHaveBeenCalledWith('lb_1', { name: 'Sundown', priority: 0 })
      })
      expect(await screen.findByText('Sundown')).toBeInTheDocument()
    })

    it('deletes a label after confirming in the styled dialog', async () => {
      fetchMock.mockResolvedValue([label('lb_1', 'Sunset')])
      deleteMock.mockResolvedValue(undefined)
      const user = userEvent.setup()
      renderPage()

      await screen.findByText('Sunset')
      // The menu item opens the dialog; nothing is deleted until it is confirmed.
      await user.click(await openMenu(user, 'Sunset', 'Delete'))
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

    it('hides every mutation control from viewers', async () => {
      fetchMock.mockResolvedValue([label('lb_1', 'Sunset')])
      renderPage(false)

      await screen.findByText('Sunset')
      expect(screen.queryByRole('button', { name: 'New label' })).not.toBeInTheDocument()
      expect(
        screen.queryByRole('button', { name: 'Actions for the label “Sunset”' }),
      ).not.toBeInTheDocument()
    })
  })

  describe('the review-game setting', () => {
    it('marks a label the game skips, for editors', async () => {
      fetchMock.mockResolvedValue([label('lb_1', 'Sunset'), label('lb_2', 'Cats', 0, false)])
      renderPage()

      await screen.findByText('Sunset')
      // The glyph says nothing out loud, so the accessible name carries the state.
      expect(
        screen.getByRole('link', { name: 'Cats, 5 photos, The review game skips this label' }),
      ).toBeInTheDocument()
      expect(screen.getByRole('link', { name: 'Sunset, 5 photos' })).toBeInTheDocument()
    })

    it('switches a label out of the game, carrying its other fields across', async () => {
      // The endpoint takes the whole editable record, so the action has to send
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
      await user.click(await openMenu(user, 'Sunset', 'Skip it in the review game'))

      await waitFor(() => {
        expect(updateMock).toHaveBeenCalledWith('lb_1', {
          name: 'Sunset',
          priority: 3,
          review_enabled: false,
        })
      })
      expect(
        await screen.findByRole('link', {
          name: 'Sunset, 5 photos, The review game skips this label',
        }),
      ).toBeInTheDocument()
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
      await user.click(await openMenu(user, 'Sunset', 'Ask about it in the review game'))

      await waitFor(() => {
        expect(updateMock).toHaveBeenCalledWith('lb_1', {
          name: 'Sunset',
          priority: 0,
          review_enabled: true,
        })
      })
      expect(await screen.findByRole('link', { name: 'Sunset, 5 photos' })).toBeInTheDocument()
    })

    it('rolls the chip back and says so when the save fails', async () => {
      // A chip left flipped after a failed save is a lie: the operator would walk
      // away believing the label is out of the game when it is not.
      fetchMock.mockResolvedValue([label('lb_1', 'Sunset')])
      updateMock.mockRejectedValue(new Error('boom'))
      const user = userEvent.setup()
      renderPage()

      await screen.findByText('Sunset')
      await user.click(await openMenu(user, 'Sunset', 'Skip it in the review game'))

      expect(await screen.findByText('The action failed. Please try again.')).toBeInTheDocument()
      expect(screen.getByRole('link', { name: 'Sunset, 5 photos' })).toBeInTheDocument()
    })
  })

  it('keeps a long unbroken name inside its chip', async () => {
    // A label name is user data and can be long and unbroken: it has to be
    // allowed to shrink and truncate, or one chip stretches the whole cloud past
    // the viewport.
    const long = 'Fotky'.repeat(20)
    fetchMock.mockResolvedValue([label('lb_1', long)])
    renderPage()

    const name = await screen.findByText(long)
    expect(name).toHaveClass('kk-label-chip__name')
    // The count rides beside the truncated name and keeps its own width.
    const chip = name.closest('a')
    expect(within(chip as HTMLElement).getByText('5')).toHaveClass('kk-label-chip__count')
  })
})

/** The visible name a chip link shows (its first line of text). */
function chipName(chip: HTMLElement): string {
  return chip.querySelector('.kk-label-chip__name')?.textContent ?? ''
}

/** The visible names of the cloud's top-level chips, in the order they are drawn. */
function chipNames(): string[] {
  const cloud = screen.getByRole('list', { name: 'Labels' })
  return within(cloud).getAllByRole('link').map(chipName)
}

/** Opens a chip's actions menu and yields the named item inside it. */
async function openMenu(
  user: ReturnType<typeof userEvent.setup>,
  name: string,
  item: string,
): Promise<HTMLElement> {
  await user.click(screen.getByRole('button', { name: `Actions for the label “${name}”` }))
  return screen.findByRole('button', { name: item })
}
