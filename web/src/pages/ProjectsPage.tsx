import Badge from 'react-bootstrap/Badge'
import Button from 'react-bootstrap/Button'
import Card from 'react-bootstrap/Card'
import Col from 'react-bootstrap/Col'
import Container from 'react-bootstrap/Container'
import Row from 'react-bootstrap/Row'
import { useTranslation } from 'react-i18next'
import { Link, useNavigate } from 'react-router-dom'

import { useAuth } from '../auth/AuthContext'
import { EmptyState } from '../components/EmptyState'
import { Icon } from '../components/Icon'
import { useDocumentTitle } from '../hooks/useDocumentTitle'
import { useProject } from '../project/ProjectContext'
import { isEnterable, projectPathFor } from '../project/paths'
import type { ProjectMembership } from '../services/auth'

/**
 * Where a maintainer manages the instance's projects. The page itself arrives
 * in a later step of the multi-tenancy work; the link is offered already, and
 * until then it lands on the ordinary not-found page.
 */
export const PROJECT_ADMIN_PATH = '/system/projects'

/**
 * `/projects` — the project chooser: one card per membership of the account.
 *
 * It is where `/` and every old address land when the entry rule has no single
 * project to pick (several projects and none remembered, or none at all), and
 * where the header switcher's „Všechny projekty…" leads. It is signed-in but
 * outside any project, so it has no project `Layout` — its header carries the
 * two ways out an account needs whatever it belongs to: the account page and
 * sign-out (plus, for a maintainer, the project administration).
 *
 * A card shows the project's name, its description (clamped to three lines;
 * `GET /auth/me` does not send one yet, and an absent one is not drawn) and the
 * account's role in it. An enterable card — approved, in an active project — is
 * one big link to the project's home (a stretched link: the name is the link,
 * the whole card its target). A deactivated project is greyed with „Nedostupné",
 * an unapproved membership says „Čeká na schválení"; neither is a link. No
 * membership at all is an empty state asking for an invitation.
 */
export function ProjectsPage() {
  const { t } = useTranslation()
  const { projects } = useProject()
  const { isMaintainer, logout } = useAuth()
  const navigate = useNavigate()
  useDocumentTitle(t('projects.title'))

  async function handleLogout() {
    await logout()
    void navigate('/login', { replace: true })
  }

  return (
    <Container as="main" className="pt-3 pt-md-4 kukatko-main">
      <header className="d-flex flex-wrap align-items-center gap-2 mb-3">
        <h1 className="h3 mb-0 me-auto">{t('projects.title')}</h1>
        {isMaintainer && (
          <Link
            to={PROJECT_ADMIN_PATH}
            title={t('projects.manageTitle')}
            className="btn btn-outline-secondary btn-sm kukatko-tap-target d-inline-flex align-items-center gap-2"
          >
            <Icon name="gear" />
            {t('projects.manage')}
          </Link>
        )}
        <Link
          to="/account"
          className="btn btn-outline-secondary btn-sm kukatko-tap-target d-inline-flex align-items-center gap-2"
        >
          <Icon name="person-circle" />
          {t('projects.account')}
        </Link>
        <Button
          variant="outline-secondary"
          size="sm"
          className="kukatko-tap-target d-inline-flex align-items-center gap-2"
          onClick={() => {
            void handleLogout()
          }}
        >
          <Icon name="box-arrow-right" />
          {t('projects.logout')}
        </Button>
      </header>
      {projects.length === 0 ? (
        <EmptyState icon={<Icon name="folder2-open" />} title={t('projects.empty')} />
      ) : (
        <>
          <p className="text-body-secondary">{t('projects.intro')}</p>
          <Row xs={1} md={2} xl={3} className="g-3">
            {projects.map((membership) => (
              <Col key={membership.uid}>
                <ProjectCard membership={membership} />
              </Col>
            ))}
          </Row>
        </>
      )}
    </Container>
  )
}

/** One membership on the chooser; see {@link ProjectsPage}. */
function ProjectCard({ membership }: { membership: ProjectMembership }) {
  const { t } = useTranslation()
  const enterable = isEnterable(membership)
  // Why a card cannot be entered: the project is off (whatever the membership),
  // else the membership still waits for a project admin.
  const blocked = !membership.active ? 'unavailable' : !membership.approved ? 'pending' : null
  const description = membership.description?.trim() ?? ''

  return (
    <Card
      className={`kk-project-card h-100${blocked === 'unavailable' ? ' kk-project-card--inactive' : ''}${
        enterable ? ' kk-project-card--enterable' : ''
      }`}
      aria-disabled={enterable ? undefined : true}
      data-testid="project-card"
    >
      <Card.Body className="d-flex flex-column gap-2">
        <div className="d-flex align-items-start gap-2">
          <Card.Title as="h2" className="h5 mb-0 me-auto kk-project-card__name">
            {enterable ? (
              <Link
                to={projectPathFor(membership.slug, '/')}
                title={t('projects.open', { name: membership.name })}
                className="stretched-link"
              >
                {membership.name}
              </Link>
            ) : (
              membership.name
            )}
          </Card.Title>
          <Badge bg="secondary">{t(`roles.${membership.role}`)}</Badge>
        </div>
        {description !== '' && (
          <Card.Text className="kk-project-card__description">{description}</Card.Text>
        )}
        {blocked !== null && (
          <div>
            <Badge
              bg={blocked === 'pending' ? 'warning' : 'dark'}
              title={t(
                blocked === 'pending' ? 'projects.pendingTitle' : 'projects.unavailableTitle',
              )}
            >
              {t(blocked === 'pending' ? 'projects.pending' : 'projects.unavailable')}
            </Badge>
          </div>
        )}
      </Card.Body>
    </Card>
  )
}
