import type { ReactNode } from 'react'

interface NarrowPageProps {
  /** The whole page: its `<h1>`, intro and the cards or sections below them. */
  children: ReactNode
}

/**
 * The column every reading and form page sits in — the account, the instance
 * settings, the help and the leaderboard — so switching between them does not
 * change the page's width. It is capped at a measure a line of text can be
 * followed across (about seventy characters of body copy) and centred in the
 * shell's container; below that width it is simply the container.
 *
 * Grid and list pages (the library, albums, people, tasks, the admin tables)
 * keep the full container: they show more when they have more room. One
 * component carries the width so no page keeps a magic column span of its own.
 */
export function NarrowPage({ children }: NarrowPageProps) {
  return (
    <div className="kk-page-narrow" data-testid="narrow-page">
      {children}
    </div>
  )
}
