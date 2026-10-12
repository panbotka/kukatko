import { useEffect, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Badge from 'react-bootstrap/Badge'
import Button from 'react-bootstrap/Button'
import ListGroup from 'react-bootstrap/ListGroup'
import Spinner from 'react-bootstrap/Spinner'
import { type TFunction } from 'i18next'
import { useTranslation } from 'react-i18next'

import { ConfirmModal } from '../components/ConfirmModal'
import { EmptyState } from '../components/EmptyState'
import { ErrorState } from '../components/ErrorState'
import { Icon } from '../components/Icon'
import { SaveSearchModal } from '../components/savedsearch/SaveSearchModal'
import { useDocumentTitle } from '../hooks/useDocumentTitle'
import { useReloadKey } from '../hooks/useReloadKey'
import { type SavedSearchBadge, savedSearchHref, savedSearchSummary } from '../lib/savedSearchView'
import { ProjectLink } from '../project/ProjectLink'
import { deleteSavedSearch, fetchSavedSearches, type SavedSearch } from '../services/savedSearches'

/** Fetch lifecycle of the saved-searches list. */
type State =
  | { status: 'loading' }
  | { status: 'error' }
  | { status: 'ready'; searches: SavedSearch[] }

/**
 * The saved-searches index ("My saved searches"): the current user's saved
 * library/search views, each linking back to the exact view it captured. Every
 * saved search can be renamed or deleted. Deletion is optimistic — the row is
 * removed immediately and restored if the request fails.
 */
export function SavedSearchesPage() {
  const { t } = useTranslation()
  useDocumentTitle(t('savedSearches.title'))
  const [state, setState] = useState<State>({ status: 'loading' })
  const [editing, setEditing] = useState<SavedSearch | null>(null)
  const [pendingDelete, setPendingDelete] = useState<SavedSearch | null>(null)
  const [actionError, setActionError] = useState(false)
  const [reloadKey, reload] = useReloadKey()

  useEffect(() => {
    const controller = new AbortController()
    setState({ status: 'loading' })
    fetchSavedSearches(controller.signal)
      .then((searches) => {
        setState({ status: 'ready', searches })
      })
      .catch((err: unknown) => {
        if (err instanceof DOMException && err.name === 'AbortError') {
          return
        }
        setState({ status: 'error' })
      })
    return () => {
      controller.abort()
    }
  }, [reloadKey])

  async function remove(search: SavedSearch) {
    setActionError(false)
    // Optimistically drop the row, remembering the prior list to restore on error.
    let previous: SavedSearch[] = []
    setState((prev) => {
      if (prev.status !== 'ready') {
        return prev
      }
      previous = prev.searches
      return { status: 'ready', searches: prev.searches.filter((s) => s.uid !== search.uid) }
    })
    try {
      await deleteSavedSearch(search.uid)
    } catch {
      setActionError(true)
      setState({ status: 'ready', searches: previous })
    }
  }

  function upsert(saved: SavedSearch) {
    setState((prev) => {
      if (prev.status !== 'ready') {
        return prev
      }
      const searches = prev.searches.map((s) => (s.uid === saved.uid ? { ...s, ...saved } : s))
      return { status: 'ready', searches }
    })
  }

  return (
    <>
      <h1 className="kk-page-title mb-3">{t('savedSearches.title')}</h1>

      {actionError && <Alert variant="danger">{t('savedSearches.actionError')}</Alert>}

      {state.status === 'loading' && (
        <div className="d-flex justify-content-center py-5">
          <Spinner animation="border" role="status">
            <span className="visually-hidden">{t('savedSearches.loading')}</span>
          </Spinner>
        </div>
      )}

      {state.status === 'error' && <ErrorState title={t('savedSearches.error')} onRetry={reload} />}

      {state.status === 'ready' && state.searches.length === 0 && (
        <EmptyState title={t('savedSearches.empty.title')} hint={t('savedSearches.empty.hint')} />
      )}

      {state.status === 'ready' && state.searches.length > 0 && (
        <ListGroup>
          {state.searches.map((search) => (
            <ListGroup.Item
              key={search.uid}
              className="d-flex align-items-center justify-content-between gap-2"
            >
              {/* The whole name + query block is the one link that opens the
                  search; the actions stay separate buttons beside it. Both
                  lines truncate rather than pushing the row past the viewport
                  — a name is typed by the user and a query can be long or
                  unbroken — and the query keeps its full text in a tooltip.
                  The link is named by the saved search's name alone and
                  described by its query line, so a screen reader hears the
                  name first and the query as the description. */}
              <ProjectLink
                to={savedSearchHref(search.params)}
                aria-label={search.name}
                aria-describedby={`saved-search-${search.uid}-query`}
                className="text-decoration-none flex-grow-1 kk-min-w-0"
              >
                <span className="d-block text-truncate">{search.name}</span>
                <SavedSearchQueryLine id={`saved-search-${search.uid}-query`} search={search} />
              </ProjectLink>
              {/* Both actions keep a glyph and drop their word below `sm`, so a
                  phone row never has to fit a name plus two Czech-worded buttons
                  across ~336px. The `aria-label` carries the same word the
                  button shows, so the accessible name is identical at every
                  width. */}
              <div className="d-flex gap-1 flex-shrink-0">
                <Button
                  variant="outline-secondary"
                  size="sm"
                  className="d-inline-flex align-items-center gap-2 kukatko-tap-target-touch"
                  aria-label={t('savedSearches.rename')}
                  title={t('savedSearches.rename')}
                  onClick={() => {
                    setEditing(search)
                  }}
                >
                  <Icon name="pencil" />
                  <span className="d-none d-sm-inline">{t('savedSearches.rename')}</span>
                </Button>
                <Button
                  variant="outline-danger"
                  size="sm"
                  className="d-inline-flex align-items-center gap-2 kukatko-tap-target-touch"
                  aria-label={t('savedSearches.delete')}
                  title={t('savedSearches.delete')}
                  onClick={() => {
                    setPendingDelete(search)
                  }}
                >
                  <Icon name="trash" />
                  <span className="d-none d-sm-inline">{t('savedSearches.delete')}</span>
                </Button>
              </div>
            </ListGroup.Item>
          ))}
        </ListGroup>
      )}

      <SaveSearchModal
        search={editing}
        show={editing !== null}
        onHide={() => {
          setEditing(null)
        }}
        onSaved={(saved) => {
          upsert(saved)
          setEditing(null)
        }}
      />

      <ConfirmModal
        show={pendingDelete !== null}
        title={t('savedSearches.confirmTitle')}
        confirmLabel={t('savedSearches.deleteConfirm')}
        onCancel={() => {
          setPendingDelete(null)
        }}
        onConfirm={() => {
          const search = pendingDelete
          setPendingDelete(null)
          if (search) {
            void remove(search)
          }
        }}
      >
        {pendingDelete && t('savedSearches.confirmDelete', { name: pendingDelete.name })}
      </ConfirmModal>
    </>
  )
}

/** The badge's words for the one piece of extra state a saved search carries. */
function badgeText(badge: SavedSearchBadge, t: TFunction): string {
  switch (badge.kind) {
    case 'filters':
      return t('savedSearches.badge.filters', { count: badge.count })
    case 'mode':
      return t(`savedSearches.badge.mode.${badge.mode}`)
    case 'sort':
      return t('savedSearches.badge.sort', {
        sort: t(`library.sort.${badge.sort}`, { defaultValue: badge.sort }),
      })
  }
}

/**
 * The muted second line of a saved-search row: the query it was saved with in
 * small monospace, on one line with an ellipsis and the whole text in a
 * tooltip, followed by a tiny badge for the most relevant extra state (filters,
 * mode or sort; see {@link savedSearchSummary}). A search saved without any
 * query text says so instead of leaving the line blank.
 */
function SavedSearchQueryLine({ id, search }: { id: string; search: SavedSearch }) {
  const { t } = useTranslation()
  const { query, badge } = savedSearchSummary(search.params)
  return (
    <span id={id} className="d-flex align-items-center gap-2 kk-min-w-0 text-secondary">
      {query === '' ? (
        <span className="text-truncate small fst-italic">{t('savedSearches.noQuery')}</span>
      ) : (
        <span className="text-truncate small font-monospace" title={query}>
          {query}
        </span>
      )}
      {badge && (
        <Badge bg="secondary" className="flex-shrink-0 fw-normal">
          {badgeText(badge, t)}
        </Badge>
      )}
    </span>
  )
}
