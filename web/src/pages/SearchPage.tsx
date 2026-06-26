import { useEffect, useMemo, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import Col from 'react-bootstrap/Col'
import Form from 'react-bootstrap/Form'
import Row from 'react-bootstrap/Row'
import { useTranslation } from 'react-i18next'

import { FilterBar } from '../components/library/FilterBar'
import { GridSkeleton } from '../components/library/GridSkeleton'
import { PhotoGrid } from '../components/library/PhotoGrid'
import { usePhotoSearch } from '../hooks/usePhotoSearch'
import { viewToParams } from '../lib/libraryView'
import { SEARCH_DEFAULTS, type SearchView, toMode } from '../lib/searchView'
import { useUrlState } from '../lib/urlState'

/** Delay before a typed query is committed to the URL and a search runs. */
const SEARCH_DEBOUNCE_MS = 350

/**
 * The search page: a prominent query input and a mode selector over the same
 * virtualized, infinite-scroll grid the library uses, with the library filters
 * applicable. Query, mode and filters all live in the URL, so Back/Forward
 * restore the exact search and the URL is shareable. Typing is debounced before
 * it commits to the URL (and triggers a fetch). When a semantic/hybrid search
 * falls back to full-text because the embeddings sidecar is offline, a
 * non-blocking notice explains that semantic ranking was skipped.
 */
export function SearchPage() {
  const { t } = useTranslation()
  const [view, setView] = useUrlState<SearchView>(SEARCH_DEFAULTS)

  const params = useMemo(() => viewToParams(view), [view])
  const mode = toMode(view.mode)
  const { photos, total, status, degraded, loadingMore, moreError, loadMore, retry } =
    usePhotoSearch(params, mode)

  // Local, debounced mirror of the URL query so typing stays responsive but the
  // URL (and the fetch) only update after the user pauses. The query is the
  // page's own input, separate from the filter bar.
  const [text, setText] = useState(view.q)

  // Keep the input in sync when the URL query changes from elsewhere (the navbar
  // search, Back/Forward, a shared link).
  useEffect(() => {
    setText(view.q)
  }, [view.q])

  // Debounce committing the typed query to the URL; an unchanged value is a no-op.
  useEffect(() => {
    if (text === view.q) {
      return
    }
    const id = setTimeout(() => {
      setView({ q: text }, { replace: true })
    }, SEARCH_DEBOUNCE_MS)
    return () => {
      clearTimeout(id)
    }
  }, [text, view.q, setView])

  return (
    <>
      <h1 className="h3 mb-3">{t('search.title')}</h1>

      <Form
        role="search"
        aria-label={t('search.formLabel')}
        className="mb-3"
        onSubmit={(e) => {
          e.preventDefault()
          // Commit immediately on submit, bypassing the debounce.
          setView({ q: text }, { replace: true })
        }}
      >
        <Row className="g-2 align-items-end">
          <Col xs={12} md={8} lg={9}>
            <Form.Group controlId="search-query">
              <Form.Label className="small mb-1">{t('search.queryLabel')}</Form.Label>
              <Form.Control
                type="search"
                value={text}
                autoFocus
                placeholder={t('search.placeholder')}
                onChange={(e) => {
                  setText(e.target.value)
                }}
              />
            </Form.Group>
          </Col>
          <Col xs={12} md={4} lg={3}>
            <Form.Group controlId="search-mode">
              <Form.Label className="small mb-1">{t('search.modeLabel')}</Form.Label>
              <Form.Select
                value={view.mode}
                onChange={(e) => {
                  setView({ mode: e.target.value })
                }}
              >
                <option value="hybrid">{t('search.mode.hybrid')}</option>
                <option value="fulltext">{t('search.mode.fulltext')}</option>
                <option value="semantic">{t('search.mode.semantic')}</option>
              </Form.Select>
            </Form.Group>
          </Col>
        </Row>
      </Form>

      <FilterBar view={view} onChange={setView} total={total} showSearch={false} showSort={false} />

      {degraded && (
        <Alert variant="warning" className="py-2">
          {t('search.degraded')}
        </Alert>
      )}

      {status === 'idle' && (
        <div className="text-center text-secondary py-5">
          <p className="mb-0 fs-5">{t('search.prompt')}</p>
        </div>
      )}

      {status === 'loading' && <GridSkeleton />}

      {status === 'error' && (
        <Alert variant="danger" className="d-flex align-items-center justify-content-between">
          <span>{t('search.error.load')}</span>
          <Button variant="outline-light" size="sm" onClick={retry}>
            {t('library.error.retry')}
          </Button>
        </Alert>
      )}

      {status === 'ready' && photos.length === 0 && (
        <div className="text-center text-secondary py-5">
          <p className="mb-1 fs-5">{t('search.empty.title')}</p>
          <p className="mb-0 small">{t('search.empty.hint')}</p>
        </div>
      )}

      {status === 'ready' && photos.length > 0 && (
        <PhotoGrid
          photos={photos}
          loadingMore={loadingMore}
          moreError={moreError}
          onEndReached={loadMore}
          onRetry={retry}
        />
      )}
    </>
  )
}
