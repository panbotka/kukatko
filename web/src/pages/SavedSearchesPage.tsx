import { useEffect, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import ListGroup from 'react-bootstrap/ListGroup'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { EmptyState } from '../components/EmptyState'
import { SaveSearchModal } from '../components/savedsearch/SaveSearchModal'
import { savedSearchHref } from '../lib/savedSearchView'
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
  const [state, setState] = useState<State>({ status: 'loading' })
  const [editing, setEditing] = useState<SavedSearch | null>(null)
  const [actionError, setActionError] = useState(false)

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
  }, [])

  async function remove(search: SavedSearch) {
    if (!window.confirm(t('savedSearches.confirmDelete', { name: search.name }))) {
      return
    }
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

      {state.status === 'error' && <Alert variant="danger">{t('savedSearches.error')}</Alert>}

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
              <Link
                to={savedSearchHref(search.params)}
                className="text-decoration-none flex-grow-1"
              >
                {search.name}
              </Link>
              <div className="d-flex gap-1">
                <Button
                  variant="outline-secondary"
                  size="sm"
                  onClick={() => {
                    setEditing(search)
                  }}
                >
                  {t('savedSearches.rename')}
                </Button>
                <Button
                  variant="outline-danger"
                  size="sm"
                  onClick={() => {
                    void remove(search)
                  }}
                >
                  {t('savedSearches.delete')}
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
    </>
  )
}
