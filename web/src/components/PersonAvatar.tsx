import { useState } from 'react'

import { MAX_RENDITION_DPR, squareRenditionName } from '../lib/rendition'
import { thumbUrl } from '../services/photos'

import { InitialAvatar } from './InitialAvatar'

/** Props for {@link PersonAvatar}. */
export interface PersonAvatarProps {
  /** The person's name; drives the letter and the colour of the fallback. */
  name: string
  /**
   * The photo to show instead of a letter — the cover photo of the person this
   * account is linked to. Undefined (the common case) draws initials.
   */
  photoUid?: string
  /** Extra classes for spacing at the call site. */
  className?: string
}

/**
 * How wide the circle is drawn, in CSS pixels — `.kk-avatar`'s `2rem` at the
 * app's 16 px root. It is a constant here rather than a measurement because the
 * class fixes the size: nothing renders this avatar at any other one, and
 * measuring would mean fetching a first picture before knowing which to fetch.
 */
const AVATAR_CSS_PX = 32

/**
 * The thumbnail size the circle is cut from: the smallest square rung that still
 * covers 32 CSS pixels on the sharpest screen worth sizing for, which is
 * `tile_100`. The avatar used to take `tile_224` — five times the pixels it can
 * draw — and a comment thread pays that for every distinct author in it.
 */
const AVATAR_SIZE = squareRenditionName(AVATAR_CSS_PX, MAX_RENDITION_DPR)

/**
 * Somebody as a small round picture: the cover photo of the person their account
 * says they are, or — when there is no account, no linked person, or no cover
 * photo chosen for that person — the coloured initial of {@link InitialAvatar}.
 *
 * The fallback is the normal case, not an error path. Most accounts name no
 * person, and most people in a family archive have no hand-picked cover photo,
 * so the letter has to look like a deliberate design rather than a hole where a
 * face failed to load. A photo that fails to load at request time falls back to
 * exactly the same letter, so a purged or unreachable original degrades to what
 * the reader saw yesterday instead of to a broken-image glyph.
 *
 * It is `aria-hidden` for the same reason the initial is: the avatar never
 * appears without the name written out beside it.
 */
export function PersonAvatar({ name, photoUid, className }: PersonAvatarProps) {
  const [failed, setFailed] = useState(false)

  if (photoUid === undefined || photoUid === '' || failed) {
    return <InitialAvatar name={name} className={className} />
  }
  return (
    <img
      className={`kk-avatar kk-avatar--photo${className === undefined ? '' : ` ${className}`}`}
      // No download token: the browser sends the session cookie with a
      // same-origin <img>, which is how every other thumbnail in the app is
      // addressed by UID alone.
      src={thumbUrl(photoUid, AVATAR_SIZE)}
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
