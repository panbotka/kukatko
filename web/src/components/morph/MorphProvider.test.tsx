import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Route, Routes, useLocation, useNavigate } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { type ViewTransitionHandle, type ViewTransitionHost } from '../../lib/viewTransition'

import { useMorph, useMorphMark } from './MorphContext'
import { MorphLink } from './MorphLink'
import { MorphProvider } from './MorphProvider'

/**
 * A stand-in `document.startViewTransition`.
 *
 * jsdom implements none of the View Transitions API — and cannot: there is no
 * compositor to capture a snapshot with. So the tests here exercise everything
 * around the animation (does the navigation happen, is the right element marked,
 * is the transition held open for a pop) and the animation itself is verified in
 * a real browser; the evidence is in `docs/FRONTEND.md`.
 */
function fakeHost() {
  const updates: (() => void | Promise<void>)[] = []
  const startViewTransition = vi.fn((update: () => void | Promise<void>): ViewTransitionHandle => {
    updates.push(update)
    // The browser captures the old state first and only then runs the update.
    // Running it synchronously here is the same ordering as far as the DOM is
    // concerned, and keeps the tests free of timers.
    const done = update()
    return { finished: Promise.resolve(done).then(() => undefined) }
  })
  const host: ViewTransitionHost = { startViewTransition }
  return { host, startViewTransition, updates }
}

/** The grid side: one tile-shaped link into the viewer. */
function Grid() {
  return (
    <MorphLink morphId="p1" to="/photos/p1" data-testid="tile" state={{ from: 'grid' }}>
      open
    </MorphLink>
  )
}

/** The viewer side: the marked figure plus the close button that pops back. */
function Viewer() {
  const navigate = useNavigate()
  const location = useLocation()
  const { morph } = useMorph()
  const mark = useMorphMark('p1')
  return (
    <div>
      <div data-testid="figure" {...mark} />
      <output data-testid="handoff">{JSON.stringify(location.state)}</output>
      <button
        type="button"
        onClick={() => {
          morph('p1', () => {
            void navigate(-1)
          })
        }}
      >
        close
      </button>
    </div>
  )
}

/** Mounts the pair under a router, with or without a browser that can morph. */
function renderApp(host?: ViewTransitionHost) {
  return render(
    <MemoryRouter initialEntries={['/']}>
      <MorphProvider document={host}>
        <Routes>
          <Route path="/" element={<Grid />} />
          <Route path="/photos/:uid" element={<Viewer />} />
        </Routes>
      </MorphProvider>
    </MemoryRouter>,
  )
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('the grid ⇄ viewer morph', () => {
  it('navigates through a view transition and marks the tile it leaves', async () => {
    const { host, startViewTransition } = fakeHost()
    renderApp(host)

    // The mark has to be on the tile BEFORE the transition is asked for: the
    // browser captures the old state at the first rendering opportunity after
    // that call, and an unmarked tile would have nothing to morph from.
    startViewTransition.mockImplementation((update: () => void | Promise<void>) => {
      expect(screen.getByTestId('tile')).toHaveAttribute('data-kk-morph')
      void update()
      return { finished: Promise.resolve() }
    })

    await userEvent.click(screen.getByTestId('tile'))

    expect(startViewTransition).toHaveBeenCalledTimes(1)
    expect(screen.getByTestId('figure')).toBeInTheDocument()
    // …and the other half of the pair carries the mark once it is on screen.
    expect(screen.getByTestId('figure')).toHaveAttribute('data-kk-morph')
  })

  it('carries the link state across the transition, so the handoff survives', async () => {
    const { host } = fakeHost()
    renderApp(host)

    await userEvent.click(screen.getByTestId('tile'))

    // The viewer reads the grid's already-decoded preview address off the
    // navigation state (see `lib/photoHandoff`); taking the click over must not
    // drop it, or the viewer would open on a blank stage instead of the photo.
    expect(screen.getByTestId('handoff')).toHaveTextContent('{"from":"grid"}')
  })

  it('holds the transition open until the route it navigated to has rendered', async () => {
    const { host, startViewTransition } = fakeHost()
    renderApp(host)
    await userEvent.click(screen.getByTestId('tile'))

    let settled = false
    startViewTransition.mockImplementation((update: () => void | Promise<void>) => {
      const done = Promise.resolve(update()).then(() => {
        settled = true
      })
      return { finished: done }
    })

    await userEvent.click(screen.getByRole('button', { name: 'close' }))

    // The router applies its location update inside `startTransition` and a pop is
    // asynchronous besides, so the callback must not resolve before the grid is
    // back — otherwise the browser would snapshot the page being left.
    await waitFor(() => {
      expect(screen.getByTestId('tile')).toBeInTheDocument()
    })
    await waitFor(() => {
      expect(settled).toBe(true)
    })
  })
})

describe('the fallback path', () => {
  it('navigates exactly as before where the browser cannot morph', async () => {
    renderApp(undefined)

    await userEvent.click(screen.getByTestId('tile'))

    expect(screen.getByTestId('figure')).toBeInTheDocument()
    // Nothing is marked: the mark only means "taking part in a morph".
    expect(screen.getByTestId('figure')).not.toHaveAttribute('data-kk-morph')
  })

  it('still goes back on close where the browser cannot morph', async () => {
    renderApp(undefined)
    await userEvent.click(screen.getByTestId('tile'))

    await userEvent.click(screen.getByRole('button', { name: 'close' }))

    await waitFor(() => {
      expect(screen.getByTestId('tile')).toBeInTheDocument()
    })
  })

  it('does not morph when the reader asked for reduced motion', async () => {
    vi.stubGlobal(
      'matchMedia',
      vi.fn().mockReturnValue({
        matches: true,
        media: '(prefers-reduced-motion: reduce)',
        onchange: null,
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
        addListener: vi.fn(),
        removeListener: vi.fn(),
        dispatchEvent: vi.fn(),
      }),
    )
    const { host, startViewTransition } = fakeHost()
    renderApp(host)

    await userEvent.click(screen.getByTestId('tile'))

    expect(startViewTransition).not.toHaveBeenCalled()
    expect(screen.getByTestId('figure')).toBeInTheDocument()
  })

  it('leaves a modified click to the browser', async () => {
    const { host, startViewTransition } = fakeHost()
    renderApp(host)

    // Cmd/Ctrl-click opens a new tab; taking it over would break that. One
    // session, so the held modifier is still down when the click lands.
    const user = userEvent.setup()
    await user.keyboard('{Control>}')
    await user.click(screen.getByTestId('tile'))
    await user.keyboard('{/Control}')

    expect(startViewTransition).not.toHaveBeenCalled()
  })
})
