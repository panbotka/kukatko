import { useCallback, useEffect, useState } from 'react'
import Badge from 'react-bootstrap/Badge'
import Button from 'react-bootstrap/Button'
import Card from 'react-bootstrap/Card'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'
import { Link, useNavigate } from 'react-router-dom'

import { EmptyState } from '../components/EmptyState'
import { ErrorState } from '../components/ErrorState'
import { Icon } from '../components/Icon'
import { RecordTable, type RecordColumn } from '../components/RecordTable'
import { systemProjectPath } from '../components/navItems'
import { NewProjectModal } from '../components/system/NewProjectModal'
import { useDocumentTitle } from '../hooks/useDocumentTitle'
import { formatLimits } from '../lib/byteLimit'
import { formatBytes, formatCount, formatDate } from '../lib/format'
import { type AdminProject, fetchAdminProjects } from '../services/projects'

/** Fetch lifecycle of the project list. */
type LoadState =
  | { status: 'loading' }
  | { status: 'error' }
  | { status: 'ready'; projects: AdminProject[] }

/**
 * `/system/projects` — the system administrator's list of every project of the
 * instance (`GET /api/v1/projects`): name, slug, whether it is active, members,
 * photos, the bytes of its originals, its limits and when it was founded. The
 * name leads to the project's detail page; „Nový projekt" opens the create
 * dialog, and a created project is opened right away, since its members and
 * limits are what one sets up next.
 *
 * Instance-level and maintainer-only: it lists projects the administrator need
 * not belong to, so it lives outside `/p/<slug>`.
 */
export function SystemProjectsPage() {
  const { t, i18n } = useTranslation()
  const navigate = useNavigate()
  useDocumentTitle(t('systemProjects.title'))

  const [load, setLoad] = useState<LoadState>({ status: 'loading' })
  const [creating, setCreating] = useState(false)

  const reload = useCallback(async (signal?: AbortSignal) => {
    setLoad({ status: 'loading' })
    try {
      const projects = await fetchAdminProjects(signal)
      if (signal?.aborted !== true) {
        setLoad({ status: 'ready', projects })
      }
    } catch (error: unknown) {
      if (!(error instanceof DOMException && error.name === 'AbortError')) {
        setLoad({ status: 'error' })
      }
    }
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    void reload(controller.signal)
    return () => {
      controller.abort()
    }
  }, [reload])

  const locale = i18n.language
  const columns: RecordColumn<AdminProject>[] = [
    {
      key: 'name',
      header: t('systemProjects.columns.name'),
      cell: (p) => <Link to={systemProjectPath(p.slug)}>{p.name}</Link>,
    },
    {
      key: 'slug',
      header: t('systemProjects.columns.slug'),
      cell: (p) => <span className="font-monospace">{p.slug}</span>,
    },
    {
      key: 'status',
      header: t('systemProjects.columns.status'),
      cell: (p) =>
        p.active ? (
          <Badge bg="success">{t('systemProjects.active')}</Badge>
        ) : (
          <Badge bg="secondary">{t('systemProjects.inactive')}</Badge>
        ),
    },
    {
      key: 'members',
      header: t('systemProjects.columns.members'),
      cell: (p) => formatCount(p.members_count, locale),
    },
    {
      key: 'photos',
      header: t('systemProjects.columns.photos'),
      cell: (p) => formatCount(p.photos_count, locale),
    },
    {
      key: 'originals',
      header: t('systemProjects.columns.originals'),
      cell: (p) => formatBytes(p.originals_bytes, locale),
      cellClassName: 'text-nowrap',
    },
    {
      key: 'limits',
      header: t('systemProjects.columns.limits'),
      cell: (p) => formatLimits(p, t, locale),
    },
    {
      key: 'created',
      header: t('systemProjects.columns.created'),
      cell: (p) => formatDate(p.created_at, locale),
      cellClassName: 'text-nowrap',
    },
  ]

  return (
    <>
      <div className="d-flex justify-content-between align-items-start gap-3 mb-1">
        <h1 className="kk-page-title mb-0">{t('systemProjects.title')}</h1>
        <Button
          variant="primary"
          onClick={() => {
            setCreating(true)
          }}
        >
          <Icon name="plus-lg" className="me-1" />
          {t('systemProjects.new')}
        </Button>
      </div>
      <p className="text-secondary">{t('systemProjects.intro')}</p>

      <Card>
        <Card.Body>
          {load.status === 'loading' && (
            <div className="d-flex justify-content-center py-4">
              <Spinner animation="border" role="status">
                <span className="visually-hidden">{t('systemProjects.loading')}</span>
              </Spinner>
            </div>
          )}
          {load.status === 'error' && (
            <ErrorState
              title={t('systemProjects.loadFailed')}
              onRetry={() => {
                void reload()
              }}
            />
          )}
          {load.status === 'ready' && load.projects.length === 0 && (
            <EmptyState title={t('systemProjects.empty')} />
          )}
          {load.status === 'ready' && load.projects.length > 0 && (
            <RecordTable
              records={load.projects}
              columns={columns}
              rowKey={(p) => p.uid}
              className="align-middle mb-0"
            />
          )}
        </Card.Body>
      </Card>

      <NewProjectModal
        show={creating}
        onHide={() => {
          setCreating(false)
        }}
        onCreated={(project) => {
          setCreating(false)
          void navigate(systemProjectPath(project.slug))
        }}
      />
    </>
  )
}
