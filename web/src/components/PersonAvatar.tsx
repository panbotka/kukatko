import { useContext, useState } from 'react'

import { AuthContext } from '../auth/AuthContext'
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
 * When the account it draws is the signed-in one, it asks for the version the
 * auth context counts, so a picture just changed on the account page redraws
 * everywhere it stands for that person — the bar, a comment of theirs — instead
 * of every one of those keeping the picture the browser already had. No call
 * site has to know whether it is drawing the reader themselves, which is the
 * point: the bar is written once and does not think about profile pictures.
 *
 * It is `aria-hidden`: the avatar never appears without the name written out
 * beside it, so announcing a lone letter would only make a screen reader repeat
 * itself.
 */
export function PersonAvatar({ name, userUid, className }: PersonAvatarProps) {
  const auth = useContext(AuthContext)
  // The URL that failed, not a bare flag: the fallback must last exactly as long
  // as the picture that provoked it. An account that had none answered 404 and
  // fell back to its letter, and a flag would leave it wearing that letter after
  // its owner uploaded a picture — the very moment they are watching for it.
  const [failedSrc, setFailedSrc] = useState<string | null>(null)

  if (userUid === undefined || userUid === '') {
    return <InitialAvatar name={name} className={className} />
  }
  const src = userAvatarUrl(userUid, auth?.user?.uid === userUid ? auth.pictureVersion : 0)
  if (failedSrc === src) {
    return <InitialAvatar name={name} className={className} />
  }
  return (
    <img
      className={`kk-avatar kk-avatar--photo${className === undefined ? '' : ` ${className}`}`}
      // No download token: the browser sends the session cookie with a
      // same-origin <img>, which is how every other protected picture in the app
      // is addressed.
      src={src}
      alt=""
      aria-hidden="true"
      loading="lazy"
      decoding="async"
      onError={() => {
        setFailedSrc(src)
      }}
    />
  )
}
