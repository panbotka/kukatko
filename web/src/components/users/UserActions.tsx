import { useId } from 'react'
import Button, { type ButtonProps } from 'react-bootstrap/Button'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'

import { MAX_NOTE_LENGTH, type AdminUser } from '../../services/users'
import { MIN_PASSWORD_LENGTH } from '../../services/auth'
import { ReasonedButton } from '../ReasonedButton'

import { isWaitingForApproval } from './account'
import { type ActionReasons, useActionReasons } from './actionReasons'
import { type ErrorKey } from './errors'

/** The things an administrator can do to one account, as callbacks. */
export interface UserActionHandlers {
  onApprove: () => void
  onEdit: () => void
  onPassword: () => void
  onResetLink: () => void
  onToggle: () => void
}

/** Everything both roster layouts need to offer one account's actions. */
export interface UserActionsProps extends UserActionHandlers {
  user: AdminUser
  /** True when the row is the signed-in administrator's own account. */
  self: boolean
  /**
   * True when the signed-in actor may manage this account. A non-maintainer
   * cannot touch a maintainer's account (edit, reset the password of, approve or
   * disable), so its actions are disabled — mirroring the backend
   * `guardMaintainerBoundary`.
   */
  canManage: boolean
  /** True while this account's approval request is in flight. */
  approving: boolean
  /** Why this account's last approval was refused, or null. */
  approveError: ErrorKey | null
}

/** Props of {@link ApproveButton}. */
interface ApproveButtonProps {
  /** Why approving is off, or undefined when it may be pressed. */
  reason: string | undefined
  /** The id of the visible line that carries `reason`. */
  reasonId: string
  /** True while the request runs: the button is disabled and spins. */
  busy: boolean
  variant: ButtonProps['variant']
  size?: ButtonProps['size']
  className?: string
  onApprove: () => void
}

/**
 * Approve, in one press — no confirmation step. Approving is the errand the
 * roster exists for, and a wrong approval is cheap to undo by blocking the
 * account. While the request runs the button is natively disabled and shows a
 * spinner, so a second tap cannot fire a second request.
 */
export function ApproveButton({
  reason,
  reasonId,
  busy,
  variant,
  size,
  className,
  onApprove,
}: ApproveButtonProps) {
  const { t } = useTranslation()
  if (busy) {
    return (
      <Button variant={variant} size={size} className={className} disabled aria-busy="true">
        <Spinner animation="border" size="sm" aria-hidden="true" className="me-2" />
        {t('users.approve.action')}
      </Button>
    )
  }
  return (
    <ReasonedButton
      variant={variant}
      size={size}
      className={className}
      disabledReason={reason}
      reasonId={reasonId}
      onClick={onApprove}
    >
      {t('users.approve.action')}
    </ReasonedButton>
  )
}

/** The inline message of a refused approval, on the row or card it belongs to. */
export function ApproveError({ error, className }: { error: ErrorKey; className?: string }) {
  const { t } = useTranslation()
  return (
    <div
      role="alert"
      className={`text-danger small${className === undefined ? '' : ` ${className}`}`}
    >
      {t(error, { min: MIN_PASSWORD_LENGTH, max: MAX_NOTE_LENGTH })}
    </div>
  )
}

/** Props of {@link SecondaryActions}. */
interface SecondaryActionsProps extends Omit<UserActionHandlers, 'onApprove'> {
  user: AdminUser
  reasons: ActionReasons
  /** The id of the visible line under the buttons that carries their reason. */
  hintId: string
  size?: ButtonProps['size']
}

/**
 * Edit, change password, reset link and block/enable — everything but Approve.
 * The table shows them inline; a phone card folds them behind "More actions".
 *
 * Every reason a button here can have is the one printed on the shared hint
 * line, so they all describe themselves by that line rather than each carrying
 * a hidden copy of the same sentence for a screen reader to repeat.
 */
export function SecondaryActions({
  user,
  reasons,
  hintId,
  size,
  onEdit,
  onPassword,
  onResetLink,
  onToggle,
}: SecondaryActionsProps) {
  const { t } = useTranslation()
  return (
    <>
      <ReasonedButton
        variant="outline-secondary"
        size={size}
        disabledReason={reasons.outOfReach}
        reasonId={hintId}
        onClick={onEdit}
      >
        {t('users.edit')}
      </ReasonedButton>
      <ReasonedButton
        variant="outline-secondary"
        size={size}
        disabledReason={reasons.outOfReach}
        reasonId={hintId}
        onClick={onPassword}
      >
        {t('users.changePassword')}
      </ReasonedButton>
      <ReasonedButton
        variant="outline-secondary"
        size={size}
        disabledReason={reasons.outOfReach}
        reasonId={hintId}
        onClick={onResetLink}
      >
        {t('users.resetLink.action')}
      </ReasonedButton>
      <ReasonedButton
        variant={user.disabled ? 'outline-success' : 'outline-danger'}
        size={size}
        disabledReason={reasons.toggleOff}
        reasonId={hintId}
        onClick={onToggle}
      >
        {user.disabled ? t('users.enable') : t('users.disable')}
      </ReasonedButton>
    </>
  )
}

/**
 * The table cell's action cluster: Approve (on a waiting row only) followed by
 * the secondary actions, plus the one-line reason when a control is disabled and
 * the inline message when an approval was refused.
 *
 * **Approve appears only on a waiting row.** It is not a power an administrator
 * has over every account but the answer to a question one particular account is
 * asking, so on an account that was already let in there is nothing to press and
 * nothing to grey out.
 */
export function UserActions({
  user,
  self,
  canManage,
  approving,
  approveError,
  onApprove,
  ...handlers
}: UserActionsProps) {
  const hintId = useId()
  const approveHintId = useId()
  const reasons = useActionReasons(user, self, canManage)
  // The reason under the cluster: the toggle's covers the boundary too, and is
  // the only one when the row is the actor's own account.
  const hint = reasons.toggleOff
  const waiting = isWaitingForApproval(user)
  // Approve has its own line only when it is the one control that is off — a
  // blocked account; the shared line already covers the maintainer boundary.
  const approveOwnHint = reasons.outOfReach === undefined ? reasons.approveOff : undefined
  return (
    <>
      <div className="d-flex gap-1 flex-wrap">
        {waiting && (
          <ApproveButton
            variant="outline-success"
            size="sm"
            reason={reasons.approveOff}
            reasonId={reasons.outOfReach === undefined ? approveHintId : hintId}
            busy={approving}
            onApprove={onApprove}
          />
        )}
        <SecondaryActions user={user} reasons={reasons} hintId={hintId} size="sm" {...handlers} />
      </div>
      {hint !== undefined && (
        <div id={hintId} className="text-secondary small mt-1">
          {hint}
        </div>
      )}
      {waiting && approveOwnHint !== undefined && (
        <div id={approveHintId} className="text-secondary small mt-1">
          {approveOwnHint}
        </div>
      )}
      {approveError !== null && <ApproveError error={approveError} className="mt-1" />}
    </>
  )
}
