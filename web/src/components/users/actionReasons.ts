import { useTranslation } from 'react-i18next'

import { type AdminUser } from '../../services/users'

/** Why each of an account's controls is off, if it is; undefined = live. */
export interface ActionReasons {
  /**
   * The per-row maintainer boundary: this administrator may manage users, just
   * not *this* one — so every control stays on the row and says why it is off.
   */
  outOfReach: string | undefined
  /** Why the block/enable control is off: the actor's own account, or the boundary. */
  toggleOff: string | undefined
  /**
   * Why Approve is off: the boundary, or a blocked account. Approving a blocked
   * account is refused by the backend (409) and would be a half-measure anyway —
   * the person still could not sign in.
   */
  approveOff: string | undefined
}

/**
 * Works out why each control of one account is off. One derivation for both
 * layouts, so the table cluster and the phone card can never disagree about a
 * reason; it is a hook only because the reasons are translated sentences.
 */
export function useActionReasons(
  user: AdminUser,
  self: boolean,
  canManage: boolean,
): ActionReasons {
  const { t } = useTranslation()
  const outOfReach = canManage ? undefined : t('users.maintainerManageHint')
  const toggleOff = self ? t('users.selfDisableHint') : outOfReach
  const blocked = user.disabled ? t('users.approve.blockedHint') : undefined
  return { outOfReach, toggleOff, approveOff: outOfReach ?? blocked }
}
