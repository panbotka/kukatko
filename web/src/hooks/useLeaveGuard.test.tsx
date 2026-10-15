import { act, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { BrowserRouter, Link, MemoryRouter, Route, Routes } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'

import { listenForHistoryLeaves, useLeaveGuard } from './useLeaveGuard'

// Before any router mounts, as main.tsx does.
listenForHistoryLeaves()

/**
 * A page under the guard, with every kind of link a real shell puts on screen:
 * an in-app one, an external one, a download, one opening a new tab, and one
 * back to the page we are already on.
 */
function Guarded({ active }: { active: boolean }) {
  const guard = useLeaveGuard(active)
  return (
    <>
      <h1>Upload</h1>
      <Link to="/albums">In-app</Link>
      <Link to="/upload">Same page</Link>
      <a href="https://example.com/help">External</a>
      <a href="/api/v1/photos/p1/download" download="p1.jpg">
        Download
      </a>
      <a href="/albums" target="_blank" rel="noreferrer">
        New tab
      </a>
      {guard.asking && (
        <div role="dialog">
          <button type="button" onClick={guard.confirm}>
            Leave
          </button>
          <button type="button" onClick={guard.cancel}>
            Stay
          </button>
        </div>
      )}
    </>
  )
}

function renderGuarded(active = true) {
  return render(
    <MemoryRouter initialEntries={['/upload']}>
      <Routes>
        <Route path="/upload" element={<Guarded active={active} />} />
        <Route path="/albums" element={<h1>Albums</h1>} />
      </Routes>
    </MemoryRouter>,
  )
}

/** True while the guarded page (rather than its destination) is on screen. */
function stillHere(): boolean {
  return screen.queryByRole('heading', { name: 'Upload' }) !== null
}

describe('useLeaveGuard', () => {
  it('holds an in-app link back and releases it on confirm', async () => {
    const user = userEvent.setup()
    renderGuarded()

    await user.click(screen.getByRole('link', { name: 'In-app' }))
    expect(screen.getByRole('dialog')).toBeInTheDocument()
    expect(stillHere()).toBe(true)

    await user.click(screen.getByRole('button', { name: 'Leave' }))
    expect(await screen.findByRole('heading', { name: 'Albums' })).toBeInTheDocument()
  })

  it('stays put on cancel, with the question gone', async () => {
    const user = userEvent.setup()
    renderGuarded()

    await user.click(screen.getByRole('link', { name: 'In-app' }))
    await user.click(screen.getByRole('button', { name: 'Stay' }))

    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(stillHere()).toBe(true)
  })

  it('lets everything that does not lose the page through', async () => {
    const user = userEvent.setup()
    renderGuarded()

    // A link back to this very page changes nothing…
    await user.click(screen.getByRole('link', { name: 'Same page' }))
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()

    // …a download does not navigate…
    await user.click(screen.getByRole('link', { name: 'Download' }))
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()

    // …an explicit new tab leaves this one running…
    await user.click(screen.getByRole('link', { name: 'New tab' }))
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()

    // …and so does a modified click on an ordinary in-app link.
    await user.keyboard('{Meta>}')
    await user.click(screen.getByRole('link', { name: 'In-app' }))
    await user.keyboard('{/Meta}')
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()

    expect(stillHere()).toBe(true)
  })

  it('asks the browser to warn on a tab close, and stops once it is idle', () => {
    const add = vi.spyOn(window, 'addEventListener')
    const remove = vi.spyOn(window, 'removeEventListener')

    const { rerender } = renderGuarded()
    expect(add.mock.calls.some(([type]) => type === 'beforeunload')).toBe(true)

    rerender(
      <MemoryRouter initialEntries={['/upload']}>
        <Routes>
          <Route path="/upload" element={<Guarded active={false} />} />
        </Routes>
      </MemoryRouter>,
    )
    expect(remove.mock.calls.some(([type]) => type === 'beforeunload')).toBe(true)
  })

  it('does not hold anything back while there is nothing to lose', async () => {
    const user = userEvent.setup()
    renderGuarded(false)

    await user.click(screen.getByRole('link', { name: 'In-app' }))

    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(await screen.findByRole('heading', { name: 'Albums' })).toBeInTheDocument()
  })

  describe('Back and Forward', () => {
    /**
     * The guarded page in a real browser router, with the queue started by a
     * button so a test can arm the guard after walking the history.
     */
    function Armed() {
      const [active, setActive] = useState(false)
      const guard = useLeaveGuard(active)
      return (
        <>
          <h1>Upload</h1>
          <button
            type="button"
            onClick={() => {
              setActive(true)
            }}
          >
            Start
          </button>
          <Link to="/albums">In-app</Link>
          {guard.asking && (
            <div role="dialog">
              <button type="button" onClick={guard.confirm}>
                Leave
              </button>
              <button type="button" onClick={guard.cancel}>
                Stay
              </button>
            </div>
          )}
        </>
      )
    }

    function renderInBrowser() {
      return render(
        <BrowserRouter>
          <Routes>
            <Route path="/upload" element={<Armed />} />
            <Route
              path="/albums"
              element={
                <>
                  <h1>Albums</h1>
                  <Link to="/upload">Upload</Link>
                </>
              }
            />
          </Routes>
        </BrowserRouter>,
      )
    }

    /** Walks albums → upload through the app, and starts the queue. */
    async function uploadingAfterAlbums(user: ReturnType<typeof userEvent.setup>) {
      window.history.replaceState(null, '', '/albums')
      renderInBrowser()
      await user.click(screen.getByRole('link', { name: 'Upload' }))
      await user.click(screen.getByRole('button', { name: 'Start' }))
    }

    /** A browser history step, the way the Back button or a gesture makes one. */
    async function traverse(delta: number) {
      await act(async () => {
        window.history.go(delta)
        // jsdom fires popstate on a later task, like a browser.
        await new Promise((resolve) => setTimeout(resolve, 20))
      })
    }

    it('holds Back during a running queue and asks first', async () => {
      const user = userEvent.setup()
      await uploadingAfterAlbums(user)

      await traverse(-1)

      expect(screen.getByRole('dialog')).toBeInTheDocument()
      expect(stillHere()).toBe(true)
      // The browser is stepped back onto the guarded entry, so the address bar
      // and a later Back agree with what is on screen.
      await waitFor(() => {
        expect(window.location.pathname).toBe('/upload')
      })
    })

    it('goes Back for real on confirm', async () => {
      const user = userEvent.setup()
      await uploadingAfterAlbums(user)
      await traverse(-1)
      await waitFor(() => {
        expect(window.location.pathname).toBe('/upload')
      })

      await user.click(screen.getByRole('button', { name: 'Leave' }))

      expect(await screen.findByRole('heading', { name: 'Albums' })).toBeInTheDocument()
      expect(window.location.pathname).toBe('/albums')
      // A traversal, not a push: Forward leads back to the upload page.
      await traverse(1)
      expect(await screen.findByRole('heading', { name: 'Upload' })).toBeInTheDocument()
    })

    it('stays on cancel, and asks again on the next Back', async () => {
      const user = userEvent.setup()
      await uploadingAfterAlbums(user)
      await traverse(-1)
      await waitFor(() => {
        expect(window.location.pathname).toBe('/upload')
      })

      await user.click(screen.getByRole('button', { name: 'Stay' }))
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
      expect(stillHere()).toBe(true)

      await traverse(-1)
      expect(screen.getByRole('dialog')).toBeInTheDocument()
      expect(stillHere()).toBe(true)
    })

    it('holds Forward the same way', async () => {
      const user = userEvent.setup()
      window.history.replaceState(null, '', '/upload')
      renderInBrowser()
      // upload → albums → Back to upload, then the queue starts with an entry ahead.
      await user.click(screen.getByRole('link', { name: 'In-app' }))
      await traverse(-1)
      await user.click(await screen.findByRole('button', { name: 'Start' }))

      await traverse(1)

      expect(screen.getByRole('dialog')).toBeInTheDocument()
      expect(stillHere()).toBe(true)
      await waitFor(() => {
        expect(window.location.pathname).toBe('/upload')
      })

      await user.click(screen.getByRole('button', { name: 'Leave' }))
      expect(await screen.findByRole('heading', { name: 'Albums' })).toBeInTheDocument()
    })

    it('holds a Back onto an entry the router never stamped', async () => {
      const user = userEvent.setup()
      // An entry from before the app, with no `idx`: the distance is unknown.
      window.history.replaceState(null, '', '/albums')
      window.history.pushState(null, '', '/upload')
      renderInBrowser()
      await user.click(screen.getByRole('button', { name: 'Start' }))

      await traverse(-1)

      expect(screen.getByRole('dialog')).toBeInTheDocument()
      expect(stillHere()).toBe(true)
      expect(window.location.pathname).toBe('/upload')

      await user.click(screen.getByRole('button', { name: 'Leave' }))
      expect(await screen.findByRole('heading', { name: 'Albums' })).toBeInTheDocument()
    })

    it('lets Back through while there is nothing to lose', async () => {
      const user = userEvent.setup()
      window.history.replaceState(null, '', '/albums')
      renderInBrowser()
      await user.click(screen.getByRole('link', { name: 'Upload' }))

      await traverse(-1)

      expect(await screen.findByRole('heading', { name: 'Albums' })).toBeInTheDocument()
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    })
  })
})
