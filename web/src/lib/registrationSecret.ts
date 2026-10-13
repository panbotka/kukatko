/**
 * The letters a generated registration word is made of: lowercase and digits,
 * without the ones read aloud or copied by hand wrongly (`0`/`o`, `1`/`l`/`i`).
 * The word is told to people — over the phone, in a family chat — so it has to
 * survive being spoken and retyped.
 */
const ALPHABET = 'abcdefghjkmnpqrstuvwxyz23456789'

/** How many letters a generated word has, split into two halves by a hyphen. */
const LENGTH = 8

/**
 * Generates a fresh shared registration word for a project, such as
 * `k7mq-x2pd`: eight letters of {@link ALPHABET} in two halves, about 39 bits —
 * plenty for a word that only keeps strangers from filling in a form whose
 * every account a project admin still approves.
 *
 * Letters are drawn by rejection sampling, so every one is equally likely
 * (a plain `byte % 31` would favour the first letters).
 *
 * @param fill fills a byte array with random values; the browser's CSPRNG by
 *   default, injectable for tests.
 */
export function generateRegistrationSecret(
  fill: (bytes: Uint8Array) => void = (bytes) => {
    crypto.getRandomValues(bytes)
  },
): string {
  // The largest multiple of the alphabet's size that fits in a byte: anything
  // at or above it is thrown away rather than folded onto the first letters.
  const limit = 256 - (256 % ALPHABET.length)
  const letters: string[] = []
  const bytes = new Uint8Array(LENGTH * 2)
  while (letters.length < LENGTH) {
    fill(bytes)
    for (const byte of bytes) {
      if (byte < limit && letters.length < LENGTH) {
        letters.push(ALPHABET[byte % ALPHABET.length])
      }
    }
  }
  return `${letters.slice(0, LENGTH / 2).join('')}-${letters.slice(LENGTH / 2).join('')}`
}
