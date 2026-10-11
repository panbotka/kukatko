import { useState } from 'react'
import Button from 'react-bootstrap/Button'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { Icon } from '../Icon'

import Modal from '../Modal'
import { SearchQueryReference } from './SearchQueryReference'

/**
 * The query-language help: a small `?` button next to the search box and a
 * modal holding {@link SearchQueryReference} — every filter with one worked
 * example, plus the operators (AND/OR/NOT, ranges, quoting, wildcards). A query
 * language nobody knows about is a query language nobody uses — this is its
 * discoverability at the field; the Search chapter of `/help` renders the same
 * reference for the reader who looks for it there.
 *
 * `fullSearchHref`, when given, adds a way out at the foot of the dialog to
 * ranked full-text and semantic search. The library's quick filter passes it:
 * that link used to be the tail of a helper sentence under the field, a whole
 * row of chrome above the photographs that mostly repeated the placeholder, and
 * the explanation of what the field understands is where the reader who wonders
 * goes looking. Omit it (the search page, which *is* that destination) and the
 * dialog has no footer.
 */
export function SearchQueryHelp({ fullSearchHref }: { fullSearchHref?: string }) {
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
        {fullSearchHref !== undefined && (
          <Modal.Footer>
            {/* Navigating away unmounts nothing here (the bar stays), so the
                dialog closes itself rather than following the reader. */}
            <Link to={fullSearchHref} className="text-decoration-none" onClick={close}>
              {t('library.filters.fullSearchLink')}
            </Link>
          </Modal.Footer>
        )}
      </Modal>
    </>
  )
}
