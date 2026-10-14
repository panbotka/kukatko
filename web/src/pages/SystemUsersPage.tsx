import { useCallback, useEffect, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Badge from 'react-bootstrap/Badge'
import Button from 'react-bootstrap/Button'
import Card from 'react-bootstrap/Card'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'

import { useAuth } from '../auth/AuthContext'
import { ConfirmModal } from '../components/ConfirmModal'
import { EmptyState } from '../components/EmptyState'
import { ErrorState } from '../components/ErrorState'
import { Icon } from '../components/Icon'
import { ReasonedButton } from '../components/ReasonedButton'
import { RecordTable, type RecordColumn } from '../components/RecordTable'
import { isWaitingForApproval } from '../components/users/account'
import { AccountFormModal, PasswordModal } from '../components/users/AccountFormModal'
import { actionErrorFor } from '../components/users/errors'
import { ResetLinkModal } from '../components/users/ResetLinkModal'
import { UserEmail } from '../components/users/UserEmail'
import { useDocumentTitle } from '../hooks/useDocumentTitle'
import { useMailEnabled } from '../hooks/useMailEnabled'
import type { InstanceRole } from '../services/auth'
import {
  type AccountMembership,
  fetchInstanceAccounts,
  type InstanceAccount,
  ProjectApiError,
  setInstanceRole,
} from '../services/projects'
import { setUserDisabled } from '../services/users'

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

/** The block or unblock waiting for its confirmation. */
interface PendingBlock {
  show: boolean
  account: InstanceAccount | null
  /** True to block, false to unblock. */
  block: boolean
}

/** The account dialog open on the page, if any. */
type Dialog =
  | { kind: 'none' }
  | { kind: 'create' }
  | { kind: 'edit'; account: InstanceAccount }
  | { kind: 'password'; account: InstanceAccount }
  | { kind: 'resetLink'; account: InstanceAccount }

/** The name an account is called by in a question: its display name, else its username. */
function nameOf(account: { display_name: string; username: string }): string {
  return account.display_name.trim() === '' ? account.username : account.display_name
}

/**
 * `/system/users` — every account of the instance (`GET /api/v1/users`, the
 * unprefixed one) and the system administrator's actions on the account record.
 *
 * Each row shows the account — username, name, e-mail (a `.invalid` placeholder
 * as "no address"), its instance role, whether it is blocked or still waiting
 * for an approval — and its memberships as `slug (role)` chips; a membership
 * linked to a person of its project names that person in its chip, read-only
 * (the link is per membership, set by the account in that project). A waiting
 * membership or one in a deactivated project is a muted chip that says why on
 * hover.
 *
 * The actions are the account record's, all maintainer-only on the backend
 * (`/admin/users`): „Nový účet" (an account with no membership — it joins a
 * project on that project's members page or by an invitation), „Upravit"
 * (username, e-mail, name, note), „Změnit heslo", „Odkaz na heslo" (the
 * one-time reset link, the only way back in for somebody who forgot their
 * password), „Zablokovat"/„Odblokovat" behind a confirmation, and the instance
 * role („Povýšit na správce systému"/„Odebrat správce", also confirmed). One's
 * own row keeps block and the role greyed out with the reason. Everything about
 * an account inside a project — its role there, its approval — is that
 * project's members page.
 */
export function SystemUsersPage() {
  const { t } = useTranslation()
  const { user } = useAuth()
  const mailEnabled = useMailEnabled()
  useDocumentTitle(t('systemUsers.title'))

  const [load, setLoad] = useState<LoadState>({ status: 'loading' })
  const [pending, setPending] = useState<Pending>({ show: false, account: null, role: 'user' })
  const [pendingBlock, setPendingBlock] = useState<PendingBlock>({
    show: false,
    account: null,
    block: true,
  })
  const [dialog, setDialog] = useState<Dialog>({ kind: 'none' })
  const [busy, setBusy] = useState(false)
  const [failure, setFailure] = useState<string | null>(null)
  const [notice, setNotice] = useState<string | null>(null)

  const reload = useCallback(async (signal?: AbortSignal, quiet = false) => {
    if (!quiet) {
      setLoad({ status: 'loading' })
    }
    try {
      const accounts = await fetchInstanceAccounts(signal)
      if (signal?.aborted !== true) {
        setLoad({ status: 'ready', accounts })
      }
    } catch (error: unknown) {
      if (!(error instanceof DOMException && error.name === 'AbortError') && !quiet) {
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

  /** Re-reads the list in place after a change, keeping the table on screen. */
  function refresh() {
    void reload(undefined, true)
  }

  function closeDialog() {
    setDialog({ kind: 'none' })
  }

  async function handleConfirm() {
    const account = pending.account
    if (account === null || busy) {
      return
    }
    setBusy(true)
    setFailure(null)
    setNotice(null)
    try {
      const next = await setInstanceRole(account.uid, pending.role)
      // An instance role says nothing about the memberships, and the answer
      // does not name their people, so the row keeps the ones it has.
      setLoad((prev) =>
        prev.status === 'ready'
          ? {
              status: 'ready',
              accounts: prev.accounts.map((a) =>
                a.uid === next.uid ? { ...a, ...next, memberships: a.memberships } : a,
              ),
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

  async function handleBlockConfirm() {
    const account = pendingBlock.account
    if (account === null || busy) {
      return
    }
    setBusy(true)
    setFailure(null)
    setNotice(null)
    try {
      await setUserDisabled(account, pendingBlock.block)
      setNotice(
        t(pendingBlock.block ? 'systemUsers.notices.blocked' : 'systemUsers.notices.unblocked', {
          name: nameOf(account),
        }),
      )
      refresh()
    } catch (error: unknown) {
      setFailure(t(actionErrorFor(error)))
    } finally {
      setBusy(false)
      setPendingBlock((prev) => ({ ...prev, show: false }))
    }
  }

  /** One membership as a chip: `slug (role)`, plus its person; muted when it cannot be entered. */
  function chip(m: AccountMembership) {
    const role = t(`roles.${m.role}`)
    const person = m.subject_uid === null ? '' : m.subject_name.trim()
    const text =
      person === ''
        ? t('systemUsers.membership', { slug: m.project_slug, role })
        : t('systemUsers.membershipPerson', { slug: m.project_slug, role, person })
    const why = !m.approved
      ? t('systemUsers.membershipPending')
      : !m.active
        ? t('systemUsers.membershipInactive')
        : undefined
    const hint =
      person === ''
        ? m.project_name
        : t('systemUsers.membershipPersonHint', { project: m.project_name, person })
    return (
      <Badge
        key={m.project_uid}
        bg={why === undefined ? 'primary' : 'secondary'}
        className="fw-normal"
        title={why ?? hint}
      >
        {text}
      </Badge>
    )
  }

  /** The account-record actions of one row, and its promote/demote button. */
  function actions(account: InstanceAccount) {
    const promote = account.instance_role !== 'maintainer'
    const self = user !== null && account.uid === user.uid
    return (
      <span className="d-inline-flex flex-wrap gap-1">
        <Button
          size="sm"
          variant="outline-secondary"
          onClick={() => {
            setDialog({ kind: 'edit', account })
          }}
        >
          {t('systemUsers.actions.edit')}
        </Button>
        <Button
          size="sm"
          variant="outline-secondary"
          onClick={() => {
            setDialog({ kind: 'resetLink', account })
          }}
        >
          {t('systemUsers.actions.resetLink')}
        </Button>
        <Button
          size="sm"
          variant="outline-secondary"
          onClick={() => {
            setDialog({ kind: 'password', account })
          }}
        >
          {t('systemUsers.actions.password')}
        </Button>
        <ReasonedButton
          size="sm"
          variant={account.disabled ? 'outline-success' : 'outline-danger'}
          disabledReason={self ? t('systemUsers.selfBlockReason') : null}
          onClick={() => {
            setPendingBlock({ show: true, account, block: !account.disabled })
          }}
        >
          {account.disabled ? t('systemUsers.actions.unblock') : t('systemUsers.actions.block')}
        </ReasonedButton>
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
      </span>
    )
  }

  const columns: RecordColumn<InstanceAccount>[] = [
    {
      key: 'username',
      header: t('systemUsers.columns.username'),
      cell: (a) => <span className="font-monospace">{a.username}</span>,
    },
    { key: 'name', header: t('systemUsers.columns.displayName'), cell: (a) => a.display_name },
    {
      key: 'email',
      header: t('systemUsers.columns.email'),
      cell: (a) => <UserEmail email={a.email} />,
    },
    {
      key: 'role',
      header: t('systemUsers.columns.role'),
      cell: (a) => (
        <span className="d-inline-flex flex-wrap gap-1">
          <Badge bg={a.instance_role === 'maintainer' ? 'warning' : 'secondary'}>
            {t(`systemUsers.instanceRoles.${a.instance_role}`)}
          </Badge>
          {a.disabled && <Badge bg="danger">{t('systemUsers.disabled')}</Badge>}
          {isWaitingForApproval(a) && (
            <Badge bg="warning" text="dark">
              {t('systemUsers.pending')}
            </Badge>
          )}
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
      cell: actions,
    },
  ]

  const target = pending.account
  const promoting = pending.role === 'maintainer'
  const blockTarget = pendingBlock.account

  return (
    <>
      <div className="d-flex flex-wrap align-items-start justify-content-between gap-2 mb-1">
        <h1 className="kk-page-title mb-0">{t('systemUsers.title')}</h1>
        <Button
          variant="primary"
          className="d-inline-flex align-items-center gap-2"
          onClick={() => {
            setDialog({ kind: 'create' })
          }}
        >
          <Icon name="person-plus" />
          {t('systemUsers.create')}
        </Button>
      </div>
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
      {notice !== null && (
        <Alert
          variant="success"
          role="status"
          dismissible
          onClose={() => {
            setNotice(null)
          }}
        >
          {notice}
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
              cardActions={actions}
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

      <ConfirmModal
        show={pendingBlock.show}
        title={
          blockTarget === null
            ? ''
            : pendingBlock.block
              ? t('systemUsers.blockTitle', { name: nameOf(blockTarget) })
              : t('systemUsers.unblockTitle', { name: nameOf(blockTarget) })
        }
        confirmLabel={
          pendingBlock.block ? t('systemUsers.actions.block') : t('systemUsers.actions.unblock')
        }
        variant={pendingBlock.block ? 'danger' : 'primary'}
        busy={busy}
        onConfirm={() => {
          void handleBlockConfirm()
        }}
        onCancel={() => {
          setPendingBlock((prev) => ({ ...prev, show: false }))
        }}
      >
        {pendingBlock.block ? t('systemUsers.blockBody') : t('systemUsers.unblockBody')}
      </ConfirmModal>

      {(dialog.kind === 'create' || dialog.kind === 'edit') && (
        <AccountFormModal
          account={dialog.kind === 'edit' ? dialog.account : null}
          onHide={closeDialog}
          onRenamed={refresh}
          onSaved={(saved) => {
            setFailure(null)
            setNotice(
              t(
                dialog.kind === 'create'
                  ? 'systemUsers.notices.created'
                  : 'systemUsers.notices.saved',
                { name: nameOf(saved) },
              ),
            )
            closeDialog()
            refresh()
          }}
        />
      )}
      {dialog.kind === 'password' && (
        <PasswordModal
          account={dialog.account}
          onHide={closeDialog}
          onDone={() => {
            setFailure(null)
            setNotice(t('systemUsers.notices.passwordChanged', { name: nameOf(dialog.account) }))
            closeDialog()
          }}
        />
      )}
      {dialog.kind === 'resetLink' && (
        <ResetLinkModal user={dialog.account} mailEnabled={mailEnabled} onHide={closeDialog} />
      )}
    </>
  )
}
