import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { Link, MemoryRouter, Route, Routes } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'

import { useLeaveGuard } from './useLeaveGuard'

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
})
