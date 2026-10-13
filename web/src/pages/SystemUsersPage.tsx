import { useCallback, useEffect, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Badge from 'react-bootstrap/Badge'
import Card from 'react-bootstrap/Card'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'

import { useAuth } from '../auth/AuthContext'
import { ConfirmModal } from '../components/ConfirmModal'
import { EmptyState } from '../components/EmptyState'
import { ErrorState } from '../components/ErrorState'
import { ReasonedButton } from '../components/ReasonedButton'
import { RecordTable, type RecordColumn } from '../components/RecordTable'
import { useDocumentTitle } from '../hooks/useDocumentTitle'
import type { InstanceRole } from '../services/auth'
import {
  type AccountMembership,
  fetchInstanceAccounts,
  type InstanceAccount,
  ProjectApiError,
  setInstanceRole,
} from '../services/projects'

/** Fetch lifecycle of the account list. */
type LoadState =
  | { status: 'loading' }
  | { status: 'error' }
  | { status: 'ready'; accounts: InstanceAccount[] }

/** The role change waiting for its confirmation. */
interface Pending {
  show: boolean
  account: InstanceAccount | null
  role: InstanceRole
}

/** The name an account is called by in a question: its display name, else its username. */
function nameOf(account: InstanceAccount): string {
  return account.display_name.trim() === '' ? account.username : account.display_name
}

/**
 * `/system/users` — every account of the instance (`GET /api/v1/users`, the
 * unprefixed one) with its instance role, whether it is blocked, and its
 * memberships as `slug (role)` chips; a waiting membership or one in a
 * deactivated project is a muted chip that says why on hover.
 *
 * The one action here is the instance role: „Povýšit na správce systému" and
 * „Odebrat správce", each behind a confirmation. One's own row keeps the
 * button greyed out with the reason (the backend refuses `self` anyway —
 * somebody else must demote you). Everything about an account inside a project
 * — its role there, its approval — is that project's members page.
 */
export function SystemUsersPage() {
  const { t } = useTranslation()
  const { user } = useAuth()
  useDocumentTitle(t('systemUsers.title'))

  const [load, setLoad] = useState<LoadState>({ status: 'loading' })
  const [pending, setPending] = useState<Pending>({ show: false, account: null, role: 'user' })
  const [busy, setBusy] = useState(false)
  const [failure, setFailure] = useState<string | null>(null)

  const reload = useCallback(async (signal?: AbortSignal) => {
    setLoad({ status: 'loading' })
    try {
      const accounts = await fetchInstanceAccounts(signal)
      if (signal?.aborted !== true) {
        setLoad({ status: 'ready', accounts })
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

  async function handleConfirm() {
    const account = pending.account
    if (account === null || busy) {
      return
    }
    setBusy(true)
    setFailure(null)
    try {
      const next = await setInstanceRole(account.uid, pending.role)
      setLoad((prev) =>
        prev.status === 'ready'
          ? {
              status: 'ready',
              accounts: prev.accounts.map((a) => (a.uid === next.uid ? next : a)),
            }
          : prev,
      )
    } catch (error: unknown) {
      const code = error instanceof ProjectApiError ? error.code : ''
      if (code === 'last_maintainer') {
        setFailure(t('systemUsers.lastMaintainer'))
      } else if (code === 'self') {
        setFailure(t('systemUsers.selfReason'))
      } else {
        setFailure(t('systemUsers.roleFailed'))
      }
    } finally {
      setBusy(false)
      setPending((prev) => ({ ...prev, show: false }))
    }
  }

  /** One membership as a chip: `slug (role)`, muted when it cannot be entered. */
  function chip(m: AccountMembership) {
    const text = t('systemUsers.membership', { slug: m.project_slug, role: t(`roles.${m.role}`) })
    const why = !m.approved
      ? t('systemUsers.membershipPending')
      : !m.active
        ? t('systemUsers.membershipInactive')
        : undefined
    return (
      <Badge
        key={m.project_uid}
        bg={why === undefined ? 'primary' : 'secondary'}
        className="fw-normal"
        title={why ?? m.project_name}
      >
        {text}
      </Badge>
    )
  }

  /** The promote/demote button of one row. */
  function roleAction(account: InstanceAccount) {
    const promote = account.instance_role !== 'maintainer'
    const self = user !== null && account.uid === user.uid
    return (
      <ReasonedButton
        size="sm"
        variant={promote ? 'outline-primary' : 'outline-danger'}
        disabledReason={self ? t('systemUsers.selfReason') : null}
        onClick={() => {
          setPending({ show: true, account, role: promote ? 'maintainer' : 'user' })
        }}
      >
        {promote ? t('systemUsers.promote') : t('systemUsers.demote')}
      </ReasonedButton>
    )
  }

  const columns: RecordColumn<InstanceAccount>[] = [
    {
      key: 'username',
      header: t('systemUsers.columns.username'),
      cell: (a) => <span className="font-monospace">{a.username}</span>,
    },
    { key: 'name', header: t('systemUsers.columns.displayName'), cell: (a) => a.display_name },
    { key: 'email', header: t('systemUsers.columns.email'), cell: (a) => a.email },
    {
      key: 'role',
      header: t('systemUsers.columns.role'),
      cell: (a) => (
        <span className="d-inline-flex flex-wrap gap-1">
          <Badge bg={a.instance_role === 'maintainer' ? 'warning' : 'secondary'}>
            {t(`systemUsers.instanceRoles.${a.instance_role}`)}
          </Badge>
          {a.disabled && <Badge bg="danger">{t('systemUsers.disabled')}</Badge>}
        </span>
      ),
    },
    {
      key: 'memberships',
      header: t('systemUsers.columns.memberships'),
      cell: (a) =>
        a.memberships.length === 0 ? (
          <span className="text-secondary">{t('systemUsers.noMemberships')}</span>
        ) : (
          <span className="d-inline-flex flex-wrap gap-1">{a.memberships.map(chip)}</span>
        ),
    },
    {
      key: 'actions',
      header: t('systemUsers.columns.actions'),
      cardHidden: true,
      cell: roleAction,
    },
  ]

  const target = pending.account
  const promoting = pending.role === 'maintainer'

  return (
    <>
      <h1 className="kk-page-title mb-1">{t('systemUsers.title')}</h1>
      <p className="text-secondary">{t('systemUsers.intro')}</p>

      {failure !== null && (
        <Alert
          variant="danger"
          role="alert"
          dismissible
          onClose={() => {
            setFailure(null)
          }}
        >
          {failure}
        </Alert>
      )}

      <Card>
        <Card.Body>
          {load.status === 'loading' && (
            <div className="d-flex justify-content-center py-4">
              <Spinner animation="border" role="status">
                <span className="visually-hidden">{t('systemUsers.loading')}</span>
              </Spinner>
            </div>
          )}
          {load.status === 'error' && (
            <ErrorState
              title={t('systemUsers.loadFailed')}
              onRetry={() => {
                void reload()
              }}
            />
          )}
          {load.status === 'ready' && load.accounts.length === 0 && (
            <EmptyState title={t('systemUsers.empty')} />
          )}
          {load.status === 'ready' && load.accounts.length > 0 && (
            <RecordTable
              records={load.accounts}
              columns={columns}
              rowKey={(a) => a.uid}
              cardActions={roleAction}
              className="align-middle mb-0"
            />
          )}
        </Card.Body>
      </Card>

      <ConfirmModal
        show={pending.show}
        title={
          target === null
            ? ''
            : promoting
              ? t('systemUsers.promoteTitle', { name: nameOf(target) })
              : t('systemUsers.demoteTitle', { name: nameOf(target) })
        }
        confirmLabel={promoting ? t('systemUsers.promote') : t('systemUsers.demote')}
        variant={promoting ? 'primary' : 'danger'}
        busy={busy}
        onConfirm={() => {
          void handleConfirm()
        }}
        onCancel={() => {
          setPending((prev) => ({ ...prev, show: false }))
        }}
      >
        {promoting ? t('systemUsers.promoteBody') : t('systemUsers.demoteBody')}
      </ConfirmModal>
    </>
  )
}
