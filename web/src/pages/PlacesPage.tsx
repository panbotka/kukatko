import { useCallback, useEffect, useMemo, useState } from 'react'
import Button from 'react-bootstrap/Button'
import ListGroup from 'react-bootstrap/ListGroup'
import { useTranslation } from 'react-i18next'
import { useLocation } from 'react-router-dom'

import { EmptyState } from '../components/EmptyState'
import { ErrorState } from '../components/ErrorState'
import { FilterBar } from '../components/library/FilterBar'
import { GridSkeleton } from '../components/library/GridSkeleton'
import { PhotoGrid } from '../components/library/PhotoGrid'
import { BulkEditControl } from '../components/organize/BulkEditControl'
import { SelectionBar } from '../components/organize/SelectionBar'
import { PlaceRow } from '../components/places/PlaceRow'
import { useBulkEdit } from '../hooks/useBulkEdit'
import { useDocumentTitle } from '../hooks/useDocumentTitle'
import { useGridScrollMemory } from '../hooks/useGridScrollMemory'
import { useReloadKey } from '../hooks/useReloadKey'
import { useScopedPhotos } from '../hooks/useScopedPhotos'
import { gridScrollKey, readGridScroll } from '../lib/gridScroll'
import { LIBRARY_DEFAULTS, type LibraryView, viewToParams } from '../lib/libraryView'
import { resolvePlaceDrill } from '../lib/placeDrill'
import { useUrlState } from '../lib/urlState'
import { fetchPlaces, type PlaceCountry } from '../services/places'

/** No hierarchy yet: the drill has nothing to resolve against. */
const NO_COUNTRIES: PlaceCountry[] = []

/**
 * URL-encoded view state for the Places page: the library filter/sort fields
 * plus the place drill (`country`, `city`). A type alias (intersection) so it
 * keeps the implicit index signature the urlState `Record<string, string>`
 * constraint requires; the whole view round-trips through the query string, so
 * Back/Forward restore the exact drill and filters.
 */
type PlacesView = LibraryView & {
  country: string
  city: string
}

/** Default Places view: no place selected, library defaults for the rest. */
const PLACES_DEFAULTS: PlacesView = {
  ...LIBRARY_DEFAULTS,
  country: '',
  city: '',
}

/** Fetch lifecycle of the place hierarchy (countries + their cities). */
type State =
  | { status: 'loading' }
  | { status: 'error' }
  | { status: 'ready'; countries: PlaceCountry[] }

/**
 * Places page: browse the library by location. Lists countries with photo
 * counts and a preview of each; selecting a country reveals its cities, and
 * selecting a city shows the photo grid scoped to that place (reusing
 * {@link FilterBar} + {@link PhotoGrid}, exactly like an album or label gallery).
 * The place drill and the filters live in the URL (`/places?country=…&city=…`),
 * so Back/Forward step through the drill. The country → city hierarchy is fetched
 * once; the grid loads only once a city is chosen.
 *
 * A level holding a single row is stepped past rather than shown
 * (`lib/placeDrill`): this library is entirely Czech, so the country list was one
 * row that every visit began by clicking through. The skip is resolved from the
 * fetched hierarchy and never written into the URL — the address keeps saying
 * what the user chose, and a country that grows a second one simply reappears.
 *
 * Editors can multi-select over that grid straight away — the corner checkmark is
 * offered from the outset, as on the library — and bulk-edit the picked
 * photos, after which it refetches — an edit can move a photo's location, and so
 * out of the place being browsed. Walking the drill leaves selection mode, since
 * every place is its own list.
 */
export function PlacesPage() {
  const { t } = useTranslation()
  useDocumentTitle(t('places.title'))
  const location = useLocation()
  const [state, setState] = useState<State>({ status: 'loading' })
  // Bumped to re-run the hierarchy fetch after an error retry.
  const [hierarchyKey, reloadHierarchy] = useReloadKey()
  // Bumped to refetch the scoped grid after a bulk edit.
  const [photosKey, reloadPhotos] = useReloadKey()

  const [view, setView] = useUrlState<PlacesView>(PLACES_DEFAULTS)

  // What is on screen: the URL's choice, with any level of exactly one row
  // stepped past. Everything below reads these, not the raw view fields.
  const countries = state.status === 'ready' ? state.countries : NO_COUNTRIES
  const drill = useMemo(
    () => resolvePlaceDrill(countries, view.country, view.city),
    [countries, view.country, view.city],
  )
  const { country, city, selected: selectedCountry } = drill

  const params = useMemo(() => viewToParams(view), [view])
  const scope = useMemo(() => ({ country, city }), [country, city])
  // The grid is only meaningful once a city (within a country) is selected.
  const gridEnabled = country !== '' && city !== ''
  // Where the grid was left, per view, so opening a photo and coming back — Back,
  // or the viewer's own "back to list", which pops the same entry — returns to
  // the tile it was opened from. This list only ever grew by appending pages, so
  // it also has to come back as long as it was before the offset means anything.
  const scrollKey = gridScrollKey(location.pathname, location.search)
  const restoreCount = useMemo(() => readGridScroll(scrollKey)?.count ?? 0, [scrollKey])
  const { photos, total, status, loadingMore, moreError, loadMore, retry } = useScopedPhotos(
    scope,
    params,
    { enabled: gridEnabled, reloadKey: photosKey, initialCount: restoreCount },
  )
  const gridScroll = useGridScrollMemory({ key: scrollKey, count: photos.length })

  // Hover-select: a writer's tiles carry the corner checkmark from the outset,
  // so the toolbar below keys off what is picked rather than an explicit mode.
  const bulk = useBulkEdit({ onEdited: reloadPhotos, hoverSelect: true })
  const selection = bulk.selection
  const hasPhotos = gridEnabled && status === 'ready' && photos.length > 0

  useEffect(() => {
    const controller = new AbortController()
    setState({ status: 'loading' })
    fetchPlaces(undefined, controller.signal)
      .then((countries) => {
        setState({ status: 'ready', countries })
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
  }, [hierarchyKey])

  // Each place is its own list: stepping through the drill (or back out of it)
  // must not carry a selection of the previous place's photos into the next.
  const leaveSelection = selection.disable
  useEffect(() => {
    leaveSelection()
  }, [country, city, leaveSelection])

  const selectCountry = useCallback(
    (name: string) => {
      setView({ country: name, city: '' })
    },
    [setView],
  )
  const selectCity = useCallback(
    (name: string) => {
      // Write the country too: it may only be implied (a library with one
      // country never puts it in the URL), and a link to a city without its
      // country is not an address anyone else can open.
      setView({ country, city: name })
    },
    [setView, country],
  )
  const clearPlace = useCallback(() => {
    setView({ country: '', city: '' })
  }, [setView])
  const clearCity = useCallback(() => {
    setView({ city: '' })
  }, [setView])

  return (
    <>
      <div className="d-flex justify-content-between align-items-center gap-2 mb-3 flex-wrap">
        <h1 className="kk-page-title mb-0">{t('places.title')}</h1>
      </div>

      {/* Breadcrumb drill: Places / Country / City. A level is a link only when
          going back to it would show something new — a level that was stepped
          past holds one row, and a link that lands on the same screen is worse
          than plain text. */}
      {(country !== '' || city !== '') && (
        <nav aria-label={t('places.breadcrumb')} className="mb-3">
          {drill.canClearCountry ? (
            <Button variant="link" className="p-0 text-decoration-none" onClick={clearPlace}>
              {t('places.title')}
            </Button>
          ) : (
            <span className="text-secondary">{t('places.title')}</span>
          )}
          {country !== '' && (
            <>
              <span className="text-secondary mx-2">/</span>
              {drill.canClearCity ? (
                <Button variant="link" className="p-0 text-decoration-none" onClick={clearCity}>
                  {country}
                </Button>
              ) : (
                <span className={city === '' ? 'fw-semibold' : 'text-secondary'}>{country}</span>
              )}
            </>
          )}
          {city !== '' && (
            <>
              <span className="text-secondary mx-2">/</span>
              <span className="fw-semibold">{city}</span>
            </>
          )}
        </nav>
      )}

      {state.status === 'loading' && <GridSkeleton />}

      {state.status === 'error' && (
        <ErrorState title={t('places.error')} onRetry={reloadHierarchy} />
      )}

      {state.status === 'ready' && (
        <>
          {/* Level 1: countries. */}
          {country === '' &&
            (countries.length === 0 ? (
              <EmptyState title={t('places.empty.title')} hint={t('places.empty.hint')} />
            ) : (
              <ListGroup>
                {countries.map((c) => (
                  <PlaceRow
                    key={c.country}
                    name={c.country}
                    count={c.count}
                    coverUid={c.cover_uid}
                    onSelect={() => {
                      selectCountry(c.country)
                    }}
                  />
                ))}
              </ListGroup>
            ))}

          {/* Level 2: cities of the selected country. */}
          {country !== '' &&
            city === '' &&
            (selectedCountry === undefined || selectedCountry.cities.length === 0 ? (
              <EmptyState title={t('places.noCities.title')} hint={t('places.noCities.hint')} />
            ) : (
              <ListGroup>
                {selectedCountry.cities.map((c) => (
                  <PlaceRow
                    key={c.city}
                    name={c.city}
                    count={c.count}
                    coverUid={c.cover_uid}
                    onSelect={() => {
                      selectCity(c.city)
                    }}
                  />
                ))}
              </ListGroup>
            ))}

          {/* Level 3: the photo grid scoped to the selected place. */}
          {gridEnabled && (
            <>
              {selection.count > 0 && (
                <SelectionBar count={selection.count} onCancel={selection.disable}>
                  <BulkEditControl bulk={bulk} />
                </SelectionBar>
              )}

              <FilterBar view={view} onChange={setView} total={total} />

              {status === 'loading' && <GridSkeleton />}

              {status === 'error' && <ErrorState title={t('library.error.load')} onRetry={retry} />}

              {status === 'ready' && photos.length === 0 && (
                <EmptyState
                  title={t('places.emptyPhotos.title')}
                  hint={t('places.emptyPhotos.hint')}
                />
              )}

              {hasPhotos && (
                <PhotoGrid
                  photos={photos}
                  loadingMore={loadingMore}
                  moreError={moreError}
                  onEndReached={loadMore}
                  onRetry={retry}
                  selection={bulk.gridSelection}
                  restoreStateFrom={gridScroll.restoreFrom}
                  onStateChanged={gridScroll.onStateChanged}
                />
              )}
            </>
          )}
        </>
      )}
    </>
  )
}
