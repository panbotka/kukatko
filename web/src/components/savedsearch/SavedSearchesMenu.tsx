import { useState } from 'react'
import NavDropdown from 'react-bootstrap/NavDropdown'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { savedSearchHref } from '../../lib/savedSearchView'
import { fetchSavedSearches, type SavedSearch } from '../../services/savedSearches'

/** Fetch lifecycle of the dropdown's saved-search list. */
type State =
  | { status: 'idle' }
  | { status: 'loading' }
  | { status: 'error' }
  | { status: 'ready'; searches: SavedSearch[] }

/**
 * A compact navbar dropdown giving quick access to the current user's saved
 * searches: opening one restores its library/search view via {@link savedSearchHref}.
 * The list is fetched lazily the first time the menu opens (so it costs nothing on
 * pages that never touch it), then refreshed on each open to stay current. A
 * "manage" entry links to the dedicated `/saved` page for renaming and deleting.
 */
export function SavedSearchesMenu() {
  const { t } = useTranslation()
  const [state, setState] = useState<State>({ status: 'idle' })

  function load() {
    setState({ status: 'loading' })
    fetchSavedSearches()
      .then((searches) => {
        setState({ status: 'ready', searches })
      })
      .catch(() => {
        setState({ status: 'error' })
      })
  }

  return (
    <NavDropdown
      align="end"
      title={t('savedSearches.nav')}
      id="saved-searches-menu"
      onToggle={(open) => {
        if (open) {
          load()
        }
      }}
    >
      {state.status === 'loading' && (
        <NavDropdown.ItemText className="d-flex align-items-center gap-2">
          <Spinner animation="border" size="sm" role="status" />
          {t('savedSearches.loading')}
        </NavDropdown.ItemText>
      )}
      {state.status === 'error' && (
        <NavDropdown.ItemText className="text-danger small">
          {t('savedSearches.error')}
        </NavDropdown.ItemText>
      )}
      {state.status === 'ready' && state.searches.length === 0 && (
        <NavDropdown.ItemText className="text-secondary small">
          {t('savedSearches.empty.title')}
        </NavDropdown.ItemText>
      )}
      {state.status === 'ready' &&
        state.searches.map((search) => (
          <NavDropdown.Item key={search.uid} as={Link} to={savedSearchHref(search.params)}>
            {search.name}
          </NavDropdown.Item>
        ))}
      <NavDropdown.Divider />
      <NavDropdown.Item as={Link} to="/saved">
        {t('savedSearches.manage')}
      </NavDropdown.Item>
    </NavDropdown>
  )
}
