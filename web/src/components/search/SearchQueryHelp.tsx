import { useState } from 'react'
import Button from 'react-bootstrap/Button'
import { useTranslation } from 'react-i18next'

import { ProjectLink } from '../../project/ProjectLink'
import { Icon } from '../Icon'

import Modal from '../Modal'
import { SearchQueryReference } from './SearchQueryReference'

/**
 * The query-language help: a `?` icon button next to the search box and a
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
export function SearchQueryHelp({
  fullSearchHref,
  className,
}: {
  fullSearchHref?: string
  /** Extra classes for the `?` trigger, e.g. to seat it inside an input group. */
  className?: string
}) {
  const { t } = useTranslation()
  const [show, setShow] = useState(false)

  const close = () => {
    setShow(false)
  }

  return (
    <>
      {/* The app's standard icon button — the same quiet `outline-secondary`
          `btn-sm` square (~32px) the edit panel's rotate pair and the other
          icon-only actions wear, lifted to a 44px square on coarse pointers by
          `kukatko-tap-target-touch`. It used to be a bare 16px `link` glyph in
          the muted grey, easy to miss right where the query language needs
          finding. */}
      <Button
        variant="outline-secondary"
        size="sm"
        className={[
          'd-inline-flex align-items-center justify-content-center kukatko-tap-target-touch',
          className,
        ]
          .filter(Boolean)
          .join(' ')}
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
            <ProjectLink to={fullSearchHref} className="text-decoration-none" onClick={close}>
              {t('library.filters.fullSearchLink')}
            </ProjectLink>
          </Modal.Footer>
        )}
      </Modal>
    </>
  )
}
