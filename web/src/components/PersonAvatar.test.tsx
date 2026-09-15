import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'

import { PersonAvatar } from './PersonAvatar'

/** A signed-in session that has changed its own picture `pictureVersion` times. */
function auth(uid: string, pictureVersion: number): AuthContextValue {
  return {
    status: 'authenticated',
    user: { uid, username: 'jarmila', display_name: 'Jarmila' },
    pictureVersion,
  } as unknown as AuthContextValue
}

/**
 * The rendered picture, if there is one. An `alt=""` image maps to the
 * `presentation` role and is `aria-hidden`, so it takes the hidden query — the
 * avatar is decoration beside a name that is written out anyway.
 */
function photo(): HTMLElement | null {
  return screen.queryByRole('presentation', { hidden: true })
}

describe('PersonAvatar', () => {
  it('draws the account’s picture when it has one', () => {
    render(<PersonAvatar name="Jarmila" userUid="usr_1" />)
    const img = screen.getByRole('presentation', { hidden: true })
    // One endpoint, whatever the picture turns out to be: the server resolves
    // the chain and the client never has to know which source answered.
    expect(img.getAttribute('src')).toBe('/api/v1/users/usr_1/avatar')
    expect(img).toHaveAttribute('aria-hidden', 'true')
    expect(img).toHaveAttribute('alt', '')
  })

  it('falls back to the coloured initial without an account', () => {
    render(<PersonAvatar name="Jarmila" />)
    expect(photo()).toBeNull()
    expect(screen.getByText('J')).toBeInTheDocument()
  })

  it('treats an empty user uid as no account', () => {
    // An authorless comment (its account was deleted) carries exactly this.
    render(<PersonAvatar name="Jarmila" userUid="" />)
    expect(photo()).toBeNull()
    expect(screen.getByText('J')).toBeInTheDocument()
  })

  it('falls back to the initial when the picture 404s', () => {
    // The common case, not an error path: an account with no picture from any
    // source answers 404, which the browser reports as a failed load.
    render(<PersonAvatar name="Jarmila" userUid="usr_none" />)
    fireEvent.error(screen.getByRole('presentation', { hidden: true }))
    expect(photo()).toBeNull()
    expect(screen.getByText('J')).toBeInTheDocument()
  })

  it('busts the cache for the reader’s own picture once they have changed it', () => {
    render(
      <AuthContext.Provider value={auth('usr_1', 3)}>
        <PersonAvatar name="Jarmila" userUid="usr_1" />
      </AuthContext.Provider>,
    )
    expect(screen.getByRole('presentation', { hidden: true }).getAttribute('src')).toBe(
      '/api/v1/users/usr_1/avatar?v=3',
    )
  })

  it('leaves somebody else’s picture on the shared URL', () => {
    // Only the reader's own picture changes under them; everyone else's is the
    // plain URL the whole thread shares, cached by the browser for ten minutes.
    render(
      <AuthContext.Provider value={auth('usr_1', 3)}>
        <PersonAvatar name="Bohumil" userUid="usr_2" />
      </AuthContext.Provider>,
    )
    expect(screen.getByRole('presentation', { hidden: true }).getAttribute('src')).toBe(
      '/api/v1/users/usr_2/avatar',
    )
  })

  it('asks again after a change, even though the picture 404d before it', () => {
    // The account had no picture, so the letter is what shows. Uploading one
    // must not leave it wearing that letter until the next page load.
    const { rerender } = render(
      <AuthContext.Provider value={auth('usr_1', 0)}>
        <PersonAvatar name="Jarmila" userUid="usr_1" />
      </AuthContext.Provider>,
    )
    fireEvent.error(screen.getByRole('presentation', { hidden: true }))
    expect(photo()).toBeNull()

    rerender(
      <AuthContext.Provider value={auth('usr_1', 1)}>
        <PersonAvatar name="Jarmila" userUid="usr_1" />
      </AuthContext.Provider>,
    )
    expect(photo()?.getAttribute('src')).toBe('/api/v1/users/usr_1/avatar?v=1')
  })
})
