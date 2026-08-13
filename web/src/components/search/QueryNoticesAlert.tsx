import Alert from 'react-bootstrap/Alert'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

/** Props for {@link QueryNoticesAlert}. */
export interface QueryNoticesAlertProps {
  /**
   * The machine-readable reason codes the backend reported in `notices`.
   * Nothing renders when empty, or when no code is one this component knows.
   */
  notices: readonly string[]
}

/** The one reason code the backend sends today. */
const PERSON_ME_UNLINKED = 'person_me_unlinked'

/**
 * The "this grid is empty on purpose" notice.
 *
 * A query that cannot be satisfied is answered with nothing rather than with
 * everything — `person:me` asked by an account that has never said which person
 * it is — and an empty grid with no explanation reads as "you are in no
 * photographs", which is a much sadder and quite untrue message. So the reason
 * is stated, together with the one link that fixes it: the account page, where
 * that is set.
 *
 * Unknown codes are ignored rather than printed raw: a client older than the
 * server must not show the reader an identifier.
 */
export function QueryNoticesAlert({ notices }: QueryNoticesAlertProps) {
  const { t } = useTranslation()
  if (!notices.includes(PERSON_ME_UNLINKED)) {
    return null
  }
  return (
    <Alert variant="info" className="py-2">
      {t('search.personMeUnlinked')} <Link to="/account">{t('search.personMeUnlinkedLink')}</Link>
    </Alert>
  )
}
