import type { TFunction } from 'i18next'

import { type LastComment } from '../services/photos'

import { takenAtSource } from './photoFacts'
import { truncateText } from './text'

/**
 * How much of a comment a ledger row shows. The agent's convention is that the
 * first line of its note is the summary — what it did and why — and the whole
 * thread is one click away on the photo, so a row shows the first line and no
 * more of it than fits a line of a table.
 */
export const COMMENT_EXCERPT_MAX = 200

/**
 * The one line a ledger row shows of a comment: its first non-blank line,
 * collapsed to single spacing and cut at {@link COMMENT_EXCERPT_MAX} with an
 * ellipsis. A comment that is nothing but whitespace yields `''`, which the row
 * renders as "no comment" — a blank line would look like a rendering fault.
 */
export function commentExcerpt(comment: LastComment | null | undefined): string {
  if (comment === null || comment === undefined) {
    return ''
  }
  const first = comment.body
    .split('\n')
    .map((line) => line.replace(/\s+/g, ' ').trim())
    .find((line) => line !== '')
  return first === undefined ? '' : truncateText(first, COMMENT_EXCERPT_MAX)
}

/**
 * The label of a capture-date source in a ledger row's badge: the name the
 * technical panel already uses for a source this version knows (`EXIF`, `Set
 * manually`, …) and the stored value verbatim for one it does not, so a source
 * an agent introduced tomorrow (`estimate`, say) is still read rather than
 * rendered as "unknown". `''` when the photo records no source at all.
 */
export function sourceLabel(source: string | undefined, t: TFunction): string {
  if (source === undefined || source.trim() === '') {
    return ''
  }
  const raw = source.trim()
  const known = takenAtSource(raw)
  if (known === undefined || (known === 'unknown' && raw.toLowerCase() !== 'unknown')) {
    return raw
  }
  return t(`photo.technical.takenAtSourceValue.${known}`)
}
