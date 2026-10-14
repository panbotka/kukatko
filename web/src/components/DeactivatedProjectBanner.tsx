import { useContext } from 'react'
import Alert from 'react-bootstrap/Alert'
import { Trans } from 'react-i18next'
import { Link } from 'react-router-dom'

import { ProjectContext } from '../project/ProjectContext'

import { Icon } from './Icon'
import { systemProjectPath } from './navItems'

/**
 * The warning a maintainer sees on every page of a deactivated project: only
 * the system administrator can enter one, so whoever is looking at it should
 * know that its members cannot — and where it is switched back on. It reads the
 * project in the context and renders nothing unless the address names a
 * project that is not active (an instance page, whose project is only the entry
 * rule's fallback, never shows it). Not dismissible: it is a state, not news.
 */
export function DeactivatedProjectBanner() {
  const ctx = useContext(ProjectContext)
  const project = ctx?.project ?? null
  if (ctx === null || !ctx.inAddress || project === null || project.active) {
    return null
  }
  return (
    <Alert
      variant="warning"
      className="d-flex align-items-start gap-2"
      data-testid="deactivated-project-banner"
    >
      <Icon name="exclamation-triangle" className="mt-1 flex-shrink-0" />
      <span className="text-break">
        <Trans
          i18nKey="projectDeactivated.banner"
          values={{ name: project.name }}
          components={{
            strong: <strong />,
            manage: (
              <Link
                to={systemProjectPath(project.slug)}
                className="text-reset text-decoration-underline"
              />
            ),
          }}
        />
      </span>
    </Alert>
  )
}
