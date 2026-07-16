import { useState } from 'react'
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
      <Button
        variant="link"
        size="sm"
        className="p-0 text-secondary d-inline-flex align-items-center"
        aria-label={t('search.help.open')}
        title={t('search.help.open')}
        onClick={() => {
          setShow(true)
        }}
      >
        <Icon name="question-circle" />
      </Button>

      <Modal
        show={show}
        onHide={close}
        centered
        scrollable
        size="lg"
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
            <Table size="sm" borderless className="mb-0 align-middle">
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
            <Table size="sm" borderless className="mb-0 align-middle">
              <tbody>
                {QUERY_HELP_ROWS.map((row) => (
                  <tr key={row.id}>
                    <td className="text-nowrap pe-3">
                      <code>{row.keys}</code>
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
