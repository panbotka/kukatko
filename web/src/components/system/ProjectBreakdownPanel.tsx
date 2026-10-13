import Badge from 'react-bootstrap/Badge'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { formatBytes, formatCount } from '../../lib/format'
import { formatRelativeTime } from '../../lib/relativeTime'
import { projectPathFor } from '../../project/paths'
import type { ProjectSummary } from '../../services/system'
import { RecordTable, type RecordColumn } from '../RecordTable'

/**
 * The per-project breakdown of the dashboard: one row per project, saying what
 * its catalogue holds and what is still queued for it. Every other section of
 * the page is an instance total; this is how those totals divide, so an
 * operator can see which library the queue, the trash or the disk is going to.
 *
 * A deactivated project keeps its row (its data still occupies the disk) with a
 * badge, and its duplicate count stays "—": the background scan visits active
 * projects only. The project name leads into that project's library.
 */
export function ProjectBreakdownPanel({ projects }: { projects: readonly ProjectSummary[] }) {
  const { t, i18n } = useTranslation()
  const locale = i18n.language
  const count = (value: number) => formatCount(value, locale)

  const columns: RecordColumn<ProjectSummary>[] = [
    {
      key: 'project',
      header: t('system.projects.project'),
      cell: (p) => (
        <>
          <Link to={projectPathFor(p.slug, '/')}>{p.name}</Link>{' '}
          <span className="text-secondary small">/{p.slug}</span>
          {!p.active && (
            <>
              {' '}
              <Badge bg="secondary">{t('system.projects.inactive')}</Badge>
            </>
          )}
        </>
      ),
    },
    { key: 'photos', header: t('system.projects.photos'), cell: (p) => count(p.photos) },
    { key: 'videos', header: t('system.projects.videos'), cell: (p) => count(p.videos) },
    { key: 'trashed', header: t('system.projects.trashed'), cell: (p) => count(p.trashed) },
    {
      key: 'originals',
      header: t('system.projects.originals'),
      cell: (p) => formatBytes(p.originals_bytes, locale),
      cellClassName: 'text-nowrap',
    },
    { key: 'faces', header: t('system.projects.faces'), cell: (p) => count(p.faces) },
    { key: 'subjects', header: t('system.projects.subjects'), cell: (p) => count(p.subjects) },
    { key: 'albums', header: t('system.projects.albums'), cell: (p) => count(p.albums) },
    {
      key: 'queue',
      header: t('system.projects.queue'),
      cell: (p) =>
        t('system.projects.queueValue', {
          queued: count(p.queue.queued),
          running: count(p.queue.running),
          failed: count(p.queue.failed),
        }),
      cellClassName: 'text-nowrap',
    },
    {
      key: 'duplicates',
      header: t('system.projects.duplicates'),
      cell: (p) => (p.duplicates === null ? '—' : count(p.duplicates)),
    },
    {
      key: 'import',
      header: t('system.projects.lastImport'),
      cell: (p) =>
        p.last_import === null
          ? t('system.projects.neverImported')
          : formatRelativeTime(p.last_import.finished_at ?? p.last_import.started_at, locale),
    },
  ]

  return (
    <section className="mb-4" aria-labelledby="system-projects-title">
      <h2 id="system-projects-title" className="kk-section-title mb-1">
        {t('system.projects.title')}
      </h2>
      <p className="text-secondary small">{t('system.projects.intro')}</p>
      <RecordTable
        records={projects}
        columns={columns}
        rowKey={(p) => p.uid}
        size="sm"
        className="align-middle"
      />
    </section>
  )
}
