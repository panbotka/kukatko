import type { TFunction } from 'i18next'
import { describe, expect, it } from 'vitest'

import { COMMENT_EXCERPT_MAX, commentExcerpt, sourceLabel } from './taskLedger'

/** A comment with the given body; the rest is what any comment carries. */
function comment(body: string) {
  return {
    uid: 'c1',
    body,
    author_uid: 'u1',
    author_name: 'Agent',
    created_at: '2026-09-20T10:00:00Z',
  }
}

/** A translator that echoes the key, so a test can see which one was asked for. */
const t = ((key: string) => key) as unknown as TFunction

describe('commentExcerpt', () => {
  it('shows the first line only — the summary by convention', () => {
    expect(commentExcerpt(comment('Dated from the reunion badge.\n\nDetails follow.'))).toBe(
      'Dated from the reunion badge.',
    )
  })

  it('skips leading blank lines and collapses inner whitespace', () => {
    expect(commentExcerpt(comment('\n  \n  Set   to\tJune 1974  '))).toBe('Set to June 1974')
  })

  it('cuts a long line with an ellipsis', () => {
    const long = 'x'.repeat(COMMENT_EXCERPT_MAX + 50)
    const excerpt = commentExcerpt(comment(long))
    expect(excerpt.endsWith('…')).toBe(true)
    expect(excerpt).toHaveLength(COMMENT_EXCERPT_MAX)
  })

  it('is empty for no comment, or a comment of nothing but whitespace', () => {
    expect(commentExcerpt(null)).toBe('')
    expect(commentExcerpt(undefined)).toBe('')
    expect(commentExcerpt(comment('  \n \n'))).toBe('')
  })
})

describe('sourceLabel', () => {
  it("names a known source through the technical panel's dictionary", () => {
    expect(sourceLabel('exif', t)).toBe('photo.technical.takenAtSourceValue.exif')
    expect(sourceLabel('manual', t)).toBe('photo.technical.takenAtSourceValue.manual')
    expect(sourceLabel('unknown', t)).toBe('photo.technical.takenAtSourceValue.unknown')
  })

  it('shows an unrecognised source verbatim rather than as "unknown"', () => {
    expect(sourceLabel('estimate', t)).toBe('estimate')
  })

  it('is empty when no source is recorded', () => {
    expect(sourceLabel(undefined, t)).toBe('')
    expect(sourceLabel('  ', t)).toBe('')
  })
})
