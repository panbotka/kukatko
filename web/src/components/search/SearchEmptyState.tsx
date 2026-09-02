import Button from 'react-bootstrap/Button'
import { useTranslation } from 'react-i18next'

import { EmptyState } from '../EmptyState'

/** Props for {@link SearchEmptyState}. */
export interface SearchEmptyStateProps {
  /** The query that found nothing, trimmed; `''` when only filters narrowed. */
  query: string
  /** Whether any filter besides the query is narrowing the search. */
  hasFilters: boolean
  /** Drops every filter, keeping the query and the mode. */
  onClearFilters: () => void
  /**
   * Whether describing the photo is still an option to offer — the instance can
   * answer a semantic search and the reader is not already running one.
   */
  canDescribe: boolean
  /** Switches the search to describing the photo (semantic mode). */
  onDescribe: () => void
}

/**
 * What a search that found nothing says.
 *
 * "Nic nenalezeno" over an empty page is true and useless: it leaves the reader
 * to guess whether the photograph is missing, misspelled, or merely hidden
 * behind a filter they set ten minutes ago on another page. So the empty result
 * names the query it failed on and offers the three things that actually fix
 * one, each only when it applies: **drop the filters** (a button, not advice —
 * the filters are the commonest reason a good query finds nothing), **check the
 * spelling**, and **describe the photo instead**, which is the one thing this
 * library can do that a filename search cannot and which nobody discovers on
 * their own. Describing is offered only while the embeddings box is reachable
 * and the search is not already running that way, so the page never proposes a
 * step that would change nothing.
 */
export function SearchEmptyState({
  query,
  hasFilters,
  onClearFilters,
  canDescribe,
  onDescribe,
}: SearchEmptyStateProps) {
  const { t } = useTranslation()

  return (
    <EmptyState
      title={t('search.empty.title')}
      hint={query === '' ? t('search.empty.hint') : t('search.empty.hintQuery', { query })}
      action={
        <div className="d-flex flex-column align-items-center gap-3">
          {/* The steps read as a list rather than one long sentence: they are
              alternatives, and a reader scanning an empty page reads three short
              lines and none of a paragraph. */}
          <ul className="kk-text-caption text-start mb-0 ps-3">
            {hasFilters && <li>{t('search.empty.tips.filters')}</li>}
            <li>{t('search.empty.tips.spelling')}</li>
            {canDescribe && <li>{t('search.empty.tips.describe')}</li>}
          </ul>
          {(hasFilters || canDescribe) && (
            <div className="d-flex flex-wrap gap-2 justify-content-center">
              {hasFilters && (
                <Button variant="primary" onClick={onClearFilters}>
                  {t('search.empty.clearFilters')}
                </Button>
              )}
              {canDescribe && (
                <Button variant="outline-primary" onClick={onDescribe}>
                  {t('search.empty.describe')}
                </Button>
              )}
            </div>
          )}
        </div>
      }
    />
  )
}
