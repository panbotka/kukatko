import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, useLocation, useNavigate } from 'react-router-dom'
import { describe, expect, it } from 'vitest'

import { readUrlState, useUrlState, writeUrlState } from './urlState'

// Module-scope defaults so the hook's setter keeps a stable identity.
const DEFAULTS = { q: '', sort: 'newest', page: '1' }

describe('readUrlState / writeUrlState', () => {
  it('reads known keys from params and falls back to defaults', () => {
    const params = new URLSearchParams('q=cat&page=3&unknown=x')
    expect(readUrlState(params, DEFAULTS)).toEqual({ q: 'cat', sort: 'newest', page: '3' })
  })

  it('writes only non-default, non-empty values to keep URLs minimal', () => {
    const params = writeUrlState({ q: 'cat', sort: 'newest', page: '2' }, DEFAULTS)
    expect(params.toString()).toBe('q=cat&page=2')
  })

  it('round-trips state through encode then decode', () => {
    const state = { q: 'dog', sort: 'oldest', page: '5' }
    const encoded = writeUrlState(state, DEFAULTS)
    expect(readUrlState(encoded, DEFAULTS)).toEqual(state)
  })
})

/** Probe exercising the hook plus history navigation. */
function Probe() {
  const [state, setState] = useUrlState(DEFAULTS)
  const location = useLocation()
  const navigate = useNavigate()
  return (
    <div>
      <span data-testid="page">{state.page}</span>
      <span data-testid="q">{state.q}</span>
      <span data-testid="search">{location.search}</span>
      <button
        onClick={() => {
          setState({ page: '2', q: 'cat' })
        }}
      >
        go
      </button>
      <button
        onClick={() => {
          void navigate(-1)
        }}
      >
        back
      </button>
    </div>
  )
}

describe('useUrlState', () => {
  it('hydrates state from the initial query string', () => {
    render(
      <MemoryRouter initialEntries={['/?q=fox&page=4']}>
        <Probe />
      </MemoryRouter>,
    )

    expect(screen.getByTestId('q')).toHaveTextContent('fox')
    expect(screen.getByTestId('page')).toHaveTextContent('4')
  })

  it('writes state to the query string and Back restores the prior state', async () => {
    const user = userEvent.setup()
    render(
      <MemoryRouter initialEntries={['/']}>
        <Probe />
      </MemoryRouter>,
    )

    // Initial: defaults, clean URL.
    expect(screen.getByTestId('page')).toHaveTextContent('1')
    expect(screen.getByTestId('search')).toHaveTextContent('')

    await user.click(screen.getByRole('button', { name: 'go' }))

    expect(screen.getByTestId('page')).toHaveTextContent('2')
    expect(screen.getByTestId('q')).toHaveTextContent('cat')
    expect(screen.getByTestId('search')).toHaveTextContent('?q=cat&page=2')

    // Back must restore the prior (default) view state — "Zpět vždy funguje".
    await user.click(screen.getByRole('button', { name: 'back' }))

    expect(screen.getByTestId('page')).toHaveTextContent('1')
    expect(screen.getByTestId('q')).toHaveTextContent('')
    expect(screen.getByTestId('search')).toHaveTextContent('')
  })
})
