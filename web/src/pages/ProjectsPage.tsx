import Container from 'react-bootstrap/Container'
import ListGroup from 'react-bootstrap/ListGroup'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { NarrowPage } from '../components/NarrowPage'
import { useDocumentTitle } from '../hooks/useDocumentTitle'
import { useProject } from '../project/ProjectContext'
import { isEnterable, projectPathFor } from '../project/paths'

/**
 * `/projects` — the plain list of the account's projects, each a link to its
 * library. It is where `/` and every old address land when the entry rule has
 * no single project to pick: several projects and none remembered, or none at
 * all. A project that cannot be entered (deactivated, or a membership still
 * awaiting approval) is listed without a link and says why.
 *
 * A placeholder: the real chooser (descriptions, roles) is the next step.
 */
export function ProjectsPage() {
  const { t } = useTranslation()
  const { projects } = useProject()
  useDocumentTitle(t('projects.title'))

  return (
    <Container as="main" className="pt-3 pt-md-4">
      <NarrowPage>
        <h1 className="h3">{t('projects.title')}</h1>
        {projects.length === 0 ? (
          <p className="text-body-secondary">{t('projects.empty')}</p>
        ) : (
          <>
            <p className="text-body-secondary">{t('projects.intro')}</p>
            <ListGroup>
              {projects.map((membership) =>
                isEnterable(membership) ? (
                  <ListGroup.Item
                    key={membership.uid}
                    action
                    as={Link}
                    to={projectPathFor(membership.slug, '/')}
                  >
                    {membership.name}
                  </ListGroup.Item>
                ) : (
                  <ListGroup.Item key={membership.uid} className="text-body-secondary">
                    {membership.name}{' '}
                    <small>
                      ({membership.active ? t('projects.pending') : t('projects.unavailable')})
                    </small>
                  </ListGroup.Item>
                ),
              )}
            </ListGroup>
          </>
        )}
      </NarrowPage>
    </Container>
  )
}
