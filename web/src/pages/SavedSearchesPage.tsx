import { useEffect, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import ListGroup from 'react-bootstrap/ListGroup'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { ConfirmModal } from '../components/ConfirmModal'
import { EmptyState } from '../components/EmptyState'
import { ErrorState } from '../components/ErrorState'
import { Icon } from '../components/Icon'
import { SaveSearchModal } from '../components/savedsearch/SaveSearchModal'
import { useDocumentTitle } from '../hooks/useDocumentTitle'
import { useReloadKey } from '../hooks/useReloadKey'
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
              {/* The name truncates rather than pushing the row past the
                  viewport — a saved search is named by the user and can be long
                  or unbroken. */}
              <Link
                to={savedSearchHref(search.params)}
                className="text-decoration-none flex-grow-1 kk-min-w-0 text-truncate"
              >
                {search.name}
              </Link>
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
