import { type SyntheticEvent, useCallback, useEffect, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Badge from 'react-bootstrap/Badge'
import Button from 'react-bootstrap/Button'
import Card from 'react-bootstrap/Card'
import Col from 'react-bootstrap/Col'
import Form from 'react-bootstrap/Form'
import Row from 'react-bootstrap/Row'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'

import type { ProjectRole } from '../../services/auth'
import {
  createInvite,
  fetchInvites,
  type Invite,
  InviteApiError,
  inviteAddress,
  type InviteState,
  revokeInvite,
} from '../../services/invites'
import { MEMBER_ROLES } from '../../services/members'
import { formatDate } from '../../lib/format'
import { ConfirmModal } from '../ConfirmModal'
import { EmptyState } from '../EmptyState'
import { ErrorState } from '../ErrorState'
import { Icon } from '../Icon'
import { CopyLinkButton, LinkAddress } from '../LinkAddress'
import { RecordTable, type RecordColumn } from '../RecordTable'
import { useToast } from '../toast/ToastContext'

/** Fetch lifecycle of the invite list, with the create form's validity bounds. */
type LoadState =
  | { status: 'loading' }
  | { status: 'error' }
  | { status: 'ready'; invites: Invite[]; defaultDays: number; maxDays: number }

/** The revocation waiting for its confirmation; the invite stays while the dialog fades. */
interface PendingRevoke {
  show: boolean
  invite: Invite | null
}

/** The badge colour of each state. */
const STATE_VARIANT: Record<InviteState, string> = {
  active: 'success',
  expired: 'secondary',
  revoked: 'danger',
  used_up: 'secondary',
}

/** The 422 codes of `POST /invites` the form words itself. */
const CREATE_REFUSALS = ['invalid_role', 'invalid_days', 'invalid_max_uses'] as const

/** Whether `value` is a whole number in `min..max`. */
function inRange(value: number, min: number, max: number): boolean {
  return Number.isInteger(value) && value >= min && value <= max
}

/** Props of {@link CreateInviteForm}. */
interface CreateInviteFormProps {
  defaultDays: number
  maxDays: number
  /** Called with the invite the backend created. */
  onCreated: (invite: Invite) => void
}

/**
 * The create form: the role the invite grants, its validity in days (the
 * backend's default, at most its maximum) and an optional cap on uses. An admin
 * invite carries a note that registering through it grants editor at most.
 */
function CreateInviteForm({ defaultDays, maxDays, onCreated }: CreateInviteFormProps) {
  const { t } = useTranslation()
  const [role, setRole] = useState<ProjectRole>('viewer')
  const [days, setDays] = useState(String(defaultDays))
  const [maxUses, setMaxUses] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const daysValid = inRange(Number(days), 1, maxDays)
  const maxUsesValid = maxUses.trim() === '' || inRange(Number(maxUses), 1, Number.MAX_SAFE_INTEGER)
  const canSubmit = !submitting && daysValid && maxUsesValid

  async function submit(event: SyntheticEvent) {
    event.preventDefault()
    if (!canSubmit) {
      return
    }
    setSubmitting(true)
    setError(null)
    try {
      const created = await createInvite({
        role,
        days: Number(days),
        ...(maxUses.trim() === '' ? {} : { max_uses: Number(maxUses) }),
      })
      onCreated(created)
    } catch (err: unknown) {
      const code = err instanceof InviteApiError ? err.code : ''
      const refusal = CREATE_REFUSALS.find((known) => known === code)
      setError(
        refusal === undefined
          ? t('invites.createFailed')
          : t(`invites.refusal.${refusal}`, { max: maxDays }),
      )
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Form
      noValidate
      onSubmit={(event) => {
        void submit(event)
      }}
      data-testid="invite-create-form"
    >
      {error !== null && (
        <Alert variant="danger" role="alert">
          {error}
        </Alert>
      )}
      <Row className="g-3 align-items-start">
        <Col xs={12} sm={4}>
          <Form.Group controlId="invite-role">
            <Form.Label>{t('invites.roleLabel')}</Form.Label>
            <Form.Select
              value={role}
              disabled={submitting}
              onChange={(event) => {
                setRole(event.currentTarget.value as ProjectRole)
              }}
            >
              {MEMBER_ROLES.map((option) => (
                <option key={option} value={option}>
                  {t(`roles.${option}`)}
                </option>
              ))}
            </Form.Select>
          </Form.Group>
        </Col>
        <Col xs={6} sm={4}>
          <Form.Group controlId="invite-days">
            <Form.Label>{t('invites.daysLabel')}</Form.Label>
            <Form.Control
              type="number"
              inputMode="numeric"
              min={1}
              max={maxDays}
              value={days}
              disabled={submitting}
              isInvalid={!daysValid}
              onChange={(event) => {
                setDays(event.target.value)
              }}
            />
            <Form.Control.Feedback type="invalid">
              {t('invites.refusal.invalid_days', { max: maxDays })}
            </Form.Control.Feedback>
          </Form.Group>
        </Col>
        <Col xs={6} sm={4}>
          <Form.Group controlId="invite-max-uses">
            <Form.Label>{t('invites.maxUsesLabel')}</Form.Label>
            <Form.Control
              type="number"
              inputMode="numeric"
              min={1}
              value={maxUses}
              placeholder={t('invites.maxUsesPlaceholder')}
              disabled={submitting}
              isInvalid={!maxUsesValid}
              onChange={(event) => {
                setMaxUses(event.target.value)
              }}
            />
            <Form.Control.Feedback type="invalid">
              {t('invites.refusal.invalid_max_uses')}
            </Form.Control.Feedback>
          </Form.Group>
        </Col>
      </Row>
      {role === 'admin' && (
        <p className="small text-secondary mt-2 mb-0" data-testid="invite-admin-note">
          {t('invites.adminNote')}
        </p>
      )}
      <div className="mt-3">
        <Button type="submit" variant="primary" disabled={!canSubmit}>
          {submitting ? (
            <Spinner animation="border" size="sm" className="me-2" aria-hidden="true" />
          ) : (
            <Icon name="plus-lg" className="me-1" />
          )}
          {t('invites.create')}
        </Button>
      </div>
    </Form>
  )
}

/**
 * The members page's "Pozvánky" section: a project admin creates an invitation
 * link (role, validity in days, optional cap on uses), sees the created address
 * in a read-only field with a copy button, and manages the list — role, state,
 * expiry, uses of the cap, who created it, copy for a live one (the code stays
 * readable for the admin, so the address can be copied again at any time) and
 * revoke behind a confirmation.
 */
export function InvitesSection() {
  const { t, i18n } = useTranslation()
  const toast = useToast()
  const [load, setLoad] = useState<LoadState>({ status: 'loading' })
  const [created, setCreated] = useState<Invite | null>(null)
  const [revoke, setRevoke] = useState<PendingRevoke>({ show: false, invite: null })
  const [revoking, setRevoking] = useState(false)

  const reload = useCallback(async (signal?: AbortSignal) => {
    try {
      const list = await fetchInvites(signal)
      if (signal?.aborted !== true) {
        setLoad({
          status: 'ready',
          invites: list.invites,
          defaultDays: list.default_days,
          maxDays: list.max_days,
        })
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

  function handleCreated(invite: Invite) {
    setCreated(invite)
    setLoad((prev) =>
      prev.status === 'ready' ? { ...prev, invites: [invite, ...prev.invites] } : prev,
    )
  }

  async function handleRevoke() {
    const target = revoke.invite
    if (target === null || revoking) {
      return
    }
    setRevoking(true)
    try {
      await revokeInvite(target.uid)
      const now = new Date().toISOString()
      setLoad((prev) =>
        prev.status === 'ready'
          ? {
              ...prev,
              invites: prev.invites.map((inv) =>
                inv.uid === target.uid ? { ...inv, state: 'revoked', revoked_at: now } : inv,
              ),
            }
          : prev,
      )
      if (created?.uid === target.uid) {
        setCreated(null)
      }
      toast.show({ message: t('invites.revokedToast'), variant: 'success' })
    } catch (error: unknown) {
      if (error instanceof InviteApiError && error.code === 'not_found') {
        toast.show({ message: t('invites.notFound'), variant: 'danger' })
        void reload()
      } else {
        toast.show({ message: t('invites.revokeFailed'), variant: 'danger' })
      }
    } finally {
      setRevoking(false)
      setRevoke((prev) => ({ ...prev, show: false }))
    }
  }

  /** The copy and revoke buttons of a live invite (its own line on a phone card). */
  function actions(invite: Invite) {
    if (invite.state !== 'active') {
      return null
    }
    return (
      <span className="d-inline-flex flex-wrap gap-2">
        <CopyLinkButton url={inviteAddress(invite)} size="sm" />
        <Button
          size="sm"
          variant="outline-danger"
          onClick={() => {
            setRevoke({ show: true, invite })
          }}
        >
          <Icon name="slash-circle" className="me-1" />
          {t('invites.revoke')}
        </Button>
      </span>
    )
  }

  const columns: RecordColumn<Invite>[] = [
    {
      key: 'role',
      header: t('invites.columns.role'),
      cell: (inv) => t(`roles.${inv.role}`),
    },
    {
      key: 'state',
      header: t('invites.columns.state'),
      cell: (inv) => <Badge bg={STATE_VARIANT[inv.state]}>{t(`invites.state.${inv.state}`)}</Badge>,
    },
    {
      key: 'expires',
      header: t('invites.columns.expires'),
      cellClassName: 'text-nowrap',
      cell: (inv) => formatDate(inv.expires_at, i18n.language),
    },
    {
      key: 'uses',
      header: t('invites.columns.uses'),
      cellClassName: 'text-nowrap',
      cell: (inv) =>
        inv.max_uses === null
          ? t('invites.usesUnlimited', { uses: inv.uses })
          : t('invites.uses', { uses: inv.uses, max: inv.max_uses }),
    },
    {
      key: 'createdBy',
      header: t('invites.columns.createdBy'),
      cell: (inv) =>
        inv.created_by_name === '' ? (
          <span className="text-secondary">{t('invites.creatorGone')}</span>
        ) : (
          inv.created_by_name
        ),
    },
    {
      key: 'actions',
      header: t('invites.columns.actions'),
      cardHidden: true,
      cell: actions,
    },
  ]

  return (
    <section aria-labelledby="invites-title" className="mt-4" data-testid="invites-section">
      <h2 id="invites-title" className="h4">
        {t('invites.title')}
      </h2>
      <p className="text-secondary">{t('invites.intro')}</p>

      <Card className="mb-3">
        <Card.Body>
          {load.status === 'loading' && (
            <div className="d-flex justify-content-center py-4">
              <Spinner animation="border" role="status">
                <span className="visually-hidden">{t('invites.loading')}</span>
              </Spinner>
            </div>
          )}
          {load.status === 'error' && (
            <ErrorState
              title={t('invites.loadFailed')}
              onRetry={() => {
                setLoad({ status: 'loading' })
                void reload()
              }}
            />
          )}
          {load.status === 'ready' && (
            <>
              <CreateInviteForm
                defaultDays={load.defaultDays}
                maxDays={load.maxDays}
                onCreated={handleCreated}
              />
              {created !== null && (
                <div className="mt-3" data-testid="invite-created">
                  <p className="mb-2">{t('invites.createdLead')}</p>
                  <LinkAddress url={inviteAddress(created)} label={t('invites.linkLabel')} />
                  <p className="small text-secondary mb-0">{t('invites.copyLater')}</p>
                </div>
              )}
            </>
          )}
        </Card.Body>
      </Card>

      {load.status === 'ready' && (
        <Card>
          <Card.Body>
            {load.invites.length === 0 ? (
              <EmptyState title={t('invites.empty')} />
            ) : (
              <RecordTable
                records={load.invites}
                columns={columns}
                rowKey={(inv) => inv.uid}
                cardActions={actions}
                className="align-middle mb-0"
              />
            )}
          </Card.Body>
        </Card>
      )}

      <ConfirmModal
        show={revoke.show}
        title={t('invites.revokeTitle')}
        confirmLabel={t('invites.revoke')}
        busy={revoking}
        onConfirm={() => {
          void handleRevoke()
        }}
        onCancel={() => {
          setRevoke((prev) => ({ ...prev, show: false }))
        }}
      >
        {t('invites.revokeBody')}
      </ConfirmModal>
    </section>
  )
}
