import Badge from 'react-bootstrap/Badge'
import { useTranslation } from 'react-i18next'

import { isPlaceholderEmail } from './account'

/**
 * One account's e-mail address as the roster shows it: the address itself when
 * there is a real one, and otherwise a badge saying there is none plus the line
 * asking for it to be filled in.
 *
 * The missing case is deliberately not styled as an address at all. Every mail
 * the app sends — approval, password reset — goes to this field, so "there is
 * something in the column" and "this person can be reached" have to be visibly
 * different things.
 */
export function UserEmail({ email }: { email: string }) {
  const { t } = useTranslation()

  if (!isPlaceholderEmail(email)) {
    return <span className="text-break">{email}</span>
  }
  return (
    <>
      <Badge bg="warning" text="dark">
        {t('users.email.none')}
      </Badge>
      <div className="text-secondary small">{t('users.email.noneHint')}</div>
    </>
  )
}
