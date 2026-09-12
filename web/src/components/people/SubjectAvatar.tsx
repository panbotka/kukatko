import { useState } from 'react'

import { subjectAvatarUrl } from '../../services/people'
import { InitialAvatar } from '../InitialAvatar'

/** Props for {@link SubjectAvatar}. */
export interface SubjectAvatarProps {
  /** The subject whose face is drawn. */
  uid: string
  /** Their name; drives the letter and the colour of the fallback. */
  name: string
  /**
   * How many photos the subject appears on. Zero means there is no picture of
   * them anywhere, so the letter is drawn without firing a request that could
   * only 404.
   */
  photoCount: number
  /** Extra classes for spacing at the call site. */
  className?: string
}

/**
 * A subject as a small round picture: the square the backend cuts for them
 * (`GET /subjects/{uid}/avatar` — the chosen cover, else their own face taken
 * from a photo they appear on), falling back to the coloured initial of
 * {@link InitialAvatar}.
 *
 * The fallback is an ordinary case here, not an error path. A family archive is
 * full of people nobody photographed — a great-grandmother is a normal node of a
 * family tree and an empty gallery of a subject page — so the letter has to look
 * like a deliberate design rather than a hole where a face failed to load. A
 * subject with no photos at all never asks for a picture in the first place, and
 * one whose rendition cannot be produced degrades to the same letter instead of
 * the browser's broken-image glyph.
 *
 * It is `aria-hidden`, like every other avatar in the app: the circle never
 * appears without the name written out beside it.
 */
export function SubjectAvatar({ uid, name, photoCount, className }: SubjectAvatarProps) {
  const [failed, setFailed] = useState(false)

  if (photoCount === 0 || failed) {
    return <InitialAvatar name={name} className={className} />
  }
  return (
    <img
      className={`kk-avatar kk-avatar--photo${className === undefined ? '' : ` ${className}`}`}
      // No download token: the browser sends the session cookie with a
      // same-origin <img>, which is how every other thumbnail in the app is
      // addressed by UID alone.
      src={subjectAvatarUrl(uid)}
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
