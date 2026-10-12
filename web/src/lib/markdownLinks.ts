import type { Root, RootContent, Text } from 'mdast'
import remarkGfm from 'remark-gfm'
import remarkParse from 'remark-parse'
import { unified } from 'unified'

import { PHOTO_UID_PATTERN, photoRefHref, splitPhotoRefs } from './photoRefs'

/**
 * Links inside Markdown somebody typed — a task's body, a comment, the
 * first-sign-in welcome — and what each of them should do when followed.
 *
 * Most links the curating agent writes point back into Kukátko itself
 * (`https://fotky…/photos/<uid>`, `/tasks/<uid>`). Opening those in a new tab
 * scatters the reader's session across tabs, and a full page load throws away
 * the app's state, so they become in-app navigations. Everything else leaves the
 * app and keeps the new tab. The decision is pure — the origin comes in as an
 * argument — so the renderer and the comment's thumbnail strip agree on what
 * counts as "our own" without either touching `window` in a test.
 */

/**
 * What following a link does.
 *
 * - `app`: a page of this app — a react-router navigation to `to`, same tab.
 * - `page`: stays in this tab as a plain anchor — an in-page `#anchor`, an own
 *   server route that is not a page of the SPA (`/api/…`), or a scheme the app
 *   does not handle (`mailto:`).
 * - `external`: another site — a new tab, `noopener noreferrer`.
 */
export type MarkdownLinkTarget =
  | { kind: 'app'; to: string }
  | { kind: 'page'; href: string }
  | { kind: 'external'; href: string }

/**
 * Own-origin paths the server answers itself rather than the SPA: a router
 * navigation there would render the app's 404 instead of the file.
 */
const SERVER_PATH_PREFIXES = ['/api/']

/**
 * A photo's detail page, with the uid captured: `/photos/<uid>` as links were
 * written before projects existed, or `/p/<slug>/photos/<uid>` inside one.
 */
const PHOTO_PATH = /^(?:\/p\/[^/]+)?\/photos\/([^/]+)$/

/** The leading `/p/<slug>` of a pathname, when it lives in a project. */
const PROJECT_PREFIX = /^\/p\/[^/]+/

/**
 * The path, query and fragment of an href when it points at this app's own
 * origin — a root-relative `/…` or an absolute URL whose origin is `origin` —
 * and null for anything else (another site, a scheme-relative `//host`, an
 * unparsable string).
 */
function ownPath(href: string, origin: string): URL | null {
  if (href.startsWith('/') && !href.startsWith('//')) {
    return new URL(href, origin)
  }
  let url: URL
  try {
    url = new URL(href)
  } catch {
    return null
  }
  return url.origin === origin ? url : null
}

/**
 * Decides what a link in rendered Markdown does. Inside a task (`taskUid` set),
 * a bare link to a photo's page gains `?task=` exactly as {@link photoRefHref}
 * writes it, so the viewer pages within the task's group and Back returns to
 * the question; a link that already carries a query is left as written.
 *
 * An `app` target keeps the project the link names (`/p/<slug>/…`); a link
 * written without one — every comment from before projects existed — comes
 * back as the page's own path, which the renderer resolves into the current
 * project the way it resolves any other in-app link.
 */
export function resolveMarkdownLink(
  href: string,
  origin: string,
  taskUid?: string,
): MarkdownLinkTarget {
  if (href.startsWith('#')) {
    return { kind: 'page', href }
  }
  const own = ownPath(href, origin)
  if (own === null) {
    return /^https?:/i.test(href) ? { kind: 'external', href } : { kind: 'page', href }
  }
  if (SERVER_PATH_PREFIXES.some((prefix) => own.pathname.startsWith(prefix))) {
    return { kind: 'page', href: own.pathname + own.search + own.hash }
  }
  const photo = PHOTO_PATH.exec(own.pathname)
  if (photo !== null && own.search === '') {
    const prefix = PROJECT_PREFIX.exec(own.pathname)?.[0] ?? ''
    return {
      kind: 'app',
      to: prefix + photoRefHref(decodeURIComponent(photo[1]), taskUid) + own.hash,
    }
  }
  return { kind: 'app', to: own.pathname + own.search + own.hash }
}

/**
 * The photo uid an own-origin `/photos/<uid>` link (with or without its
 * `/p/<slug>` project prefix) points at, or null for any
 * other link — the second source (next to bare uid tokens) of the thumbnails
 * under a comment.
 */
export function photoUidFromHref(href: string, origin: string): string | null {
  const own = ownPath(href, origin)
  if (own === null) {
    return null
  }
  const photo = PHOTO_PATH.exec(own.pathname)
  if (photo === null) {
    return null
  }
  const uid = decodeURIComponent(photo[1])
  return PHOTO_UID_PATTERN.test(uid) ? uid : null
}

/**
 * Splits one text node at its photo uid tokens, turning each token into a link
 * to the photo with the uid as its text.
 */
function textWithPhotoRefs(node: Text): RootContent[] {
  const segments = splitPhotoRefs(node.value)
  if (segments.length === 1 && segments[0].kind === 'text') {
    return [node]
  }
  return segments.map((segment) =>
    segment.kind === 'text'
      ? { type: 'text', value: segment.text }
      : {
          type: 'link',
          url: photoRefHref(segment.uid),
          children: [{ type: 'text', value: segment.uid }],
        },
  )
}

/**
 * Rewrites a tree in place: every photo uid token in plain text becomes a
 * link. Code is never touched (`inlineCode` and `code` carry a value, not text
 * children), and neither is text that already sits inside a link.
 */
function linkPhotoRefs(parent: { children: RootContent[] }): void {
  const children: RootContent[] = []
  for (const child of parent.children) {
    if (child.type === 'text') {
      children.push(...textWithPhotoRefs(child))
      continue
    }
    if (child.type !== 'link' && child.type !== 'linkReference' && 'children' in child) {
      linkPhotoRefs(child)
    }
    children.push(child)
  }
  parent.children = children
}

/**
 * A remark plugin turning bare photo uids in the text into links to the photo
 * — what a comment has always done for "these three are wrong: ph…", now as a
 * step of the Markdown pipeline so it can stay out of code spans and links.
 */
export function remarkPhotoRefs() {
  return (tree: Root) => {
    linkPhotoRefs(tree)
  }
}

/** The parser the strip reads a body with — the renderer's own syntax. */
const photoRefParser = unified().use(remarkParse).use(remarkGfm).use(remarkPhotoRefs)

/**
 * The distinct photos a Markdown body refers to, in order of first mention:
 * bare uid tokens outside code and own-origin `/photos/<uid>` links (written
 * links, autolinked URLs and reference definitions alike). It parses the body
 * with the same syntax the renderer uses, so a uid the reader sees as code is
 * not counted and a uid the reader sees as a link is.
 */
export function markdownPhotoUids(body: string, origin: string): string[] {
  const tree = photoRefParser.parse(body)
  // The plugins rewrite the tree in place; the parsed root is the result.
  photoRefParser.runSync(tree)
  const seen = new Set<string>()
  const walk = (node: Root | RootContent) => {
    if (node.type === 'link' || node.type === 'definition') {
      const uid = photoUidFromHref(node.url, origin)
      if (uid !== null) {
        seen.add(uid)
      }
    }
    if ('children' in node) {
      for (const child of node.children) {
        walk(child)
      }
    }
  }
  walk(tree)
  return [...seen]
}
