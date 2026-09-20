import { describe, expect, it } from 'vitest'

import {
  discussionBody,
  PHOTO_UID_LENGTH,
  PHOTO_UID_PATTERN,
  photoRefHref,
  photoRefsThatFit,
  referencedPhotoUids,
  splitPhotoRefs,
} from './photoRefs'

/** A uid of the catalogue's shape: `ph` + 24 base32 characters. */
function uid(seed: string): string {
  return `ph${seed.padEnd(24, '0').slice(0, 24)}`
}

describe('PHOTO_UID_PATTERN', () => {
  it.each([
    [uid('abc123'), true],
    ['ph0123456789abcdefghijklmn', true],
    // The alphabet stops at `v`: `w`–`z` are not base32 here.
    ['ph0123456789abcdefghijklmw', false],
    // Too short, too long, upper case, another prefix.
    ['phabc', false],
    [`${uid('abc')}0`, false],
    [uid('abc').toUpperCase(), false],
    [`st${uid('abc').slice(2)}`, false],
  ])('%s → %s', (token, matches) => {
    expect(PHOTO_UID_PATTERN.test(token)).toBe(matches)
  })
})

describe('splitPhotoRefs', () => {
  it('keeps every character of the text and lifts out the uids in order', () => {
    const a = uid('a')
    const b = uid('b')
    expect(splitPhotoRefs(`these two: ${a}, ${b}!`)).toEqual([
      { kind: 'text', text: 'these two: ' },
      { kind: 'uid', uid: a },
      { kind: 'text', text: ', ' },
      { kind: 'uid', uid: b },
      { kind: 'text', text: '!' },
    ])
  })

  it('finds a uid on its own line and glued to punctuation', () => {
    const a = uid('a')
    expect(splitPhotoRefs(`wrong:\n${a}\n(${a})`)).toEqual([
      { kind: 'text', text: 'wrong:\n' },
      { kind: 'uid', uid: a },
      { kind: 'text', text: '\n(' },
      { kind: 'uid', uid: a },
      { kind: 'text', text: ')' },
    ])
  })

  it('leaves a longer word that merely starts like a uid alone', () => {
    const body = `${uid('a')}extra and phone`
    expect(splitPhotoRefs(body)).toEqual([{ kind: 'text', text: body }])
  })

  it('yields nothing for an empty body and one text segment for plain prose', () => {
    expect(splitPhotoRefs('')).toEqual([])
    expect(splitPhotoRefs('1987.')).toEqual([{ kind: 'text', text: '1987.' }])
  })
})

describe('referencedPhotoUids', () => {
  it('lists each uid once, in order of first mention', () => {
    const a = uid('a')
    const b = uid('b')
    expect(referencedPhotoUids(`${b} then ${a} and ${b} again`)).toEqual([b, a])
  })
})

describe('photoRefHref', () => {
  it('links the detail page, carrying the task context when there is one', () => {
    expect(photoRefHref(uid('a'))).toBe(`/photos/${uid('a')}`)
    expect(photoRefHref(uid('a'), '')).toBe(`/photos/${uid('a')}`)
    expect(photoRefHref(uid('a'), 'tk_1')).toBe(`/photos/${uid('a')}?task=tk_1`)
  })
})

describe('discussionBody', () => {
  it('writes the message, a blank line, then one uid per line', () => {
    expect(discussionBody('  these are wrong \n', [uid('a'), uid('b')])).toBe(
      `these are wrong\n\n${uid('a')}\n${uid('b')}`,
    )
  })

  it('writes only the uids when the message is empty', () => {
    expect(discussionBody('   ', [uid('a')])).toBe(uid('a'))
  })
})

describe('photoRefsThatFit', () => {
  it('fits as many uids as the limit leaves room for', () => {
    // 2 uids + 1 newline = 53 characters exactly.
    expect(photoRefsThatFit('', 2 * PHOTO_UID_LENGTH + 1)).toBe(2)
    expect(photoRefsThatFit('', 2 * PHOTO_UID_LENGTH)).toBe(1)
    expect(photoRefsThatFit('', PHOTO_UID_LENGTH - 1)).toBe(0)
  })

  it('charges the message and its blank line first', () => {
    // "hi" + "\n\n" = 4, leaving 26 for exactly one uid.
    expect(photoRefsThatFit('hi', 30)).toBe(1)
    expect(photoRefsThatFit('hi', 29)).toBe(0)
    // Surrounding whitespace is trimmed off before it is charged.
    expect(photoRefsThatFit('  hi  ', 30)).toBe(1)
  })

  it('never goes negative under a message that fills the limit', () => {
    expect(photoRefsThatFit('x'.repeat(50), 40)).toBe(0)
  })

  it('uses the comment limit by default', () => {
    // (2000 + 1) / 27 = 74.1…
    expect(photoRefsThatFit('')).toBe(74)
  })
})
