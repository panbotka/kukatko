import { type ReactNode, useId, useState } from 'react'
import Button from 'react-bootstrap/Button'
import Card from 'react-bootstrap/Card'
import { useTranslation } from 'react-i18next'

import { formatDateTime } from '../../lib/format'
import { Icon } from '../Icon'

import { isWaitingForApproval } from './account'
import { useActionReasons } from './actionReasons'
import { ApproveButton, ApproveError, SecondaryActions, type UserActionsProps } from './UserActions'
import { UserEmail } from './UserEmail'
import { UserStateBadges } from './UserStateBadges'

/** Props of {@link UserCard}. */
export interface UserCardProps extends UserActionsProps {
  /** The account's linked person, already resolved to a name (or null for none). */
  person: string | null
}

/** One "label: value" line of a card's compact field list. */
function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <>
      <dt className="col-4 fw-normal text-secondary text-break">{label}</dt>
      <dd className="col-8 mb-1 text-break">{children}</dd>
    </>
  )
}

/**
 * One account of the user roster on a phone.
 *
 * The phone is where an administrator usually lets newly registered people in,
 * so the card is shaped around that errand rather than around the table's ten
 * columns. A **waiting** account shows who it is, where they can be reached and
 * when they registered, then one large full-width Approve that acts on a single
 * tap (see {@link ApproveButton}); a refusal is printed on the card itself. An
 * already approved account shows its state, role, linked person, last sign-in
 * and note.
 *
 * Everything else an administrator can do — edit, change the password, issue a
 * reset link, block — sits folded behind one "More actions" toggle per card, so
 * a list of waiting people reads as a list of people rather than a wall of
 * buttons. The fold is a disclosure (`aria-expanded`/`aria-controls`) whose
 * content is only mounted while open; the disabled-reason line rides inside it,
 * next to the buttons it explains. Approve's own reason (a blocked account, the
 * maintainer boundary) stays visible under Approve, outside the fold.
 */
export function UserCard({
  user,
  self,
  canManage,
  approving,
  approveError,
  person,
  onApprove,
  ...handlers
}: UserCardProps) {
  const { t, i18n } = useTranslation()
  const [open, setOpen] = useState(false)
  const foldId = useId()
  const hintId = useId()
  const approveHintId = useId()
  const reasons = useActionReasons(user, self, canManage)
  const waiting = isWaitingForApproval(user)
  const named = user.display_name !== ''

  return (
    <Card className="kk-user-card">
      <Card.Body>
        {/* The badges sit beside the name while both fit and wrap under it once
            they would squeeze a long name into breaking mid-word. */}
        <div className="d-flex flex-wrap justify-content-between align-items-start column-gap-2 row-gap-1">
          <div className="kk-user-card__name kk-min-w-0">
            <div className="fw-semibold text-break">
              {named ? user.display_name : user.username}
            </div>
            {named && <div className="small text-secondary text-break">{user.username}</div>}
          </div>
          <UserStateBadges user={user} />
        </div>

        <dl className="row g-0 small mb-0 mt-2">
          <Field label={t('users.columns.email')}>
            <UserEmail email={user.email} />
          </Field>
          {waiting ? (
            <Field label={t('users.card.registered')}>
              {formatDateTime(user.created_at, i18n.language)}
            </Field>
          ) : (
            <>
              <Field label={t('users.columns.role')}>{t(`roles.${user.role}`)}</Field>
              {person !== null && <Field label={t('users.columns.subject')}>{person}</Field>}
              <Field label={t('users.columns.lastLogin')}>
                {user.last_login_at === undefined
                  ? t('users.never')
                  : formatDateTime(user.last_login_at, i18n.language)}
              </Field>
              {user.note !== '' && (
                <Field label={t('users.columns.note')}>
                  <span className="kk-multiline">{user.note}</span>
                </Field>
              )}
            </>
          )}
        </dl>

        {waiting && (
          <div className="mt-3">
            <ApproveButton
              variant="success"
              size="lg"
              className="w-100"
              reason={reasons.approveOff}
              reasonId={approveHintId}
              busy={approving}
              onApprove={onApprove}
            />
            {reasons.approveOff !== undefined && (
              <div id={approveHintId} className="text-secondary small mt-1">
                {reasons.approveOff}
              </div>
            )}
            {approveError !== null && <ApproveError error={approveError} className="mt-1" />}
          </div>
        )}

        <Button
          variant="outline-secondary"
          className="w-100 mt-2 d-flex align-items-center justify-content-center gap-2"
          aria-expanded={open}
          aria-controls={open ? foldId : undefined}
          onClick={() => {
            setOpen((value) => !value)
          }}
        >
          {t('users.card.more')}
          <Icon name={open ? 'chevron-up' : 'chevron-down'} />
        </Button>
        {open && (
          <div id={foldId} className="mt-2">
            <div className="kk-user-card__more">
              <SecondaryActions user={user} reasons={reasons} hintId={hintId} {...handlers} />
            </div>
            {reasons.toggleOff !== undefined && (
              <div id={hintId} className="text-secondary small mt-1">
                {reasons.toggleOff}
              </div>
            )}
          </div>
        )}
      </Card.Body>
    </Card>
  )
}
