import Badge from 'react-bootstrap/Badge'
import { useTranslation } from 'react-i18next'

import { type AdminUser } from '../../services/users'

import { isWaitingForApproval } from './account'

/**
 * One account's state, as the one or two badges that describe it.
 *
 * Blocked and waiting are independent states, not two names for the same thing:
 * an account that was never approved has never been able to sign in, while a
 * blocked one was let in and later shut out — and an account can be both, in
 * which case an administrator has two things to undo. So they get two badges in
 * two colours, and a waiting account is never painted the reassuring green of an
 * active one.
 */
export function UserStateBadges({ user }: { user: Pick<AdminUser, 'approved_at' | 'disabled'> }) {
  const { t } = useTranslation()
  const waiting = isWaitingForApproval(user)

  return (
    <span className="d-inline-flex flex-wrap gap-1">
      {user.disabled ? (
        <Badge bg="danger">{t('users.state.disabled')}</Badge>
      ) : (
        // A waiting account is enabled in the database, but calling it "active"
        // would claim it can sign in — which is exactly what it cannot do.
        !waiting && <Badge bg="success">{t('users.state.enabled')}</Badge>
      )}
      {waiting && (
        <Badge bg="warning" text="dark">
          {t('users.state.pending')}
        </Badge>
      )}
    </span>
  )
}
