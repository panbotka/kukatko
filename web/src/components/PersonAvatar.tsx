import { useState } from 'react'

import { userAvatarUrl } from '../services/userpic'

import { InitialAvatar } from './InitialAvatar'

/** Props for {@link PersonAvatar}. */
export interface PersonAvatarProps {
  /** The person's name; drives the letter and the colour of the fallback. */
  name: string
  /**
   * The account whose picture is drawn. Undefined — an authorless comment, a
   * person who has no account — draws initials without firing a request.
   */
  userUid?: string
  /**
   * Bumped by the page that just changed this picture, to defeat the ten-minute
   * cache on its own preview. Readers elsewhere want the cache and omit it.
   */
  version?: number
  /** Extra classes for spacing at the call site. */
  className?: string
}

/**
 * Somebody as a small round picture: whatever `GET /users/{uid}/avatar` answers
 * for their account — a picture they uploaded, a photo of the library they
 * picked, or the face of the person the account says they are — falling back to
 * the coloured initial of {@link InitialAvatar}.
 *
 * Which of those three answered is deliberately not visible here. The server
 * resolves the chain and hands back either a picture or a 404; a client that had
 * to know which source won would have to re-implement the chain, and would get
 * it wrong the moment a picked photo was archived.
 *
 * The fallback is the normal case, not an error path. Most accounts have set
 * nothing and name no person, so the letter has to look like a deliberate design
 * rather than a hole where a face failed to load — which is also what a 404 or an
 * unreachable original degrades to, instead of a broken-image glyph.
 *
 * It is `aria-hidden`: the avatar never appears without the name written out
 * beside it, so announcing a lone letter would only make a screen reader repeat
 * itself.
 */
export function PersonAvatar({ name, userUid, version, className }: PersonAvatarProps) {
  const [failed, setFailed] = useState(false)

  if (userUid === undefined || userUid === '' || failed) {
    return <InitialAvatar name={name} className={className} />
  }
  return (
    <img
      className={`kk-avatar kk-avatar--photo${className === undefined ? '' : ` ${className}`}`}
      // No download token: the browser sends the session cookie with a
      // same-origin <img>, which is how every other protected picture in the app
      // is addressed.
      src={userAvatarUrl(userUid, version)}
      alt=""
      aria-hidden="true"
      loading="lazy"
      decoding="async"
      onError={() => {
        setFailed(true)
      }}
    />
  )
}
