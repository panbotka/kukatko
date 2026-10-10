import { act, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { createMemoryRouter, RouterProvider, useLocation } from 'react-router-dom'
import { beforeEach, describe, expect, it } from 'vitest'

import { CapabilitiesContext } from '../../capabilities/CapabilitiesContext'
import i18n from '../../i18n'
import { LIBRARY_DEFAULTS } from '../../lib/libraryView'
import { useUrlState } from '../../lib/urlState'

import { FilterBar } from './FilterBar'

/**
 * The bar the way a page mounts it: its view lives in the URL through the real
 * `useUrlState`, so every write it makes is a real history step. The probes
 * state the URL and the query the grid would be running.
 */
function Library() {
  const [view, setView] = useUrlState(LIBRARY_DEFAULTS)
  const location = useLocation()
  return (
    <>
      <FilterBar view={view} onChange={setView} total={0} />
      <output data-testid="path">{location.pathname}</output>
      <output data-testid="query">{view.q}</output>
      <output data-testid="sort">{view.sort}</output>
    </>
  )
}

/**
 * Mounts the library behind a real memory router whose history starts on a page
 * outside it, so Back leaving the library is visible rather than a no-op.
 */
function renderLibrary() {
  const router = createMemoryRouter(
    [
      { path: '/', element: <Library /> },
      { path: '/elsewhere', element: <p>Elsewhere</p> },
    ],
    { initialEntries: ['/elsewhere', '/'], initialIndex: 1 },
  )
  render(
    <I18nextProvider i18n={i18n}>
      <CapabilitiesContext.Provider
        value={{ semantic_search: true, known: true, passkeys: false, video_streaming: false }}
      >
        <RouterProvider router={router} />
      </CapabilitiesContext.Provider>
    </I18nextProvider>,
  )
  return router
}

/** Presses the browser's Back button. */
async function back(router: ReturnType<typeof createMemoryRouter>) {
  await act(async () => {
    await router.navigate(-1)
  })
}

/** The query the router's current location carries. */
function urlQuery(router: ReturnType<typeof createMemoryRouter>): string | null {
  return new URLSearchParams(router.state.location.search).get('q')
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
})

describe('FilterBar history', () => {
  it('returns to the unfiltered library when Back follows a submitted query', async () => {
    const user = userEvent.setup()
    const router = renderLibrary()

    await user.type(screen.getByLabelText('Filter the library'), 'year:2024{Enter}')
    await waitFor(() => {
      expect(screen.getByTestId('query')).toHaveTextContent('year:2024')
    })
    expect(urlQuery(router)).toBe('year:2024')

    await user.selectOptions(screen.getByLabelText('Sort'), 'oldest')
    await waitFor(() => {
      expect(screen.getByTestId('sort')).toHaveTextContent('oldest')
    })

    // Back from the sort lands on the submitted query…
    await back(router)
    expect(urlQuery(router)).toBe('year:2024')
    expect(screen.getByTestId('sort')).toHaveTextContent('newest')

    // …and Back from the query on the unfiltered library, field and grid alike —
    // not out of the app, which is where it used to go.
    await back(router)
    expect(router.state.location.pathname).toBe('/')
    expect(router.state.location.search).toBe('')
    expect(screen.getByTestId('query')).toBeEmptyDOMElement()
    expect(screen.getByLabelText('Filter the library')).toHaveValue('')

    await back(router)
    expect(screen.getByText('Elsewhere')).toBeInTheDocument()
  })

  it('gives one edit one entry, however many pauses committed it', async () => {
    const user = userEvent.setup()
    const router = renderLibrary()
    const field = screen.getByLabelText('Filter the library')

    // A pause commits mid-word — the grid follows the typing…
    await user.type(field, 'svat')
    await waitFor(() => {
      expect(urlQuery(router)).toBe('svat')
    })
    await user.type(field, 'b')
    await waitFor(() => {
      expect(urlQuery(router)).toBe('svatb')
    })
    // …and Enter settles it.
    await user.type(field, 'a{Enter}')
    await waitFor(() => {
      expect(urlQuery(router)).toBe('svatba')
    })

    // None of the prefixes is a view of its own: one Back undoes the whole edit.
    await back(router)
    expect(router.state.location.search).toBe('')
    expect(screen.getByLabelText('Filter the library')).toHaveValue('')
  })

  it('steps back through submitted queries one at a time', async () => {
    const user = userEvent.setup()
    const router = renderLibrary()
    const field = screen.getByLabelText('Filter the library')

    await user.type(field, 'svatba{Enter}')
    await waitFor(() => {
      expect(urlQuery(router)).toBe('svatba')
    })
    // Enter closed that edit, so the next one gets an entry of its own rather
    // than rewriting the query the reader just settled on.
    await user.clear(field)
    await user.type(field, 'year:2024{Enter}')
    await waitFor(() => {
      expect(urlQuery(router)).toBe('year:2024')
    })

    await back(router)
    expect(urlQuery(router)).toBe('svatba')
    expect(screen.getByLabelText('Filter the library')).toHaveValue('svatba')

    await back(router)
    expect(router.state.location.search).toBe('')
  })

  it('never rewrites a view the reader reached some other way', async () => {
    const user = userEvent.setup()
    const router = renderLibrary()
    const field = screen.getByLabelText('Filter the library')

    await user.type(field, 'svatba{Enter}')
    await waitFor(() => {
      expect(urlQuery(router)).toBe('svatba')
    })
    await user.selectOptions(screen.getByLabelText('Sort'), 'oldest')
    await waitFor(() => {
      expect(screen.getByTestId('sort')).toHaveTextContent('oldest')
    })

    // Typing after the sort change — committed by a pause, no Enter — is a new
    // edit: it pushes, so the sorted view keeps its own entry.
    await user.type(field, ' 2024')
    await waitFor(() => {
      expect(urlQuery(router)).toBe('svatba 2024')
    })

    await back(router)
    expect(urlQuery(router)).toBe('svatba')
    expect(screen.getByTestId('sort')).toHaveTextContent('oldest')
  })

  it('writes nothing for an Enter on a query the URL already holds', async () => {
    const user = userEvent.setup()
    const router = renderLibrary()
    const field = screen.getByLabelText('Filter the library')

    await user.type(field, 'svatba')
    await waitFor(() => {
      expect(urlQuery(router)).toBe('svatba')
    })
    // The pause already wrote it; Enter only closes the edit, it does not stack
    // a second entry of the same query for Back to stall on.
    await user.type(field, '{Enter}')

    await back(router)
    expect(router.state.location.search).toBe('')
  })
})
