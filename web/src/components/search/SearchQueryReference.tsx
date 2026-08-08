import { Fragment } from 'react'
import Table from 'react-bootstrap/Table'
import { useTranslation } from 'react-i18next'

import { QUERY_HELP_OPERATORS, QUERY_HELP_ROWS } from '../../lib/queryLanguage'

/**
 * The query-language reference itself: what a query is made of, one whole
 * worked query, and the two tables — operators and filters, each row with its
 * own example.
 *
 * It lives apart from its `?` modal because it is rendered in two places: the
 * modal beside every search field ({@link SearchQueryHelp}) and the Search
 * chapter of `/help`. A reader who never presses `?` still needs to learn that
 * `year:1965` works, and a second, hand-written copy of the syntax in the help
 * texts would start drifting from this one the day a filter is added.
 */
export function SearchQueryReference() {
  const { t } = useTranslation()

  return (
    <>
      <p className="text-secondary small">{t('search.help.intro')}</p>
      <p className="small">
        <code>{t('search.help.example')}</code>
      </p>

      <section className="mb-3">
        <h3 className="kk-section-title text-secondary">{t('search.help.operatorsTitle')}</h3>
        <Table size="sm" borderless responsive className="mb-0 align-middle">
          <tbody>
            {QUERY_HELP_OPERATORS.map((op) => (
              <tr key={op.id}>
                <td className="text-nowrap pe-3">
                  <code>{op.example}</code>
                </td>
                <td className="text-secondary small">{t(`search.help.op.${op.id}`)}</td>
              </tr>
            ))}
          </tbody>
        </Table>
      </section>

      <section className="mb-0">
        <h3 className="kk-section-title text-secondary">{t('search.help.filtersTitle')}</h3>
        <Table size="sm" borderless responsive className="mb-0 align-middle">
          <tbody>
            {QUERY_HELP_ROWS.map((row) => (
              <tr key={row.id}>
                {/* A row can list several keys (`favorite: private: archived:`).
                    Each key alone stays unbroken, but the cell may wrap between
                    them — otherwise the widest rows would force the whole table
                    to scroll on a phone. */}
                <td className="pe-3">
                  {row.keys.split(' ').map((key, index) => (
                    <Fragment key={key}>
                      {index > 0 && ' '}
                      <code className="text-nowrap">{key}</code>
                    </Fragment>
                  ))}
                </td>
                <td className="text-secondary small">
                  {t(`search.help.desc.${row.id}`)}{' '}
                  <code className="text-nowrap">{row.example}</code>
                </td>
              </tr>
            ))}
          </tbody>
        </Table>
      </section>
    </>
  )
}
