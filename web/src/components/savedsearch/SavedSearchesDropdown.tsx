import { useState } from 'react'
import Dropdown from 'react-bootstrap/Dropdown'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { savedSearchHref } from '../../lib/savedSearchView'
import { fetchSavedSearches, type SavedSearch } from '../../services/savedSearches'
import { Icon } from '../Icon'

/** Fetch lifecycle of the dropdown's saved-search list. */
type State =
  | { status: 'idle' }
  | { status: 'loading' }
  | { status: 'error' }
  | { status: 'ready'; searches: SavedSearch[] }

/**
 * The search page's saved-searches entry point: a compact dropdown that lists
 * the current user's saved searches and applies one on click, restoring its
 * library/search view via {@link savedSearchHref}. The list is fetched lazily the
 * first time the menu opens (so it costs nothing until asked for), then refreshed
 * on each open to stay current. A "manage" entry links to the dedicated `/saved`
 * page for renaming and deleting.
 */
export function SavedSearchesDropdown() {
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
    <Dropdown
      align="end"
      onToggle={(open) => {
        if (open) {
          load()
        }
      }}
    >
      <Dropdown.Toggle
        variant="outline-secondary"
        size="sm"
        id="saved-searches-menu"
        title={t('savedSearches.navTitle')}
        className="d-inline-flex align-items-center gap-2"
      >
        <Icon name="bookmarks" />
        {t('savedSearches.nav')}
      </Dropdown.Toggle>
      <Dropdown.Menu>
        {state.status === 'loading' && (
          <Dropdown.ItemText className="d-flex align-items-center gap-2">
            <Spinner animation="border" size="sm" role="status" />
            {t('savedSearches.loading')}
          </Dropdown.ItemText>
        )}
        {state.status === 'error' && (
          <Dropdown.ItemText className="text-danger small">
            {t('savedSearches.error')}
          </Dropdown.ItemText>
        )}
        {state.status === 'ready' && state.searches.length === 0 && (
          <Dropdown.ItemText className="text-secondary small">
            {t('savedSearches.empty.title')}
          </Dropdown.ItemText>
        )}
        {state.status === 'ready' &&
          state.searches.map((search) => (
            <Dropdown.Item
              key={search.uid}
              as={Link}
              to={savedSearchHref(search.params)}
              title={t('savedSearches.openTitle', { name: search.name })}
              className="d-flex align-items-center gap-2"
            >
              <Icon name="search" />
              {search.name}
            </Dropdown.Item>
          ))}
        <Dropdown.Divider />
        <Dropdown.Item
          as={Link}
          to="/saved"
          title={t('savedSearches.manageTitle')}
          className="d-flex align-items-center gap-2"
        >
          <Icon name="sliders" />
          {t('savedSearches.manage')}
        </Dropdown.Item>
      </Dropdown.Menu>
    </Dropdown>
  )
}
