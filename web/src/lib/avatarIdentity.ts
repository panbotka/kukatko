/**
 * Deterministic identity for an initial-circle avatar: which letter it shows and
 * which of the theme's avatar tones it wears.
 *
 * There are no uploaded profile pictures in Kukátko and there is no plan for any —
 * the archive stores photographs, not avatars — so a person in a conversation is
 * drawn as the first letter of their name in a coloured circle. The colour is a
 * pure function of the name, which is what makes it *recognition* rather than
 * decoration: the same person is the same colour on every photo, in every session,
 * for every reader, with nothing stored anywhere.
 *
 * The tones live in `styles/tokens.css` as `--kk-avatar-N-bg`; this module only
 * decides the index, so the palette can be re-tuned without touching any logic.
 */

/** How many avatar tones the theme defines (`.kk-avatar--tone-0` … `--tone-7`). */
export const AVATAR_TONE_COUNT = 8

/** Shown when there is no name at all — an authorless comment, say. */
const UNKNOWN_INITIAL = '?'

/**
 * Splits a name into user-perceived characters. A grapheme, not a code unit and
 * not a code point: "Ǎda" written with a combining caron is one letter to the
 * reader, and half of a surrogate pair is not a letter at all.
 */
const graphemes = new Intl.Segmenter(undefined, { granularity: 'grapheme' })

/**
 * The letter for a name: the first character of its first word, upper-cased.
 *
 * The upper-casing is `toLocaleUpperCase` so "šárka" gives "Š" — plain ASCII
 * casing would leave the diacritic behind.
 *
 * @returns the single-character initial, or `'?'` for a blank/absent name.
 */
export function avatarInitial(name: string): string {
  const trimmed = name.trim()
  if (trimmed === '') {
    return UNKNOWN_INITIAL
  }
  const [first] = graphemes.segment(trimmed)
  return first.segment.toLocaleUpperCase()
}

/**
 * The tone index for a name, in `[0, AVATAR_TONE_COUNT)`.
 *
 * An FNV-1a hash over the name's code points: stable across sessions and machines
 * (unlike anything derived from an object identity or a random seed) and well
 * spread, so two people in the same thread rarely collide. Case and surrounding
 * whitespace are folded first, so "Jana" and " jana " are one person, not two.
 */
export function avatarTone(name: string): number {
  const key = name.trim().toLocaleLowerCase()
  let hash = 0x811c9dc5
  for (const char of key) {
    hash ^= char.codePointAt(0) ?? 0
    // FNV prime, multiplied in 32-bit pieces so the result stays exact in a double.
    hash = Math.imul(hash, 0x01000193) >>> 0
  }
  return hash % AVATAR_TONE_COUNT
}
