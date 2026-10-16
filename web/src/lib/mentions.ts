import type { Parent, PhrasingContent, Root, RootContent } from 'mdast'
import { defaultSchema, type Options as SanitizeSchema } from 'rehype-sanitize'

import { foldText } from './text'

/**
 * `@`-mentions in a comment: how one is written, how it is found in the
 * rendered Markdown, and how the composer offers and inserts one.
 *
 * A mention is stored as `@[Petr Kolář](user:<uid>)` — an `@` straight before
 * a Markdown link whose destination is the `user:` pseudo-scheme and the
 * account's uid. The uid is what counts: a renamed account keeps every old
 * mention, and the name in the token is only what to show once the account is
 * gone. The syntax is a link so it survives every Markdown pipeline untouched,
 * and the backend (`internal/comments` `ParseMentions`) recognises exactly this
 * shape, so what the thread highlights and what the server announces agree.
 *
 * A bare `@Tomáš` typed without picking from the list stays plain text: a
 * mention is a chosen account, never a guess at one.
 */

/** The pseudo-scheme of a mention token's link destination. */
export const MENTION_SCHEME = 'user:'

/** The class the rendered mention element carries; the renderer maps it to a chip. */
export const MENTION_CLASS = 'kk-mention'

/** The id of a comment's element on its page, for the `#comment-<uid>` anchor. */
export function commentAnchorId(uid: string): string {
  return `comment-${uid}`
}

/**
 * The token the composer inserts for the account `uid` shown as `name`.
 * Brackets and line breaks in the name would end the token early, so they go.
 */
export function mentionToken(name: string, uid: string): string {
  const clean = name.trim().replace(/[[\]]/g, '').replace(/\n/g, ' ')
  return `@[${clean}](${MENTION_SCHEME}${uid})`
}

/** The uid a mention link points at, or null for any other link. */
function mentionUid(url: string): string | null {
  if (!url.startsWith(MENTION_SCHEME)) {
    return null
  }
  const uid = url.slice(MENTION_SCHEME.length)
  return /^[A-Za-z0-9_-]{1,32}$/.test(uid) ? uid : null
}

/** The plain text of a node's children, for the name a mention link carries. */
function textOf(node: Parent): string {
  return node.children
    .map((child) => ('value' in child ? child.value : 'children' in child ? textOf(child) : ''))
    .join('')
}

/**
 * Rewrites a tree in place: every `@` followed by a `user:` link becomes a
 * mention node, rendered as `<span class="kk-mention" data-mention-uid
 * data-mention-name>` with `@name` as its text. Code is never touched —
 * `inlineCode` and `code` carry a value, not children — and a `user:` link
 * without the `@` is left to the sanitiser, which strips its unknown scheme.
 */
function linkMentions(parent: Parent): void {
  const children: RootContent[] = []
  for (const child of parent.children) {
    const previous = children.at(-1)
    const uid = child.type === 'link' ? mentionUid(child.url) : null
    if (uid !== null && previous?.type === 'text' && previous.value.endsWith('@')) {
      const name = textOf(child as Parent).trim() || uid
      previous.value = previous.value.slice(0, -1)
      if (previous.value === '') {
        children.pop()
      }
      children.push(mentionNode(uid, name))
      continue
    }
    if ('children' in child) {
      linkMentions(child)
    }
    children.push(child)
  }
  parent.children = children
}

/**
 * The mdast node of one mention. It is a node type mdast-util-to-hast does not
 * know, so it renders it through `data.hName`/`hProperties` — the documented
 * way to emit a custom element — with its text as the child.
 */
function mentionNode(uid: string, name: string): PhrasingContent {
  const node = {
    type: 'mention',
    children: [{ type: 'text', value: `@${name}` }],
    data: {
      hName: 'span',
      hProperties: { className: [MENTION_CLASS], dataMentionUid: uid, dataMentionName: name },
    },
  }
  return node as unknown as PhrasingContent
}

/** A remark plugin turning `@[name](user:uid)` tokens into mention nodes. */
export function remarkMentions() {
  return (tree: Root) => {
    linkMentions(tree)
  }
}

/**
 * The sanitiser's default (GitHub) schema widened for exactly the mention
 * element: a `span` may carry the `kk-mention` class — no other — and the two
 * `data-mention-*` attributes the chip reads. Nothing else changes, so a
 * `javascript:` href, an `onclick` or an `<iframe>` die as before.
 */
export const mentionSanitizeSchema: SanitizeSchema = {
  ...defaultSchema,
  attributes: {
    ...defaultSchema.attributes,
    span: [
      ...(defaultSchema.attributes?.span ?? []),
      ['className', MENTION_CLASS],
      'dataMentionUid',
      'dataMentionName',
    ],
  },
}

/** The `@` query the caret sits in, as the composer sees it. */
export interface MentionQuery {
  /** Index of the `@` in the text. */
  start: number
  /** What was typed after the `@`, up to the caret. */
  query: string
}

/** How far back from the caret an `@` is looked for — longer than any name. */
const MAX_QUERY_LEN = 40

/**
 * Finds the mention being typed at `caret`: the nearest `@` before it that
 * starts a word (the start of the text, or after whitespace or an opening
 * bracket), with nothing but name-like text between — no line break, no
 * brackets (an already inserted token), not starting with a space — and at
 * most {@link MAX_QUERY_LEN} characters of it. Null when the caret is not in
 * one, which closes the list.
 */
export function findMentionQuery(text: string, caret: number): MentionQuery | null {
  const from = Math.max(0, caret - MAX_QUERY_LEN - 1)
  for (let i = caret - 1; i >= from; i--) {
    const ch = text[i]
    if (ch === '\n' || ch === '[' || ch === ']' || ch === '(' || ch === ')') {
      return null
    }
    if (ch !== '@') {
      continue
    }
    const before = i === 0 ? ' ' : text[i - 1]
    if (!/[\s(]/.test(before)) {
      return null
    }
    const query = text.slice(i + 1, caret)
    if (query.startsWith(' ')) {
      return null
    }
    return { start: i, query }
  }
  return null
}

/** Somebody who can be mentioned: an account of the directory. */
export interface MentionCandidate {
  uid: string
  name: string
}

/** How many names the list offers at once. */
export const MAX_MENTION_SUGGESTIONS = 8

/**
 * The accounts matching `query`, accent- and case-insensitively: names with a
 * word starting with the query first, then names merely containing it, each
 * group in the directory's own order, at most {@link MAX_MENTION_SUGGESTIONS}.
 * An empty query offers the first names of the directory.
 */
export function filterMentionCandidates<T extends MentionCandidate>(
  users: readonly T[],
  query: string,
): T[] {
  const q = foldText(query)
  if (q === '') {
    return users.slice(0, MAX_MENTION_SUGGESTIONS)
  }
  const starts: T[] = []
  const contains: T[] = []
  for (const user of users) {
    const folded = foldText(user.name)
    if (folded.startsWith(q) || folded.split(/\s+/).some((word) => word.startsWith(q))) {
      starts.push(user)
    } else if (folded.includes(q)) {
      contains.push(user)
    }
  }
  return [...starts, ...contains].slice(0, MAX_MENTION_SUGGESTIONS)
}

/** The text after a mention was picked, and where the caret belongs in it. */
export interface MentionInsertion {
  text: string
  caret: number
}

/**
 * Replaces the `@query` at `at` (through `caret`) with the token of `user` and
 * a space, so typing goes on after the mention.
 */
export function insertMention(
  text: string,
  at: MentionQuery,
  caret: number,
  user: MentionCandidate,
): MentionInsertion {
  const token = `${mentionToken(user.name, user.uid)} `
  const before = text.slice(0, at.start)
  return { text: before + token + text.slice(caret), caret: before.length + token.length }
}
