import { render, screen } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { describe, expect, it } from 'vitest'

import { LibraryRedirect } from './LibraryRedirect'

/** Shows where the redirect landed: the full address, query and hash included. */
function Landing() {
  const { pathname, search, hash } = useLocation()
  return <div data-testid="landing">{`${pathname}${search}${hash}`}</div>
}

/** Mounts the redirect where the app does: under a project's `library/*`. */
function renderAt(entry: string) {
  return render(
    <MemoryRouter initialEntries={[entry]}>
      <Routes>
        <Route path="/p/:slug" element={<Landing />} />
        <Route path="/p/:slug/library/*" element={<LibraryRedirect />} />
      </Routes>
    </MemoryRouter>,
  )
}

describe('LibraryRedirect', () => {
  it("forwards a retired /library address to the project's library, query and hash kept", () => {
    renderAt('/p/v/library?year=2024&odd=1#top')

    expect(screen.getByTestId('landing')).toHaveTextContent('/p/v?year=2024&odd=1#top')
  })

  it('forwards everything under /library, not just the bare branch', () => {
    renderAt('/p/v/library/login')

    expect(screen.getByTestId('landing')).toHaveTextContent(/^\/p\/v$/)
  })
})
