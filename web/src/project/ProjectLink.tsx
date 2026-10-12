import { forwardRef } from 'react'
import { Link, type LinkProps } from 'react-router-dom'

import { useProjectPath } from './ProjectContext'

/** Props of {@link ProjectLink}: a router `Link` whose `to` is a page's own path. */
export interface ProjectLinkProps extends Omit<LinkProps, 'to'> {
  /** The page's own absolute path (`/albums/abc`), resolved in the current project. */
  to: string
}

/**
 * A router `Link` into the current project: `to="/albums/abc"` renders
 * `/p/<slug>/albums/abc`. Instance-level pages and addresses already carrying a
 * project pass through unchanged (see `useProjectPath`).
 */
export const ProjectLink = forwardRef<HTMLAnchorElement, ProjectLinkProps>(function ProjectLink(
  { to, ...rest },
  ref,
) {
  const projectPath = useProjectPath()
  return <Link ref={ref} to={projectPath(to)} {...rest} />
})
