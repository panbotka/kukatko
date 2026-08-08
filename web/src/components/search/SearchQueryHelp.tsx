import { useState } from 'react'
import Button from 'react-bootstrap/Button'
import Modal from 'react-bootstrap/Modal'
import { useTranslation } from 'react-i18next'

import { Icon } from '../Icon'

import { SearchQueryReference } from './SearchQueryReference'

/**
 * The query-language help: a small `?` button next to the search box and a
 * modal holding {@link SearchQueryReference} — every filter with one worked
 * example, plus the operators (AND/OR/NOT, ranges, quoting, wildcards). A query
 * language nobody knows about is a query language nobody uses — this is its
 * discoverability at the field; the Search chapter of `/help` renders the same
 * reference for the reader who looks for it there.
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
          <SearchQueryReference />
        </Modal.Body>
      </Modal>
    </>
  )
}
