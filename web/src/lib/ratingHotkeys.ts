import { type RatingFlag } from '../services/photos'

/**
 * A rating action decoded from a keyboard key: either a star rating (0–5) or a
 * personal mark. Number keys `0`–`5` set the rating; `p` = thumbs-up, `r` =
 * thumbs-down and `e` = the eye mark. Any other key yields `null` (not a rating
 * hotkey).
 */
export type RatingHotkey =
  | { readonly kind: 'rating'; readonly value: number }
  | { readonly kind: 'flag'; readonly value: RatingFlag }

/**
 * Maps a `KeyboardEvent.key` to a rating action, or `null` when the key is not a
 * rating shortcut. `0`–`5` set the rating; `p`/`r`/`e` (case-insensitive) set the
 * personal mark to thumbs-up/thumbs-down/eye respectively. A pure function so it
 * is trivially testable and shared by the photo detail page and the focused grid
 * tile.
 */
export function ratingHotkey(key: string): RatingHotkey | null {
  if (key.length === 1 && key >= '0' && key <= '5') {
    return { kind: 'rating', value: Number(key) }
  }
  const lower = key.toLowerCase()
  if (lower === 'p') {
    return { kind: 'flag', value: 'pick' }
  }
  if (lower === 'r') {
    return { kind: 'flag', value: 'reject' }
  }
  if (lower === 'e') {
    return { kind: 'flag', value: 'eye' }
  }
  return null
}

/**
 * Reports whether an event target is a text-entry element (an `<input>`,
 * `<textarea>`, `<select>` or a `contenteditable` node). Rating hotkeys must not
 * fire while the user is typing, so callers skip the shortcut when this is true.
 */
export function isTypingElement(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) {
    return false
  }
  const tag = target.tagName
  if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') {
    return true
  }
  // `isContentEditable` is a boolean in real browsers but unimplemented in jsdom
  // (undefined), so also consult the attribute so the guard works under test.
  return target.isContentEditable || target.getAttribute('contenteditable') === 'true'
}
