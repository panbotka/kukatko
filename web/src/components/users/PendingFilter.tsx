import Form from 'react-bootstrap/Form'
import { useTranslation } from 'react-i18next'

/** Props for {@link PendingFilter}. */
export interface PendingFilterProps {
  /** Whether the roster is currently narrowed to the waiting accounts. */
  pendingOnly: boolean
  /** How many accounts are waiting for an administrator, across the whole roster. */
  pendingCount: number
  /** Switches the filter. */
  onChange: (pendingOnly: boolean) => void
}

/**
 * The one control above the user roster: show everybody, or only the accounts
 * waiting to be let in — with the number of those printed beside it.
 *
 * The count is the point of the strip. Self-service registration leaves accounts
 * sitting in a state only an administrator can end, and an administrator who
 * opens this page for an unrelated reason has no way to notice that unless the
 * page says so. The count is therefore shown whether the filter is on or off,
 * and stays the count of the **whole** roster rather than of what is on screen —
 * a filtered list that said "1" while hiding two others would be a trap. The
 * number goes in as `pending` rather than as i18next's magic `count`: it is
 * printed after a label, where neither language inflects anything around it.
 *
 * It filters what is already loaded rather than re-asking the backend (which
 * does offer `GET /admin/users?pending=true`): the count needs every account
 * anyway, so a second request could only make the two disagree.
 */
export function PendingFilter({ pendingOnly, pendingCount, onChange }: PendingFilterProps) {
  const { t } = useTranslation()

  return (
    <div className="d-flex flex-wrap align-items-center justify-content-between gap-2 mb-3">
      <Form.Check
        type="switch"
        id="users-pending-only"
        label={t('users.filter.pendingOnly')}
        checked={pendingOnly}
        onChange={(event) => {
          onChange(event.target.checked)
        }}
      />
      <span className={pendingCount > 0 ? 'fw-semibold text-warning-emphasis' : 'text-secondary'}>
        {t('users.filter.waiting', { pending: pendingCount })}
      </span>
    </div>
  )
}
