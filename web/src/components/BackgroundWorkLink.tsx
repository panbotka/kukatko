import Card from 'react-bootstrap/Card'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

/** Props of {@link BackgroundWorkLink}. */
export interface BackgroundWorkLinkProps {
  /** The section heading, already translated. */
  title: string
  /** What background work this page starts, already translated. */
  intro: string
}

/**
 * Points an operations page at the job queue instead of showing a copy of it.
 * The queue is read — and acted on, per type and state, with the requeue of the
 * permanently failed — only on System status (`/system`), so a page that starts
 * background work names it and links there rather than rendering counts nobody
 * can do anything about from where they stand. Every host is maintainer-only, as
 * `/system` is, so the link never leads somewhere the reader cannot go.
 */
export function BackgroundWorkLink({ title, intro }: BackgroundWorkLinkProps) {
  const { t } = useTranslation()
  return (
    <Card className="mb-4">
      <Card.Body>
        <h2 className="kk-section-title mb-1">{title}</h2>
        <p className="text-secondary small">{intro}</p>
        <Link to="/system" className="btn btn-outline-primary btn-sm">
          {t('jobStates.systemLink')}
        </Link>
      </Card.Body>
    </Card>
  )
}
