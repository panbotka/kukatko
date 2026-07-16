import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'
import { ApiError } from '../../services/auth'
import { type Place } from '../../services/map'

import { PlaceSearch } from './PlaceSearch'

vi.mock('../../services/map', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../services/map')>()
  return { ...actual, searchPlaces: vi.fn() }
})

const { searchPlaces } = await import('../../services/map')
const searchPlacesMock = vi.mocked(searchPlaces)

/** The two Veselís: the town and the chateau — the ambiguity the field resolves. */
const VESELI: Place[] = [
  {
    name: 'Veselí nad Moravou',
    label: 'Town',
    type: 'regional.municipality',
    location: 'Czechia',
    lat: 48.95363,
    lng: 17.37649,
  },
  {
    name: 'Zámek Veselí nad Moravou',
    label: 'Chateau',
    type: 'poi',
    location: 'Veselí nad Moravou, Hodonín District, Czechia',
    lat: 48.95367,
    lng: 17.37619,
  },
]

function renderSearch(onPick = vi.fn()) {
  render(
    <I18nextProvider i18n={i18n}>
      <PlaceSearch id="place" onPick={onPick} />
    </I18nextProvider>,
  )
  return { onPick, input: screen.getByLabelText('Find a place by name') }
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  vi.clearAllMocks()
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('PlaceSearch', () => {
  it('debounces typing into a single lookup, not one per character', async () => {
    searchPlacesMock.mockResolvedValue(VESELI)
    const user = userEvent.setup()
    const { input } = renderSearch()

    await user.type(input, 'Veselí')

    await waitFor(() => {
      expect(screen.getByRole('option', { name: /^Veselí nad Moravou/ })).toBeInTheDocument()
    })
    // Six keystrokes, one request — a lookup per character is how a mapy.com
    // credit budget dies.
    expect(searchPlacesMock).toHaveBeenCalledTimes(1)
    expect(searchPlacesMock).toHaveBeenCalledWith('Veselí', 6, expect.any(AbortSignal))
  })

  it('never searches a query too short to mean anything', async () => {
    const user = userEvent.setup()
    const { input } = renderSearch()

    await user.type(input, 'V')
    await new Promise((resolve) => setTimeout(resolve, 400))
    expect(searchPlacesMock).not.toHaveBeenCalled()
  })

  it('cancels an in-flight lookup when the query changes again', async () => {
    searchPlacesMock.mockResolvedValue(VESELI)
    const user = userEvent.setup()
    const { input } = renderSearch()

    await user.type(input, 'Brno')
    await waitFor(() => {
      expect(searchPlacesMock).toHaveBeenCalledTimes(1)
    })
    const firstSignal = searchPlacesMock.mock.calls[0][2]
    await user.type(input, 'x')
    await waitFor(() => {
      expect(firstSignal?.aborted).toBe(true)
    })
  })

  it('shows enough per suggestion to tell the Veselís apart', async () => {
    searchPlacesMock.mockResolvedValue(VESELI)
    const user = userEvent.setup()
    const { input } = renderSearch()

    await user.type(input, 'Veselí')
    await screen.findByRole('listbox')

    const options = screen.getAllByRole('option')
    expect(options).toHaveLength(2)
    // Name plus the kind of place plus what contains it.
    expect(options[0]).toHaveTextContent('Veselí nad Moravou')
    expect(options[0]).toHaveTextContent('Town')
    expect(options[1]).toHaveTextContent('Zámek Veselí nad Moravou')
    expect(options[1]).toHaveTextContent('Hodonín District')
  })

  it('picks a suggestion by click, handing back its coordinates', async () => {
    searchPlacesMock.mockResolvedValue(VESELI)
    const user = userEvent.setup()
    const { input, onPick } = renderSearch()

    await user.type(input, 'Veselí')
    await user.click(await screen.findByRole('option', { name: /Zámek/ }))

    expect(onPick).toHaveBeenCalledWith(VESELI[1])
    // The chosen name stays in the field as confirmation, the list closes, and
    // picking it does not immediately search for it again.
    expect(input).toHaveValue('Zámek Veselí nad Moravou')
    expect(screen.queryByRole('listbox')).not.toBeInTheDocument()
    expect(searchPlacesMock).toHaveBeenCalledTimes(1)
  })

  it('navigates with arrows and picks with Enter', async () => {
    searchPlacesMock.mockResolvedValue(VESELI)
    const user = userEvent.setup()
    const { input, onPick } = renderSearch()

    await user.type(input, 'Veselí')
    await screen.findByRole('listbox')

    await user.keyboard('{ArrowDown}{ArrowDown}')
    expect(screen.getAllByRole('option')[1]).toHaveAttribute('aria-selected', 'true')
    await user.keyboard('{ArrowUp}')
    expect(screen.getAllByRole('option')[0]).toHaveAttribute('aria-selected', 'true')

    await user.keyboard('{Enter}')
    expect(onPick).toHaveBeenCalledWith(VESELI[0])
  })

  it('takes the best match on Enter when nothing is highlighted', async () => {
    searchPlacesMock.mockResolvedValue(VESELI)
    const user = userEvent.setup()
    const { input, onPick } = renderSearch()

    await user.type(input, 'Veselí')
    await screen.findByRole('listbox')
    await user.keyboard('{Enter}')

    expect(onPick).toHaveBeenCalledWith(VESELI[0])
  })

  it('closes the list on Escape without picking anything', async () => {
    searchPlacesMock.mockResolvedValue(VESELI)
    const user = userEvent.setup()
    const { input, onPick } = renderSearch()

    await user.type(input, 'Veselí')
    await screen.findByRole('listbox')
    await user.keyboard('{Escape}')

    expect(screen.queryByRole('listbox')).not.toBeInTheDocument()
    expect(onPick).not.toHaveBeenCalled()
  })

  it('says so when a name matches nothing, without calling it an error', async () => {
    searchPlacesMock.mockResolvedValue([])
    const user = userEvent.setup()
    const { input } = renderSearch()

    await user.type(input, 'qqqq')
    expect(await screen.findByText('No place matches that.')).toBeInTheDocument()
  })

  it('reports an unavailable provider and keeps the field usable', async () => {
    searchPlacesMock.mockRejectedValue(new ApiError(503, 'place search is not configured'))
    const user = userEvent.setup()
    const { input } = renderSearch()

    await user.type(input, 'Brno')
    expect(
      await screen.findByText(
        'Place search is unavailable. You can still type coordinates or click the map.',
      ),
    ).toBeInTheDocument()
    // The failure is one line of text, not a broken form: the field still takes
    // input and the rest of the editor is untouched.
    expect(input).toBeEnabled()
    expect(screen.queryByRole('listbox')).not.toBeInTheDocument()
  })

  it('reports a retryable failure distinctly from an unavailable provider', async () => {
    searchPlacesMock.mockRejectedValue(new ApiError(429, 'rate limit exceeded'))
    const user = userEvent.setup()
    const { input } = renderSearch()

    await user.type(input, 'Brno')
    expect(await screen.findByText('Place search failed. Please try again.')).toBeInTheDocument()
  })
})
