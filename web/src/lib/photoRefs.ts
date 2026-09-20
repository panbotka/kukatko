import { MAX_COMMENT_LENGTH } from '../services/comments'

/**
 * References to photographs inside plain-text comments.
 *
 * A comment body is stored and rendered as text — the backend parses nothing —
 * but a photo uid dropped into it ("these three are wrong: phabc…") is the one
 * token worth recognising: it is how a reviewer points at a picture from the
 * discussion, and how `kukatko ctl tasks comment` has always pointed at one.
 * This module is the pure half of that: it finds the uids, builds the links
 * they become, writes the body the batch bar posts for a selection, and says
 * how many uids fit under the comment length limit. Nothing here touches the
 * DOM or the network, so the renderer and the composer share one definition of
 * what a reference is.
 */

/**
 * The shape of a photo uid: `ph` and 24 characters of the catalogue's base32
 * alphabet (digits and `a`–`v`, see `internal/photos/uid.go`). Word-bounded, so
 * a uid glued to punctuation ("phabc…," / "(phabc…)") is still found and a
 * longer word that merely starts with one is not.
 */
const PHOTO_UID_SOURCE = String.raw`\bph[0-9a-v]{24}\b`

/** The pattern of one photo uid, anchored — for testing a single token. */
export const PHOTO_UID_PATTERN = new RegExp(`^${PHOTO_UID_SOURCE}$`)

/** The length of one uid — two for the prefix, twenty-four for the suffix. */
export const PHOTO_UID_LENGTH = 26

/** One piece of a tokenised body: either text as written, or a recognised uid. */
export type PhotoRefSegment = { kind: 'text'; text: string } | { kind: 'uid'; uid: string }

/**
 * Splits a body into the text between uids and the uids themselves, in order,
 * so a renderer can keep every character the writer typed and swap only the
 * recognised tokens for links. An empty body yields no segments.
 */
export function splitPhotoRefs(body: string): PhotoRefSegment[] {
  const segments: PhotoRefSegment[] = []
  const pattern = new RegExp(PHOTO_UID_SOURCE, 'g')
  let last = 0
  for (const match of body.matchAll(pattern)) {
    if (match.index > last) {
      segments.push({ kind: 'text', text: body.slice(last, match.index) })
    }
    segments.push({ kind: 'uid', uid: match[0] })
    last = match.index + match[0].length
  }
  if (last < body.length) {
    segments.push({ kind: 'text', text: body.slice(last) })
  }
  return segments
}

/**
 * The distinct photo uids a body refers to, in order of first mention — the
 * list the thumbnail strip under a comment draws.
 */
export function referencedPhotoUids(body: string): string[] {
  const seen = new Set<string>()
  for (const segment of splitPhotoRefs(body)) {
    if (segment.kind === 'uid') {
      seen.add(segment.uid)
    }
  }
  return [...seen]
}

/**
 * Where a reference leads: the photo's detail page, and — from inside a task's
 * thread — carrying `?task=` so the viewer's prev/next walk the task's group
 * and Back returns to the question rather than to the whole library. The
 * parameter is the same one the task page's own tiles write (`LibraryView.task`).
 */
export function photoRefHref(uid: string, taskUid?: string): string {
  const path = `/photos/${encodeURIComponent(uid)}`
  return taskUid === undefined || taskUid === ''
    ? path
    : `${path}?task=${encodeURIComponent(taskUid)}`
}

/**
 * The body the batch bar posts for a selection: the message (trimmed), a blank
 * line, then one uid per line — or just the uids when the message is empty.
 * Plain text: a reader of the raw comment (the CLI, a mail) sees a list, and the
 * web renders each line as a link.
 */
export function discussionBody(message: string, uids: readonly string[]): string {
  const text = message.trim()
  const list = uids.join('\n')
  return text === '' ? list : `${text}\n\n${list}`
}

/**
 * How many uids fit into one comment next to `message` without crossing the
 * body length limit — each uid costs its own length plus the newline before it,
 * and a non-empty message costs its trimmed length plus the blank line. The
 * composer refuses a selection larger than this rather than letting the server
 * reject the post. Never negative: a message that alone fills the limit leaves
 * room for none.
 */
export function photoRefsThatFit(message: string, limit = MAX_COMMENT_LENGTH): number {
  const text = message.trim()
  const prefix = text === '' ? 0 : text.length + 2
  const room = limit - prefix
  // The first uid costs PHOTO_UID_LENGTH, every further one a newline more.
  if (room < PHOTO_UID_LENGTH) {
    return 0
  }
  return Math.floor((room + 1) / (PHOTO_UID_LENGTH + 1))
}
