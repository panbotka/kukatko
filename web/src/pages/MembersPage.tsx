import type { TFunction } from 'i18next'
import { useCallback, useEffect, useId, useState } from 'react'
import Badge from 'react-bootstrap/Badge'
import Button from 'react-bootstrap/Button'
import Card from 'react-bootstrap/Card'
import Form from 'react-bootstrap/Form'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'

import { useAuth } from '../auth/AuthContext'
import { ConfirmModal } from '../components/ConfirmModal'
import { EmptyState } from '../components/EmptyState'
import { ErrorState } from '../components/ErrorState'
import { Icon } from '../components/Icon'
import { PersonAvatar } from '../components/PersonAvatar'
import { ReasonedButton } from '../components/ReasonedButton'
import { RecordTable, type RecordColumn } from '../components/RecordTable'
import { AddMemberModal } from '../components/members/AddMemberModal'
import { InvitesSection } from '../components/members/InvitesSection'
import { useToast } from '../components/toast/ToastContext'
import { useDocumentTitle } from '../hooks/useDocumentTitle'
import { formatDateTime } from '../lib/format'
import type { ProjectRole } from '../services/auth'
import {
  approveMember,
  fetchMembers,
  MEMBER_ROLES,
  MemberApiError,
  type ProjectMemberRow,
  removeMember,
  setMemberRole,
} from '../services/members'

/** Fetch lifecycle of the member list. */
type LoadState =
  | { status: 'loading' }
  | { status: 'error' }
  | { status: 'ready'; members: ProjectMemberRow[] }

/** The removal waiting for its confirmation; the member stays while the dialog fades. */
interface PendingRemoval {
  show: boolean
  member: ProjectMemberRow | null
}

/** The catalogue key of each refusal the page words itself. */
type Failure = 'roleFailed' | 'approveFailed' | 'removeFailed'

/**
 * The name a member is called by: the display name, else the username — or,
 * for an account added by an exact match that has not opened the project yet,
 * a neutral label, as the server withholds who it is until then.
 */
function nameOf(member: ProjectMemberRow, t: TFunction): string {
  if (member.identity_withheld) {
    return t('members.withheld.name')
  }
  return member.display_name.trim() === '' ? member.username : member.display_name
}

/** Props of {@link RoleSelect}. */
interface RoleSelectProps {
  member: ProjectMemberRow
  /** Why the select is off (one's own row), or null when it may be used. */
  disabledReason: string | null
  busy: boolean
  onChange: (role: ProjectRole) => void
}

/**
 * The role of one member as a select that changes it at once. On the caller's
 * own row it is off, and says why: the hover tooltip sits on a wrapper (a
 * disabled control receives no pointer events in every browser) and the same
 * sentence is the select's description for a screen reader.
 */
function RoleSelect({ member, disabledReason, busy, onChange }: RoleSelectProps) {
  const { t } = useTranslation()
  const reasonId = useId()
  const off = disabledReason !== null
  return (
    <span className="d-inline-block" title={disabledReason ?? undefined}>
      <Form.Select
        size="sm"
        className="w-auto"
        aria-label={t('members.roleLabel', { name: nameOf(member, t) })}
        aria-describedby={off ? reasonId : undefined}
        value={member.role}
        disabled={off || busy}
        onChange={(event) => {
          onChange(event.currentTarget.value as ProjectRole)
        }}
      >
        {MEMBER_ROLES.map((role) => (
          <option key={role} value={role}>
            {t(`roles.${role}`)}
          </option>
        ))}
      </Form.Select>
      {off && (
        <span id={reasonId} className="visually-hidden">
          {disabledReason}
        </span>
      )}
    </span>
  )
}

/**
 * `/p/<slug>/members` — the project admin's list of who belongs to this project
 * (`GET /p/<slug>/members`): picture, name, username, the role as a select that
 * changes it at once, the approval state (a waiting membership gets the badge
 * „Čeká na schválení" and a „Schválit" button), the last sign-in and a remove
 * button behind a confirmation. „Přidat člena" adds an existing account by its
 * exact username or e-mail ({@link AddMemberModal}).
 *
 * The page manages **memberships**, never accounts: removing somebody takes this
 * project away from them and leaves the account (and every other project of it)
 * alone. One's own role and one's own removal are off with the reason — another
 * admin has to do that — and the backend's refusals arrive as toasts: `last_admin`
 * („Projekt musí mít alespoň jednoho administrátora"), `self`, and `not_member`
 * (somebody else removed the member meanwhile; the list is refetched).
 *
 * Below the list sits the invitations section ({@link InvitesSection}): links
 * that let somebody in who may not have an account yet.
 */
export function MembersPage() {
  const { t, i18n } = useTranslation()
  const { user } = useAuth()
  const toast = useToast()
  useDocumentTitle(t('members.title'))

  const [load, setLoad] = useState<LoadState>({ status: 'loading' })
  const [busyUid, setBusyUid] = useState<string | null>(null)
  const [removal, setRemoval] = useState<PendingRemoval>({ show: false, member: null })
  const [adding, setAdding] = useState(false)

  const reload = useCallback(async (signal?: AbortSignal) => {
    try {
      const members = await fetchMembers(signal)
      if (signal?.aborted !== true) {
        setLoad({ status: 'ready', members })
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

  /** Swaps one row for the membership the server now holds. */
  function adopt(next: ProjectMemberRow) {
    setLoad((prev) =>
      prev.status === 'ready'
        ? {
            status: 'ready',
            members: prev.members.map((m) => (m.user_uid === next.user_uid ? next : m)),
          }
        : prev,
    )
  }

  /** Says a refusal in the page's words; a vanished member also refreshes the list. */
  function report(error: unknown, fallback: Failure) {
    const code = error instanceof MemberApiError ? error.code : ''
    if (code === 'last_admin') {
      toast.show({ message: t('members.lastAdmin'), variant: 'danger' })
    } else if (code === 'self') {
      toast.show({ message: t('members.self'), variant: 'danger' })
    } else if (code === 'not_member') {
      toast.show({ message: t('members.notMember'), variant: 'danger' })
      void reload()
    } else {
      toast.show({ message: t(`members.${fallback}`), variant: 'danger' })
    }
  }

  async function handleRole(member: ProjectMemberRow, role: ProjectRole) {
    if (busyUid !== null || role === member.role) {
      return
    }
    setBusyUid(member.user_uid)
    try {
      const next = await setMemberRole(member.user_uid, role)
      adopt(next)
      toast.show({
        message: t('members.roleChanged', { name: nameOf(next, t), role: t(`roles.${next.role}`) }),
        variant: 'success',
      })
    } catch (error: unknown) {
      report(error, 'roleFailed')
    } finally {
      setBusyUid(null)
    }
  }

  async function handleApprove(member: ProjectMemberRow) {
    if (busyUid !== null) {
      return
    }
    setBusyUid(member.user_uid)
    try {
      const next = await approveMember(member.user_uid)
      adopt(next)
      toast.show({
        message: t('members.approvedToast', { name: nameOf(next, t) }),
        variant: 'success',
      })
    } catch (error: unknown) {
      report(error, 'approveFailed')
    } finally {
      setBusyUid(null)
    }
  }

  async function handleRemove() {
    const member = removal.member
    if (member === null || busyUid !== null) {
      return
    }
    setBusyUid(member.user_uid)
    try {
      await removeMember(member.user_uid)
      setLoad((prev) =>
        prev.status === 'ready'
          ? {
              status: 'ready',
              members: prev.members.filter((m) => m.user_uid !== member.user_uid),
            }
          : prev,
      )
      toast.show({
        message: t('members.removedToast', { name: nameOf(member, t) }),
        variant: 'success',
      })
    } catch (error: unknown) {
      report(error, 'removeFailed')
    } finally {
      setBusyUid(null)
      setRemoval((prev) => ({ ...prev, show: false }))
    }
  }

  const isSelf = (member: ProjectMemberRow) => user !== null && member.user_uid === user.uid

  /** The remove button of one row (its own line on a phone card). */
  function removeAction(member: ProjectMemberRow) {
    return (
      <ReasonedButton
        size="sm"
        variant="outline-danger"
        disabledReason={isSelf(member) ? t('members.selfRemoveReason') : null}
        onClick={() => {
          setRemoval({ show: true, member })
        }}
      >
        <Icon name="trash" className="me-1" />
        {t('members.remove')}
      </ReasonedButton>
    )
  }

  const columns: RecordColumn<ProjectMemberRow>[] = [
    {
      key: 'name',
      header: t('members.columns.name'),
      cell: (m) => (
        <span className="d-inline-flex align-items-center gap-2">
          <PersonAvatar name={nameOf(m, t)} userUid={m.has_picture ? m.user_uid : undefined} />
          {m.identity_withheld ? (
            <span className="d-inline-flex flex-column">
              <span className="fst-italic">{nameOf(m, t)}</span>
              <small className="text-secondary">{t('members.withheld.hint')}</small>
            </span>
          ) : (
            <span>{nameOf(m, t)}</span>
          )}
        </span>
      ),
    },
    {
      key: 'username',
      header: t('members.columns.username'),
      cell: (m) =>
        m.identity_withheld ? (
          <span className="text-secondary">{t('members.withheld.unknown')}</span>
        ) : (
          <span className="font-monospace">{m.username}</span>
        ),
    },
    {
      key: 'role',
      header: t('members.columns.role'),
      cell: (m) => (
        <RoleSelect
          member={m}
          disabledReason={isSelf(m) ? t('members.selfRoleReason') : null}
          busy={busyUid === m.user_uid}
          onChange={(role) => {
            void handleRole(m, role)
          }}
        />
      ),
    },
    {
      key: 'state',
      header: t('members.columns.state'),
      cell: (m) =>
        m.approved_at === null ? (
          <span className="d-inline-flex flex-wrap align-items-center gap-2">
            <Badge bg="warning">{t('members.pending')}</Badge>
            <Button
              size="sm"
              variant="primary"
              disabled={busyUid === m.user_uid}
              onClick={() => {
                void handleApprove(m)
              }}
            >
              <Icon name="person-check" className="me-1" />
              {t('members.approve')}
            </Button>
          </span>
        ) : (
          <span className="text-secondary">{t('members.approved')}</span>
        ),
    },
    {
      key: 'lastLogin',
      header: t('members.columns.lastLogin'),
      cell: (m) =>
        m.identity_withheld ? (
          <span className="text-secondary">{t('members.withheld.unknown')}</span>
        ) : m.last_login_at === null ? (
          <span className="text-secondary">{t('members.never')}</span>
        ) : (
          formatDateTime(m.last_login_at, i18n.language)
        ),
    },
    {
      key: 'actions',
      header: t('members.columns.actions'),
      cardHidden: true,
      cell: removeAction,
    },
  ]

  const target = removal.member

  return (
    <>
      <div className="d-flex flex-wrap align-items-start justify-content-between gap-2 mb-2">
        <h1 className="kk-page-title mb-0">{t('members.title')}</h1>
        <Button
          variant="primary"
          onClick={() => {
            setAdding(true)
          }}
        >
          <Icon name="person-plus" className="me-1" />
          {t('members.add.open')}
        </Button>
      </div>
      <p className="text-secondary">{t('members.intro')}</p>

      <Card>
        <Card.Body>
          {load.status === 'loading' && (
            <div className="d-flex justify-content-center py-4">
              <Spinner animation="border" role="status">
                <span className="visually-hidden">{t('members.loading')}</span>
              </Spinner>
            </div>
          )}
          {load.status === 'error' && (
            <ErrorState
              title={t('members.loadFailed')}
              onRetry={() => {
                setLoad({ status: 'loading' })
                void reload()
              }}
            />
          )}
          {load.status === 'ready' && load.members.length === 0 && (
            <EmptyState title={t('members.empty')} />
          )}
          {load.status === 'ready' && load.members.length > 0 && (
            <RecordTable
              records={load.members}
              columns={columns}
              rowKey={(m) => m.user_uid}
              cardActions={removeAction}
              className="align-middle mb-0"
            />
          )}
        </Card.Body>
      </Card>

      <InvitesSection />

      <AddMemberModal
        show={adding}
        onClose={() => {
          setAdding(false)
        }}
        onSubmitted={() => {
          void reload()
        }}
      />

      <ConfirmModal
        show={removal.show}
        title={target === null ? '' : t('members.removeTitle', { name: nameOf(target, t) })}
        confirmLabel={t('members.remove')}
        busy={target !== null && busyUid === target.user_uid}
        onConfirm={() => {
          void handleRemove()
        }}
        onCancel={() => {
          setRemoval((prev) => ({ ...prev, show: false }))
        }}
      >
        {t('members.removeBody')}
      </ConfirmModal>
    </>
  )
}
