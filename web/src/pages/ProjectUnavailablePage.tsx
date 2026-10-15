import { useContext } from 'react'
import { useTranslation } from 'react-i18next'
import { Link, useLocation } from 'react-router-dom'

import { EmptyState } from '../components/EmptyState'
import { Icon } from '../components/Icon'
import { useDocumentTitle } from '../hooks/useDocumentTitle'
import { ProjectContext } from '../project/ProjectContext'
import { PROJECTS_PATH, projectSlugInPath } from '../project/paths'

/**
 * What `/p/<slug>` shows a member of a project that has been deactivated — the
 * SPA's side of the API's 404 for it. It names the project and says it is
 * switched off, exactly as the chooser's "unavailable" card does, and points
 * back to the chooser. A stranger to the project never gets it: without a
 * membership the address is the ordinary not-found page.
 */
export function ProjectUnavailablePage() {
  const { t } = useTranslation()
  const { pathname } = useLocation()
  const slug = projectSlugInPath(pathname)
  const name = useContext(ProjectContext)?.projects.find((m) => m.slug === slug)?.name ?? slug ?? ''
  useDocumentTitle(t('projectUnavailable.title'))

  return (
    <div data-testid="project-unavailable-page">
      <EmptyState
        titleAs="h1"
        icon={<Icon name="slash-circle" />}
        title={t('projectUnavailable.title')}
        hint={t('projectUnavailable.message', { project: name })}
        action={
          <Link to={PROJECTS_PATH} className="btn btn-primary">
            {t('projectUnavailable.projects')}
          </Link>
        }
      />
    </div>
  )
}
