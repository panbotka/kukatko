import { type AnchorHTMLAttributes, type ComponentProps, createElement, useMemo } from 'react'
import ReactMarkdown, { type Components, type ExtraProps } from 'react-markdown'
import { Link, useInRouterContext } from 'react-router-dom'
import rehypeSanitize from 'rehype-sanitize'
import remarkBreaks from 'remark-breaks'
import remarkGfm from 'remark-gfm'

import { remarkPhotoRefs, resolveMarkdownLink } from '../lib/markdownLinks'
import { remarkEmptyLists } from '../lib/remarkEmptyLists'

/** A heading level a rendered `#` may be mapped to. */
export type HeadingLevel = 1 | 2 | 3 | 4 | 5 | 6

/** Props for {@link Markdown}. */
export interface MarkdownProps {
  /** The Markdown source. An empty string renders nothing. */
  children: string
  /** Extra classes for the wrapper element. */
  className?: string
  /**
   * A single newline is a line break, as typed (the `remark-breaks` behaviour)
   * — for short prose like a comment, where the writer's Enter means "new line"
   * rather than "same paragraph". Off: standard Markdown paragraphs.
   */
  breaks?: boolean
  /**
   * The level the source's `#` becomes; deeper headings follow, capped at 6.
   * For text shown under a page's own headings (a task body, a comment), so a
   * typed `# Title` does not outrank the page — the headings are then also drawn
   * small. Unset: the source's levels as written.
   */
  headingFloor?: HeadingLevel
  /** Bare photo uid tokens in the text become links to the photo. */
  photoRefs?: boolean
  /**
   * The task the text belongs to: a link to a photo's page then carries
   * `?task=`, so the viewer pages within the task's group and Back returns to it.
   */
  taskUid?: string
}

/** Props of the anchor react-markdown hands to {@link MarkdownLink}. */
type MarkdownLinkProps = AnchorHTMLAttributes<HTMLAnchorElement> & ExtraProps & { taskUid?: string }

/**
 * Renders an anchor produced by the Markdown, according to where it leads.
 *
 * A link into Kukátko itself — a root-relative `/…` or an absolute URL on this
 * origin — is an in-app react-router navigation in the same tab: following
 * `/photos/<uid>` from a task's discussion must not scatter the reader across
 * tabs or reload the app. Anything else points away from the app — the family
 * calendar, a shared folder — and following it in the same tab throws away
 * whatever the reader was doing, so it opens a new tab. `rel` carries the usual
 * pair: `noopener` so the opened page cannot reach back through
 * `window.opener`, `noreferrer` so it is not told where the visitor came from.
 *
 * Outside a router (a component rendered on its own) an own link is a plain
 * same-tab anchor — still the right page, just a full load.
 */
function MarkdownLink({ href, children, taskUid, node: _node, ...rest }: MarkdownLinkProps) {
  const inRouter = useInRouterContext()
  if (href === undefined) {
    // The sanitiser dropped the href (a `javascript:` URL): nothing to follow.
    return <a {...rest}>{children}</a>
  }
  const target = resolveMarkdownLink(href, window.location.origin, taskUid)
  if (target.kind === 'external') {
    return (
      <a {...rest} href={target.href} target="_blank" rel="noopener noreferrer">
        {children}
      </a>
    )
  }
  if (target.kind === 'app' && inRouter) {
    return (
      <Link {...rest} to={target.to}>
        {children}
      </Link>
    )
  }
  return (
    <a {...rest} href={target.kind === 'app' ? target.to : target.href}>
      {children}
    </a>
  )
}

/**
 * A GFM table, with Bootstrap's compact table look, inside a container that
 * scrolls sideways: a wide table on a 360px phone scrolls on its own instead of
 * pushing the whole page wider than the screen.
 */
function MarkdownTable({ node: _node, className, ...rest }: ComponentProps<'table'> & ExtraProps) {
  return (
    <div className="kk-markdown__table">
      <table {...rest} className={['table table-sm', className].filter(Boolean).join(' ')} />
    </div>
  )
}

/** The six heading tags, by level. */
const HEADING_TAGS = ['h1', 'h2', 'h3', 'h4', 'h5', 'h6'] as const

/** A component rendering a source heading of `level` at its shifted level. */
function shiftedHeading(level: number, floor: HeadingLevel) {
  const tag = HEADING_TAGS[Math.min(6, level + floor - 1) - 1]
  return function ShiftedHeading({ node: _node, ...rest }: ComponentProps<'h1'> & ExtraProps) {
    return createElement(tag, rest)
  }
}

/**
 * The app's one Markdown renderer, for wherever prose somebody typed is shown as
 * formatted text. Its callers are the administrator's live preview of the
 * first-sign-in welcome (`SettingsPage`) and the welcome itself
 * (`WelcomeModal`) — so the preview is not a lookalike of the real thing but
 * literally the same renderer — the body of a task (`TaskQuestion`), and every
 * comment, in a task's thread and a photo's alike (`CommentBody`).
 *
 * The syntax is CommonMark plus GitHub's extensions (`remark-gfm`): tables,
 * strikethrough, autolinked URLs and task lists, which is what the curating
 * agent writes. A table scrolls sideways inside its own container, a link into
 * the app stays in the tab (see {@link MarkdownLink}), and `breaks`,
 * `headingFloor`, `photoRefs` and `taskUid` tune it for text shown inside a
 * page rather than as one. A list of nothing but empty items — a comment that
 * is just `1987.` — shows as the text it was typed as (`remarkEmptyLists`).
 *
 * Sanitising is not optional, whoever writes the text. `rehype-sanitize` runs
 * on the rendered tree with its default (GitHub) schema, so a `javascript:`
 * href, an `onclick`, an `<iframe>` or a `<style>` block cannot survive the
 * trip — an account that gets taken over must not become a way to run script
 * in every other user's browser, and the welcome text is shown to everybody
 * exactly once, unprompted.
 *
 * Raw HTML in the source is inert for a second reason as well: `rehype-raw` is
 * deliberately not installed, so react-markdown never parses HTML in the first
 * place. The sanitiser is the belt to that pair of braces.
 */
export function Markdown({
  children,
  className,
  breaks = false,
  headingFloor,
  photoRefs = false,
  taskUid,
}: MarkdownProps) {
  const remarkPlugins = useMemo(
    () => [
      remarkGfm,
      remarkEmptyLists,
      ...(photoRefs ? [remarkPhotoRefs] : []),
      ...(breaks ? [remarkBreaks] : []),
    ],
    [breaks, photoRefs],
  )
  const components = useMemo<Components>(() => {
    const base: Components = {
      a: (props) => <MarkdownLink {...props} taskUid={taskUid} />,
      table: MarkdownTable,
    }
    if (headingFloor === undefined) {
      return base
    }
    return {
      ...base,
      h1: shiftedHeading(1, headingFloor),
      h2: shiftedHeading(2, headingFloor),
      h3: shiftedHeading(3, headingFloor),
      h4: shiftedHeading(4, headingFloor),
      h5: shiftedHeading(5, headingFloor),
      h6: shiftedHeading(6, headingFloor),
    }
  }, [headingFloor, taskUid])
  const classes = [
    'kk-markdown',
    headingFloor === undefined ? undefined : 'kk-markdown--embedded',
    className,
  ]
    .filter(Boolean)
    .join(' ')

  return (
    <div className={classes}>
      <ReactMarkdown
        remarkPlugins={remarkPlugins}
        rehypePlugins={[rehypeSanitize]}
        components={components}
      >
        {children}
      </ReactMarkdown>
    </div>
  )
}
