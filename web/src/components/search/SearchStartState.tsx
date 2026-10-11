import Button from 'react-bootstrap/Button'
import { useTranslation } from 'react-i18next'

import { useSearchHistory } from '../../hooks/useSearchHistory'
import { SEARCH_EXAMPLES, START_RECENT_LIMIT } from '../../lib/searchStart'
import { Icon } from '../Icon'

/** Props for {@link SearchStartState}. */
export interface SearchStartStateProps {
  /** Runs a whole query at once — a recent search is one already submitted. */
  onRun: (query: string) => void
  /** Puts an example term into the box, for the reader to adjust or run. */
  onInsert: (term: string) => void
}

/**
 * What the search page shows before anything is asked: the one-line prompt, the
 * reader's recent searches and a few example queries.
 *
 * A lone "type something" over an empty page leaves the reader with a blank box
 * and no idea what it understands. The recent searches — the same server-side
 * history the command palette offers, so the phone shows what was typed on the
 * laptop — make repeating one a single tap, and picking one runs it. The
 * examples teach the query language by showing it: picking one only *inserts*
 * the term, because `person:Jarmila` is a shape to adapt, not a search the
 * reader necessarily wants to run as it is.
 *
 * Both rows are chips that wrap, so the phone gets the same state as the
 * desktop. A failed or empty history simply leaves its row out, and forgetting
 * the history (offered beside its heading) removes the row at once.
 */
export function SearchStartState({ onRun, onInsert }: SearchStartStateProps) {
  const { t } = useTranslation()
  const history = useSearchHistory(true)
  const recent = history.entries.slice(0, START_RECENT_LIMIT)

  return (
    <div className="text-center text-secondary py-5">
      <p className="mb-0 kk-section-title">{t('search.prompt')}</p>

      {recent.length > 0 && (
        <section className="mt-4" aria-labelledby="kk-search-start-recent">
          <div className="d-flex justify-content-center align-items-baseline gap-2 mb-2">
            <h2 id="kk-search-start-recent" className="kk-text-caption mb-0">
              {t('search.history.label')}
            </h2>
            {/* The box's dropdown, which used to carry this action, no longer
                lists the history while it is empty — so it lives here now. */}
            <Button
              variant="link"
              size="sm"
              className="kk-text-caption p-0"
              onClick={history.clear}
            >
              {t('search.history.clear')}
            </Button>
          </div>
          <ul className="list-unstyled d-flex flex-wrap justify-content-center gap-2 mb-0">
            {recent.map((entry) => (
              <li key={entry.query} className="mw-100">
                <Button
                  variant="outline-secondary"
                  size="sm"
                  className="kk-search-start__chip rounded-pill"
                  onClick={() => {
                    onRun(entry.query)
                  }}
                >
                  <Icon name="clock-history" className="me-1" />
                  <span className="text-truncate">{entry.query}</span>
                </Button>
              </li>
            ))}
          </ul>
        </section>
      )}

      <section className="mt-4" aria-labelledby="kk-search-start-examples">
        <h2 id="kk-search-start-examples" className="kk-text-caption mb-2">
          {t('search.start.examples')}
        </h2>
        <ul className="list-unstyled d-flex flex-wrap justify-content-center gap-2 mb-0">
          {SEARCH_EXAMPLES.map((example) => (
            <li key={example}>
              <Button
                variant="outline-secondary"
                size="sm"
                className="kk-search-start__chip font-monospace rounded-pill"
                title={t('search.start.insertTitle', { query: example })}
                onClick={() => {
                  onInsert(example)
                }}
              >
                {example}
              </Button>
            </li>
          ))}
        </ul>
      </section>
    </div>
  )
}
