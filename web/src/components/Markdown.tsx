import type { AnchorHTMLAttributes } from 'react'
import ReactMarkdown from 'react-markdown'
import rehypeSanitize from 'rehype-sanitize'

/** Props for {@link Markdown}. */
export interface MarkdownProps {
  /** The Markdown source. An empty string renders nothing. */
  children: string
  /** Extra classes for the wrapper element. */
  className?: string
}

/**
 * Renders an anchor produced by the Markdown, opening it in a new tab.
 *
 * A link inside instance-authored prose points away from the app — the family
 * calendar, a shared folder — and following it in the same tab throws away
 * whatever the reader was doing. `rel` carries the usual pair: `noopener` so the
 * opened page cannot reach back through `window.opener`, `noreferrer` so it is
 * not told where the visitor came from.
 */
function MarkdownLink({ children, ...props }: AnchorHTMLAttributes<HTMLAnchorElement>) {
  return (
    <a {...props} target="_blank" rel="noopener noreferrer">
      {children}
    </a>
  )
}

/**
 * The app's one Markdown renderer, for wherever prose somebody typed is shown as
 * formatted text. Its first caller is the administrator's live preview of the
 * first-sign-in welcome (`SettingsPage`); when the welcome itself gets a screen
 * it renders through this same component, so the preview is not a lookalike of
 * the real thing but literally the same renderer.
 *
 * Sanitising is not optional even though only an administrator can write the
 * text. `rehype-sanitize` runs on the rendered tree with its default (GitHub)
 * schema, so a `javascript:` href, an `onclick`, an `<iframe>` or a `<style>`
 * block cannot survive the trip — an administrator account that gets taken over
 * must not become a way to run script in every other user's browser, and the
 * welcome text is shown to everybody exactly once, unprompted.
 *
 * Raw HTML in the source is inert for a second reason as well: `rehype-raw` is
 * deliberately not installed, so react-markdown never parses HTML in the first
 * place. The sanitiser is the belt to that pair of braces.
 */
export function Markdown({ children, className }: MarkdownProps) {
  return (
    <div className={className}>
      <ReactMarkdown rehypePlugins={[rehypeSanitize]} components={{ a: MarkdownLink }}>
        {children}
      </ReactMarkdown>
    </div>
  )
}
