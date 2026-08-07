import Alert from 'react-bootstrap/Alert'
import { useTranslation } from 'react-i18next'

/** Props for {@link UnknownFiltersAlert}. */
export interface UnknownFiltersAlertProps {
  /**
   * The raw filter-shaped tokens the query language did not understand, as the
   * backend reported them in `unknown_tokens`. Nothing renders when empty.
   */
  tokens: readonly string[]
}

/**
 * The "I don't understand these filters" notice: a mistyped key (`osoba:Jarmila`
 * for `person:`) degrades to free text server-side, so the grid silently reads
 * as "no such photos" rather than "you typed it wrong". This says which tokens
 * fell through, in one wording shared by every grid that accepts a query — the
 * search page and the library's own quick filter — so the same mistake never
 * gets two different explanations.
 */
export function UnknownFiltersAlert({ tokens }: UnknownFiltersAlertProps) {
  const { t } = useTranslation()
  if (tokens.length === 0) {
    return null
  }
  return (
    <Alert variant="info" className="py-2">
      {t('search.unknownTokens')}{' '}
      {tokens.map((token, i) => (
        // A query may repeat a token, so the index is part of the key.
        <span key={token + String(i)}>
          {i > 0 && ', '}
          <code>{token}</code>
        </span>
      ))}
    </Alert>
  )
}
