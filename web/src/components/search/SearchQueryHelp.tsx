import { Fragment, useState } from 'react'
import Button from 'react-bootstrap/Button'
import Modal from 'react-bootstrap/Modal'
import Table from 'react-bootstrap/Table'
import { useTranslation } from 'react-i18next'

import { QUERY_HELP_OPERATORS, QUERY_HELP_ROWS } from '../../lib/queryLanguage'
import { Icon } from '../Icon'

/**
 * The query-language help: a small `?` button next to the search box and a
 * modal listing every filter with one worked example, plus the operators
 * (AND/OR/NOT, ranges, quoting, wildcards). A query language nobody knows
 * about is a query language nobody uses — this is its discoverability.
 */
export function SearchQueryHelp() {
  const { t } = useTranslation()
  const [show, setShow] = useState(false)

  const close = () => {
    setShow(false)
  }

  return (
    <>
      {/* The glyph stays small, but on a finger the button still needs a real
          target: `kukatko-tap-target-touch` gives it a 44px square on coarse
          pointers only, leaving the desktop `?` as compact as before. */}
      <Button
        variant="link"
        size="sm"
        className="p-0 text-secondary d-inline-flex align-items-center justify-content-center kukatko-tap-target-touch"
        aria-label={t('search.help.open')}
        title={t('search.help.open')}
        onClick={() => {
          setShow(true)
        }}
      >
        <Icon name="question-circle" />
      </Button>

      {/* On a phone the help takes the whole screen: the columns are code that
          must not break mid-token, so there is no width to give up. Whatever
          still does not fit scrolls inside its own `.table-responsive` wrapper
          instead of pushing the dialog past the viewport. */}
      <Modal
        show={show}
        onHide={close}
        centered
        scrollable
        size="lg"
        fullscreen="sm-down"
        aria-labelledby="query-help-title"
      >
        <Modal.Header closeButton closeLabel={t('search.help.close')}>
          <Modal.Title id="query-help-title" className="h5">
            {t('search.help.title')}
          </Modal.Title>
        </Modal.Header>
        <Modal.Body>
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
                    {/* A row can list several keys (`favorite: private:
                        archived:`). Each key alone stays unbroken, but the cell
                        may wrap between them — otherwise the widest rows would
                        force the whole table to scroll on a phone. */}
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
        </Modal.Body>
      </Modal>
    </>
  )
}
