import { useMemo } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import { useTranslation } from 'react-i18next'

import { applyFilterKeyFix, suggestFilterKey, withFilterKey } from '../../lib/queryLanguage'

/** Props for {@link UnknownFiltersAlert}. */
export interface UnknownFiltersAlertProps {
  /**
   * The raw filter-shaped tokens the query language did not understand, as the
   * backend reported them in `unknown_tokens`. Nothing renders when empty.
   */
  tokens: readonly string[]
  /**
   * The query those tokens came from. Only needed together with
   * {@link UnknownFiltersAlertProps.onFix}: it is what a repaired key is written
   * back into.
   */
  query?: string
  /**
   * Called with the repaired query when the reader accepts a suggestion. Without
   * it the suggestion is still stated — just as a sentence rather than a button,
   * which is all a caller that cannot rewrite its own query can offer.
   */
  onFix?: (query: string) => void
}

/** One unknown token and the key it was probably meant to carry. */
interface TokenSuggestion {
  /** The token exactly as typed. */
  token: string
  /** The nearest valid filter key. */
  key: string
  /** The whole token re-keyed to it — what accepting the hint would type. */
  fixed: string
}

/**
 * The "I don't understand these filters" notice: a mistyped key (`osoba:Jarmila`
 * for `person:`) degrades to free text server-side, so the grid silently reads
 * as "no such photos" rather than "you typed it wrong". This says which tokens
 * fell through, in one wording shared by every grid that accepts a query — the
 * search page and the library's own quick filter — so the same mistake never
 * gets two different explanations.
 *
 * Naming the token is only half an answer, though: the reader still has to guess
 * which of four dozen keys they were reaching for. So whenever one of them is
 * close enough ({@link suggestFilterKey} — a spelling slip, or the Czech word
 * for an English key), the notice offers it, and accepting rewrites the query in
 * place. Nothing here blocks or re-runs anything on its own: the search that
 * degraded to free text has already answered, and the hint is a suggestion the
 * reader may ignore.
 */
export function UnknownFiltersAlert({ tokens, query, onFix }: UnknownFiltersAlertProps) {
  const { t } = useTranslation()

  // One suggestion per distinct token: a repeated mistake is one mistake, and
  // fixing it repairs every occurrence at once.
  const suggestions = useMemo<TokenSuggestion[]>(() => {
    const seen = new Set<string>()
    const out: TokenSuggestion[] = []
    for (const token of tokens) {
      if (seen.has(token)) {
        continue
      }
      seen.add(token)
      const key = suggestFilterKey(token)
      if (key !== null) {
        out.push({ token, key, fixed: withFilterKey(token, key) })
      }
    }
    return out
  }, [tokens])

  if (tokens.length === 0) {
    return null
  }

  return (
    <Alert variant="info" className="py-2">
      <div>
        {t('search.unknownTokens')}{' '}
        {tokens.map((token, i) => (
          // A query may repeat a token, so the index is part of the key.
          <span key={token + String(i)}>
            {i > 0 && ', '}
            <code>{token}</code>
          </span>
        ))}
      </div>
      {suggestions.map(({ token, key, fixed }) => (
        <div key={`fix:${token}`} className="mt-1">
          {query !== undefined && onFix !== undefined ? (
            <Button
              variant="link"
              size="sm"
              // The alert's own text colour, underlined: the page's link blue
              // (and Bootstrap's `alert-link`, which is white here) is barely
              // legible on this alert's filled background.
              className="p-0 align-baseline text-reset text-decoration-underline"
              onClick={() => {
                onFix(applyFilterKeyFix(query, token, key))
              }}
            >
              {t('search.didYouMean', { suggestion: fixed })}
            </Button>
          ) : (
            <span>{t('search.didYouMean', { suggestion: fixed })}</span>
          )}
        </div>
      ))}
    </Alert>
  )
}
