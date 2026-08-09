import { avatarInitial, avatarTone } from '../lib/avatarIdentity'

/** Props for {@link InitialAvatar}. */
export interface InitialAvatarProps {
  /** The person's name; drives both the letter and the colour. */
  name: string
  /** Extra classes for spacing at the call site. */
  className?: string
}

/**
 * A person as a coloured circle with the first letter of their name — the stand-in
 * for a profile picture in a library that stores photographs, not avatars.
 *
 * The colour comes from the name alone (see `lib/avatarIdentity.ts`), so one person
 * keeps one colour everywhere, and it is drawn from the theme's own tokens — no
 * image is fetched, nothing is stored, and there is no third-party gravatar-style
 * lookup leaking who reads what.
 *
 * It is `aria-hidden`: the avatar never appears without the name written out beside
 * it, so announcing a lone letter would only make a screen reader repeat itself.
 */
export function InitialAvatar({ name, className }: InitialAvatarProps) {
  return (
    <span
      className={`kk-avatar kk-avatar--tone-${avatarTone(name)}${className === undefined ? '' : ` ${className}`}`}
      aria-hidden="true"
    >
      {avatarInitial(name)}
    </span>
  )
}
